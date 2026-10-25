package catalogcache

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"testing"
)

func TestPackedCacheInvalidatesMemberAndFingerprintEdits(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Nintendo 64", "Example")
	if e := os.MkdirAll(game, 0755); e != nil {
		t.Fatal(e)
	}
	writeGameYAML(t, game, "title: First\n")
	writeFingerprintsYAML(t, game, "items: []\n")
	catalogfstest.Pack(t, filepath.Dir(game))
	first, _, e := ReadGames(root, nil)
	if e != nil || len(first) != 1 || first[0].Title != "First" {
		t.Fatal(first, e)
	}
	if e = catalogfs.WriteFile(filepath.Join(game, "game.yaml"), []byte("title: Other\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e = catalogfs.WriteFile(filepath.Join(game, "fingerprints.yaml"), []byte("items:\n  - release: Example\n    file: {name: new.z64, size: 10, sha1: abc}\n"), 0644); e != nil {
		t.Fatal(e)
	}
	second, _, e := ReadGames(root, nil)
	if e != nil || len(second) != 1 || second[0].Title != "Other" || len(second[0].Fingerprints) != 1 {
		t.Fatal(second, e)
	}
}
