package catalogchanges

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBatchCoalescesAndPublishesAfterRefresh(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "platforms", "Example")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "_platform.yaml"), []byte("name: Example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	b := Begin(func(r, p string) error {
		calls++
		if r != root || p != "Example" {
			t.Fatalf("wrong scope %s %s", r, p)
		}
		if _, err := os.Stat(filepath.Join(root, RevisionFile)); !os.IsNotExist(err) {
			t.Fatal("revision published before refresh")
		}
		return nil
	})
	defer b.Finish()
	for _, name := range []string{"game.yaml", "fingerprints.yaml", "media/cover.png"} {
		if err := Touch(filepath.Join(p, "Game", name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Finish(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	token, err := os.ReadFile(filepath.Join(root, RevisionFile))
	if err != nil || len(token) != 33 {
		t.Fatal(string(token), err)
	}
	if err = b.Finish(); err != nil || calls != 1 {
		t.Fatal("finish not idempotent", err)
	}
}
func TestNoWritesAndPrivateStagingDoNotPublish(t *testing.T) {
	root := t.TempDir()
	calls := 0
	b := Begin(func(string, string) error { calls++; return nil })
	defer b.Finish()
	if err := Touch(filepath.Join(root, ".rommeta", "fold", "platforms", "Example", "Game", "game.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := b.Finish(); err != nil || calls != 0 {
		t.Fatal(calls, err)
	}
	if _, err := os.Stat(filepath.Join(root, RevisionFile)); !os.IsNotExist(err) {
		t.Fatal("read/no-op created revision")
	}
}
func TestRefreshFailureStillSignalsCommittedWrites(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "platforms", "Example")
	os.MkdirAll(p, 0700)
	os.WriteFile(filepath.Join(p, "_platform.yaml"), []byte("name: Example"), 0600)
	b := Begin(func(string, string) error { return errors.New("cache not writable") })
	defer b.Finish()
	if err := Touch(filepath.Join(p, "Game", "game.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := b.Finish(); err == nil {
		t.Fatal("cache failure hidden")
	}
	if _, err := os.Stat(filepath.Join(root, RevisionFile)); err != nil {
		t.Fatal("payload change not signalled", err)
	}
}
