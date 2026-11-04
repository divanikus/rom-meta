package catalogfold

import (
	"context"
	"os"
	"path/filepath"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"strings"
	"testing"
)

func webPlanFixture(t *testing.T, root string) {
	t.Helper()
	writeFoldGame(t, root, "Console", "One", "Orbit (USA)", "Orbit", "10")
	writeFoldGame(t, root, "Console", "Two", "Orbit (Japan)", "Orbit", "10")
}
func TestPlanPreservesFieldsAndAppliesOnlySelected(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			webPlanFixture(t, root)
			for _, folder := range []string{"One", "Two"} {
				path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
				writeFoldFile(t, path, readFoldFile(t, path)+"\ndescription: "+folder+" description\ncustom:\n  "+strings.ToLower(folder)+": keep\n")
			}
			writeFoldGame(t, root, "Console", "Unrelated", "Other (USA)", "Other", "11")
			if packed {
				catalogfstest.Pack(t, filepath.Join(root, "platforms", "Console"))
			}
			before, err := catalogfs.ReadFile(filepath.Join(root, "platforms", "Console", "Unrelated", "game.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			if len(plan.Groups) != 1 || plan.Groups[0].Status != "review" {
				t.Fatalf("%+v", plan.Groups)
			}
			pg, err := plan.Preview(0, Draft{Primary: "Two", Fields: catalogedit.Patch{"description": "Chosen description", "title": "Chosen title"}})
			if err != nil || pg.Status != "ready" {
				t.Fatal(pg, err)
			}
			expected := pg.Result.YAML
			result, err := plan.Apply(context.Background(), []Decision{{ID: 0, Revision: pg.Revision, Action: "merge"}})
			if err != nil || result.Applied != 1 {
				t.Fatal(result, err)
			}
			after, err := catalogfs.ReadFile(filepath.Join(root, "platforms", "Console", "Two", "game.yaml"))
			if err != nil || string(after) != expected {
				t.Fatalf("preview differs: %s / %v", after, err)
			}
			for _, wanted := range []string{"one: keep", "two: keep", "Chosen description", "Orbit (USA)", "Orbit (Japan)"} {
				if !strings.Contains(string(after), wanted) {
					t.Fatal(wanted, string(after))
				}
			}
			untouched, err := catalogfs.ReadFile(filepath.Join(root, "platforms", "Console", "Unrelated", "game.yaml"))
			if err != nil || string(untouched) != string(before) {
				t.Fatal("unselected card changed", err)
			}
		})
	}
}
func TestPlanStaleAndUnknownConflictsBlockWrites(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	plan, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if len(plan.Groups) != 1 || plan.Groups[0].Status != "ready" {
		t.Fatal(plan.Groups)
	}
	path := filepath.Join(root, "platforms", "Console", "Two", "game.yaml")
	writeFoldFile(t, path, readFoldFile(t, path)+"\n# external edit\n")
	if _, err := plan.Apply(context.Background(), []Decision{{ID: 0, Revision: 1, Action: "merge"}}); err == nil {
		t.Fatal("stale plan applied")
	}
	if _, err := os.Stat(filepath.Join(root, "backups")); !os.IsNotExist(err) {
		t.Fatal("stale apply staged a backup")
	}
	for _, folder := range []string{"One", "Two"} {
		path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
		writeFoldFile(t, path, readFoldFile(t, path)+"\ncustom: "+folder+"\n")
	}
	next, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.Groups[0].Status != "blocked" {
		t.Fatal(next.Groups)
	}
}
func TestKeepSeparateAndManualIdentityReview(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	plan, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	result, err := plan.Apply(context.Background(), []Decision{{ID: 0, Revision: 1, Action: "separate"}})
	if err != nil || result.KeptSeparate != 1 {
		t.Fatal(result, err)
	}
	next, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil || len(next.Groups) != 0 {
		t.Fatal(next, err)
	}
	next.Close()
	manual, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, []string{"One", "Two"})
	if err != nil {
		t.Fatal(err)
	}
	defer manual.Close()
	if manual.Groups[0].Status != "review" {
		t.Fatal(manual.Groups)
	}
	if _, err := manual.Preview(0, Draft{Confirm: true}); err == nil {
		t.Fatal("missing identity reason accepted")
	}
	pg, err := manual.Preview(0, Draft{Confirm: true, Reason: "Reviewed same release family"})
	if err != nil || pg.Status != "ready" {
		t.Fatal(pg, err)
	}
}
func TestReviewedUnknownSourceFieldsSurvive(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	path := filepath.Join(root, "platforms", "Console", "Two", "game.yaml")
	body := strings.Replace(readFoldFile(t, path), "  launchbox:\n", "  launchbox:\n    private_note: preserve me\n", 1)
	writeFoldFile(t, path, body)
	r, err := Fold(Options{DataRoot: root, Platform: "Console", Apply: true})
	if err != nil || r.AppliedGroupCount != 1 {
		t.Fatal(r, err)
	}
	text := readFoldFile(t, filepath.Join(root, "platforms", "Console", r.Groups[0].Keep.Folder, "game.yaml"))
	if !strings.Contains(text, "private_note: preserve me") {
		t.Fatal(text)
	}
}

func TestPlanDoesNotHideCatalogScanErrorsOrCancellation(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildPlan(Options{DataRoot: root, Platform: "Console", Context: ctx}, nil); err == nil {
		t.Fatal("cancelled plan succeeded")
	}
	writeFoldFile(t, filepath.Join(root, "platforms", "Console", "One", "game.yaml"), "title: [unterminated\n")
	if _, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil); err == nil {
		t.Fatal("invalid catalog metadata silently omitted")
	}
}
