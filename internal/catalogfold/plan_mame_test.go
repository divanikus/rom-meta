package catalogfold

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalogfs"
	"strings"
	"testing"
)

func TestVisualMAMEPlanAppliesSelectedGroup(t *testing.T) {
	root, old := mameFixture(t, false, nil)
	beforeBeta := mameTree(t, filepath.Join(root, "platforms", "Arcade", old.Bindings["d"].Game))
	plan, err := BuildPlan(Options{DataRoot: root, Platform: "Arcade"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	selected := -1
	for i, group := range plan.Groups {
		if strings.Contains(group.Group.Keep.Title, "Alpha") && len(group.Group.Merge) > 0 {
			selected = i
		}
	}
	if selected < 0 {
		t.Fatal(plan.Groups)
	}
	group := plan.Groups[selected]
	if group.Status != "ready" || group.Result == nil {
		t.Fatal(group)
	}
	expected := group.Result.YAML
	result, err := plan.Apply(context.Background(), []Decision{{ID: group.ID, Revision: group.Revision, Action: "merge"}})
	if err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
	bindings := readMAMEBindings(t, root, old.Snapshot)
	if bindings["a"].Game != bindings["b"].Game || bindings["d"] != old.Bindings["d"] || bindings["e"] != old.Bindings["e"] {
		t.Fatal(bindings)
	}
	data, err := catalogfs.ReadFile(filepath.Join(root, "platforms", "Arcade", group.Group.TargetFolder, "game.yaml"))
	if err != nil || string(data) != expected {
		t.Fatal("MAME preview changed", err)
	}
	if !reflect.DeepEqual(beforeBeta, mameTree(t, filepath.Join(root, "platforms", "Arcade", old.Bindings["d"].Game))) {
		t.Fatal("unselected MAME card changed")
	}
}
func TestVisualMAMEConflictDoesNotBlockIndependentGroup(t *testing.T) {
	root, old := mameFixture(t, false, nil)
	for _, id := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(root, "platforms", "Arcade", old.Bindings[id].Game, "media", "cover.png"), []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
	}
	beforeAlpha := mameTree(t, filepath.Join(root, "platforms", "Arcade", old.Bindings["a"].Game))
	plan, err := BuildPlan(Options{DataRoot: root, Platform: "Arcade"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	var decisions []Decision
	blocked := 0
	for _, pg := range plan.Groups {
		if pg.Group.NativeDigest == "" {
			t.Fatal("MAME card entered provider folding", pg)
		}
		if pg.Status == "blocked" {
			blocked++
		}
		if strings.Contains(pg.Group.Keep.Title, "Beta") && len(pg.Group.Merge) > 0 {
			if pg.Status != "ready" {
				t.Fatal(pg)
			}
			decisions = append(decisions, Decision{ID: pg.ID, Revision: pg.Revision, Action: "merge"})
		}
	}
	if blocked != 1 || len(decisions) != 1 {
		t.Fatal(plan.Groups)
	}
	if result, err := plan.Apply(context.Background(), decisions); err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
	if !reflect.DeepEqual(beforeAlpha, mameTree(t, filepath.Join(root, "platforms", "Arcade", old.Bindings["a"].Game))) {
		t.Fatal("conflicting group changed")
	}
	bindings := readMAMEBindings(t, root, old.Snapshot)
	if bindings["a"] != old.Bindings["a"] || bindings["b"] != old.Bindings["b"] || bindings["d"].Game != bindings["e"].Game {
		t.Fatal(bindings)
	}
}
func TestInspectRetainsResolvedDraft(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	plan, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	pg, err := plan.Preview(0, Draft{Primary: "Two", Fields: map[string]any{"description": "Reviewed result"}})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := plan.Inspect(0)
	if err != nil || pg.Revision != reopened.Revision || reopened.Group.Keep.Folder != "Two" || !strings.Contains(reopened.Result.YAML, "Reviewed result") {
		t.Fatal(reopened, err)
	}
}
