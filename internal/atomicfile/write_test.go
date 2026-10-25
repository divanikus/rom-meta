package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFilePreservesHardlinkedOriginal(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original.yaml")
	staged := filepath.Join(root, "stage.yaml")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, staged); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	if err := WriteFile(staged, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{original: "original", staged: "updated"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v", path, data, err)
		}
	}
}

func TestWriteFileCleansTemporaryAfterFailedRename(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "occupied")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(path, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("replacement"), 0o644); err == nil {
		t.Fatal("expected rename failure")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "keep" {
		t.Fatalf("original changed: %q, %v", data, err)
	}
}

func TestWriteFileCreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "game.yaml")
	for _, value := range []string{"first", "second"} {
		if err := WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != value {
			t.Fatalf("contents=%q, error=%v", data, err)
		}
	}
	if strings.HasPrefix(TempPattern, ".") || !strings.HasSuffix(TempPattern, ".tmp") {
		t.Fatalf("unsafe temporary pattern %q", TempPattern)
	}
}
