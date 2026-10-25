package catalogcache

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rommeta/internal/taskprogress"
)

func TestReadGamesRejectsUnversionedCatalogCache(t *testing.T) {
	root := t.TempDir()
	cachePath := Path(root)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE catalog_games (path TEXT PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = ReadGames(root, nil)
	if err == nil || !strings.Contains(err.Error(), "delete "+cachePath+" and rerun") {
		t.Fatalf("expected explicit cache rebuild error, got %v", err)
	}
}

func TestReadGamesRejectsUnsupportedCatalogCacheVersion(t *testing.T) {
	root := t.TempDir()
	cachePath := Path(root)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE catalog_cache_meta (id INTEGER PRIMARY KEY, schema_version INTEGER NOT NULL); INSERT INTO catalog_cache_meta (id, schema_version) VALUES (1, 999)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = ReadGames(root, nil)
	if err == nil || !strings.Contains(err.Error(), "schema version 999") || !strings.Contains(err.Error(), "delete "+cachePath) {
		t.Fatalf("expected unsupported schema error, got %v", err)
	}
}

func TestReadGamesCachesParsedGameYAML(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Nintendo 64", "Legend of Zelda, The - Ocarina of Time")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, `title: "Legend of Zelda, The - Ocarina of Time"
default_release: "Legend of Zelda, The - Ocarina of Time (USA)"
alternate_titles: ["The Legend of Zelda: Ocarina of Time"]
releases:
  Legend of Zelda, The - Ocarina of Time (USA):
    title: "The Legend of Zelda: Ocarina of Time"
    regions: [USA]
sources:
  launchbox:
    matches:
      - release: "Legend of Zelda, The - Ocarina of Time (USA)"
        title: "Stale Source Title"
        alternate_titles: ["Stale Source Alias"]
        media:
          box_front:
            - url: https://example.test/front.jpg
              region: "North America"
`)
	writeFingerprintsYAML(t, gameDir, `items:
  - id: "no-intro.0001"
    release: "Legend of Zelda, The - Ocarina of Time (USA)"
    file:
      name: "Legend of Zelda, The - Ocarina of Time (USA).z64"
      size: 33554432
      crc32: cd16c529
      md5: 05f0e7b7b7f4d070b7db5daeb9708d68
      sha1: ad69c91157f6705e8ab06c79fe08aad47bb57ba7
    dump:
      format: "z64"
`)

	var stages []string
	progress := taskprogress.Func(func(snapshot taskprogress.Snapshot) {
		if len(stages) == 0 || stages[len(stages)-1] != snapshot.Stage {
			stages = append(stages, snapshot.Stage)
		}
	})
	games, platformCount, err := ReadGames(root, progress)
	if err != nil {
		t.Fatalf("ReadGames returned error: %v", err)
	}
	if platformCount != 1 {
		t.Fatalf("platform count = %d, want 1", platformCount)
	}
	if len(games) != 1 {
		t.Fatalf("game count = %d, want 1", len(games))
	}
	game := games[0]
	if game.Title != "Legend of Zelda, The - Ocarina of Time" {
		t.Fatalf("title = %q", game.Title)
	}
	if !HasSource(game.Sources, "launchbox") {
		t.Fatalf("expected launchbox source in %#v", game.Sources)
	}
	if len(game.SourceStats) != 1 || game.SourceStats[0].Source != "launchbox" || game.SourceStats[0].MatchCount != 1 {
		t.Fatalf("unexpected source stats: %#v", game.SourceStats)
	}
	if len(game.Releases) != 1 || game.Releases[0].Name != "Legend of Zelda, The - Ocarina of Time (USA)" {
		t.Fatalf("unexpected releases: %#v", game.Releases)
	}
	if !containsString(game.Aliases, "The Legend of Zelda: Ocarina of Time") {
		t.Fatalf("catalog alias not found: %#v", game.Aliases)
	}
	if containsString(game.Aliases, "Stale Source Title") || containsString(game.Aliases, "Stale Source Alias") {
		t.Fatalf("source-derived aliases should not be cached as catalog aliases: %#v", game.Aliases)
	}
	if len(game.MediaURLs) != 1 || game.MediaURLs[0].MediaType != "box_front" || game.MediaURLs[0].URL != "https://example.test/front.jpg" || game.MediaURLs[0].Region != "North America" {
		t.Fatalf("unexpected media URLs: %#v", game.MediaURLs)
	}
	if len(game.Fingerprints) != 1 || game.Fingerprints[0].MD5 != "05f0e7b7b7f4d070b7db5daeb9708d68" || game.Fingerprints[0].Size != 33554432 || game.Fingerprints[0].Format != "z64" {
		t.Fatalf("unexpected fingerprints: %#v", game.Fingerprints)
	}
	if _, err := os.Stat(filepath.Join(root, ".rommeta", "cache", "cache.sqlite")); err != nil {
		t.Fatalf("cache file was not written: %v", err)
	}
	for _, stage := range []string{"scanning catalog", "reading catalog"} {
		if !containsStage(stages, stage) {
			t.Fatalf("progress stages = %#v, missing %q", stages, stage)
		}
	}
}

func TestReadGamesParsesReleaseTags(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Nintendo - Nintendo Entertainment System", "Aladdin (World, Unl, Pirate)")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, `title: "Aladdin"
releases:
  "Aladdin (World) (Unl) (Pirate)":
    title: "Aladdin"
    tags: ["unlicensed", "pirate"]
`)

	games, _, err := ReadGames(root, nil)
	if err != nil {
		t.Fatalf("ReadGames returned error: %v", err)
	}
	if len(games) != 1 || len(games[0].Releases) != 1 || !containsString(games[0].Releases[0].Tags, "unlicensed") || !containsString(games[0].Releases[0].Tags, "pirate") {
		t.Fatalf("unexpected release tags: %#v", games)
	}
}

func TestReadGamesWithOptionsIgnoresSourcesFromCachedView(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Sega - 32X", "Doom")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, `title: "Doom"
default_release: "Doom (USA)"
releases:
  "Doom (USA)":
    title: "Doom"
sources:
  launchbox:
    matches:
      - release: "Doom (USA)"
        title: "Doom"
        media:
          box_front:
            - url: https://example.test/doom.jpg
  igdb:
    status: not_found
`)

	if _, _, err := ReadGames(root, nil); err != nil {
		t.Fatalf("initial ReadGames returned error: %v", err)
	}
	games, _, err := ReadGamesWithOptions(root, ReadOptions{IgnoredSources: []string{"launchbox"}}, nil)
	if err != nil {
		t.Fatalf("ReadGamesWithOptions returned error: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("game count = %d, want 1", len(games))
	}
	game := games[0]
	if game.Title != "Doom" || len(game.Releases) != 1 {
		t.Fatalf("basic catalog fields should remain: %#v", game)
	}
	if HasSource(game.Sources, "launchbox") || !HasSource(game.Sources, "igdb") {
		t.Fatalf("unexpected sources after filter: %#v", game.Sources)
	}
	if len(game.SourceStats) != 1 || game.SourceStats[0].Source != "igdb" {
		t.Fatalf("unexpected source stats after filter: %#v", game.SourceStats)
	}
	if len(game.MediaURLs) != 0 {
		t.Fatalf("ignored source media should be hidden: %#v", game.MediaURLs)
	}
}

func TestReadGamesInvalidatesChangedGameYAML(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Nintendo 64", "40 Winks")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, "title: 40 Winks\n")

	games, _, err := ReadGames(root, nil)
	if err != nil {
		t.Fatalf("initial ReadGames returned error: %v", err)
	}
	if games[0].Title != "40 Winks" {
		t.Fatalf("initial title = %q", games[0].Title)
	}

	time.Sleep(10 * time.Millisecond)
	writeGameYAML(t, gameDir, "title: 40 Winks Deluxe\nsources:\n  igdb:\n    matches: []\n")

	games, _, err = ReadGames(root, nil)
	if err != nil {
		t.Fatalf("second ReadGames returned error: %v", err)
	}
	if games[0].Title != "40 Winks Deluxe" {
		t.Fatalf("changed title = %q", games[0].Title)
	}
	if !HasSource(games[0].Sources, "igdb") {
		t.Fatalf("changed sources = %#v, want igdb", games[0].Sources)
	}
}

func TestReadGamesParsesGroupedSourceMatchReleases(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Nintendo 64", "Contra")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, `title: "Contra"
releases:
  "Contra (Europe)":
    title: "Contra"
  "Contra (USA)":
    title: "Contra"
sources:
  launchbox:
    matches:
      - releases: ["Contra (Europe)", "Contra (USA)"]
        media:
          box_front:
            - url: https://example.test/contra.jpg
              region: "World"
`)

	games, _, err := ReadGames(root, nil)
	if err != nil {
		t.Fatalf("ReadGames returned error: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("game count = %d, want 1", len(games))
	}
	if len(games[0].MediaURLs) != 1 {
		t.Fatalf("expected shared media URL to stay game-wide, got %#v", games[0].MediaURLs)
	}
	for _, media := range games[0].MediaURLs {
		if media.Release != "" || media.URL != "https://example.test/contra.jpg" || media.Region != "World" {
			t.Fatalf("unexpected media record: %#v", media)
		}
	}
}

func TestReadGamesParsesCompactSourceMatchReleaseMedia(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Sega - 32X", "Doom")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, `title: "Doom"
releases:
  "Doom (Europe)":
    title: "Doom"
  "Doom (USA)":
    title: "Doom"
sources:
  launchbox:
    matches:
      - database_id: "100"
        title: "Doom"
        releases:
          - release: "Doom (Europe)"
            media:
              box_front:
                - url: https://example.test/doom-eu.jpg
                  region: Europe
          - release: "Doom (USA)"
            media:
              box_front:
                - url: https://example.test/doom-us.jpg
                  region: USA
        media:
          screenshot:
            - url: https://example.test/doom-shot.jpg
`)

	games, _, err := ReadGames(root, nil)
	if err != nil {
		t.Fatalf("ReadGames returned error: %v", err)
	}
	if len(games) != 1 || len(games[0].MediaURLs) != 3 {
		t.Fatalf("unexpected media URLs: %#v", games)
	}
	byURL := map[string]MediaURL{}
	for _, media := range games[0].MediaURLs {
		byURL[media.URL] = media
	}
	if byURL["https://example.test/doom-eu.jpg"].Release != "Doom (Europe)" || byURL["https://example.test/doom-eu.jpg"].Region != "Europe" {
		t.Fatalf("unexpected Europe media: %#v", byURL["https://example.test/doom-eu.jpg"])
	}
	if byURL["https://example.test/doom-us.jpg"].Release != "Doom (USA)" || byURL["https://example.test/doom-us.jpg"].Region != "USA" {
		t.Fatalf("unexpected USA media: %#v", byURL["https://example.test/doom-us.jpg"])
	}
	if byURL["https://example.test/doom-shot.jpg"].Release != "" {
		t.Fatalf("shared media should be game-wide: %#v", byURL["https://example.test/doom-shot.jpg"])
	}
}

func TestReadGamesWithOptionsFiltersPlatformsBeforeReading(t *testing.T) {
	root := t.TempDir()
	n64Dir := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "40 Winks")
	segaCDDir := filepath.Join(root, "platforms", "Sega - Mega CD & Sega CD", "Snatcher")
	if err := os.MkdirAll(n64Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(segaCDDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, n64Dir, "title: 40 Winks\n")
	writeGameYAML(t, segaCDDir, "title: Snatcher\n")

	games, platformCount, err := ReadGamesWithOptions(root, ReadOptions{Platforms: []string{"Sega - Mega CD & Sega CD"}}, nil)
	if err != nil {
		t.Fatalf("ReadGamesWithOptions returned error: %v", err)
	}

	if platformCount != 1 {
		t.Fatalf("platform count = %d, want 1", platformCount)
	}
	if len(games) != 1 || games[0].Platform != "Sega - Mega CD & Sega CD" || games[0].Title != "Snatcher" {
		t.Fatalf("unexpected filtered games: %#v", games)
	}
}

func TestReadGameBundleReportsMalformedYAMLPath(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Sega - 32X", "Doom")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, "title: [broken\n")

	_, err := ReadGameBundle(root, "Sega - 32X", "Doom")
	if err == nil || !strings.Contains(err.Error(), filepath.Join("Sega - 32X", "Doom", "game.yaml")) {
		t.Fatalf("expected path-aware YAML error, got %v", err)
	}
}

func TestForEachGameWithOptionsStreamsGames(t *testing.T) {
	root := t.TempDir()
	n64Dir := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "40 Winks")
	nesDir := filepath.Join(root, "platforms", "Nintendo - Nintendo Entertainment System", "Contra")
	if err := os.MkdirAll(n64Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, n64Dir, "title: 40 Winks\n")
	writeGameYAML(t, nesDir, "title: Contra\n")

	var titles []string
	platformCount, gameCount, err := ForEachGameWithOptions(root, ReadOptions{}, nil, func(game Game) error {
		titles = append(titles, game.Title)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachGameWithOptions returned error: %v", err)
	}
	if platformCount != 2 || gameCount != 2 {
		t.Fatalf("counts = %d platforms, %d games; want 2, 2", platformCount, gameCount)
	}
	if !containsString(titles, "40 Winks") || !containsString(titles, "Contra") {
		t.Fatalf("unexpected streamed titles: %#v", titles)
	}
}

func TestForEachGameWithOptionsUsesCustomProgressStages(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "40 Winks")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, gameDir, "title: 40 Winks\n")

	var stages []string
	progress := taskprogress.Func(func(snapshot taskprogress.Snapshot) {
		if len(stages) == 0 || stages[len(stages)-1] != snapshot.Stage {
			stages = append(stages, snapshot.Stage)
		}
	})
	_, _, err := ForEachGameWithOptions(root, ReadOptions{
		ScanningStage: "counting platforms",
		ReadingStage:  "checking existing metadata",
	}, progress, func(Game) error {
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachGameWithOptions returned error: %v", err)
	}
	for _, stage := range []string{"counting platforms", "checking existing metadata"} {
		if !containsStage(stages, stage) {
			t.Fatalf("progress stages = %#v, missing %q", stages, stage)
		}
	}
}

func TestReadGamesIgnoresBIOSGameFolders(t *testing.T) {
	root := t.TempDir()
	biosDir := filepath.Join(root, "platforms", "Sega - Master System - Mark III", "[BIOS] Alex Kidd in Miracle World")
	gameDir := filepath.Join(root, "platforms", "Sega - Master System - Mark III", "Alex Kidd in Miracle World")
	if err := os.MkdirAll(biosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGameYAML(t, biosDir, "title: BIOS\n")
	writeGameYAML(t, gameDir, "title: Alex Kidd in Miracle World\n")

	games, platformCount, err := ReadGames(root, nil)
	if err != nil {
		t.Fatalf("ReadGames returned error: %v", err)
	}
	if platformCount != 1 {
		t.Fatalf("platform count = %d, want 1", platformCount)
	}
	if len(games) != 1 || games[0].Folder != "Alex Kidd in Miracle World" {
		t.Fatalf("unexpected games after BIOS filter: %#v", games)
	}
}

func writeGameYAML(t *testing.T, gameDir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gameDir, "game.yaml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFingerprintsYAML(t *testing.T, gameDir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gameDir, "fingerprints.yaml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsStage(stages []string, target string) bool {
	for _, stage := range stages {
		if stage == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
