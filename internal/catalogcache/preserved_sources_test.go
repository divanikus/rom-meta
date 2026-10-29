package catalogcache

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreservedSourcesSurviveColdWarmForceAndResume(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "platforms", "Example", "Game", "game.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("title: Game\nfetch: {preserve_sources: [igdb, OpenVGDB]}\nsources: {igdb: {status: not_found}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, o := range []ReadOptions{MetadataReadOptions(nil, "igdb", true), MetadataReadOptions(nil, "igdb", true), MetadataReadOptions(nil, "igdb", false)} {
		games, _, err := ReadGamesWithOptions(root, o, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(games) != 1 || !reflect.DeepEqual(games[0].PreservedSources, []string{"igdb", "openvgdb"}) {
			t.Fatalf("lost owner fetch controls: %#v", games)
		}
	}
}
func TestVersionFourCacheMigratesPreservedSources(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "platforms", "Example", "Game", "game.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("title: Game\nfetch: {preserve_sources: [igdb]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadGames(root, nil); err != nil {
		t.Fatal(err)
	}
	c, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.db.Exec("CREATE TABLE retained_snapshot (value TEXT); INSERT INTO retained_snapshot VALUES ('keep'); DROP INDEX idx_catalog_games_resume; ALTER TABLE catalog_games DROP COLUMN preserved_sources_json; UPDATE catalog_cache_meta SET schema_version = 4"); err != nil {
		t.Fatal(err)
	}
	c.close()
	games, _, err := ReadGames(root, nil)
	if err != nil || len(games) != 1 || !HasSource(games[0].PreservedSources, "igdb") {
		t.Fatalf("migrated read %#v %v", games, err)
	}
	c, err = open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var value string
	if err = c.db.QueryRow("SELECT value FROM retained_snapshot").Scan(&value); err != nil || value != "keep" {
		t.Fatalf("snapshot lost: %s %v", value, err)
	}
}
