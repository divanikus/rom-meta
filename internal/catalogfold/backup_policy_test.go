package catalogfold

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFoldBackupRequiresOptIn(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "keep"}[keep], func(t *testing.T) {
			root := t.TempDir()
			webPlanFixture(t, root)
			result, err := Fold(Options{DataRoot: root, Apply: true, KeepBackup: keep})
			if err != nil || result.AppliedGroupCount != 1 {
				t.Fatal(result, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "backups"))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if keep {
				want = 1
			}
			if len(entries) != want || len(result.Backups) != want {
				t.Fatal(result, entries)
			}
		})
	}
}
func TestPlanDefaultDiscardsOriginalAfterActivation(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	result, err := p.Apply(context.Background(), []Decision{{ID: 0, Revision: p.Groups[0].Revision, Action: "merge"}})
	if err != nil || result.Applied != 1 || len(result.Backups) != 0 {
		t.Fatal(result, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "backups"))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
func TestMAMEFoldDefaultDiscardsOriginalAfterActivation(t *testing.T) {
	root, old := mameFixture(t, false, nil)
	result, err := Fold(Options{DataRoot: root, Platform: "Arcade", Apply: true})
	if err != nil || result.AppliedGroupCount == 0 || len(result.Backups) != 0 {
		t.Fatal(result, err)
	}
	bindings := readMAMEBindings(t, root, old.Snapshot)
	if bindings["a"].Game != bindings["b"].Game {
		t.Fatal("binding not activated")
	}
	entries, err := os.ReadDir(filepath.Join(root, "backups"))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
