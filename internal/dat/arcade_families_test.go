package dat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalog"
)

func arcadeFamilyFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "category.zip")
	// Synthetic titles/links on real routing keys isolate family and fold rules.
	xml := `<mame build="0.289 (fixture)">
 <machine name="emeralda" sourcefile="namco/namcona1.cpp"><description>Emeraldia (World)</description><rom name="a" size="1" crc="12345678"/></machine>
 <machine name="emeraldaj" sourcefile="namco/namcona1.cpp" cloneof="emeralda" romof="emeralda"><description>Emeraldia (Japan)</description><rom name="b" size="1" crc="23456789"/></machine>
 <machine name="familya" sourcefile="sega/segas16a.cpp"><description>Family Game (World)</description></machine>
 <machine name="altbeast" sourcefile="sega/segas16b.cpp" cloneof="familya"><description>Family Game (Japan)</description></machine>
 <machine name="fantzn2x" sourcefile="sega/segas16b.cpp" cloneof="familya"><description>Family Game (USA, bootleg hack)</description></machine>
 <machine name="future" sourcefile="future/board.cpp" cloneof="familya"><description>Family Game (USA)</description></machine>
 <machine name="home" sourcefile="sega/segas16a.cpp"><description>Home computer</description></machine>
 </mame>`
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\nemeralda\nemeraldaj\nfamilya\naltbeast\nfantzn2x\nfuture\n",
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Puzzle / Drop]\nemeralda\nemeraldaj\nfamilya\naltbeast\nfantzn2x\nfuture\n[Computer / Home]\nhome\n",
	})
	return input, classes
}

func TestArcadeFamiliesApplyFoldAndRetainBoards(t *testing.T) {
	input, classes := arcadeFamilyFixture(t)
	root := t.TempDir()
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true}
	result, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts["games"] != 4 || result.Counts["fold_groups"] != 2 || result.Counts["excluded"] != 1 {
		t.Fatal(result.Counts)
	}
	a, b := result.Bindings["emeralda"], result.Bindings["emeraldaj"]
	if a.Family != "Arcade - Namco - NA" || b.Family != a.Family || a.Platform != "Arcade" || b.Platform != a.Platform || a.Game != b.Game ||
		a.Hardware != "Arcade - Namco - NA-2" || b.Hardware != "Arcade - Namco - NA-1" {
		t.Fatalf("family/board identity lost: %+v %+v", a, b)
	}
	c, d := result.Bindings["familya"], result.Bindings["altbeast"]
	if c.Family != "Arcade - Sega - System 16" || c.Game != d.Game || c.Hardware == d.Hardware {
		t.Fatalf("reviewed cross-driver family not folded: %+v %+v", c, d)
	}
	if result.Bindings["fantzn2x"].Game == c.Game {
		t.Fatal("bootleg folded")
	}
	if result.Bindings["future"].Hardware != "" {
		t.Fatal("fallback collection claimed hardware")
	}
	for _, entry := range result.Entries {
		if entry.Name == "home" && (entry.Platform != "" || entry.Hardware != "") {
			t.Fatal("home routed before classification")
		}
		if entry.Name == "emeraldaj" && (entry.Hardware != b.Hardware || entry.Platform != b.Platform) {
			t.Fatal(entry)
		}
	}
	file := filepath.Join(root, "platforms", a.Platform, a.Game, "game.yaml")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Releases map[string]struct {
			MAME struct {
				Hardware string
				Snapshot string
			}
		}
	}
	if err = yaml.Unmarshal(data, &card); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"emeralda", "emeraldaj"} {
		if card.Releases[name].MAME.Hardware != result.Bindings[name].Hardware || card.Releases[name].MAME.Snapshot != result.Snapshot {
			t.Fatal(card)
		}
	}
	src := filepath.Join(root, "bindings", "romsets", "mame", result.Snapshot)
	bindings, err := readArcadeBindings(filepath.Join(src, "catalog.yaml"))
	if err != nil || bindings["emeraldaj"] != b {
		t.Fatalf("saved binding: %v %+v", err, bindings)
	}
	saved, err := os.ReadFile(filepath.Join(src, "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	var routing ArcadeRouting
	if err = json.Unmarshal(saved, &routing); err != nil {
		t.Fatal(err)
	}
	if routing.Families[b.Hardware] != b.Family || routing.SourceSets["namco/namcona1.cpp"]["emeraldaj"] != b.Hardware {
		t.Fatal("family replaced board evidence")
	}
	repeat, err := ImportArcade(input, options)
	if err != nil || repeat.Snapshot != result.Snapshot {
		t.Fatalf("repeat: %v", err)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var beforeValue, afterValue map[string]any
	if err = yaml.Unmarshal(data, &beforeValue); err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(after, &afterValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeValue, afterValue) {
		t.Fatal("repeat changed card values")
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validate: %v %v", err, issues)
	}
	// Hardware is verified against each release even when two releases share a card.
	changed := strings.Replace(string(after), "hardware: "+b.Hardware, "hardware: "+a.Hardware, 1)
	if changed == string(after) {
		t.Fatal("test failed to change hardware")
	}
	if err = os.WriteFile(file, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	issues, err = catalog.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "hardware disagrees") {
			found = true
		}
	}
	if !found {
		t.Fatal("board mismatch not detected")
	}
}

func TestArcadeFamilyMapOverridesAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routing.json")
	for _, tc := range []struct {
		body    string
		invalid bool
	}{
		{`{"version":1,"families":{"Arcade - Namco - NA-1":"Arcade - Namco - NA-1"}}`, false},
		{`{"version":1,"families":{"missing":"Arcade - Namco - NA"}}`, true},
		{`{"version":1,"families":{"Arcade - Namco - NA-1":"missing"}}`, true},
		{`{"version":1,"families":{"Arcade - Namco - NA-1":"Arcade - Other"}}`, true},
		{`{"version":1,"families":{"Arcade - Other":"Arcade - Namco - NA"}}`, true},
		{`{"version":1,"families":{"Arcade - Namco - NA":"Arcade - Namco - NB"}}`, true},
		{`{"version":1,"families":{"Arcade - Namco - NA-1":"Arcade - Namco - NA-2","Arcade - Namco - NA-2":"Arcade - Namco - NA-1"}}`, true},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		r, err := loadArcadeRouting(path)
		if (err != nil) != tc.invalid {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if !tc.invalid && arcadeCatalogPlatform("Arcade - Namco - NA-1", r) != "Arcade - Namco - NA-1" {
			t.Fatal("identity override ignored")
		}
	}
	r, err := loadArcadeRouting("")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Capcom - CPS-1", "Capcom - CPS-2", "Capcom - CPS-3", "SNK - Neo Geo", "SNK - Hyper Neo Geo 64", "Sega - Naomi", "Sammy - Atomiswave", "Namco - System 22.5", "Sega - System 1", "Sega - System 2"} {
		full := "Arcade - " + name
		if arcadeCatalogPlatform(full, r) != full {
			t.Fatal("distinct platform collapsed", name)
		}
	}
}

func TestArcadeFamilyNoFoldAndUnknownDriver(t *testing.T) {
	input, classes := arcadeFamilyFixture(t)
	result, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: t.TempDir()}, ClassificationPath: classes, Fold: false})
	if err != nil || result.Counts["games"] != 6 {
		t.Fatalf("no-fold: %v %v", err, result.Counts)
	}
	r := defaultArcadeRouting()
	games := []datGame{{Name: "a", Description: "Game (World)", SourceFile: "one.cpp"}, {Name: "b", Description: "Game (Japan)", CloneOf: "a"}}
	boards := map[string]string{"a": "Arcade - Sega - System 16A", "b": "Arcade - Sega - System 16B"}
	bundles, notes := arcadeBundleGamesWithHardware(games, true, boards, r)
	if len(bundles) != 2 || notes["b"] == "" {
		t.Fatal("unknown driver folded")
	}
}

func TestArcadeFamilyChangeBlocksDuplicateAcrossSnapshots(t *testing.T) {
	input, classes := arcadeFamilyFixture(t)
	root := t.TempDir()
	override := filepath.Join(t.TempDir(), "routing.json")
	data := `{"version":1,"families":{"Arcade - Namco - NA-1":"Arcade - Namco - NA-1","Arcade - Namco - NA-2":"Arcade - Namco - NA-2"}}`
	if err := os.WriteFile(override, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, RoutingPath: override, Sets: []string{"emeralda", "emeraldaj"}, Fold: true}
	old, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	options.RoutingPath = ""
	options.Apply = false
	preview, err := ImportArcade(input, options)
	if err != nil || len(preview.Conflicts) < 2 || preview.Snapshot == old.Snapshot {
		t.Fatalf("preview: %v %+v", err, preview)
	}
	options.Apply = true
	blocked, err := ImportArcade(input, options)
	if err == nil || blocked.Applied {
		t.Fatal("silently duplicated prior bound cards")
	}
	if _, err = os.Stat(filepath.Join(root, "platforms", "Arcade", "Emeraldia")); !os.IsNotExist(err) {
		t.Fatal("new family created despite conflict")
	}
	snapshots, err := os.ReadDir(filepath.Join(root, "bindings", "romsets", "mame"))
	if err != nil || len(snapshots) != 1 {
		t.Fatal("new snapshot persisted despite conflict")
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("original catalog invalid: %v %v", err, issues)
	}
}
