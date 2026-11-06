package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCLICommandPreviewAndDelete(t *testing.T) {
	root := t.TempDir()
	id := "Console.fold-backup-123"
	path := filepath.Join(root, "backups", id)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "game.yaml"), []byte("title: Orbit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := os.Args
	defer func() { os.Args = old }()
	args := []string{"rom-meta", "catalog", "backups", "--data-root", root, "--delete", id}
	os.Args = args
	if code := runCommand(); code != 0 {
		t.Fatal("preview", code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	os.Args = append(append([]string{}, args...), "--apply")
	if code := runCommand(); code != 0 {
		t.Fatal("apply", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("backup still exists", err)
	}
}
