package arcaderepair

import (
	"archive/zip"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalog"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/dat"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, string, dat.ArcadeResult) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "catalog")
	out := filepath.Join(dir, "review")
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "classes.zip")
	zipFile := func(path string, files map[string]string) {
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		z := zip.NewWriter(f)
		for n, s := range files {
			w, e := z.Create(n)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = w.Write([]byte(s)); e != nil {
				t.Fatal(e)
			}
		}
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
	}
	zipFile(input, map[string]string{"mame.xml": `<mame build="0.289 (fixture)"><machine name="a" sourcefile="capcom/cps1.cpp"><description>Game (World)</description><rom name="a.rom" size="1" crc="12345678"/></machine><machine name="b" cloneof="a" sourcefile="capcom/cps1.cpp"><description>Game (Japan)</description><rom name="b.rom" size="1" crc="23456789"/></machine><machine name="c" sourcefile="capcom/cps1.cpp"><description>Untouched</description><rom name="c.rom" size="1" crc="34567890"/></machine><machine name="home"><description>Computer</description></machine></mame>`})
	zipFile(classes, map[string]string{"mame.ini": ";; MAME.ini 0.289\n[ROOT_FOLDER]\na\nb\nc\n", "mess.ini": ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n", "category.ini": ";; CATEGORY.ini 0.289\n[Shooter / Misc.]\na\nb\nc\n[Computer / Home]\nhome\n"})
	r, e := dat.ImportArcade(input, dat.ArcadeOptions{ImportOptions: dat.ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: false})
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b"} {
		path := filepath.Join(root, "platforms", "Arcade", r.Bindings[id].Game, "game.yaml")
		d, e := readDocument(path)
		if e != nil {
			t.Fatal(e)
		}
		var extra *catalogyaml.Document
		text := fmt.Sprintf("sources:\n  launchbox:\n    matches:\n      - release: %s\n        database_id: '42'\n        description: Keep me exactly\n        custom: {future_field: preserved}\n  igdb:\n    status: not_found\n    checked_at: '2026-09-27T10:00:00Z'\ncuration:\n  note: Keep this\n", id)
		if id == "b" {
			text = fmt.Sprintf("sources:\n  launchbox:\n    matches:\n      - release: %s\n        database_id: '43'\n        description: Another result\n  igdb:\n    matches:\n      - release: %s\n        game_id: '99'\ncuration:\n  note: Keep this\n", id, id)
		}
		extra, e = catalogyaml.Parse([]byte(text))
		if e != nil {
			t.Fatal(e)
		}
		if e = mergeNode(d.Root(), extra.Root(), "game"); e != nil {
			t.Fatal(e)
		}
		bytes, e := d.Marshal()
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, bytes, 0644); e != nil {
			t.Fatal(e)
		}
		if e = writeNew(filepath.Join(filepath.Dir(path), "media", id, "logo.png"), []byte("unique media "+id)); e != nil {
			t.Fatal(e)
		}
	}
	return root, out, r
}

func TestPrepareApplyPreservesEnrichmentAndBindings(t *testing.T) {
	root, out, old := fixture(t)
	before, e := treeHashes(root)
	if e != nil {
		t.Fatal(e)
	}
	r, e := Prepare(root, out, nil)
	if e != nil || len(r.Conflicts) > 0 {
		t.Fatal(e, r.Conflicts)
	}
	if r.BeforeCards != 3 || r.AfterCards != 2 || r.Releases != 2 || r.Matches != 3 || r.Fingerprints != 2 || r.OtherFiles != 2 {
		t.Fatal(r)
	}
	readable, e := os.ReadFile(filepath.Join(out, "PREVIEW.md"))
	if e != nil || !strings.Contains(string(readable), "### Game") || !strings.Contains(string(readable), old.Bindings["b"].Game) {
		t.Fatal("missing reviewable merge", e)
	}
	after, e := treeHashes(root)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("prepare changed catalog", e)
	}
	d, e := readDocument(filepath.Join(out, "staged", "platforms", "Arcade", "Game", "game.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	sources := catalogyaml.Value(d.Root(), "sources")
	if catalogyaml.Value(catalogyaml.Value(sources, "igdb"), "status") != nil {
		t.Fatal("stale negative status alongside matches")
	}
	if h := catalogyaml.Value(catalogyaml.Value(d.Root(), "arcade_repair"), "original_cards"); h == nil || len(h.Content) != 2 {
		t.Fatal("lost scoped check history")
	}
	for _, id := range []string{"a", "b"} {
		prev, e := readDocument(filepath.Join(root, "platforms", "Arcade", old.Bindings[id].Game, "game.yaml"))
		if e != nil {
			t.Fatal(e)
		}
		if !equalNode(catalogyaml.Value(catalogyaml.Value(prev.Root(), "releases"), id), catalogyaml.Value(catalogyaml.Value(d.Root(), "releases"), id)) {
			t.Fatal("release changed", id)
		}
	}
	backup, e := Apply(root, filepath.Join(out, "review.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range r.Groups {
		for _, from := range g.Sources {
			if e = checkTree(filepath.Join(backup, "original", "platforms", "Arcade", from), g.Before[from]); e != nil {
				t.Fatal(e)
			}
		}
		if e = checkTree(filepath.Join(root, "platforms", "Arcade", g.Target), g.After); e != nil {
			t.Fatal(e)
		}
	}
	if issues, e := catalog.Validate(root); e != nil || len(issues) > 0 {
		t.Fatal(e, issues)
	}
	if _, e = Apply(root, filepath.Join(out, "review.json")); e == nil {
		t.Fatal("stale apply accepted")
	}
	if _, e = Prepare(root, filepath.Join(filepath.Dir(out), "repeat"), nil); e != nil {
		t.Fatal("repeat", e)
	}
}

func TestRepairRejectsChangesAndConflicts(t *testing.T) {
	for _, mode := range []string{"input", "output", "extra-file", "media-conflict", "local-field-conflict"} {
		t.Run(mode, func(t *testing.T) {
			root, out, old := fixture(t)
			if mode == "media-conflict" {
				for _, id := range []string{"a", "b"} {
					if e := writeNew(filepath.Join(root, "platforms", "Arcade", old.Bindings[id].Game, "media", "logo.png"), []byte(id)); e != nil {
						t.Fatal(e)
					}
				}
			}
			if mode == "local-field-conflict" {
				p := filepath.Join(root, "platforms", "Arcade", old.Bindings["b"].Game, "game.yaml")
				s, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(p, []byte(strings.ReplaceAll(string(s), "Keep this", "Other decision")), 0644); e != nil {
					t.Fatal(e)
				}
			}
			r, e := Prepare(root, out, nil)
			if e != nil {
				t.Fatal(e)
			}
			if strings.HasSuffix(mode, "conflict") {
				if len(r.Conflicts) == 0 {
					t.Fatal("conflict ignored")
				}
			} else {
				if len(r.Conflicts) > 0 {
					t.Fatal(r.Conflicts)
				}
				p := filepath.Join(root, "platforms", "Arcade", old.Bindings["a"].Game, "game.yaml")
				if mode == "output" {
					p = filepath.Join(out, "staged", "platforms", "Arcade", "Game", "game.yaml")
				}
				if mode == "extra-file" {
					p = filepath.Join(filepath.Dir(p), "new.txt")
				}
				if e = os.WriteFile(p, []byte("changed"), 0644); e != nil {
					t.Fatal(e)
				}
			}
			before, e := treeHashes(root)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = Apply(root, filepath.Join(out, "review.json")); e == nil {
				t.Fatal("unsafe apply accepted")
			}
			after, e := treeHashes(root)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed preflight changed catalog", e)
			}
		})
	}
}

func TestRepairRollsBackPublicationFailure(t *testing.T) {
	root, out, _ := fixture(t)
	r, e := Prepare(root, out, nil)
	if e != nil || len(r.Conflicts) > 0 {
		t.Fatal(e, r.Conflicts)
	}
	calls := 0
	backup, e := applyReview(root, filepath.Join(out, "review.json"), func(from, to string) error {
		calls++
		if calls == 3 {
			return fmt.Errorf("injected publication failure")
		}
		return os.Rename(from, to)
	})
	if e == nil || !strings.Contains(e.Error(), "rolled back") {
		t.Fatal(e)
	}
	for _, g := range r.Groups {
		for _, from := range g.Sources {
			if e = checkTree(filepath.Join(root, "platforms", "Arcade", from), g.Before[from]); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = checkFile(filepath.Join(root, "bindings", "romsets", "mame", r.Snapshot, "catalog.yaml"), r.BindingBefore); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(backup, "review.json")); e != nil {
		t.Fatal(e)
	}
}

func TestNodeMergeRejectsUnknownScalarLoss(t *testing.T) {
	a := &yaml.Node{}
	b := &yaml.Node{}
	if e := a.Encode(map[string]any{"unknown": map[string]string{"note": "a"}}); e != nil {
		t.Fatal(e)
	}
	if e := b.Encode(map[string]any{"unknown": map[string]string{"note": "b"}}); e != nil {
		t.Fatal(e)
	}
	if e := mergeNode(a, b, "game"); e == nil {
		t.Fatal("local decision overwritten")
	}
}

func TestRepairRejectsAnchoredMerge(t *testing.T) {
	for _, filename := range []string{"game.yaml", "fingerprints.yaml"} {
		t.Run(filename, func(t *testing.T) {
			root, out, old := fixture(t)
			path := filepath.Join(root, "platforms", "Arcade", old.Bindings["a"].Game, filename)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("\ncustom: &shared {note: original}\nother: *shared\n")...)
			if err = os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			r, err := Prepare(root, out, nil)
			if err != nil || len(r.Conflicts) != 1 || !strings.Contains(r.Conflicts[0], "anchors") {
				t.Fatal("unsafe anchored merge accepted", err, r.Conflicts)
			}
		})
	}
}
