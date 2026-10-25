package catalogcache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/taskprogress"
)

func TestMetadataResumeUsesOnlyCachedHeaders(t *testing.T) {
	root, path := resumeFixture(t)
	c, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	// Invalid unused JSON proves that the shortcut does not load/decode payloads.
	_, err = c.db.Exec(`UPDATE catalog_games SET releases_json = 'unused', aliases_json = 'unused', source_stats_json = 'unused', media_urls_json = 'unused', fingerprints_json = 'unused'`)
	c.close()
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []taskprogress.Snapshot
	var seen []Game
	platforms, count, err := ForEachGameWithOptions(root, MetadataReadOptions(nil, "launchbox", false), func(s taskprogress.Snapshot) {
		snapshots = append(snapshots, s)
	}, func(game Game) error {
		seen = append(seen, game)
		return nil
	})
	if err != nil || platforms != 1 || count != 1 || len(seen) != 1 {
		t.Fatalf("unexpected resume: platforms=%d count=%d games=%#v err=%v", platforms, count, seen, err)
	}
	game := seen[0]
	if game.Title != "Game" || game.Path != filepath.Dir(path) || !HasSource(game.Sources, "launchbox") || len(game.MediaURLs) != 0 || len(game.Fingerprints) != 0 || len(game.Releases) != 0 {
		t.Fatalf("unexpected cached header: %#v", game)
	}
	last := snapshots[len(snapshots)-1]
	if last.Stage != "reading catalog" || last.Current != 1 || last.Total != 1 {
		t.Fatalf("resume lost catalog progress: %#v", snapshots)
	}
	if _, _, err := ReadGames(root, nil); err == nil {
		t.Fatal("full reads must still decode the complete cached payload")
	}
}

func TestMetadataResumeReadsFullGameWhenSourceMissingOrOverwrite(t *testing.T) {
	root, _ := resumeFixture(t)
	for _, options := range []ReadOptions{
		MetadataReadOptions(nil, "screenscraper", false),
		MetadataReadOptions(nil, "launchbox", true),
		{ExistingSource: "launchbox", IgnoredSources: []string{"launchbox"}},
		{},
	} {
		games, _, err := ReadGamesWithOptions(root, options, nil)
		if err != nil || len(games) != 1 {
			t.Fatalf("read options %#v: games=%#v, err=%v", options, games, err)
		}
		game := games[0]
		if len(game.Releases) != 1 || len(game.Fingerprints) != 1 || game.DefaultRelease != "Game (USA)" {
			t.Errorf("options %#v returned an incomplete game: %#v", options, game)
		}
		if len(options.IgnoredSources) > 0 && HasSource(game.Sources, "launchbox") {
			t.Errorf("overwrite still sees the ignored source: %#v", game)
		}
	}
}

func TestMetadataResumeRejectsStaleHeaders(t *testing.T) {
	for _, kind := range []string{"game", "fingerprints", "removed game"} {
		t.Run(kind, func(t *testing.T) {
			root, path := resumeFixture(t)
			c, err := open(root)
			if err != nil {
				t.Fatal(err)
			}
			// Stale full rows must not be decoded before checking the file stamp.
			_, err = c.db.Exec(`UPDATE catalog_games SET media_urls_json = 'stale payload'`)
			c.close()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "game":
				writeGameYAML(t, filepath.Dir(path), "title: Updated Game\nreleases:\n  Updated Game (USA):\n")
			case "fingerprints":
				writeFingerprintsYAML(t, filepath.Dir(path), "items: []\n")
			case "removed game":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			games, _, err := ReadGamesWithOptions(root, MetadataReadOptions(nil, "launchbox", false), nil)
			if kind == "removed game" {
				if err == nil {
					t.Fatal("a cached source must not hide a missing game.yaml")
				}
				return
			}
			if err != nil || len(games) != 1 {
				t.Fatalf("read changed %s: games=%#v err=%v", kind, games, err)
			}
			if kind == "game" && (games[0].Title != "Updated Game" || HasSource(games[0].Sources, "launchbox")) {
				t.Errorf("stale game header reused: %#v", games[0])
			}
			if kind == "fingerprints" && (len(games[0].Fingerprints) != 0 || len(games[0].MediaURLs) != 1) {
				t.Errorf("changed fingerprints did not invalidate the full row: %#v", games[0])
			}
		})
	}
}

func TestMetadataResumeQueryUsesCoveringIndex(t *testing.T) {
	root, _ := resumeFixture(t)
	c, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var id, parent, unused int
	var detail string
	err = c.db.QueryRow("EXPLAIN QUERY PLAN "+cachedExistingSourceSQL, "game", 0, 0, -1, 0).Scan(&id, &parent, &unused, &detail)
	if err != nil || !strings.Contains(detail, "COVERING INDEX idx_catalog_games_resume") {
		t.Fatalf("resume query would read full cache pages: %s, %v", detail, err)
	}
}

func resumeFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "platforms", "Platform", "Game")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, dir, `title: Game
default_release: Game (USA)
releases:
  Game (USA):
sources:
  launchbox:
    matches:
      - release: Game (USA)
        media:
          box_front:
            - url: https://example.test/front.jpg
`)
	writeFingerprintsYAML(t, dir, `items:
  - release: Game (USA)
    file:
      name: Game.rom
      crc32: abcd1234
`)
	if _, _, err := ReadGames(root, nil); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(dir, "game.yaml")
}

func BenchmarkMetadataResume(b *testing.B) {
	root := b.TempDir()
	c, err := open(root)
	if err != nil {
		b.Fatal(err)
	}
	if err := c.begin(); err != nil {
		b.Fatal(err)
	}
	for i := range 32 {
		folder := fmt.Sprintf("Game %02d", i)
		dir := filepath.Join(root, "platforms", "Platform", folder)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		path := filepath.Join(dir, "game.yaml")
		if err := os.WriteFile(path, []byte("title: Game\n"), 0o644); err != nil {
			b.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			b.Fatal(err)
		}
		game := Game{Platform: "Platform", Folder: folder, Title: "Game", Sources: []string{"launchbox"}}
		for j := range 400 {
			game.MediaURLs = append(game.MediaURLs, MediaURL{Source: "launchbox", URL: fmt.Sprintf("https://example.test/media/%d.jpg", j)})
		}
		if err := c.storeGame("platforms/Platform/"+folder+"/game.yaml", bundleStamp{size: info.Size(), mtimeNS: info.ModTime().UnixNano(), fingerprintSize: -1}, game); err != nil {
			b.Fatal(err)
		}
	}
	if err := c.commit(); err != nil {
		b.Fatal(err)
	}
	c.close()
	for _, mode := range []string{"full", "resume"} {
		b.Run(mode, func(b *testing.B) {
			options := ReadOptions{}
			if mode == "resume" {
				options = MetadataReadOptions(nil, "launchbox", false)
			}
			b.ReportAllocs()
			for b.Loop() {
				_, _, err := ForEachGameWithOptions(root, options, nil, func(Game) error { return nil })
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
