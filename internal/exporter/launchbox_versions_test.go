package exporter

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func versionRecord(rom, game, release, region, language, kind string) Record {
	r := Record{ROM: rom, Game: game, GameTitle: "Shared game", Release: release, Status: "matched", Fields: map[string]Value{"title": {Value: release}}, Media: map[string]Media{}, TitleContext: &TitleContext{Regional: "Regional name", Release: release, Regions: []string{region}, Languages: []string{language}}}
	if kind != "original" {
		r.Variant, r.Release = release, ""
		r.VariantInfo = &VariantInfo{Kind: kind, Languages: []string{language}, Version: "1.2", Group: "Test team"}
	}
	return r
}

func TestLaunchBoxGameVersionsAndConfiguredPrimary(t *testing.T) {
	records := []Record{
		versionRecord("jp.z64", "platforms/n64/Game", "Game (Japan)", "Japan", "ja", "original"),
		versionRecord("us.z64", "platforms/n64/Game", "Game (USA)", "USA", "en", "original"),
		versionRecord("rev.z64", "platforms/n64/Game", "Game (USA) (Rev 1)", "USA", "en", "original"),
		versionRecord("ru.z64", "platforms/n64/Game", "Russian translation", "USA", "ru", "translation"),
		versionRecord("en.z64", "platforms/n64/Game", "English translation", "USA", "en", "translation"),
		versionRecord("hack.z64", "platforms/n64/Game", "Hard mode", "USA", "en", "hack"),
	}
	plan := &Plan{System: "n64", ROMRoot: t.TempDir(), Records: records, Profile: Profile{LaunchBox: LaunchBoxPolicy{Regions: []string{"USA", "Japan"}}}}
	if err := ProjectTarget(plan, "launchbox"); err != nil {
		t.Fatal(err)
	}
	if len(plan.LaunchBox.Groups) != 1 || plan.LaunchBox.VersionCount != 6 {
		t.Fatal(plan.LaunchBox)
	}
	group := plan.LaunchBox.Groups[0]
	if group.Title != "Shared game" || group.Versions[0].Path != filepath.Join(plan.ROMRoot, "us.z64") {
		t.Fatalf("%+v", group)
	}
	gameID := group.ID
	plan.Profile.LaunchBox = LaunchBoxPolicy{Kinds: []string{"translation", "original", "hack"}, Regions: []string{"USA"}, Languages: []string{"ru", "en"}}
	if err := ProjectTarget(plan, "launchbox"); err != nil {
		t.Fatal(err)
	}
	group = plan.LaunchBox.Groups[0]
	if group.ID != gameID || group.Title != "Shared game" || group.Versions[0].Path != filepath.Join(plan.ROMRoot, "ru.z64") {
		t.Fatal("configured primary or stable game identity", group)
	}
	body, err := RenderLaunchBox(plan)
	if err != nil {
		t.Fatal(err)
	}
	var native launchBoxList
	if err := xml.Unmarshal(body, &native); err != nil {
		t.Fatal(err)
	}
	if len(native.Games) != 1 || len(native.Versions) != 6 || !strings.Contains(native.Versions[0].Name, "translation: Russian translation [ru]") {
		t.Fatal(native)
	}
	for _, v := range native.Versions {
		if v.GameID != gameID || v.Section != "Version" || !v.UseEmulator || !v.Installed {
			t.Fatal(v)
		}
	}
	before := plan.LaunchBox
	for i, j := 0, len(plan.Records)-1; i < j; i, j = i+1, j-1 {
		plan.Records[i], plan.Records[j] = plan.Records[j], plan.Records[i]
	}
	if err := ProjectTarget(plan, "launchbox"); err != nil {
		t.Fatal(err)
	}
	after := plan.LaunchBox
	// Indices are provenance positions; native identities, labels and default stay stable.
	if before.Groups[0].ID != after.Groups[0].ID || before.Groups[0].Primary != after.Groups[0].Primary {
		t.Fatal("input order changed default")
	}
}

func TestLaunchBoxNeverGroupsByDisplayTitle(t *testing.T) {
	a := versionRecord("a.z64", "platforms/n64/A", "Same name", "USA", "en", "original")
	b := versionRecord("b.z64", "platforms/n64/B", "Same name", "USA", "en", "original")
	c := a
	c.ROM, c.Status, c.Game = "unknown.z64", "unmatched", ""
	d := c
	d.ROM, d.Status = "ambiguous.z64", "ambiguous"
	plan := &Plan{System: "n64", ROMRoot: t.TempDir(), Records: []Record{a, b, c, d}}
	if err := ProjectTarget(plan, "launchbox"); err != nil {
		t.Fatal(err)
	}
	if len(plan.LaunchBox.Groups) != 4 {
		t.Fatal("grouped homonyms or unidentified files")
	}
}

func TestLaunchBoxSeparateGeneratedDiscEditionPlaylists(t *testing.T) {
	options, _, _ := fixture(t)
	options.Target, options.Platform = "launchbox", "psx"
	path := addPlatform(t, options, "psx", "placeholder", []byte("placeholder"), "none.iso")
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte("items: []\n"))
	var yaml strings.Builder
	yaml.WriteString("title: Disc game\nreleases:\n")
	for _, edition := range []string{"USA", "Europe", "USA) (Rev 1"} {
		for disc := 1; disc <= 2; disc++ {
			release := fmt.Sprintf("Disc game (%s) (Disc %d)", edition, disc)
			fmt.Fprintf(&yaml, "  %q: {}\n", release)
			put(t, filepath.Join(options.ROMRoot, release+".iso"), []byte("synthetic "+release))
		}
	}
	put(t, filepath.Join(path, "game.yaml"), []byte(yaml.String()))
	profile(t, &options, "schema_version: 1\ntarget: es-de\nlaunchbox:\n  regions: [USA, Europe]\n")
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Matched != 6 || len(plan.LaunchBox.Groups) != 1 || plan.LaunchBox.VersionCount != 3 {
		t.Fatalf("%+v", plan.LaunchBox)
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	native := readLaunchBox(t, plan)
	if len(native.Games) != 1 || len(native.Versions) != 3 {
		t.Fatal(native)
	}
	paths := map[string]bool{}
	for _, v := range plan.LaunchBox.Groups[0].Versions {
		if !v.GeneratedPlaylist || len(v.Records) != 2 || paths[v.Path] {
			t.Fatal(v)
		}
		paths[v.Path] = true
		data, err := os.ReadFile(filepath.Join(plan.Out, filepath.FromSlash(v.Path)))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 2 {
			t.Fatal(string(data))
		}
		for i, index := range v.Records {
			if lines[i] != filepath.Join(plan.ROMRoot, plan.Records[index].ROM) || !strings.Contains(lines[i], fmt.Sprintf("(Disc %d)", i+1)) {
				t.Fatal("disc mix or order", string(data))
			}
		}
	}
	data, _ := RenderLaunchBox(plan)
	if strings.Contains(string(data), "<Emulation>") {
		t.Fatal("unsupported root emulation flag")
	}
	if strings.Contains(string(data), "<Disc>") {
		t.Fatal("numbered global versions could mix regional disc sets")
	}
	options.Out = filepath.Join(filepath.Dir(options.Out), "changed")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(options.ROMRoot, plan.Records[0].ROM), []byte("changed source disc"))
	if err := Apply(plan, nil); err == nil {
		t.Fatal("changed disc accepted")
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("published invalid package")
	}
}

func TestLaunchBoxDoesNotGuessIncompleteDiscGroups(t *testing.T) {
	for _, names := range [][]string{
		{"Game (USA) (Disc 1)", "Game (USA) (Disc 3)"},
		{"Game (USA) (Disc 1 of 3)", "Game (USA) (Disc 2 of 3)"},
		{"Game (USA) (Disc 1)", "Game (Europe) (Disc 2)"},
		{"Game (USA) (Disc 1)", "Game (USA) (Disc 1)"},
	} {
		plan := &Plan{System: "psx", ROMRoot: t.TempDir()}
		for i, name := range names {
			r := versionRecord(fmt.Sprintf("%d.iso", i), "platforms/psx/Game", name, "USA", "en", "original")
			r.TitleContext.Disc = fmt.Sprintf("Disc %d", i+1)
			plan.Records = append(plan.Records, r)
		}
		if err := ProjectTarget(plan, "launchbox"); err != nil {
			t.Fatal(err)
		}
		if plan.LaunchBox.VersionCount != 2 {
			t.Fatal("assembled incomplete or mixed set", plan.LaunchBox)
		}
		for _, v := range plan.LaunchBox.Groups[0].Versions {
			if v.GeneratedPlaylist {
				t.Fatal(v)
			}
		}
	}
}

func TestLaunchBoxSuppliedRegionalAndModifiedPlaylists(t *testing.T) {
	plan := &Plan{System: "psx", ROMRoot: t.TempDir()}
	for _, entry := range []struct{ dir, region, lang, kind string }{{"USA", "USA", "en", "original"}, {"Japan", "Japan", "ja", "original"}, {"Russian", "USA", "ru", "translation"}, {"Hack", "USA", "en", "hack"}} {
		r := versionRecord(entry.dir+".m3u", "platforms/psx/Game", "Game ("+entry.region+") (Disc 1)", entry.region, entry.lang, entry.kind)
		r.Representation = "playlist-directory"
		r.LaunchFile = entry.dir + ".m3u/" + entry.dir + ".m3u"
		r.Playlist = []PlaylistInput{{ROM: entry.dir + ".m3u/Disc 1.chd"}, {ROM: entry.dir + ".m3u/Disc 2.chd"}}
		plan.Records = append(plan.Records, r)
	}
	if err := ProjectTarget(plan, "launchbox"); err != nil {
		t.Fatal(err)
	}
	if len(plan.LaunchBox.Groups) != 1 || plan.LaunchBox.VersionCount != 4 {
		t.Fatal(plan.LaunchBox)
	}
	for _, v := range plan.LaunchBox.Groups[0].Versions {
		if v.GeneratedPlaylist || len(v.Records) != 1 || !strings.HasSuffix(v.Path, ".m3u") || (!strings.Contains(v.Name, "[2 discs]") || strings.Contains(v.Name, "(Disc 1)")) {
			t.Fatal(v)
		}
	}
}

func TestLaunchBoxClientOnlyPolicy(t *testing.T) {
	input := "schema_version: 1\ntarget: es-de\nlaunchbox:\n  kinds: [translation, original]\n  regions: [USA, Europe]\n  languages: [ru, en]\ntitles: {mode: regional}\n"
	wire, policy, err := ClientExportProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wire, "launchbox:") || policy.Kinds[0] != "translation" || policy.Regions[0] != "usa" {
		t.Fatal(wire, policy)
	}
	if err := ValidateExportProfile(wire); err != nil {
		t.Fatal(err)
	}
	plain := "schema_version: 1\ntarget: es-de\n"
	same, _, err := ClientExportProfile(plain)
	if err != nil || same != plain {
		t.Fatal("rewrote unrelated profile")
	}
	for _, bad := range []string{"launchbox: {regions: ['']}", "launchbox: {kinds: [original, Original]}", "launchbox: {unknown: yes}"} {
		if _, _, err := ClientExportProfile("schema_version: 1\ntarget: es-de\n" + bad + "\n"); err == nil {
			t.Fatal("accepted invalid launchbox policy", bad)
		}
	}
}
