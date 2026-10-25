// Package catalogfstest converts synthetic test fixtures, never live catalogs.
package catalogfstest

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"rommeta/internal/catalogfs"
)

func Pack(t *testing.T, platform string) {
	t.Helper()
	stage := t.TempDir()
	games, err := os.ReadDir(platform)
	if err != nil {
		t.Fatal(err)
	}
	for _, game := range games {
		if !game.IsDir() {
			continue
		}
		dir := filepath.Join(platform, game.Name())
		files := map[string]string{}
		err = filepath.WalkDir(dir, func(n string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, e := filepath.Rel(dir, n)
			if e != nil {
				return e
			}
			data, e := os.ReadFile(n)
			if e != nil {
				return e
			}
			saved := filepath.Join(stage, game.Name(), rel)
			if e = os.MkdirAll(filepath.Dir(saved), 0755); e != nil {
				return e
			}
			if e = os.WriteFile(saved, data, 0644); e != nil {
				return e
			}
			files[filepath.ToSlash(rel)] = saved
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err = catalogfs.PutFiles(dir, files, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(platform, catalogfs.Marker), []byte(catalogfs.MarkerContent), 0644); err != nil {
		t.Fatal(err)
	}
}
