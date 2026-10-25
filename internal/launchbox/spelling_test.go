package launchbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestImportMatchesPrimaryWordBoundaryWithoutBorrowingPirateReleases(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Nintendo - Nintendo Entertainment System", "BattleCity")
	writeFileForTest(t, filepath.Join(game, "game.yaml"), `title: BattleCity
default_release: BattleCity (USA) (Pirate)
releases:
  BattleCity (USA) (Pirate):
    title: BattleCity
    tags: [pirate]
  BattleCity (Japan) (En):
    title: BattleCity
    regions: [Japan]
sources:
  launchbox:
    status: not_found
  igdb:
    matches: [{igdb_id: "fixture", title: Battle City}]
`)
	snapshot := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, snapshot, map[string]string{"Metadata.xml": `<LaunchBox><Game><DatabaseID>7819</DatabaseID><Name>Battle City</Name><Platform>Nintendo Entertainment System</Platform><Overview>Fixture description.</Overview></Game><GameAlternateName><DatabaseID>7819</DatabaseID><AlternateName>Unrelated Tank Game</AlternateName></GameAlternateName></LaunchBox>`})
	skipped, err := Import(ImportOptions{DataRoot: root, ZipPath: snapshot})
	if err != nil || skipped.SkippedExistingCount != 1 {
		t.Fatalf("saved not_found must retain normal resume: %#v %v", skipped, err)
	}
	for i := 0; i < 2; i++ { // The second pass reads the existing snapshot SQLite cache.
		got, err := Import(ImportOptions{DataRoot: root, ZipPath: snapshot, ForceOverwrite: true, Apply: true})
		if err != nil || got.MatchedReleaseCount != 1 || got.GameFileCount != 1 {
			t.Fatalf("pass %d: %#v %v", i, got, err)
		}
		text := readFileForTest(t, filepath.Join(game, "game.yaml"))
		if !strings.Contains(text, `database_id: "7819"`) || !strings.Contains(text, `release: "BattleCity (Japan) (En)"`) || !strings.Contains(text, `igdb_id: "fixture"`) {
			t.Fatalf("lost scoped metadata: %s", text)
		}
	}
}

func TestPrimarySpellingFallbackKeepsExactMatchesAndRejectsCollisions(t *testing.T) {
	a := releaseMetadata{DatabaseID: "a", Title: "Battle City", Platform: "NES", Region: "Japan"}
	b := releaseMetadata{DatabaseID: "b", Title: "BattleCity", Platform: "NES", Region: "USA"}
	index := snapshotIndex{byID: map[string]releaseMetadata{"a": a, "b": b}, byPlatformTitle: map[string]map[string][]releaseMetadata{"NES": {}}}
	for _, m := range index.byID {
		for _, key := range titleMatchKeys(m.Title) {
			index.byPlatformTitle["NES"][key] = appendLaunchBoxMetadata(index.byPlatformTitle["NES"][key], m)
		}
	}
	index = addCompactPrimaryTitleKeys(index)
	for _, tc := range []struct{ title, want string }{{"Battle City", "a"}, {"BattleCity", "b"}, {"Batt leCity", ""}} {
		got, ok := matchMetadata(index.byPlatformTitle["NES"], catalogRelease{Name: tc.title + " (Japan)", Title: tc.title, Regions: []string{"Japan"}}, tc.title)
		if ok != (tc.want != "") || got.DatabaseID != tc.want {
			t.Fatalf("%s: %#v/%v", tc.title, got, ok)
		}
	}
}

func TestPrimarySpellingFallbackDoesNotBorrowAlternateNamesOrDropPlus(t *testing.T) {
	m := releaseMetadata{DatabaseID: "a", Title: "Sonic CD", Platform: "Sega CD", AlternateTitles: []string{"Unrelated Game"}}
	index := snapshotIndex{byID: map[string]releaseMetadata{"a": m}, byPlatformTitle: map[string]map[string][]releaseMetadata{"Sega CD": {"sonic cd": {m}}}}
	index = addCompactPrimaryTitleKeys(index)
	for _, name := range []string{"SonicCD++", "UnrelatedGame"} {
		if got, ok := matchMetadata(index.byPlatformTitle["Sega CD"], catalogRelease{Name: name + " (Japan)", Title: name}, name); ok {
			t.Fatalf("unsafe spelling fallback %q: %#v", name, got)
		}
	}
}

func TestPrimarySpellingFallbackPreservesContentQualifiers(t *testing.T) {
	for _, tc := range []struct{ source, release string }{
		{"Battle City", "BattleCity (Japan) (Complete Edition)"},
		{"BattleCity (Complete Edition)", "Battle City (Japan)"},
		{"Battle City (Pirate)", "BattleCity (Japan)"},
		{"Battle City", "BattleCity (Japan) (Unknown Variant)"},
	} {
		m := releaseMetadata{DatabaseID: "one", Title: tc.source, Platform: "NES"}
		index := snapshotIndex{byID: map[string]releaseMetadata{"one": m}, byPlatformTitle: map[string]map[string][]releaseMetadata{"NES": {}}}
		for _, key := range titleMatchKeys(m.Title) {
			index.byPlatformTitle["NES"][key] = []releaseMetadata{m}
		}
		index = addCompactPrimaryTitleKeys(index)
		if c, ok := matchMetadata(index.byPlatformTitle["NES"], catalogRelease{Name: tc.release, Title: stripTags(tc.release)}, stripTags(tc.release)); ok {
			t.Fatalf("cross-content spelling fallback %q -> %q: %#v", tc.release, tc.source, c)
		}
	}
}

func TestPrimarySpellingFallbackRejectsKnownDisjointRegions(t *testing.T) {
	for _, region := range []string{"Japan", "World", ""} {
		m := releaseMetadata{DatabaseID: "one", Title: "Battle City", Platform: "NES", Region: region}
		index := snapshotIndex{byID: map[string]releaseMetadata{"one": m}, byPlatformTitle: map[string]map[string][]releaseMetadata{"NES": {"battle city": {m}}}}
		index = addCompactPrimaryTitleKeys(index)
		_, ok := matchMetadata(index.byPlatformTitle["NES"], catalogRelease{Name: "BattleCity (Europe)", Title: "BattleCity", Regions: []string{"Europe"}}, "BattleCity")
		if ok != (region != "Japan") {
			t.Fatalf("region %q accepted=%v", region, ok)
		}
	}
}
