package catalogstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/taskprogress"
)

func TestPrunePreservesIgnoredBIOSWithUserData(t *testing.T) {
	for _, extra := range []string{"media/box-front.manual.jpg", "bios.bin", "notes.txt"} {
		t.Run(extra, func(t *testing.T) {
			root := t.TempDir()
			folder := "[BIOS] Example"
			writeGame(t, root, "Platform", folder, "title: BIOS\n")
			path := filepath.Join(root, "platforms", "Platform", folder, filepath.FromSlash(extra))
			writeFile(t, path, "keep")
			result, err := PruneEmpty(PruneOptions{DataRoot: root, Apply: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.DeletedGameCount != 0 {
				t.Fatalf("deleted BIOS containing %s", extra)
			}
			mustExist(t, path)
		})
	}
}

func TestPrunePreservesIgnoredBIOSMetadata(t *testing.T) {
	for _, metadata := range []string{
		"sources:\n  launchbox:\n    matches:\n      - database_id: 1\n",
		"custom_notes: preserve me\n",
	} {
		root := t.TempDir()
		writeGame(t, root, "Platform", "[BIOS] Example", "title: BIOS\n"+metadata)
		result, err := PruneEmpty(PruneOptions{DataRoot: root, Apply: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.DeletedGameCount != 0 {
			t.Fatal("deleted BIOS containing metadata")
		}
	}
}

func TestPrunePreservesUnmatchedGameWithExtraFiles(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Platform", "Example", "title: Example\n")
	path := filepath.Join(root, "platforms", "Platform", "Example", "notes.txt")
	writeFile(t, path, "keep")
	result, err := PruneEmpty(PruneOptions{DataRoot: root, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedGameCount != 0 {
		t.Fatal("deleted unmatched game containing user files")
	}
	mustExist(t, path)
}

func TestPruneRechecksBeforeDeletion(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		root := t.TempDir()
		folder := "Example"
		if ignored {
			folder = "[BIOS] Example"
		}
		writeGame(t, root, "Platform", folder, "title: Example\n")
		path := filepath.Join(root, "platforms", "Platform", folder, "notes.txt")
		result, err := PruneEmpty(PruneOptions{
			DataRoot: root, Apply: true,
			Progress: func(snapshot taskprogress.Snapshot) {
				if snapshot.Stage == "deleting" {
					writeFile(t, path, "added after scan")
				}
			},
		})
		if err == nil || !strings.Contains(err.Error(), "refusing to delete") {
			t.Fatalf("expected changed-folder error, got %v", err)
		}
		if result.DeletedGameCount != 0 {
			t.Fatal("deleted changed folder")
		}
		mustExist(t, path)
	}
}

func TestPrunePreservesManualYAMLFields(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Platform", "Example", "title: Example\nmanual:\n  description: curated\n")
	result, err := PruneEmpty(PruneOptions{DataRoot: root, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedGameCount != 0 {
		t.Fatal("deleted manual YAML metadata")
	}
}

func TestPruneRejectsPlatformSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "platforms"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "platforms", "Platform")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeFile(t, filepath.Join(outside, "Example", "game.yaml"), "title: Example\n")
	if err := ensureGamePath(filepath.Join(root, "platforms"), filepath.Join(path, "Example")); err == nil {
		t.Fatal("allowed deletion through external platform symlink")
	}
	mustExist(t, filepath.Join(outside, "Example", "game.yaml"))
}
