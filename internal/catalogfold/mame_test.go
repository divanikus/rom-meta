package catalogfold

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalog"
	"rommeta/internal/dat"
)

func mameFixture(t *testing.T, folded bool, selected []string) (string, dat.ArcadeResult) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "catalog")
	input, classes := filepath.Join(base, "mame.zip"), filepath.Join(base, "classes.zip")
	zipFile := func(path string, files map[string]string) {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		for name, body := range files {
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	zipFile(input, map[string]string{"mame.xml": `<mame build="0.289 (fixture)">
<machine name="a" sourcefile="capcom/cps1.cpp"><description>Alpha (World)</description><rom name="a.rom" size="1" crc="12345678"/></machine>
<machine name="b" cloneof="a" sourcefile="capcom/cps1.cpp"><description>Alpha (Japan)</description><rom name="b.rom" size="1" crc="23456789"/></machine>
<machine name="c" cloneof="a" sourcefile="capcom/cps2.cpp"><description>Alpha (World)</description><rom name="c.rom" size="1" crc="34567890"/></machine>
<machine name="d" sourcefile="capcom/cps1.cpp"><description>Beta (World)</description><rom name="d.rom" size="1" crc="45678901"/></machine>
<machine name="e" cloneof="d" sourcefile="capcom/cps1.cpp"><description>Beta (USA)</description><rom name="e.rom" size="1" crc="56789012"/></machine>
<machine name="home"><description>Computer</description></machine></mame>`})
	zipFile(classes, map[string]string{"mame.ini": ";; MAME.ini 0.289\n[ROOT_FOLDER]\na\nb\nc\nd\ne\n", "mess.ini": ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n", "category.ini": ";; CATEGORY.ini 0.289\n[Shooter / Misc.]\na\nb\nc\nd\ne\n[Computer / Home]\nhome\n"})
	r, err := dat.ImportArcade(input, dat.ArcadeOptions{ImportOptions: dat.ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: folded, Sets: selected})
	if err != nil {
		t.Fatal(err)
	}
	if !folded {
		for id, binding := range r.Bindings {
			p := filepath.Join(root, "platforms", "Arcade", binding.Game, "game.yaml")
			f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fmt.Fprintf(f, "\nsources:\n  launchbox:\n    matches:\n      - release: %s\n        database_id: '42'\n        title: Alpha\n        match_score: 150\n        description: retained %s\ncuration:\n  note: local note\n", id, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			media := filepath.Join(filepath.Dir(p), "media", id, "logo.png")
			if err := os.MkdirAll(filepath.Dir(media), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(media, []byte("media "+id), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, r
}

func mameTree(t *testing.T, root string) map[string]string {
	t.Helper()
	r := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		r[rel] = fmt.Sprintf("%x", sha256.Sum256(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func readMAMEBindings(t *testing.T, root, snapshot string) map[string]dat.ArcadeBinding {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "bindings", "romsets", "mame", snapshot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Sets map[string]dat.ArcadeBinding `yaml:"sets"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Sets
}

func TestFoldMAMEPreviewApplyAndRepeat(t *testing.T) {
	root, old := mameFixture(t, false, nil)
	writeFoldGame(t, root, "Console", "Game (USA)", "usa", "Game", "99")
	writeFoldGame(t, root, "Console", "Game (Japan)", "japan", "Game", "99")
	// Unbound Arcade cards still use ordinary provider-ID folding.
	writeFoldGame(t, root, "Arcade", "Other (USA)", "other_us", "Other", "999")
	writeFoldGame(t, root, "Arcade", "Other (Japan)", "other_jp", "Other", "999")
	before := mameTree(t, root)
	dry, err := Fold(Options{DataRoot: root})
	if err != nil || dry.MergedGameCount != 4 {
		t.Fatal(err, dry)
	}
	if !reflect.DeepEqual(before, mameTree(t, root)) {
		t.Fatal("preview changed catalog")
	}
	applied, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || applied.AppliedGroupCount != dry.CandidateGroupCount || len(applied.Backups) != 3 {
		t.Fatal(err, applied)
	}
	bindings := readMAMEBindings(t, root, old.Snapshot)
	if len(bindings) != 5 || bindings["a"].Game != bindings["b"].Game || bindings["a"].Game == bindings["c"].Game || bindings["d"].Game != bindings["e"].Game {
		t.Fatal("wrong hardware grouping", bindings)
	}
	for id, b := range bindings {
		prev := old.Bindings[id]
		prev.Game = b.Game
		if prev != b {
			t.Fatal("changed set identity", id)
		}
		game := filepath.Join(root, "platforms", "Arcade", b.Game)
		contents, err := os.ReadFile(filepath.Join(game, "game.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), "retained "+id) || !strings.Contains(string(contents), "local note") {
			t.Fatal("lost metadata", id)
		}
		media, err := os.ReadFile(filepath.Join(game, "media", id, "logo.png"))
		if err != nil || string(media) != "media "+id {
			t.Fatal("lost media", id, err)
		}
		if id != "c" { // The separate CPS2 card is already canonical and untouched.
			if _, err := os.Stat(filepath.Join(applied.Backups[1], "original", "platforms", "Arcade", old.Bindings[id].Game, "game.yaml")); err != nil {
				t.Fatal("missing original", err)
			}
		}
	}
	// The fixture's ordinary cards lack platform descriptors, so validate the
	// MAME-only case below; repeat here checks hybrid/provider independence.
	repeat, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || repeat.CandidateGroupCount != 0 || len(repeat.Backups) > 0 {
		t.Fatal("repeat was not a no-op", err, repeat)
	}
}

func TestFreshMAMEImportFoldIsNoOp(t *testing.T) {
	root, _ := mameFixture(t, true, nil)
	before := mameTree(t, root)
	r, err := Fold(Options{DataRoot: root, Platform: "Arcade", Apply: true})
	if err != nil || r.CandidateGroupCount != 0 || !reflect.DeepEqual(before, mameTree(t, root)) {
		t.Fatal(err, r)
	}
	if issues, err := catalog.Validate(root); err != nil || len(issues) > 0 {
		t.Fatal(err, issues)
	}
}

func TestMAMEFoldPartialImportMatchAndModes(t *testing.T) {
	root, old := mameFixture(t, false, []string{"a", "b", "d", "e"})
	betaPath := filepath.Join(root, "platforms", "Arcade", old.Bindings["d"].Game)
	betaBefore := mameTree(t, betaPath)
	r, err := Fold(Options{DataRoot: root, Platform: "Arcade", Match: "Alpha", Apply: true})
	if err != nil || r.MergedGameCount != 1 {
		t.Fatal(err, r)
	}
	bindings := readMAMEBindings(t, root, old.Snapshot)
	if len(bindings) != 4 || bindings["a"].Game != bindings["b"].Game || bindings["d"] != old.Bindings["d"] || bindings["e"] != old.Bindings["e"] || !reflect.DeepEqual(betaBefore, mameTree(t, betaPath)) {
		t.Fatal("filter changed unselected sets", bindings)
	}
	if issues, err := catalog.Validate(root); err != nil || len(issues) > 0 {
		t.Fatal(err, issues)
	}
	before := mameTree(t, root)
	r, err = Fold(Options{DataRoot: root, EditionsOnly: true, Apply: true})
	if err != nil || r.CandidateGroupCount != 0 {
		t.Fatal(err, r)
	}
	r, err = Fold(Options{DataRoot: root, Review: func(ReviewPrompt) (ReviewDecision, error) {
		t.Fatal("MAME entered provider review")
		return ReviewApply, nil
	}})
	if err != nil || r.SkippedGroupCount == 0 || !strings.Contains(r.Groups[0].SkipReason, "MAME") {
		t.Fatal(err, r)
	}
	if !reflect.DeepEqual(before, mameTree(t, root)) {
		t.Fatal("guarded review changed data")
	}
}

func TestMAMEFoldMediaConflictHoldsBindings(t *testing.T) {
	root, old := mameFixture(t, false, nil)
	for _, id := range []string{"a", "b"} {
		p := filepath.Join(root, "platforms", "Arcade", old.Bindings[id].Game, "media", "cover.png")
		if err := os.WriteFile(p, []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
	}
	before := mameTree(t, root)
	r, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || r.SkippedGroupCount == 0 || r.AppliedGroupCount != 0 {
		t.Fatal(err, r)
	}
	if !reflect.DeepEqual(before, mameTree(t, root)) {
		t.Fatal("conflict changed catalog")
	}
}

func TestMAMEFoldRenameUpdatesBinding(t *testing.T) {
	root, old := mameFixture(t, false, []string{"a"})
	_, fresh := mameFixture(t, true, []string{"a"})
	original := old.Bindings["a"].Game
	platform := filepath.Join(root, "platforms", "Arcade")
	if err := os.Rename(filepath.Join(platform, original), filepath.Join(platform, "Legacy title")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "bindings", "romsets", "mame", old.Snapshot, "catalog.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var binding map[string]any
	if err := yaml.Unmarshal(b, &binding); err != nil {
		t.Fatal(err)
	}
	binding["sets"].(map[string]any)["a"].(map[string]any)["game"] = "Legacy title"
	b, err = yaml.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Fold(Options{DataRoot: root, Platform: "Arcade", Match: "Legacy", Apply: true})
	if err != nil || r.RenamedGameCount != 1 || r.AppliedGroupCount != 1 || r.MergedGameCount != 0 {
		t.Fatal(err, r)
	}
	if current := readMAMEBindings(t, root, old.Snapshot); current["a"].Game != fresh.Bindings["a"].Game {
		t.Fatal("rename lost canonical target", current)
	}
	if _, err := os.Stat(filepath.Join(platform, "Legacy title")); !os.IsNotExist(err) {
		t.Fatal("old folder remains", err)
	}
	if issues, err := catalog.Validate(root); err != nil || len(issues) > 0 {
		t.Fatal(err, issues)
	}
}
