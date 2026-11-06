package openvgdb

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"rommeta/internal/platformnames"
	"testing"
)

func TestVerifiedDiscoveryUsesDatabasePlatformNames(t *testing.T) {
	for _, available := range []bool{true, false} {
		dbPath := createOpenVGDBFixture(t)
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		name := "Other System"
		if available {
			name = "New System"
		}
		_, err = db.Exec("UPDATE SYSTEMS SET systemName = ? WHERE systemID = 23", name)
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		game := filepath.Join(root, "platforms", "Future Vendor - New System", "Alpha Test", "game.yaml")
		original := "title: Alpha Test\nreleases:\n  Alpha Test (USA):\n    title: Alpha Test\n"
		writeFileForTest(t, game, original)
		result, err := Import(ImportOptions{DataRoot: root, DBPath: dbPath, Apply: true, VerifyPlatform: true})
		if available {
			if err != nil || result.MatchedReleaseCount != 1 {
				t.Fatal(result, err)
			}
		} else {
			var unresolved *platformnames.ResolutionError
			data, _ := os.ReadFile(game)
			if !errors.As(err, &unresolved) || string(data) != original {
				t.Fatal("unresolved platform changed card", err)
			}
		}
	}
}
