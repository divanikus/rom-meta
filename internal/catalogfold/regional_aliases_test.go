package catalogfold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const regionalAnchorSource = `  launchbox:
    matches:
      - database_id: "10"
        title: "English Game"
        match_score: 146
        alternate_titles: ["Nippon Game"]
        titles:
          - title: "Nippon Game"
            region: "Japan"
`

func writeRegionalGame(t *testing.T, root, platform, title, region, extra, sources string) {
	t.Helper()
	if sources == "" {
		sources = "  launchbox:\n    status: not_found\n"
	}
	path := filepath.Join(root, "platforms", platform, title)
	writeFoldFile(t, filepath.Join(path, "game.yaml"), fmt.Sprintf("title: %q\ndefault_release: %q\n%sreleases:\n  %q:\n    title: %q\nsources:\n%s", title, title+" ("+region+")", extra, title+" ("+region+")", title, sources))
	writeFoldFile(t, filepath.Join(path, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: %q\n    sha1: %q\n", title+" ("+region+")", title))
}

func TestRegionalAliasFoldSafety(t *testing.T) {
	for _, tt := range []struct {
		name, anchor, targetSource, targetExtra, region string
		groups, held                                    int
	}{
		{name: "explicit regional alias", groups: 1},
		{name: "plain alias needs review", anchor: "  launchbox:\n    matches:\n      - database_id: '10'\n        title: English Game\n        alternate_titles: ['Nippon Game']\n", groups: 1, held: 1},
		{name: "wrong region", region: "Germany", groups: 1, held: 1},
		{name: "conflicting identity", targetSource: "  launchbox:\n    matches:\n      - database_id: '11'\n", groups: 1, held: 1},
		{name: "conflicting other provider", anchor: regionalAnchorSource + "  igdb:\n    matches:\n      - igdb_id: '20'\n        platforms: ['PS3']\n", targetSource: "  igdb:\n    matches:\n      - igdb_id: '21'\n        platforms: ['PS3']\n", groups: 1, held: 1},
		{name: "low score", anchor: strings.Replace(regionalAnchorSource, "146", "131", 1)},
		{name: "unrelated anchor title", anchor: strings.Replace(regionalAnchorSource, "English Game", "English Game Demo", 1)},
		{name: "ignored direct evidence", targetExtra: "fold:\n  ignore_evidence: ['launchbox.database_id=10']\n"},
		{name: "ignored alias evidence", targetExtra: "fold:\n  ignore_evidence: ['launchbox.regional_alias=10']\n"},
		{name: "unknown canonical field", targetExtra: "manual_notes: keep this\n", groups: 1, held: 1},
		{name: "unscoped IGDB", anchor: "  igdb:\n    matches:\n      - igdb_id: '10'\n        alternate_titles: ['Nippon Game']\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			anchor, region := tt.anchor, tt.region
			if anchor == "" {
				anchor = regionalAnchorSource
			}
			if region == "" {
				region = "Japan"
			}
			writeRegionalGame(t, root, "Console", "English Game", "USA", "", anchor)
			writeRegionalGame(t, root, "Console", "Nippon Game", region, tt.targetExtra, tt.targetSource)
			dry, err := Fold(Options{DataRoot: root})
			if err != nil || dry.CandidateGroupCount != tt.groups || dry.HeldGroupCount != tt.held {
				t.Fatalf("preview: %v %+v", err, dry)
			}
			applied, err := Fold(Options{DataRoot: root, Apply: true})
			if err != nil || applied.AppliedGroupCount != tt.groups-tt.held {
				t.Fatalf("apply: %v %+v", err, applied)
			}
			if tt.held > 0 && len(applied.Backups) != 0 {
				t.Fatal("held-only run published a transaction")
			}
		})
	}
}

func TestRegionalAliasAmbiguousAndTransitiveConflicts(t *testing.T) {
	for _, ambiguous := range []bool{true, false} {
		root := t.TempDir()
		writeRegionalGame(t, root, "Console", "English Game", "USA", "", regionalAnchorSource)
		writeRegionalGame(t, root, "Console", "Nippon Game", "Japan", "", "  igdb:\n    matches:\n      - igdb_id: '20'\n        platforms: ['Console']\n")
		extra := strings.Replace(strings.Replace(regionalAnchorSource, "\"10\"", "\"11\"", 1), "English Game", "Third Game", 1)
		if !ambiguous {
			extra = "  launchbox:\n    matches:\n      - database_id: '11'\n  igdb:\n    matches:\n      - igdb_id: '20'\n        platforms: ['Console']\n"
		}
		writeRegionalGame(t, root, "Console", "Third Game", "Europe", "", extra)
		result, err := Fold(Options{DataRoot: root, Apply: true})
		if err != nil || result.HeldGroupCount != 1 || result.AppliedGroupCount != 0 || len(result.Groups[0].Merge) != 2 {
			t.Fatalf("ambiguous=%v: %v %+v", ambiguous, err, result)
		}
	}
}

func TestRegionalAliasApplyPreservesMetadataAndRepeats(t *testing.T) {
	root := t.TempDir()
	writeRegionalGame(t, root, "Console", "English Game", "USA", "aliases: [Old English]\n", regionalAnchorSource)
	writeRegionalGame(t, root, "Console", "Nippon Game", "Japan", "alternate_titles: [Local Name]\n", "  hasheous:\n    matches:\n      - game_id: '123'\n        title: Nippon Game\n")
	writeRegionalGame(t, root, "Other Console", "Nippon Game", "Japan", "", "")
	result, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.AppliedGroupCount != 1 || len(result.Backups) != 1 {
		t.Fatal(err, result)
	}
	kept := filepath.Join(root, "platforms", "Console", "English Game")
	game := readFoldFile(t, filepath.Join(kept, "game.yaml"))
	for _, want := range []string{"Old English", "Local Name", "Nippon Game (Japan)", "game_id: '123'", "database_id: \"10\"", "region: \"Japan\""} {
		if !strings.Contains(game, want) {
			t.Fatalf("missing %s in %s", want, game)
		}
	}
	fingerprints := readFoldFile(t, filepath.Join(kept, "fingerprints.yaml"))
	if !strings.Contains(fingerprints, "Nippon Game") || !strings.Contains(fingerprints, "English Game") {
		t.Fatal(fingerprints)
	}
	if _, err := os.Stat(filepath.Join(result.Backups[0], "Nippon Game", "game.yaml")); err != nil {
		t.Fatal(err)
	}
	again, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || again.CandidateGroupCount != 0 {
		t.Fatal(err, again)
	}
}

func TestRegionalTitleKeyPreservesEditions(t *testing.T) {
	if regionalTitleKey("Adventures, The - The Game") != regionalTitleKey("The Adventures: The Game") {
		t.Fatal("article before subtitle")
	}
	if regionalTitleKey("007: Ein Quantum Trost") != regionalTitleKey("007 - Ein Quantum Trost") {
		t.Fatal("punctuation")
	}
	if regionalTitleKey("Game, The") != regionalTitleKey("The Game") {
		t.Fatal("article")
	}
	for _, variant := range []string{"Game 2", "Game (Collector's Edition)", "Game+", "Game - Prologue"} {
		if regionalTitleKey(variant) == regionalTitleKey("Game") {
			t.Fatal("lost edition", variant)
		}
	}
}
