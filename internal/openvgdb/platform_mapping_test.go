package openvgdb

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestImportUsesExactSnapshotPlatformNames(t *testing.T) {
	// These are SYSTEMS.systemName values from the provider snapshot. A friendly
	// display name silently returns an empty index because the SQL uses equality.
	for _, tc := range []struct{ platform, sourceName string }{
		{"Bandai - WonderSwan", "Bandai WonderSwan"},
		{"Bandai - WonderSwan Color", "Bandai WonderSwan Color"},
		{"Nintendo - Virtual Boy", "Nintendo Virtual Boy"},
		{"Nintendo - Game Boy", "Nintendo Game Boy"},
		{"Nintendo - Game Boy Color", "Nintendo Game Boy Color"},
		{"Nintendo - Game Boy Advance", "Nintendo Game Boy Advance"},
		{"NEC - PC Engine SuperGrafx", "NEC SuperGrafx"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			dbPath := createOpenVGDBFixture(t)
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec("UPDATE SYSTEMS SET systemName = ? WHERE systemID = 23", tc.sourceName)
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			writeFileForTest(t, filepath.Join(root, "platforms", tc.platform, "Alpha Test", "game.yaml"),
				"title: Alpha Test\nreleases:\n  \"Alpha Test (USA)\":\n    title: Alpha Test\n")
			// An identical title on an unrelated platform must not borrow this match.
			writeFileForTest(t, filepath.Join(root, "platforms", "Unrelated Platform", "Alpha Test", "game.yaml"),
				"title: Alpha Test\nreleases:\n  \"Alpha Test (USA)\":\n    title: Alpha Test\n")
			result, err := Import(ImportOptions{DataRoot: root, DBPath: dbPath})
			if err != nil {
				t.Fatal(err)
			}
			if result.MatchedReleaseCount != 1 || result.NotFoundGameCount != 1 {
				t.Fatalf("expected only the platform-scoped release to match: %+v", result)
			}
		})
	}
}
