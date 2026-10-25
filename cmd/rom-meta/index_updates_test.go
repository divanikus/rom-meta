package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"rommeta/internal/catalogchanges"
)

func TestCLIImportAutomaticallyMaintainsIndex(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "catalog")
	file := filepath.Join(root, "example.dat")
	xml := `<?xml version="1.0"?><datafile><header><name>Nintendo - Nintendo Entertainment System</name></header><game name="Example (USA)"><description>Example</description><rom name="Example.nes" size="3" crc="12345678" sha1="1111111111111111111111111111111111111111"/></game></datafile>`
	if err := os.WriteFile(file, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	saved := os.Args
	defer func() { os.Args = saved }()
	os.Args = []string{"rom-meta", "dat", "import", file, "--data-root", data}
	if code := runCommand(); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(filepath.Join(data, catalogchanges.RevisionFile)); !os.IsNotExist(err) {
		t.Fatal("preview published revision")
	}
	os.Args = append(os.Args, "--apply")
	if code := runCommand(); code != 0 {
		t.Fatal(code)
	}
	paths, err := filepath.Glob(filepath.Join(data, "platforms", "*", "_index.sqlite"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	db, err := sql.Open("sqlite", paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM lookup_bundles_v1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if count != 1 {
		t.Fatal(count)
	}
	marker, err := os.ReadFile(filepath.Join(data, catalogchanges.RevisionFile))
	if err != nil {
		t.Fatal(err)
	}
	if code := runCommand(); code != 0 {
		t.Fatal(code)
	}
	again, err := os.ReadFile(filepath.Join(data, catalogchanges.RevisionFile))
	if err != nil || string(marker) == string(again) {
		t.Fatal("second command not signalled", err)
	}
}
