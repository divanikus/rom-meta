package catalogbackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, root, id string) string {
	t.Helper()
	path := filepath.Join(root, "backups", id, "Game")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "game.yaml"), []byte("title: Orbit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestListAndDeleteReviewedBackup(t *testing.T) {
	root := t.TempDir()
	id := "Console.fold-backup-123"
	path := fixture(t, root, id)
	other := fixture(t, root, "Other.fold-backup-456")
	current := filepath.Join(root, "platforms", "Console", "Game")
	os.MkdirAll(current, 0755)
	if err := os.Link(filepath.Join(path, "game.yaml"), filepath.Join(current, "game.yaml")); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root)
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	entry, err := Inspect(root, id)
	if err != nil || entry.Bytes != 13 || entry.Files != 1 {
		t.Fatal(entry, err)
	}
	if err := Delete(root, id, "stale"); err == nil {
		t.Fatal("accepted stale revision")
	}
	if err := Delete(root, id, entry.Revision); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(current, "game.yaml"), filepath.Join(other, "game.yaml")} {
		if b, e := os.ReadFile(p); e != nil || string(b) != "title: Orbit\n" {
			t.Fatal(p, e)
		}
	}
	entries, err = List(root)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}
func TestChangedOrUnsafeBackupCannotBeDeleted(t *testing.T) {
	root := t.TempDir()
	id := "Console.fold-backup-123"
	p := fixture(t, root, id)
	entry, _ := Inspect(root, id)
	os.WriteFile(filepath.Join(p, "extra"), []byte("changed"), 0644)
	if err := Delete(root, id, entry.Revision); err == nil {
		t.Fatal("deleted changed backup")
	}
	for _, id := range []string{"../platforms", "../../outside", "/absolute", "C:\\file", "custom", "..\\bad", "Console.fold-backup-123/.."} {
		if err := Delete(root, id, "x"); err == nil {
			t.Fatal(id)
		}
	}
	entries, err := List(t.TempDir())
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
func TestBackupLinksAreRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	id := "Console.fold-backup-123"
	p := fixture(t, root, id)
	if err := os.Symlink(outside, filepath.Join(p, "linked")); err != nil {
		t.Skip(err)
	}
	entries, err := List(root)
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Error, "link") {
		t.Fatal(entries, err)
	}
	if err := Delete(root, id, entries[0].Revision); err == nil {
		t.Fatal("deleted linked tree")
	}
}
