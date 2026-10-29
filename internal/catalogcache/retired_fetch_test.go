package catalogcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredFetchColumnDoesNotBlockCacheOrForce(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "platforms", "Nintendo 64", "Game")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA): {}\nsources:\n  igdb: {status: not_found}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadGames(root, nil); err != nil {
		t.Fatal(err)
	}
	c, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`ALTER TABLE catalog_games ADD COLUMN preserved_sources_json TEXT NOT NULL DEFAULT '["igdb"]'`, `CREATE TABLE snapshot_sentinel (v TEXT)`, `INSERT INTO snapshot_sentinel VALUES ('keep')`} {
		if _, err := c.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	c.close()
	for pass := 0; pass < 2; pass++ {
		count := 0
		_, _, err := ForEachGameWithOptions(root, MetadataReadOptions(nil, "igdb", true), nil, func(game Game) error { count++; return nil })
		if err != nil || count != 1 {
			t.Fatalf("force cache pass %d: %d %v", pass, count, err)
		}
	}
	c, err = open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var sentinel string
	if err := c.db.QueryRow("SELECT v FROM snapshot_sentinel").Scan(&sentinel); err != nil || sentinel != "keep" {
		t.Fatalf("snapshot lost: %q %v", sentinel, err)
	}
}
