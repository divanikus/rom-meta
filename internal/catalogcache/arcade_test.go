package catalogcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArcadeIdentitySurvivesCacheAndV2Migration(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "platforms", "Arcade", "Game")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "title: Game\narcade:\n  platform: Arcade - SNK - Neo Geo\nreleases:\n  renamed-release:\n    title: Game (bootleg, set 1)\n    authority:\n      source: mame\n      entry_id: gameb\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	for step := 0; step < 3; step++ {
		games, _, err := ReadGames(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(games) != 1 || games[0].ArcadePlatform != "Arcade - SNK - Neo Geo" || games[0].Releases[0].MAMESet != "gameb" || len(games[0].Releases[0].Tags) != 1 || games[0].Releases[0].Tags[0] != "bootleg" {
			t.Fatalf("step %d: %#v", step, games)
		}
		if step == 1 {
			c, err := open(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range []string{"ALTER TABLE catalog_games DROP COLUMN arcade_platform", "UPDATE catalog_games SET releases_json='[]'", "UPDATE catalog_cache_meta SET schema_version=2", "CREATE TABLE snapshot_sentinel (v TEXT)", "INSERT INTO snapshot_sentinel VALUES ('keep')"} {
				if _, err := c.db.Exec(s); err != nil {
					t.Fatal(err)
				}
			}
			c.close()
		}
	}
	c, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var sentinel string
	if err := c.db.QueryRow("SELECT v FROM snapshot_sentinel").Scan(&sentinel); err != nil || sentinel != "keep" {
		t.Fatalf("snapshot: %s %v", sentinel, err)
	}
	home, err := parseGameYAML([]byte("title: Home\nreleases:\n  pacman:\n    title: Pac-Man\n"))
	if err != nil || home.Releases[0].MAMESet != "" {
		t.Fatal("short names are not MAME authority")
	}
}

func TestMAMEMarkersComeFromQualifiers(t *testing.T) {
	game, err := parseGameYAML([]byte("title: Shack\nreleases:\n  shack:\n    title: Hack Shack (World)\n    authority:\n      source: mame\n      entry_id: shack\n"))
	if err != nil || len(game.Releases[0].Tags) != 0 {
		t.Fatalf("ordinary title tagged as hack: %#v, %v", game, err)
	}
}
