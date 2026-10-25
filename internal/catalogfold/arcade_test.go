package catalogfold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGroupPreservesVersionedArcadeBindings(t *testing.T) {
	root := t.TempDir()
	platform := "Arcade - Test - Board"
	for _, folder := range []string{"A", "B"} {
		dir := filepath.Join(root, "platforms", platform, folder)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("title: Game\nreleases:\n  set"+folder+":\n    title: Game\n    mame:\n      snapshot: 0.289-test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fingerprints.yaml"), []byte("items: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	group := Group{Platform: platform, Keep: GameRef{Folder: "A"}, Merge: []GameRef{{Folder: "B"}}}
	if err := applyGroup(root, &group, regionRules{}); err != nil {
		t.Fatal(err)
	}
	if !group.Skipped || !strings.Contains(group.SkipReason, "bindings") {
		t.Fatal("unsafe post-import fold not held")
	}
	for _, folder := range []string{"A", "B"} {
		if _, err := os.Stat(filepath.Join(root, "platforms", platform, folder, "game.yaml")); err != nil {
			t.Fatal("arcade card changed")
		}
	}
}
