package dat

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
	"rommeta/internal/catalogcache"
	"rommeta/internal/fingerprintselect"
)

func arcadeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	xml := `<mame build="0.289 (fixture)">
<machine name="neogeo" isbios="yes"><description>Neo-Geo MV-6F</description></machine>
<machine name="mslug" sourcefile="snk/neogeo.cpp" romof="neogeo"><description>Metal Slug - Super Vehicle-001</description><year>1996</year><manufacturer>SNK</manufacturer><rom name="chip" size="1024" crc="12345678" sha1="1111111111111111111111111111111111111111"/><driver status="good"/></machine>
<machine name="mslug2" sourcefile="snk/neogeo.cpp" romof="neogeo"><description>Metal Slug 2</description><rom name="chip" size="1024" crc="12345678"/></machine>
<machine name="sf2" sourcefile="capcom/cps1.cpp"><description>Street Fighter II: The World Warrior (World 910522)</description><rom name="sf2.bin" size="1024" crc="23456789"/></machine>
<machine name="sf2ua" sourcefile="capcom/cps1.cpp" cloneof="sf2" romof="sf2"><description>Street Fighter II: The World Warrior (USA 910206)</description><rom name="sf2ua.bin" size="1024" crc="34567890"/></machine>
<machine name="sf2hack" sourcefile="capcom/cps1.cpp" cloneof="sf2" romof="sf2"><description>Street Fighter II: The World Warrior (World, bootleg hack)</description><manufacturer>bootleg</manufacturer></machine>
<machine name="cross" sourcefile="capcom/cps2.cpp" cloneof="sf2"><description>Street Fighter II: The World Warrior (Japan)</description></machine>
<machine name="kinst" sourcefile="rare/kinst.cpp" sampleof="mslug"><description>Killer Instinct</description><disk name="kinst" sha1="2222222222222222222222222222222222222222"/><device_ref name="z80"/></machine>
<machine name="playch10" isbios="yes"><description>PlayChoice-10 BIOS</description></machine>
<machine name="pc_smb" sourcefile="nintendo/playch10.cpp" romof="playch10"><description>Super Mario Bros. (PlayChoice-10)</description></machine>
<machine name="nes" sourcefile="nintendo/nes.cpp"><description>NES</description></machine>
<machine name="aes" sourcefile="snk/neogeo.cpp"><description>Neo-Geo AES</description></machine>
<machine name="c64"><description>Commodore 64</description></machine>
<machine name="pres2k"><description>Presenter 2000</description></machine>
<machine name="unknown"><description>Unclassified</description></machine>
<machine name="mechanical" ismechanical="yes"><description>Mechanical</description></machine>
<machine name="z80" isdevice="yes" runnable="no"><description>Z80</description></machine>
</mame>`
	input := filepath.Join(dir, "mame.zip")
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml})
	classify := filepath.Join(dir, "category.zip")
	arcadeTestZIP(t, classify, map[string]string{
		"folders/mame.ini":     ";; MAME.ini 0.289 / fixture\n[ROOT_FOLDER]\nneogeo\nplaych10\nmslug\nmslug2\nsf2\nsf2ua\nsf2hack\ncross\nkinst\npc_smb\npres2k\nunknown\nmechanical\n",
		"folders/mess.ini":     ";; MESS.ini 0.289 / fixture\n[ROOT_FOLDER]\nnes\naes\nc64\n",
		"folders/category.ini": ";; CATEGORY.ini 0.289 / fixture\n[System / BIOS]\nneogeo\nplaych10\n[Platform / Shooter Scrolling]\nmslug\nmslug2\n[Fighter / Versus]\nsf2\nsf2ua\nsf2hack\ncross\nkinst\n[Platform / Run, Jump & Scrolling]\npc_smb\n[Game Console / Home Videogame]\nnes\naes\n[Computer / Home System]\nc64\npres2k\n[Electromechanical / Pinball]\nmechanical\n",
	})
	return input, classify
}
func arcadeTestZIP(t *testing.T, path string, members map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range sortedArcadeKeys(members) {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(members[name])); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArcadePlanScopeAndFold(t *testing.T) {
	input, classification := arcadeFixture(t)
	root := filepath.Join(t.TempDir(), "untouched")
	r, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, ClassificationPath: classification, Fold: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("dry run created data root")
	}
	if r.Counts["selected"] != 8 || r.Counts["games"] != 7 || r.Counts["fold_groups"] != 1 {
		t.Fatalf("unexpected counts: %#v", r.Counts)
	}
	for _, name := range []string{"nes", "aes", "c64", "neogeo", "playch10", "z80", "pres2k", "unknown", "mechanical"} {
		if _, ok := r.Bindings[name]; ok {
			t.Fatalf("excluded/support/review set imported: %s", name)
		}
	}
	if r.Bindings["sf2"].Game != r.Bindings["sf2ua"].Game {
		t.Fatal("confirmed SF2 revisions not folded")
	}
	if r.Bindings["sf2hack"].Game == r.Bindings["sf2"].Game {
		t.Fatal("bootleg folded")
	}
	if r.Bindings["mslug"].Game == r.Bindings["mslug2"].Game {
		t.Fatal("BIOS-dependent games folded")
	}
	if r.Bindings["cross"].Family == r.Bindings["sf2"].Family || r.Bindings["cross"].Game == r.Bindings["sf2"].Game {
		t.Fatal("cross-platform clone misrouted")
	}
	if r.Bindings["pc_smb"].Family != "Arcade - Nintendo - PlayChoice-10" {
		t.Fatal("console-derived arcade lost")
	}
	noFold, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, ClassificationPath: classification})
	if err != nil {
		t.Fatal(err)
	}
	if noFold.Counts["games"] != 8 {
		t.Fatal(noFold.Counts)
	}
	p, err := BuildPlan(input, PlanOptions{SampleLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if p.CloneLinkCount != 3 || p.ROMDependencyCount != 5 {
		t.Fatalf("clone/dependency counts conflated: %#v", p)
	}
}

func TestArcadeApplyPreservesSnapshotPayloadsAndCuration(t *testing.T) {
	input, classifications := arcadeFixture(t)
	root := t.TempDir()
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classifications, Fold: true}
	r, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Applied {
		t.Fatal("not applied")
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validation: %v %#v", err, issues)
	}
	b := r.Bindings["sf2"]
	game, err := catalogcache.ReadGameBundle(root, b.Platform, b.Game)
	if err != nil {
		t.Fatal(err)
	}
	if game.DefaultRelease != "sf2ua" {
		t.Fatalf("region priority not used: %s", game.DefaultRelease)
	}
	if len(game.Fingerprints) != 2 {
		t.Fatalf("missing chip fingerprints: %#v", game.Fingerprints)
	}
	for _, fp := range game.Fingerprints {
		if fp.PayloadKind != "rom-component" || fingerprintselect.UsableForHashLookup(fp, 1) {
			t.Fatal("component hash usable for whole-game lookup")
		}
	}
	k := r.Bindings["kinst"]
	disk, err := catalogcache.ReadGameBundle(root, k.Platform, k.Game)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Fingerprints) != 1 || disk.Fingerprints[0].PayloadKind != "chd-disk" {
		t.Fatal("lost disk fingerprint")
	}
	src := filepath.Join(root, "bindings", "romsets", "mame", r.Snapshot)
	hash, _ := arcadeFileHash(input)
	stored, _ := arcadeFileHash(filepath.Join(src, "mame.zip"))
	if hash != stored {
		t.Fatal("source not preserved")
	}
	bindings, err := readArcadeBindings(filepath.Join(src, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 8 || bindings["sf2"] != b {
		t.Fatal("bindings disagree with cards")
	}
	gamePath := filepath.Join(root, "platforms", b.Platform, b.Game, "game.yaml")
	original, err := os.ReadFile(gamePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), "World 910522") {
		t.Fatal("full release title lost")
	}
	if err = os.WriteFile(gamePath, append(original, []byte("\nuser_note: Keep this\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	platformPath := filepath.Join(root, "platforms", b.Platform, "_platform.yaml")
	platformData, err := os.ReadFile(platformPath)
	if err != nil {
		t.Fatal(err)
	}
	platformData = []byte(strings.Replace(string(platformData), "display_name: "+b.Platform, "display_name: My CPS", 1) + "\nprovider_mapping:\n  example: arcade\n")
	if err = os.WriteFile(platformPath, platformData, 0o644); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(filepath.Dir(gamePath), "media")
	if err = os.Mkdir(media, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(media, "screenshot.png"), []byte("user-media"), 0o644); err != nil {
		t.Fatal(err)
	}
	r2, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshot != r2.Snapshot {
		t.Fatal("unstable snapshot")
	}
	data, err := os.ReadFile(gamePath)
	if err != nil || !strings.Contains(string(data), "Keep this") {
		t.Fatal("curation lost")
	}
	data, err = os.ReadFile(filepath.Join(media, "screenshot.png"))
	if err != nil || string(data) != "user-media" {
		t.Fatal("media lost")
	}
	data, err = os.ReadFile(platformPath)
	if err != nil || !strings.Contains(string(data), "example: arcade") || !strings.Contains(string(data), "display_name: My CPS") {
		t.Fatalf("platform curation lost: %v %s", err, data)
	}
	game, err = catalogcache.ReadGameBundle(root, b.Platform, b.Game)
	if err != nil || len(game.Fingerprints) != 2 {
		t.Fatal("rerun duplicated fingerprints")
	}
}

func TestArcadeRejectsIncompatibleInputsBeforeApply(t *testing.T) {
	for _, scenario := range []string{"missing classifications", "version mismatch", "excluded selection", "missing selection", "software list", "existing report"} {
		t.Run(scenario, func(t *testing.T) {
			input, c := arcadeFixture(t)
			root := filepath.Join(t.TempDir(), "catalog")
			options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: c, Fold: true}
			switch scenario {
			case "missing classifications":
				options.ClassificationPath = ""
				options.Offline = true
			case "version mismatch":
				arcadeTestZIP(t, c, map[string]string{"mame.ini": ";; MAME.ini 0.288\n[ROOT_FOLDER]\nsf2\n"})
			case "excluded selection":
				options.Sets = []string{"aes"}
			case "missing selection":
				options.Sets = []string{"nonexistent"}
			case "software list":
				arcadeTestZIP(t, input, map[string]string{"mame.xml": "<softwarelist name=\"nes\"/>"})
			case "existing report":
				options.ReportPath = input
			}
			if _, err := ImportArcade(input, options); err == nil {
				t.Fatal("expected error")
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("rejected import changed catalog")
			}
		})
	}
}

func TestArcadeSubsetAndReport(t *testing.T) {
	input, c := arcadeFixture(t)
	dir := t.TempDir()
	report := filepath.Join(dir, "plan.json")
	r, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: filepath.Join(dir, "catalog")}, ClassificationPath: c, Fold: true, Sets: []string{"sf2", "sf2ua"}, ReportPath: report})
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["games"] != 1 || len(r.Bindings) != 2 {
		t.Fatal(r.Counts)
	}
	data, err := os.ReadFile(report)
	if err != nil || !strings.Contains(string(data), "pres2k") {
		t.Fatal("report missing unselected source decisions")
	}
}

func TestArcadeFoldBoundaries(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Game (World 123456)", "Game"},
		{"Game (Revision A)", "Game"},
		{"Game (The Sequel)", "Game (The Sequel)"},
		{"Game (USSR)", "Game (USSR)"},
	}
	for _, c := range cases {
		if got := arcadeBaseTitle(c.title); got != c.want {
			t.Errorf("%q => %q", c.title, got)
		}
	}
	games := []datGame{{Name: "a", Description: "Game (USA)", CloneOf: "b"}, {Name: "b", Description: "Game (Japan)", CloneOf: "a"}}
	bundles, notes := arcadeBundleGames(games, true)
	if len(bundles) != 2 || len(notes) != 2 {
		t.Fatal("cycle folded")
	}
	c := arcadeClassification{arcade: map[string]bool{"a": true}, other: map[string]bool{}, categories: map[string]string{"a": "Quiz / Questions"}}
	state, _ := classifyArcade(datGame{Name: "a", DriverStatus: "preliminary"}, c)
	if state != "eligible" {
		t.Fatal("quiz or non-working metadata excluded")
	}
}

func TestArcadeRoutingOverride(t *testing.T) {
	input, c := arcadeFixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "routing.json")
	contents := `{"version":1,"sets":{"kinst":"Arcade - Test - Board"},"platforms":{"Arcade - Test - Board":{"kind":"hardware","naming_evidence":"reviewed test hardware"}}}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: dir}, ClassificationPath: c, RoutingPath: path, Fold: true, Sets: []string{"kinst"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Bindings["kinst"].Family != "Arcade - Test - Board" {
		t.Fatal("override ignored")
	}
}

func TestArcadeValidationDetectsBrokenSnapshot(t *testing.T) {
	input, c := arcadeFixture(t)
	root := t.TempDir()
	r, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: c, Fold: true, Sets: []string{"sf2", "sf2ua"}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bindings", "romsets", "mame", r.Snapshot, "mame.zip")
	if err = os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, err := catalog.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "SHA256 mismatch") {
			found = true
		}
	}
	if !found {
		t.Fatal("broken durable snapshot passed validation")
	}
}
func TestArcadeDoesNotFoldAcrossUnmappedDrivers(t *testing.T) {
	games := []datGame{{Name: "a", Description: "Game (USA)", SourceFile: "one.cpp"}, {Name: "b", Description: "Game (Japan)", SourceFile: "two.cpp", CloneOf: "a"}}
	bundles, notes := arcadeBundleGames(games, true)
	if len(bundles) != 2 || notes["b"] == "" {
		t.Fatal("different unknown hardware folded")
	}
}
