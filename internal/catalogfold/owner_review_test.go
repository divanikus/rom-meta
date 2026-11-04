package catalogfold

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogedit"
	"rommeta/internal/dat"
)

func TestWebOwnerConfirmsSubsetAndBatchesReadyGroups(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	writeFoldGame(t, root, "Console", "Three", "Orbit (Europe) (Special Edition)", "Orbit", "10")
	writeFoldGame(t, root, "Console", "GoodOne", "Garden (USA)", "Garden", "11")
	writeFoldGame(t, root, "Console", "GoodTwo", "Garden (Japan)", "Garden", "11")
	for _, folder := range []string{"One", "Two"} {
		path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
		id := "20"
		if folder == "Two" {
			id = "21"
		}
		writeFoldFile(t, path, readFoldFile(t, path)+"  igdb:\n    matches:\n      - igdb_id: "+id+"\n        platforms: [Console]\n")
		writeFoldFile(t, filepath.Join(root, "platforms", "Console", folder, "media", "cover.jpg"), folder+" artwork")
	}
	unchanged := readFoldFile(t, filepath.Join(root, "platforms", "Console", "Three", "game.yaml"))
	ordinary, err := Fold(Options{DataRoot: root, Platform: "Console"})
	if err != nil || ordinary.HeldGroupCount == 0 {
		t.Fatal("ordinary identity guard changed", ordinary, err)
	}
	p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var pg, good PlannedGroup
	for _, g := range p.Groups {
		if g.Group.Keep.Title == "Garden" {
			good = g
		} else {
			pg = g
		}
	}
	if good.Status != "ready" || pg.Status != "review" || !pg.NeedsIdentity || !strings.Contains(pg.IdentityReason, "content") {
		t.Fatal(pg, good)
	}
	if _, err = p.Apply(context.Background(), []Decision{{ID: pg.ID, Revision: pg.Revision, Action: "merge"}}); err == nil {
		t.Fatal("unconfirmed group applied")
	}
	for _, folders := range [][]string{{"One"}, {"One", "One"}, {"One", "Unknown"}} {
		if _, err = p.Preview(pg.ID, Draft{Folders: folders, Confirm: true}); err == nil {
			t.Fatal("invalid subset accepted", folders)
		}
	}
	pg, err = p.Preview(pg.ID, Draft{Folders: []string{"One", "Two"}, Primary: "Two", Confirm: true})
	if err != nil || pg.Status != "ready" || pg.Group.ownerReview == nil || len(pg.Candidates) != 3 || !strings.Contains(pg.IdentityReason, "conflicting igdb IDs") {
		t.Fatal(pg, err)
	}
	if len(pg.Group.ownerReview.Members) != 2 || len(pg.Group.ownerReview.Releases) != 2 {
		t.Fatal(pg.Group.ownerReview)
	}
	// Changing membership without a new explicit confirmation must revoke approval.
	pg, err = p.Preview(pg.ID, Draft{Folders: []string{"One", "Three"}})
	if err != nil || pg.Status != "review" || pg.Group.ownerReview != nil || pg.Group.ReviewAccepted {
		t.Fatal(pg, err)
	}
	pg, err = p.Preview(pg.ID, Draft{Folders: []string{"One", "Two"}, Primary: "Two", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	expected := pg.Result.YAML
	result, err := p.Apply(context.Background(), []Decision{{ID: pg.ID, Revision: pg.Revision, Action: "merge"}, {ID: good.ID, Revision: good.Revision, Action: "merge"}})
	if err != nil || result.Applied != 2 {
		t.Fatal(result, err)
	}
	mergedPath := filepath.Join(root, "platforms", "Console", "Two", "game.yaml")
	if readFoldFile(t, mergedPath) != expected {
		t.Fatal("saved bytes differ from approved preview")
	}
	doc := readContentMap(t, mergedPath)
	fold := doc["fold"].(map[string]any)
	if len(fold["owner_reviews"].(map[string]any)) != 1 || !strings.HasPrefix(fold["content_key"].(string), "reviewed.") {
		t.Fatal(fold)
	}
	if len(doc["releases"].(map[string]any)) != 2 {
		t.Fatal(doc)
	}
	for _, id := range []string{"igdb_id: 20", "igdb_id: 21"} {
		if !strings.Contains(expected, id) {
			t.Fatal("provider ID lost", id)
		}
	}
	if len(contentFiles(t, filepath.Join(root, "platforms", "Console", "Two", "media"))) != 2 {
		t.Fatal("media lost")
	}
	if readFoldFile(t, filepath.Join(root, "platforms", "Console", "Three", "game.yaml")) != unchanged {
		t.Fatal("excluded card changed")
	}
	if len(readContentMap(t, filepath.Join(root, "platforms", "Console", "Two", "fingerprints.yaml"))["items"].([]any)) != 2 {
		t.Fatal("fingerprints lost")
	}
}

func TestWebOwnerConfirmationDoesNotDiscardConflictingData(t *testing.T) {
	for _, kind := range []string{"unknown fields", "non-media files", "release payload", "editable field"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			webPlanFixture(t, root)
			for _, folder := range []string{"One", "Two"} {
				path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
				switch kind {
				case "unknown fields":
					writeFoldFile(t, path, readFoldFile(t, path)+"custom_note: "+folder+"\n")
				case "non-media files":
					writeFoldFile(t, filepath.Join(root, "platforms", "Console", folder, "notes.txt"), folder)
				case "editable field":
					writeFoldFile(t, path, readFoldFile(t, path)+"description: "+folder+"\n")
				}
			}
			if kind == "release payload" {
				for _, folder := range []string{"One", "Two"} {
					writeFoldGame(t, root, "Console", folder, "Orbit (USA)", "Orbit", "10")
					path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
					s := strings.Replace(readFoldFile(t, path), "    source: \"redump\"", "    source: \""+folder+"\"", 1)
					writeFoldFile(t, path, s)
				}
			}
			before := catalogBytes(t, root)
			p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, []string{"One", "Two"})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			pg, err := p.Preview(0, Draft{Confirm: true})
			if err != nil {
				t.Fatal(err)
			}
			want := "blocked"
			if kind == "editable field" {
				want = "review"
			}
			if pg.Status != want {
				t.Fatal(pg)
			}
			if _, err = p.Apply(context.Background(), []Decision{{ID: 0, Revision: pg.Revision, Action: "merge"}}); err == nil {
				t.Fatal("unresolved data conflict applied")
			}
			if !reflect.DeepEqual(before, catalogBytes(t, root)) {
				t.Fatal("rejected merge changed data")
			}
			if kind == "editable field" {
				pg, err = p.Preview(0, Draft{Confirm: true, Fields: catalogedit.Patch{"description": "Chosen description"}})
				if err != nil || pg.Status != "ready" {
					t.Fatal(pg, err)
				}
			}
		})
	}
}
func TestWebOwnerReviewSurvivesKnownImportAndRejectsStalePlan(t *testing.T) {
	root := t.TempDir()
	names := []string{"Orbit (USA) (Special Edition)", "Tsuki (Japan) (Kanzenban)"}
	path := contentDAT(t, names...)
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	if _, err := dat.Import(path, options); err != nil {
		t.Fatal(err)
	}
	platform := "Sony - PlayStation 2"
	dir := filepath.Join(root, "platforms", platform)
	entries, _ := os.ReadDir(dir)
	var folders []string
	for _, e := range entries {
		if e.IsDir() {
			folders = append(folders, e.Name())
		}
	}
	p, err := BuildPlan(Options{DataRoot: root, Platform: platform}, folders)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pg, err := p.Preview(0, Draft{Confirm: true})
	if err != nil || pg.Status != "ready" {
		t.Fatal(pg, err)
	}
	keep := filepath.Join(dir, pg.Group.Keep.Folder, "game.yaml")
	original := readFoldFile(t, keep)
	writeFoldFile(t, keep, original+"\n# newer edit\n")
	if _, err = p.Apply(context.Background(), []Decision{{ID: 0, Revision: pg.Revision, Action: "merge"}}); err == nil {
		t.Fatal("stale owner decision applied")
	}
	writeFoldFile(t, keep, original)
	p, err = BuildPlan(Options{DataRoot: root, Platform: platform}, folders)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pg, err = p.Preview(0, Draft{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Apply(context.Background(), []Decision{{ID: 0, Revision: pg.Revision, Action: "merge"}}); err != nil {
		t.Fatal(err)
	}
	before := readContentMap(t, keep)
	if _, err = dat.Import(path, options); err != nil {
		t.Fatal(err)
	}
	after := readContentMap(t, keep)
	if !reflect.DeepEqual(before["fold"], after["fold"]) || !reflect.DeepEqual(before["releases"], after["releases"]) {
		t.Fatal("known import lost owner review or releases")
	}
}
