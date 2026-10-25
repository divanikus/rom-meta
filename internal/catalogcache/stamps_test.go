package catalogcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadGamesInvalidatesEachFileStamp(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "resume"}[resume], func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "platforms", "Platform", "Example")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			gamePath := filepath.Join(dir, "game.yaml")
			hashesPath := filepath.Join(dir, "fingerprints.yaml")
			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			write := func(path, body string, stamp time.Time) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			write(gamePath, "title: Old\nsources:\n  hasheous:\n    status: not_found\n", base)
			write(hashesPath, "fingerprints: []\n", base.Add(time.Hour))
			options := ReadOptions{}
			if resume {
				options = MetadataReadOptions(nil, "hasheous", false)
			}
			if _, _, err := ReadGamesWithOptions(root, options, nil); err != nil {
				t.Fatal(err)
			}
			write(gamePath, "title: New\nsources:\n  hasheous:\n    status: not_found\n", base.Add(time.Minute))
			games, _, err := ReadGamesWithOptions(root, options, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(games) != 1 || games[0].Title != "New" {
				t.Fatalf("stale catalog after same-size edit: %#v", games)
			}
		})
	}
}

func TestReadGamesInvalidatesOlderFingerprintStamp(t *testing.T) {
	root, gamePath := resumeFixture(t)
	fingerprintPath := filepath.Join(filepath.Dir(gamePath), "fingerprints.yaml")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(gamePath, base.Add(time.Hour), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fingerprintPath, base, base); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadGames(root, nil); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(fingerprintPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.ReplaceAll(string(contents), "abcd1234", "dcba4321")
	if err := os.WriteFile(fingerprintPath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fingerprintPath, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	games, _, err := ReadGames(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || len(games[0].Fingerprints) != 1 || games[0].Fingerprints[0].CRC32 != "dcba4321" {
		t.Fatalf("stale fingerprint: %#v", games)
	}
}

func TestFileStampMigrationPreservesSnapshotsAndRefreshesRows(t *testing.T) {
	root, _ := resumeFixture(t)
	c, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP INDEX idx_catalog_games_resume",
		"ALTER TABLE catalog_games DROP COLUMN arcade_platform",
		"ALTER TABLE catalog_games DROP COLUMN fingerprint_size",
		"ALTER TABLE catalog_games DROP COLUMN fingerprint_mtime_ns",
		"UPDATE catalog_games SET title = 'stale legacy row'",
		"UPDATE catalog_cache_meta SET schema_version = 1",
		"CREATE TABLE snapshot_sentinel (value TEXT)",
		"INSERT INTO snapshot_sentinel VALUES ('keep snapshot')",
	} {
		if _, err := c.db.Exec(statement); err != nil {
			c.close()
			t.Fatal(err)
		}
	}
	c.close()
	options := MetadataReadOptions(nil, "launchbox", false)
	games, _, err := ReadGamesWithOptions(root, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Title != "Game" || len(games[0].Releases) == 0 {
		t.Fatalf("migration reused stale header instead of refreshing: %#v", games)
	}
	c, err = open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var sentinel string
	if err := c.db.QueryRow("SELECT value FROM snapshot_sentinel").Scan(&sentinel); err != nil || sentinel != "keep snapshot" {
		t.Fatalf("unrelated snapshot lost: %q, %v", sentinel, err)
	}
	var version int
	if err := c.db.QueryRow("SELECT schema_version FROM catalog_cache_meta").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version=%d, %v", version, err)
	}
	// Subsequent opens must not rerun ALTER TABLE or rebuild a valid full row.
	games, _, err = ReadGamesWithOptions(root, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || len(games[0].Releases) != 0 {
		t.Fatalf("warm resume did not use a header: %#v", games)
	}
}
