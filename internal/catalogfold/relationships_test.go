package catalogfold

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func relationFixture(t *testing.T, title, id, kind, parent string) catalogGame {
	t.Helper()
	text := fmt.Sprintf("title: %s\nreleases:\n  %s (USA): {}\nsources:\n  igdb:\n    matches:\n      - igdb_id: %s\n        platforms: [PlayStation]\n        match_score: 146\n", title, title, id)
	if kind != "" {
		text += fmt.Sprintf("        relationships:\n          - type: %s\n            target_id: \"%s\"\n", kind, parent)
	}
	game, err := parseGameYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	game.Folder = title
	game.Platform = "Sony - PlayStation"
	return game
}
func editionGroups(games []catalogGame) []Group {
	return buildGroups(withEditionEvidence(games, true), newRegionRules(nil, nil), nil)
}
func TestExplicitEditionChainKeepsBaseAndAllVersions(t *testing.T) {
	games := []catalogGame{relationFixture(t, "Base", "1", "", ""), relationFixture(t, "Directors Cut", "2", "expanded_edition_of", "1"), relationFixture(t, "Dual Shock", "3", "port_of", "2"), relationFixture(t, "Survivor", "4", "", ""), relationFixture(t, "Remake", "5", "remake_of", "1"), relationFixture(t, "Expansion", "6", "expansion_of", "1")}
	games[1].SourceCount = 100
	groups := editionGroups(games)
	if len(groups) != 1 || groups[0].Keep.Folder != "Base" || len(groups[0].Merge) != 2 {
		t.Fatalf("%+v", groups)
	}
	if groups[0].Evidence[0].Field != "edition_family" || groups[0].Evidence[0].Value != "1" {
		t.Fatal(groups[0].Evidence)
	}
}
func TestEditionModeDoesNotApplyUnrelatedHeldIDGroups(t *testing.T) {
	groups := editionGroups([]catalogGame{relationFixture(t, "A", "1", "", ""), relationFixture(t, "B", "1", "", "")})
	if len(groups) != 0 {
		t.Fatal(groups)
	}
}
func TestEditionEvidenceRequiresStrongScopedMatch(t *testing.T) {
	for _, mode := range []string{"weak", "unscoped", "ignored"} {
		t.Run(mode, func(t *testing.T) {
			base := relationFixture(t, "Base", "1", "", "")
			child := relationFixture(t, "Child", "2", "port_of", "1")
			switch mode {
			case "weak":
				child.Evidence = nil
			case "unscoped":
				raw := "title: Child\nsources:\n  igdb:\n    matches:\n      - igdb_id: 2\n        match_score: 146\n        relationships: [{type: port_of, target_id: '1'}]\n"
				var err error
				child, err = parseGameYAML([]byte(raw))
				if err != nil {
					t.Fatal(err)
				}
			case "ignored":
				child.IgnoredEvidence = []string{"igdb.edition_family=1"}
			}
			if got := editionGroups([]catalogGame{base, child}); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}
func TestConflictingParentsAndCyclesDoNotFold(t *testing.T) {
	for _, mode := range []string{"conflict", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			a := relationFixture(t, "A", "1", "", "")
			b := relationFixture(t, "B", "2", "edition_of", "1")
			c := relationFixture(t, "C", "3", "edition_of", "2")
			if mode == "cycle" {
				a = relationFixture(t, "A", "1", "edition_of", "3")
			} else {
				other := relationFixture(t, "Other", "2", "edition_of", "3")
				b.Relationships = append(b.Relationships, other.Relationships...)
			}
			if got := editionGroups([]catalogGame{a, b, c}); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}
func TestFoldPreservesRelationshipBlocks(t *testing.T) {
	a := relationFixture(t, "Base", "1", "", "")
	b := relationFixture(t, "Edition", "2", "edition_of", "1")
	rendered := renderMergedGameYAML([]catalogGame{a, b}, newRegionRules(nil, nil))
	if !strings.Contains(rendered, "edition_of") {
		t.Fatal("relationship lost")
	}
}

func TestPortsOfMissingParentDoNotFoldDifferentGames(t *testing.T) {
	// One compilation on another platform can be the parent of separate ports.
	a := relationFixture(t, "Part One", "1", "port_of", "100")
	b := relationFixture(t, "Part Two", "2", "port_of", "100")
	if got := editionGroups([]catalogGame{a, b}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestUnscopedBaseCannotAnchorEditionFamily(t *testing.T) {
	a := relationFixture(t, "Base", "1", "", "")
	a.RelationshipIDs = nil
	b := relationFixture(t, "Edition", "2", "edition_of", "1")
	if got := editionGroups([]catalogGame{a, b}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestEditionOfLocalPortCanFoldWithoutForeignAncestor(t *testing.T) {
	a := relationFixture(t, "Local Base", "1", "port_of", "100")
	b := relationFixture(t, "Local Edition", "2", "expanded_edition_of", "1")
	got := editionGroups([]catalogGame{a, b})
	if len(got) != 1 || got[0].Keep.Folder != "Local Base" || got[0].Evidence[0].Value != "1" {
		t.Fatal(got)
	}
}

func TestEditionFoldApplyKeepsDifferentContentSeparate(t *testing.T) {
	root := t.TempDir()
	for _, platform := range []string{"Platform A", "Platform B"} {
		for _, item := range []struct{ title, id, relation string }{
			{"Base", "1", ""},
			{"Edition", "2", "        relationships: [{type: edition_of, target_id: '1'}]\n"},
		} {
			contents := fmt.Sprintf("title: %s\nreleases:\n  %s (USA): {title: %s}\nsources:\n  igdb:\n    matches:\n      - igdb_id: %s\n        release: %s (USA)\n        platforms: [Test]\n        match_score: 146\n%s", item.title, item.title, item.title, item.id, item.title, item.relation)
			writeFoldFile(t, filepath.Join(root, "platforms", platform, item.title, "game.yaml"), contents)
			writeFoldFile(t, filepath.Join(root, "platforms", platform, item.title, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: %s (USA)\n    md5: %s\n", item.title, strings.Repeat(item.id, 32)))
		}
	}
	result, err := Fold(Options{DataRoot: root, Platform: "Platform A", EditionsOnly: true, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.AppliedGroupCount != 0 || result.HeldGroupCount != 1 || len(result.Backups) != 0 {
		t.Fatal(result)
	}
	merged, err := readGameFull(catalogGame{Path: filepath.Join(root, "platforms", "Platform A", "Base")})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Releases) != 1 || merged.FingerprintCount != 1 || len(merged.SourceBlocks["igdb"].MatchBlocks) != 1 {
		t.Fatal("lost folded data")
	}
	other, err := readGameFull(catalogGame{Path: filepath.Join(root, "platforms", "Platform B", "Edition")})
	if err != nil || len(other.Releases) != 1 {
		t.Fatal("changed other platform")
	}
}
