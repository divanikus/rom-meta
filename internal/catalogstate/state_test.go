package catalogstate

import (
	"context"
	"errors"
	"reflect"

	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"testing"

	"rommeta/internal/taskprogress"
)

func TestBuildStatusCountsSourceDataMediaAndEmptyPlaceholders(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
releases:
  Star Fox 64 (USA):
sources:
  launchbox:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: https://example.test/starfox.jpg
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Unmatched Game", `title: "Unmatched Game"
releases:
  Unmatched Game (USA):
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Checked Missing", `title: "Checked Missing"
sources:
  openvgdb:
    status: not_found
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Manual Media", `title: "Manual Media"
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Manual Media", "media", "box-front.manual.jpg"), "image")

	status, err := BuildStatus(StatusOptions{DataRoot: root})
	if err != nil {
		t.Fatalf("BuildStatus returned error: %v", err)
	}

	if status.PlatformCount != 1 || status.GameCount != 4 || status.ReleaseCount != 2 {
		t.Fatalf("unexpected scan counts: %#v", status)
	}
	if status.GamesWithSourceData != 2 || status.GamesWithoutSourceData != 2 {
		t.Fatalf("unexpected source data counts: %#v", status)
	}
	if status.GamesWithSourceMatches != 1 || status.GamesCheckedNoMatches != 1 {
		t.Fatalf("unexpected source match counts: %#v", status)
	}
	if status.GamesWithLocalMedia != 1 || status.LocalMediaFileCount != 1 {
		t.Fatalf("unexpected local media counts: %#v", status)
	}
	if status.GamesWithMediaURLs != 1 || status.SourceMediaURLCount != 1 {
		t.Fatalf("unexpected source media URL counts: %#v", status)
	}
	if status.EmptyPlaceholderCount != 2 || len(status.EmptyPlaceholders) != 2 || !hasGameRef(status.EmptyPlaceholders, "Checked Missing") || !hasGameRef(status.EmptyPlaceholders, "Unmatched Game") {
		t.Fatalf("unexpected empty games: %#v", status.EmptyPlaceholders)
	}
	if len(status.SourceCounts) != 2 {
		t.Fatalf("unexpected source counts: %#v", status.SourceCounts)
	}
	if status.SourceCounts[0].Source != "launchbox" || status.SourceCounts[0].GameCount != 1 || status.SourceCounts[0].MatchedGameCount != 1 || status.SourceCounts[0].NoMatchGameCount != 0 {
		t.Fatalf("unexpected launchbox counts: %#v", status.SourceCounts)
	}
	if status.SourceCounts[1].Source != "openvgdb" || status.SourceCounts[1].GameCount != 1 || status.SourceCounts[1].MatchedGameCount != 0 || status.SourceCounts[1].NoMatchGameCount != 1 {
		t.Fatalf("unexpected openvgdb counts: %#v", status.SourceCounts)
	}
}

func TestBuildStatusReportsSetupProgress(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
`)

	var stages []string
	_, err := BuildStatus(StatusOptions{
		DataRoot: root,
		Progress: func(snapshot taskprogress.Snapshot) {
			stages = appendUnique(stages, snapshot.Stage)
		},
	})
	if err != nil {
		t.Fatalf("BuildStatus returned error: %v", err)
	}
	for _, expected := range []string{"resolving root", "opening catalog cache", "scanning catalog", "counting games", "checking media"} {
		if !contains(stages, expected) {
			t.Fatalf("expected stage %q in %#v", expected, stages)
		}
	}
}

func TestBuildStatusDoesNotCountIdentityOnlyMatchesAsMetadata(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "NEC - PC Engine - TurboGrafx-16", "Identity Only", `title: "Identity Only"
releases:
  Identity Only (Japan):
sources:
  hasheous:
    matches:
      - release: "Identity Only (Japan)"
        hasheous_id: "42"
        igdb_id: 99
`)

	status, err := BuildStatus(StatusOptions{DataRoot: root})
	if err != nil {
		t.Fatalf("BuildStatus returned error: %v", err)
	}
	if status.GamesWithSourceMatches != 0 || status.EmptyPlaceholderCount != 1 {
		t.Fatalf("identity-only match counted as descriptive metadata: %#v", status)
	}
	if len(status.SourceCounts) != 1 || status.SourceCounts[0].MatchedGameCount != 1 {
		t.Fatalf("source match coverage should still report Hasheous identity: %#v", status.SourceCounts)
	}
}

func TestPruneEmptyDeletesOnlyEmptyPlaceholdersWhenApplied(t *testing.T) {
	root := t.TempDir()
	emptyPath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Unmatched Game")
	checkedMissingPath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Checked Missing")
	sourcePath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64")
	mediaPath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Manual Media")
	writeGame(t, root, "Nintendo - Nintendo 64", "Unmatched Game", `title: "Unmatched Game"
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Checked Missing", `title: "Checked Missing"
sources:
  openvgdb:
    status: not_found
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
sources:
  launchbox:
    matches:
      - release: "Star Fox 64 (USA)"
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Manual Media", `title: "Manual Media"
`)
	writeFile(t, filepath.Join(mediaPath, "media", "box-front.manual.jpg"), "image")

	dryRun, err := PruneEmpty(PruneOptions{DataRoot: root})
	if err != nil {
		t.Fatalf("PruneEmpty dry run returned error: %v", err)
	}
	if dryRun.DeletedGameCount != 0 || dryRun.EmptyPlaceholderCount != 2 {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}
	mustExist(t, emptyPath)
	mustExist(t, checkedMissingPath)

	applied, err := PruneEmpty(PruneOptions{DataRoot: root, Apply: true})
	if err != nil {
		t.Fatalf("PruneEmpty apply returned error: %v", err)
	}
	if applied.DeletedGameCount != 2 {
		t.Fatalf("deleted count = %d, want 2", applied.DeletedGameCount)
	}
	mustNotExist(t, emptyPath)
	mustNotExist(t, checkedMissingPath)
	mustExist(t, sourcePath)
	mustExist(t, mediaPath)
}

func TestPruneEmptyDeletesIgnoredBIOSFolders(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Master System - Mark III"
	biosPath := filepath.Join(root, "platforms", platform, "[BIOS] Alex Kidd in Miracle World")
	gamePath := filepath.Join(root, "platforms", platform, "Alex Kidd in Miracle World")
	writeGame(t, root, platform, "[BIOS] Alex Kidd in Miracle World", `title: "BIOS"
`)
	writeGame(t, root, platform, "Alex Kidd in Miracle World", `title: "Alex Kidd in Miracle World"
sources:
  launchbox:
    matches:
      - release: "Alex Kidd in Miracle World (USA)"
`)

	dryRun, err := PruneEmpty(PruneOptions{DataRoot: root})
	if err != nil {
		t.Fatalf("PruneEmpty dry run returned error: %v", err)
	}
	if dryRun.EmptyPlaceholderCount != 1 || !hasGameRef(dryRun.EmptyPlaceholders, "[BIOS] Alex Kidd in Miracle World") {
		t.Fatalf("unexpected dry-run BIOS cleanup plan: %#v", dryRun.EmptyPlaceholders)
	}
	applied, err := PruneEmpty(PruneOptions{DataRoot: root, Apply: true})
	if err != nil {
		t.Fatalf("PruneEmpty apply returned error: %v", err)
	}
	if applied.DeletedGameCount != 1 {
		t.Fatalf("deleted count = %d, want 1", applied.DeletedGameCount)
	}
	mustNotExist(t, biosPath)
	mustExist(t, gamePath)
}

func TestBuildStatusFiltersPlatform(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
`)
	writeGame(t, root, "Sega - Mega Drive - Genesis", "Sonic The Hedgehog", `title: "Sonic The Hedgehog"
`)

	status, err := BuildStatus(StatusOptions{DataRoot: root, Platform: "Sega - Mega Drive - Genesis"})
	if err != nil {
		t.Fatalf("BuildStatus returned error: %v", err)
	}
	if status.PlatformCount != 1 || status.GameCount != 1 || status.Platforms[0].Platform != "Sega - Mega Drive - Genesis" {
		t.Fatalf("unexpected filtered status: %#v", status)
	}
}

func writeGame(t *testing.T, root, platform, game, contents string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "platforms", platform, game, "game.yaml"), contents)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s should not exist, stat err: %v", path, err)
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func hasGameRef(values []GameRef, game string) bool {
	for _, value := range values {
		if value.Game == game {
			return true
		}
	}
	return false
}

func TestReadOnlyStatusMatchesCachedAndStopsMidScan(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"} {
				writeGame(t, root, "Example", name, "title: "+name+"\nreleases: {example: {}}\n")
			}
			if packed {
				catalogfstest.Pack(t, filepath.Join(root, "platforms", "Example"))
			}
			ro, err := BuildStatus(StatusOptions{DataRoot: root, ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, ".rommeta")); !os.IsNotExist(err) {
				t.Fatal("created cache", err)
			}
			cached, err := BuildStatus(StatusOptions{DataRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ro, cached) {
				t.Fatalf("read-only %+v != cached %+v", ro, cached)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err = BuildStatus(StatusOptions{DataRoot: root, ReadOnly: true, Context: ctx, Progress: func(p taskprogress.Snapshot) {
				if p.Stage == "checking media" {
					cancel()
				}
			}})
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
