package dat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalog"
)

func TestArcadeRegionalBootlegAliasesStayWithinOfficialFamily(t *testing.T) {
	games := []datGame{
		{Name: "parent", Description: "Game (World)", SourceFile: "driver.cpp"},
		{Name: "japan", Description: "Regional Name (Japan)", SourceFile: "driver.cpp", CloneOf: "parent"},
		{Name: "bootleg", Description: "Regional Name (bootleg)", SourceFile: "driver.cpp", CloneOf: "parent"},
		{Name: "other", Description: "Unrelated Name", SourceFile: "driver.cpp"},
		{Name: "unrelated", Description: "Unrelated Name (bootleg)", SourceFile: "driver.cpp", CloneOf: "parent"},
		{Name: "reskin", Description: "Reskin (hack)", SourceFile: "driver.cpp", CloneOf: "parent"},
		{Name: "reskinboot", Description: "Reskin (bootleg)", SourceFile: "driver.cpp", CloneOf: "parent"},
		{Name: "special", Description: "Game Black Edition (Japan)", SourceFile: "driver.cpp", CloneOf: "parent"},
		{Name: "specialboot", Description: "Game Black Edition (bootleg)", SourceFile: "driver.cpp", CloneOf: "parent"},
	}
	aliases := arcadeOfficialCloneAliases(games, nil, ArcadeRouting{})
	if !arcadeBootlegMatchesAliases(games[2], games[0], aliases) {
		t.Fatal("official regional title not recognized")
	}
	for _, i := range []int{4, 6, 8} {
		if arcadeBootlegMatchesAliases(games[i], games[0], aliases) {
			t.Fatal("unrelated/modified title supplied evidence", games[i])
		}
	}
	groups, _ := arcadeBundleGames(games, true)
	for _, g := range groups {
		if g.Title == "Game" && len(g.Releases) != 3 {
			t.Fatal(g)
		}
	}
	cycle := []datGame{{Name: "a", Description: "Game", SourceFile: "driver.cpp", CloneOf: "b"}, {Name: "b", Description: "Alias", SourceFile: "driver.cpp", CloneOf: "a"}}
	if len(arcadeOfficialCloneAliases(cycle, nil, ArcadeRouting{})) != 0 {
		t.Fatal("cycle supplied alias evidence")
	}
}

func TestArcadeDevelopmentCabinetAndProtectionReleases(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "category.zip")
	var xml, names strings.Builder
	xml.WriteString(`<mame build="0.289 (fixture)"><machine name="home"><description>Home computer</description></machine>`)
	add := func(name, parent, source, title, manufacturer string) {
		fmt.Fprintf(&xml, `<machine name="%s" cloneof="%s" sourcefile="%s"><description>%s</description><manufacturer>%s</manufacturer><rom name="%s.bin" size="1" crc="12345678"/></machine>`, name, parent, source, title, manufacturer, name)
		fmt.Fprintln(&names, name)
	}
	add("alpham2", "", "snk/neogeo.cpp", "Alpha Mission II / ASO II - Last Guardian (NGM-007 ~ NGH-007)", "SNK")
	add("alpham2p", "alpham2", "snk/neogeo.cpp", "Alpha Mission II / ASO II - Last Guardian (prototype)", "SNK")
	add("mhavoc", "", "atari/mhavoc.cpp", "Major Havoc (rev 3)", "Atari")
	add("alphaone", "mhavoc", "atari/mhavoc.cpp", "Alpha One (prototype, 3 lives)", "Atari")
	add("alphaonea", "mhavoc", "atari/mhavoc.cpp", "Alpha One (prototype, 5 lives)", "Atari")
	add("alpha1v", "", "irem/m52.cpp", "Alpha One (Vision Electronics)", "Vision Electronics")
	// A simple source is enough to isolate cabinet labels from hardware routing.
	add("wheel", "", "fixture/wheel.cpp", "18 Wheeler: American Pro Trucker (deluxe, Rev A)", "Sega")
	add("wheels", "wheel", "fixture/wheel.cpp", "18 Wheeler: American Pro Trucker (standard)", "Sega")
	add("wheelu", "wheel", "fixture/wheel.cpp", "18 Wheeler: American Pro Trucker (upright)", "Sega")
	add("1943", "", "capcom/1943.cpp", "1943: The Battle of Midway (Euro)", "Capcom")
	add("1943j", "1943", "capcom/1943.cpp", "1943: Midway Kaisen (Japan)", "Capcom")
	add("1943b", "1943", "capcom/1943.cpp", "1943: Battle of Midway (bootleg, hack of Japan set)", "bootleg")
	add("1943bj", "1943", "capcom/1943.cpp", "1943: Midway Kaisen (bootleg)", "bootleg")
	add("1943jah", "1943", "capcom/1943.cpp", "1943: Midway Kaisen (Japan, no protection hack)", "Capcom")
	add("1943kai", "", "capcom/1943.cpp", "1943 Kai: Midway Kaisen (Japan)", "Capcom")
	add("1943mii", "", "capcom/1943.cpp", "1943: The Battle of Midway Mark II (US)", "Capcom")
	xml.WriteString(`</mame>`)
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml.String()})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\n" + names.String(),
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Shooter / Misc.]\n" + names.String() + "[Computer / Home]\nhome\n",
	})
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true}
	result, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts["games"] != 7 || len(result.Bindings) != 16 {
		t.Fatal(result.Counts)
	}
	for _, names := range [][]string{{"alpham2", "alpham2p"}, {"mhavoc", "alphaone", "alphaonea"}, {"wheel", "wheels", "wheelu"}, {"1943", "1943j", "1943b", "1943bj", "1943jah"}} {
		for _, name := range names {
			if result.Bindings[name].Game != result.Bindings[names[0]].Game {
				t.Fatal("release not folded", name, result.Bindings[name])
			}
		}
	}
	if result.Bindings["alpha1v"].Game == result.Bindings["alphaone"].Game || result.Bindings["1943kai"].Game == result.Bindings["1943"].Game || result.Bindings["1943mii"].Game == result.Bindings["1943"].Game {
		t.Fatal("independent game roots merged")
	}
	if result.Bindings["wheel"].Game != "18 Wheeler - American Pro Trucker" {
		t.Fatal("cabinet in card title", result.Bindings["wheel"])
	}
	for _, name := range []string{"alpham2", "mhavoc", "1943"} {
		b := result.Bindings[name]
		data, err := os.ReadFile(filepath.Join(root, "platforms", b.Platform, b.Game, "game.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			DefaultRelease string `yaml:"default_release"`
			Releases       map[string]any
		}
		if err = yaml.Unmarshal(data, &card); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"alpham2p", "alphaone", "alphaonea", "1943b", "1943bj", "1943jah"} {
			if card.DefaultRelease == bad {
				t.Fatal("variant chosen over retail", name, card.DefaultRelease)
			}
		}
		if name == "alpham2" && !strings.Contains(string(data), "(prototype)") {
			t.Fatal("prototype title lost")
		}
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validate: %v %v", err, issues)
	}
	repeat, err := ImportArcade(input, options)
	if err != nil || len(repeat.Conflicts) > 0 {
		t.Fatalf("repeat: %v %v", err, repeat.Conflicts)
	}
	options.DataRoot = t.TempDir()
	options.Sets = []string{"alpham2p", "alphaonea", "1943bj", "wheels"}
	subset, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range subset.Bindings {
		if b != result.Bindings[name] {
			t.Fatal("subset identity drift", name, b)
		}
	}
}

func TestArcadeCabinetListsAndExportTitles(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"After Burner (VER 1.32, deluxe/upright)", "After Burner"},
		{"After Burner II (VER 2.00, deluxe / upright)", "After Burner II"},
		{"After Burner Climax (Export)", "After Burner Climax"},
		{"Game (Export, Rev A)", "Game"},
		{"Game (cockpit/sit-down/standard)", "Game"},
		{"Game (deluxe/uncensored)", "Game (deluxe/uncensored)"},
		{"Game (deluxe/upright, censored)", "Game (deluxe/upright, censored)"},
		{"Game (Export, uncensored)", "Game (Export, uncensored)"},
		{"Game (Export Edition)", "Game (Export Edition)"},
		{"Game Deluxe (upright)", "Game Deluxe"},
		{"After Burner (Mega-Tech, SMS based)", "After Burner (Mega-Tech, SMS based)"},
		{"Aero Fighters (newer hardware)", "Aero Fighters (newer hardware)"},
	} {
		if got := arcadeBaseTitle(tc.input); got != tc.want {
			t.Errorf("%q => %q, want %q", tc.input, got, tc.want)
		}
	}
	games := []datGame{
		{Name: "aburner", Description: "After Burner (VER 1.32, deluxe/upright)", SourceFile: "sega/segaxbd.cpp"},
		{Name: "aburner131", CloneOf: "aburner", Description: "After Burner (VER 1.31, deluxe/upright)", SourceFile: "sega/segaxbd.cpp"},
		{Name: "aburner2", Description: "After Burner II (VER 2.00, deluxe/upright)", SourceFile: "sega/segaxbd.cpp"},
		{Name: "abclimax", Description: "After Burner Climax (Export)", SourceFile: "sega/lindbergh.cpp"},
		{Name: "mt_aftrb", Description: "After Burner (Mega-Tech, SMS based)", SourceFile: "sega/megatech.cpp"},
	}
	bundles, _ := arcadeBundleGames(games, true)
	if len(bundles) != 4 {
		t.Fatal(bundles)
	}
	found := false
	for _, b := range bundles {
		if b.Title == "After Burner" {
			found = true
			if len(b.Releases) != 2 || b.Releases[0].Description != games[0].Description || b.Releases[1].Description != games[1].Description {
				t.Fatal("release provenance changed", b)
			}
		}
	}
	if !found {
		t.Fatal("clean title missing", bundles)
	}
	// Export is a release-market label, not proof of a particular region.
	if arcadeRegion("Game (Export)") != "" {
		t.Fatal("export assigned a guessed region")
	}
}
