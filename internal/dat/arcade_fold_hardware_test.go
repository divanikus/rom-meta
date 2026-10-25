package dat

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalog"
)

func TestArcadeSiblingHardwareBranches(t *testing.T) {
	game := func(name, parent, title, source, devices string) datGame {
		return datGame{Name: name, CloneOf: parent, Description: title, SourceFile: source, DeviceRefs: strings.Fields(devices)}
	}
	games := []datGame{
		game("aerofgt", "", "Aero Fighters (World) (newer hardware)", "new.cpp", "cpu new_video"),
		game("aerofgtb", "aerofgt", "Aero Fighters (Taiwan / Japan, set 1)", "old.cpp", "cpu old_video"),
		game("aerofgtc", "aerofgt", "Aero Fighters (Taiwan / Japan, set 2)", "old.cpp", "old_video cpu"),
		game("sonicwi", "aerofgt", "Sonic Wings (Japan)", "old.cpp", "cpu old_video"),
		game("aerfboot", "aerofgt", "Aero Fighters (bootleg, set 1)", "old.cpp", "cpu sound1"),
		game("aerfboo2", "aerofgt", "Aero Fighters (bootleg, set 2)", "old.cpp", "cpu sound2"),
		game("pspikes", "", "Power Spikes (World)", "old.cpp", "cpu old_video"),
		game("spikes91", "pspikes", "1991 Spikes (Italian bootleg, set 1)", "old.cpp", "cpu bootleg_sound"),
		game("spikes91b", "pspikes", "1991 Spikes (Italian bootleg, set 2)", "old.cpp", "cpu bootleg_sound"),
		game("unrelated", "", "1991 Spikes (Italian bootleg, set 3)", "old.cpp", "cpu bootleg_sound"),
	}
	bundles, _ := arcadeBundleGames(games, true)
	want := map[string][]string{
		"Aero Fighters":                        {"aerofgtb", "aerofgtc", "sonicwi"},
		"Aero Fighters (newer hardware)":       {"aerofgt"},
		"Aero Fighters (bootleg, set 1)":       {"aerfboot"},
		"Aero Fighters (bootleg, set 2)":       {"aerfboo2"},
		"Power Spikes":                         {"pspikes"},
		"1991 Spikes (Italian bootleg)":        {"spikes91", "spikes91b"},
		"1991 Spikes (Italian bootleg, set 3)": {"unrelated"},
	}
	got := map[string][]string{}
	for _, b := range bundles {
		for _, release := range b.Releases {
			got[b.Folder] = append(got[b.Folder], release.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups=%v want=%v", got, want)
	}
	for i, j := 0, len(games)-1; i < j; i, j = i+1, j-1 {
		games[i], games[j] = games[j], games[i]
	}
	reversed, _ := arcadeBundleGames(games, true)
	if !reflect.DeepEqual(bundles, reversed) {
		t.Fatal("input order changes groups or names")
	}
	unfolded, _ := arcadeBundleGames(games, false)
	if len(unfolded) != len(games) {
		t.Fatal("no-fold still folds siblings")
	}
}

func TestArcadeSiblingFoldEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]datGame)
	}{
		{"missing parent", func(g []datGame) { g[1].CloneOf, g[2].CloneOf = "missing", "missing" }},
		{"cycle", func(g []datGame) { g[0].CloneOf = "a" }},
		{"missing devices", func(g []datGame) { g[1].DeviceRefs, g[2].DeviceRefs = nil, nil }},
		{"different devices", func(g []datGame) { g[2].DeviceRefs = []string{"other"} }},
		{"different drivers", func(g []datGame) { g[2].SourceFile = "different.cpp" }},
		{"different bootleg makers", func(g []datGame) { g[2].Manufacturer = "Another maker" }},
		{"renamed derivative", func(g []datGame) { g[2].Description = "Another Game (bootleg, set 2)" }},
		{"maker qualifier", func(g []datGame) { g[2].Description = "Copy (Videotron bootleg, set 2)" }},
		{"content hack", func(g []datGame) { g[2].Description = "Copy (bootleg hack, set 2)" }},
		{"edition", func(g []datGame) { g[2].Description = "Copy (bootleg, Black Edition, set 2)" }},
		{"incomplete", func(g []datGame) { g[2].Incomplete = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := []datGame{
				{Name: "parent", Description: "Original", SourceFile: "original.cpp", DeviceRefs: []string{"original"}},
				{Name: "a", CloneOf: "parent", Description: "Copy (bootleg, set 1)", SourceFile: "copy.cpp", DeviceRefs: []string{"cpu"}},
				{Name: "b", CloneOf: "parent", Description: "Copy (bootleg, set 2)", SourceFile: "copy.cpp", DeviceRefs: []string{"cpu"}},
			}
			tc.change(g)
			b, _ := arcadeBundleGames(g, true)
			if len(b) != 3 {
				t.Fatalf("unsafe sibling fold: %+v", b)
			}
		})
	}
}

func TestArcadeHardwareGuardCannotBeBypassedByBootlegTitle(t *testing.T) {
	g := []datGame{
		{Name: "root", Description: "Game", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu", "video"}},
		{Name: "copy", CloneOf: "root", Description: "Game (bootleg)", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu", "other_video"}},
	}
	groups, notes := arcadeBundleGames(g, true)
	if len(groups) != 1 {
		t.Fatal("device differences alone split a same-driver release", groups, notes)
	}
	g[1].DeviceRefs = []string{"cpu", "video"}
	g[1].SourceFile = "copy.cpp"
	groups, _ = arcadeBundleGames(g, true)
	if len(groups) != 2 {
		t.Fatal("same title/devices bypassed driver boundary")
	}
	// Authoritative hardware identity/reviewed revisions may span drivers.
	r := defaultArcadeRouting()
	h := map[string]string{"root": "Arcade - Sega - System 16A", "copy": "Arcade - Sega - System 16B"}
	groups, _ = arcadeBundleGamesWithHardware(g, true, h, r)
	if len(groups) != 1 {
		t.Fatal("reviewed board family stopped folding")
	}
	h["copy"] = "Arcade - Sega - Mega-Tech"
	g[1].SourceFile = g[0].SourceFile
	groups, _ = arcadeBundleGamesWithHardware(g, true, h, r)
	if len(groups) != 2 {
		t.Fatal("known different hardware joined inside one driver")
	}
}

func TestArcadeSiblingImportBindingsAndSubset(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "classes.zip")
	var xml, names strings.Builder
	xml.WriteString("<mame build=\"0.289 (fixture)\"><machine name=\"home\"><description>Home computer</description></machine>")
	for _, g := range []struct{ name, parent, title, source, device string }{
		{"new", "", "Aero Fighters (newer hardware)", "new.cpp", "new_board"},
		{"old1", "new", "Aero Fighters (Taiwan / Japan, set 1)", "old.cpp", "old_board"},
		{"old2", "new", "Aero Fighters (Taiwan / Japan, set 2)", "old.cpp", "old_board"},
		{"japan", "new", "Sonic Wings (Japan)", "old.cpp", "old_board"},
		{"power", "", "Power Spikes (World)", "old.cpp", "old_board"},
		{"copy1", "power", "1991 Spikes (Italian bootleg, set 1)", "old.cpp", "copy_board"},
		{"copy2", "power", "1991 Spikes (Italian bootleg, set 2)", "old.cpp", "copy_board"},
	} {
		fmt.Fprintf(&xml, "<machine name=\"%s\" cloneof=\"%s\" sourcefile=\"%s\"><description>%s</description><device_ref name=\"%s\"/><rom name=\"%s.bin\" size=\"1\" crc=\"12345678\"/></machine>", g.name, g.parent, g.source, g.title, g.device, g.name)
		fmt.Fprintln(&names, g.name)
	}
	xml.WriteString("</mame>")
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml.String()})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\n" + names.String(),
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Shooter / Misc.]\n" + names.String() + "[Computer / Home]\nhome\n",
	})
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true}
	result, err := ImportArcade(input, options)
	if err != nil || result.Counts["games"] != 4 || len(result.Bindings) != 7 {
		t.Fatalf("%v %v", err, result.Counts)
	}
	if result.Bindings["old1"].Game != result.Bindings["old2"].Game ||
		result.Bindings["japan"].Game != result.Bindings["old1"].Game ||
		result.Bindings["copy1"].Game != "1991 Spikes (Italian bootleg)" ||
		result.Bindings["copy2"].Game != result.Bindings["copy1"].Game {
		t.Fatal(result.Bindings)
	}
	b := result.Bindings["copy1"]
	data, err := os.ReadFile(filepath.Join(root, "platforms", b.Platform, b.Game, "game.yaml"))
	if err != nil || !strings.Contains(string(data), "Italian bootleg, set 1") || !strings.Contains(string(data), "Italian bootleg, set 2") {
		t.Fatalf("lost release descriptions: %v %s", err, data)
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatal(err, issues)
	}
	repeat, err := ImportArcade(input, options)
	if err != nil || len(repeat.Conflicts) != 0 {
		t.Fatal(err, repeat.Conflicts)
	}
	options.DataRoot, options.Sets = t.TempDir(), []string{"old2", "copy2"}
	subset, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	for name, binding := range subset.Bindings {
		if binding != result.Bindings[name] {
			t.Fatal("subset renamed group", name, binding, result.Bindings[name])
		}
	}
}

func TestArcadeSiblingOfficialContentQualifiers(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"Game (Taiwan / Japan, set 1)", "Regional Game (Japan)", 2},
		{"Game (dual screen, set 1)", "Regional Game (dual screen, set 2)", 2},
		{"Game (censored)", "Game (uncensored)", 3},
		{"Game (World, censored)", "Game (World, uncensored)", 3},
		{"Game (alt questions 1)", "Game (alt questions 2)", 3},
		{"Game (set 1 changed levels)", "Game (set 2)", 3},
		{"Game Black Edition", "Game Black Edition", 2},
		{"Game Black Edition", "Another Black Edition", 3},
	} {
		games := []datGame{
			{Name: "root", Description: "Game", SourceFile: "new.cpp", DeviceRefs: []string{"new"}},
			{Name: "a", CloneOf: "root", Description: tc.a, SourceFile: "old.cpp", DeviceRefs: []string{"old"}},
			{Name: "b", CloneOf: "root", Description: tc.b, SourceFile: "old.cpp", DeviceRefs: []string{"old"}},
		}
		bundles, _ := arcadeBundleGames(games, true)
		if len(bundles) != tc.want {
			t.Fatalf("%q / %q: %+v", tc.a, tc.b, bundles)
		}
	}
}

func TestArcadeReviewedSplitCorrections(t *testing.T) {
	games := []datGame{
		{Name: "tazmania", Description: "Tazz-Mania (set 1)", SourceFile: "galaxian/galaxian.cpp", DeviceRefs: []string{"z80", "z80", "ay8910", "ay8910", "netlist_sound"}},
		{Name: "tazmani3", CloneOf: "tazmania", Description: "Tazz-Mania (Arfyc / Rodmar bootleg)", SourceFile: "galaxian/scobra.cpp", Manufacturer: "bootleg (Arfyc / Rodmar)", DeviceRefs: []string{"z80", "z80", "ay8910", "ay8910"}},
		{Name: "tazzmang", CloneOf: "tazmania", Description: "Tazz-Mania (bootleg on Galaxian hardware)", SourceFile: "galaxian/galaxold.cpp", DeviceRefs: []string{"z80", "discrete"}},
		{Name: "sf2", Description: "Street Fighter II: The World Warrior (World)", SourceFile: "capcom/cps1.cpp", DeviceRefs: []string{"m68000", "z80", "ym2151", "okim6295"}},
		{Name: "sf2b", CloneOf: "sf2", Description: "Street Fighter II: The World Warrior (bootleg, set 1)", SourceFile: "capcom/cps1bl_5205.cpp", Manufacturer: "bootleg (Playmark)", DeviceRefs: []string{"m68000", "z80", "ym2151", "msm5205", "msm5205"}},
		{Name: "sf2b2", CloneOf: "sf2", Description: "Street Fighter II: The World Warrior (bootleg, set 2)", SourceFile: "capcom/cps1bl_5205.cpp", Manufacturer: "bootleg", DeviceRefs: []string{"m68000", "z80", "ym2151", "msm5205", "msm5205"}},
	}
	bundles, _ := arcadeBundleGames(games, true)
	if len(bundles) != 4 {
		t.Fatalf("expected original/bootleg branches: %+v", bundles)
	}
	members := map[string]string{}
	for _, b := range bundles {
		for _, release := range b.Releases {
			members[release.Name] = b.Folder
		}
	}
	if members["tazmani3"] != members["tazmania"] || members["tazzmang"] == members["tazmania"] ||
		members["sf2b"] != members["sf2b2"] || members["sf2b"] == members["sf2"] || strings.Contains(members["sf2b"], "set 1") {
		t.Fatal(members)
	}
	for _, tc := range []struct {
		name   string
		change func([]datGame)
	}{
		{"unreviewed clone", func(g []datGame) { g[1].Name = "another" }},
		{"changed source", func(g []datGame) { g[1].SourceFile = "other.cpp" }},
		{"different maker", func(g []datGame) { g[5].Manufacturer = "bootleg (Other)" }},
		{"unreviewed sibling", func(g []datGame) { g[5].Name = "sf2b3" }},
		{"different sibling devices", func(g []datGame) { g[5].DeviceRefs = []string{"other"} }},
		{"changed content", func(g []datGame) { g[5].Description = "Street Fighter II: The World Warrior (bootleg hack, set 2)" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := append([]datGame(nil), games...)
			tc.change(changed)
			got, _ := arcadeBundleGames(changed, true)
			if len(got) != 5 {
				t.Fatalf("review escaped its scope: %+v", got)
			}
		})
	}
	if arcadeFoldHardwareCompatible(games[1], games[0], map[string]string{"tazmani3": "board-a", "tazmania": "board-b"}, defaultArcadeRouting()) {
		t.Fatal("review overrode conflicting identified hardware")
	}
	for i, j := 0, len(games)-1; i < j; i, j = i+1, j-1 {
		games[i], games[j] = games[j], games[i]
	}
	reversed, _ := arcadeBundleGames(games, true)
	if !reflect.DeepEqual(bundles, reversed) {
		t.Fatal("input order changed reviewed groups")
	}
	unfolded, _ := arcadeBundleGames(games, false)
	if len(unfolded) != len(games) {
		t.Fatal("review bypassed no-fold")
	}
}
