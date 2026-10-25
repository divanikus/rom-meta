package sourceaudit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCoverageReportsMissingStaleAndLowConfidence(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Matched", `title: "Matched"
releases:
  "Matched (USA)":
    title: "Matched"
sources:
  igdb:
    matches:
      - release: "Matched (USA)"
        match_score: 145
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Missing", `title: "Missing"
releases:
  "Missing (USA)":
    title: "Missing"
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Stale", `title: "Stale"
releases:
  "Stale (USA)":
    title: "Stale"
sources:
  igdb:
    matches:
      - release: "Old Release (USA)"
        match_score: 90
  launchbox:
    matches:
      - release: "Stale (USA)"
`)

	report, err := Coverage(CoverageOptions{
		DataRoot: root,
		Source:   "igdb",
		Details:  true,
		LowScore: 120,
	})
	if err != nil {
		t.Fatalf("Coverage returned error: %v", err)
	}

	if report.GameCount != 3 || len(report.Sources) != 1 {
		t.Fatalf("unexpected report shape: %#v", report)
	}
	source := report.Sources[0]
	if source.Source != "igdb" {
		t.Fatalf("unexpected source: %q", source.Source)
	}
	if source.CoveredGameCount != 2 || source.MatchedGameCount != 2 || source.MissingGameCount != 1 || source.MatchCount != 2 {
		t.Fatalf("unexpected coverage counts: %#v", source)
	}
	if source.StaleMatchCount != 1 || len(source.StaleMatches) != 1 || source.StaleMatches[0].Release != "Old Release (USA)" {
		t.Fatalf("unexpected stale matches: %#v", source.StaleMatches)
	}
	if source.LowConfidenceCount != 1 || len(source.LowConfidenceMatches) != 1 || source.LowConfidenceMatches[0].Score != 90 {
		t.Fatalf("unexpected low confidence matches: %#v", source.LowConfidenceMatches)
	}
	if len(source.MissingGames) != 1 || source.MissingGames[0].Game != "Missing" {
		t.Fatalf("unexpected missing games: %#v", source.MissingGames)
	}
}

func TestCoverageDiscoversSourcesWhenSourceIsNotSpecified(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Game", `title: "Game"
releases:
  "Game (USA)":
    title: "Game"
sources:
  launchbox:
    matches:
      - release: "Game (USA)"
  openvgdb:
    matches:
      - release: "Game (USA)"
`)

	report, err := Coverage(CoverageOptions{DataRoot: root})
	if err != nil {
		t.Fatalf("Coverage returned error: %v", err)
	}

	if len(report.Sources) != 2 || report.Sources[0].Source != "launchbox" || report.Sources[1].Source != "openvgdb" {
		t.Fatalf("unexpected sources: %#v", report.Sources)
	}
}

func TestCoverageChecksGroupedMatchReleases(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Contra", `title: "Contra"
releases:
  "Contra (USA)":
    title: "Contra"
sources:
  launchbox:
    matches:
      - database_id: "100"
        releases:
          - release: "Contra (USA)"
          - release: "Contra (Europe)"
`)

	report, err := Coverage(CoverageOptions{DataRoot: root, Source: "launchbox", Details: true})
	if err != nil {
		t.Fatalf("Coverage returned error: %v", err)
	}

	source := report.Sources[0]
	if source.MatchCount != 1 || source.StaleMatchCount != 1 {
		t.Fatalf("unexpected grouped release coverage: %#v", source)
	}
	if len(source.StaleMatches) != 1 || source.StaleMatches[0].Release != "Contra (Europe)" {
		t.Fatalf("unexpected stale matches: %#v", source.StaleMatches)
	}
}

func TestCoveragePlatformFilter(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "N64 Game", `title: "N64 Game"
releases:
  "N64 Game (USA)":
    title: "N64 Game"
`)
	writeGame(t, root, "Nintendo - Game Boy", "GB Game", `title: "GB Game"
releases:
  "GB Game (USA)":
    title: "GB Game"
`)

	report, err := Coverage(CoverageOptions{DataRoot: root, Platform: "Nintendo - Nintendo 64", Source: "igdb"})
	if err != nil {
		t.Fatalf("Coverage returned error: %v", err)
	}

	if report.PlatformCount != 1 || report.GameCount != 1 || report.Sources[0].MissingGameCount != 1 {
		t.Fatalf("unexpected platform-filtered report: %#v", report)
	}
}

func TestCoveragePercent(t *testing.T) {
	if got := CoveragePercent(397, 434); got != "91.5%" {
		t.Fatalf("CoveragePercent returned %q", got)
	}
	if got := CoveragePercent(0, 0); got != "0.0%" {
		t.Fatalf("CoveragePercent zero returned %q", got)
	}
}

func writeGame(t *testing.T, root, platform, game, contents string) {
	t.Helper()
	path := filepath.Join(root, "platforms", platform, game, "game.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
