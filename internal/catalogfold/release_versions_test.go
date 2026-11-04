package catalogfold

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/dat"
	"strings"
	"testing"
)

func TestAutomaticReleaseVersionFoldPreservesDistinctPayloads(t *testing.T) {
	root := t.TempDir()
	for _, v := range []struct{ folder, release, title string }{
		{"One", "Orbit (USA)", "Orbit"},
		{"Two", "Orbit (USA) (RE-1)", "Orbit (RE-1)"},
		{"Three", "Orbit (Europe) (Alt 2)", "Orbit (Alt 2)"},
	} {
		writeFoldGame(t, root, "Console", v.folder, v.release, v.title, "10")
		writeFoldFile(t, filepath.Join(root, "platforms", "Console", v.folder, "media", "cover.jpg"), v.folder+" art")
	}
	p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.Groups) != 1 || p.Groups[0].Status != "ready" || p.Groups[0].NeedsIdentity {
		t.Fatal(p.Groups)
	}
	pg := p.Groups[0]
	before := catalogBytes(t, root)
	if _, err := Fold(Options{DataRoot: root, Platform: "Console"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("planning changed files")
	}
	result, err := p.Apply(context.Background(), []Decision{{ID: pg.ID, Revision: pg.Revision, Action: "merge"}})
	if err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
	dir := filepath.Join(root, "platforms", "Console", pg.Group.Keep.Folder)
	doc := readContentMap(t, filepath.Join(dir, "game.yaml"))
	if len(doc["releases"].(map[string]any)) != 3 || len(readContentMap(t, filepath.Join(dir, "fingerprints.yaml"))["items"].([]any)) != 3 || len(contentFiles(t, filepath.Join(dir, "media"))) != 3 {
		t.Fatal("release payload lost")
	}
	if strings.Contains(readFoldFile(t, filepath.Join(dir, "game.yaml")), "owner_reviews:") {
		t.Fatal("automatic fold invented owner decision")
	}
}

func TestReleaseVersionsStillNeedIndependentIdentity(t *testing.T) {
	for _, kind := range []string{"different titles", "different IDs", "edition", "reviewed keys"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			writeFoldGame(t, root, "Console", "One", "Orbit (USA)", "Orbit", "10")
			title, release := "Orbit (RE)", "Orbit (Japan) (RE)"
			if kind == "different titles" {
				title = "Other (RE)"
				release = "Other (Japan) (RE)"
			}
			if kind == "edition" {
				title = "Orbit (Special Edition) (RE)"
				release = "Orbit (Japan) (Special Edition) (RE)"
			}
			writeFoldGame(t, root, "Console", "Two", release, title, "10")
			if kind == "different IDs" {
				for _, folder := range []string{"One", "Two"} {
					path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
					writeFoldFile(t, path, readFoldFile(t, path)+"  igdb:\n    matches:\n      - igdb_id: "+map[string]string{"One": "20", "Two": "21"}[folder]+"\n        platforms: [Console]\n")
				}
			}
			if kind == "reviewed keys" {
				for _, folder := range []string{"One", "Two"} {
					path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
					writeFoldFile(t, path, readFoldFile(t, path)+"fold:\n  content_key: reviewed."+folder+"\n")
				}
			}
			r, err := Fold(Options{DataRoot: root, Platform: "Console"})
			if err != nil || r.HeldGroupCount != 1 {
				t.Fatal(r, err)
			}
			p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if len(p.Groups) != 1 || p.Groups[0].Status != "review" || !p.Groups[0].NeedsIdentity {
				t.Fatal(p.Groups)
			}
		})
	}
}

func TestReleaseVersionsImportTogetherAndRepeatPreservesPayloads(t *testing.T) {
	root := t.TempDir()
	names := []string{"Orbit (USA)", "Orbit (USA) (RE)", "Orbit (Japan) (Alt 2)", "Orbit (Europe) (Rerelease)"}
	path := contentDAT(t, names...)
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	r, err := dat.Import(path, options)
	if err != nil || r.GameBundleCount != 1 {
		t.Fatal(r, err)
	}
	dir := filepath.Join(root, "platforms", "Sony - PlayStation 2")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var card string
	for _, e := range entries {
		if e.IsDir() {
			card = filepath.Join(dir, e.Name())
			break
		}
	}
	beforeGame := readContentMap(t, filepath.Join(card, "game.yaml"))
	beforeFP := readContentMap(t, filepath.Join(card, "fingerprints.yaml"))
	if len(beforeGame["releases"].(map[string]any)) != len(names) || len(beforeFP["items"].([]any)) != len(names) {
		t.Fatal("fresh import lost a revision")
	}
	if _, err := dat.Import(path, options); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeGame, readContentMap(t, filepath.Join(card, "game.yaml"))) || !reflect.DeepEqual(beforeFP, readContentMap(t, filepath.Join(card, "fingerprints.yaml"))) {
		t.Fatal("repeat changed payloads")
	}
}
