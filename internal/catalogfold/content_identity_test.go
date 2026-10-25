package catalogfold

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func catalogBytes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(filepath.Join(root, "platforms"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestContentBoundariesCannotBeOverridden(t *testing.T) {
	for _, variant := range []string{"DLC", "Expansion", "Bonus Disc", "Special Edition", "Unclassified Edition", "mixed", "relationship", "provider type", "compilation"} {
		for _, mode := range []string{"automatic", "interactive", "review plan"} {
			t.Run(variant+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				other := "Orbit (Japan) (" + variant + ")"
				if variant == "mixed" || variant == "relationship" || variant == "provider type" || variant == "compilation" {
					other = "Orbit (Japan)"
				}
				writeFoldGame(t, root, "Console", "Full", "Orbit (USA)", "Orbit", "10")
				writeFoldGame(t, root, "Console", "Other", other, "Orbit", "10")
				path := filepath.Join(root, "platforms", "Console", "Other", "game.yaml")
				data := readFoldFile(t, path)
				switch variant {
				case "mixed":
					data = strings.Replace(data, "releases:\n", "releases:\n  Orbit (Europe) (DLC): {tags: [dlc]}\n", 1)
				case "relationship":
					data += "  igdb:\n    matches:\n      - igdb_id: 2\n        platforms: [Console]\n        relationships: [{type: expanded_edition_of, target_id: '1'}]\n"
					base := filepath.Join(root, "platforms", "Console", "Full", "game.yaml")
					writeFoldFile(t, base, readFoldFile(t, base)+"  igdb:\n    matches:\n      - igdb_id: 1\n        platforms: [Console]\n")
				case "compilation":
					data = strings.Replace(data, "        database_id:", "        game_type: Compilation\n        database_id:", 1)
				case "provider type":
					data = strings.Replace(data, "        database_id:", "        game_type: DLC / Addon\n        database_id:", 1)
				}
				writeFoldFile(t, path, data)
				writeFoldFile(t, filepath.Join(root, "platforms", "Console", "Other", "media", "cover.jpg"), "local artwork")
				before := catalogBytes(t, root)
				options := Options{DataRoot: root, Apply: true}
				calls := 0
				if mode == "interactive" {
					options.Apply = false
					options.Review = func(ReviewPrompt) (ReviewDecision, error) { calls++; return ReviewApply, nil }
				}
				if mode == "review plan" {
					options.ReviewPlan = writeReviewFixture(t, root, reviewedFixture(t, root, "Console", "Full", "Other"))
				}
				result, err := Fold(options)
				if mode == "review plan" {
					if err == nil {
						t.Fatal("incompatible reviewed plan accepted")
					}
				} else if err != nil || result.HeldGroupCount != 1 {
					t.Fatal(err, result)
				}
				if result.AppliedGroupCount != 0 || len(result.Backups) != 0 {
					t.Fatal("content gate bypassed", result, calls)
				}
				if !reflect.DeepEqual(before, catalogBytes(t, root)) {
					t.Fatal("rejected merge modified catalog or media")
				}
			})
		}
	}
}
func TestIndependentRegionalConfirmation(t *testing.T) {
	for _, mode := range []string{"one broad ID", "two direct providers", "direct plus plain alias", "two plain aliases", "one plain alias", "unscoped IGDB", "Hasheous bridge"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := "  launchbox:\n    matches:\n      - database_id: '10'\n        title: English Game\n"
			target := "  launchbox:\n    matches:\n      - database_id: '10'\n        title: Nippon Game\n"
			applied, held, groups := 0, 1, 1
			switch mode {
			case "two direct providers":
				source += "  igdb:\n    matches:\n      - igdb_id: '20'\n        platforms: [Console]\n"
				target += "  igdb:\n    matches:\n      - igdb_id: '20'\n        platforms: [Console]\n"
				applied, held = 1, 0
			case "direct plus plain alias":
				source += "  igdb:\n    matches:\n      - igdb_id: '20'\n        title: English Game\n        platforms: [Console]\n        alternate_titles: ['Nippon Game']\n"
				applied, held = 1, 0
			case "two plain aliases":
				source += "        alternate_titles: ['Nippon Game']\n  igdb:\n    matches:\n      - igdb_id: '20'\n        title: English Game\n        platforms: [Console]\n        alternate_titles: ['Nippon Game']\n"
				target = ""
				applied, held = 1, 0
			case "one plain alias":
				source += "        alternate_titles: ['Nippon Game']\n"
				target = ""
			case "unscoped IGDB":
				source = "  igdb:\n    matches:\n      - igdb_id: '20'\n"
				target = source
				held, groups = 0, 0
			case "Hasheous bridge":
				source = "  hasheous:\n    matches:\n      - game_id: '30'\n        igdb_id: '20'\n"
				target = source
				held, groups = 0, 0
			}
			writeRegionalGame(t, root, "Console", "English Game", "USA", "", source)
			writeRegionalGame(t, root, "Console", "Nippon Game", "Japan", "", target)
			result, err := Fold(Options{DataRoot: root, Apply: true})
			if err != nil || result.AppliedGroupCount != applied || result.HeldGroupCount != held || result.CandidateGroupCount != groups {
				t.Fatal(err, result)
			}
		})
	}
}
func TestRegionalConfirmationDoesNotPropagateThroughThirdGame(t *testing.T) {
	root := t.TempDir()
	for _, pair := range []struct{ title, region, source, field, id, alias, aliasRegion string }{
		{"A", "USA", "launchbox", "database_id", "1", "B", "Japan"},
		{"B", "Japan", "igdb", "igdb_id", "2", "C", "Europe"},
		{"C", "Europe", "", "", "", "", ""},
	} {
		source := ""
		if pair.source != "" {
			source = fmt.Sprintf("  %s:\n    matches:\n      - %s: '%s'\n        title: %s\n        platforms: [Console]\n        titles: [{title: %s, region: %s}]\n", pair.source, pair.field, pair.id, pair.title, pair.alias, pair.aliasRegion)
		}
		writeRegionalGame(t, root, "Console", pair.title, pair.region, "", source)
	}
	before := catalogBytes(t, root)
	result, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.CandidateGroupCount != 1 || result.HeldGroupCount != 1 || result.AppliedGroupCount != 0 || !strings.Contains(result.Groups[0].HoldReason, "no independent regional identity") {
		t.Fatal(err, result)
	}
	if !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("unconfirmed chain modified files")
	}
}

func TestStagedFoldRechecksIdentityAndContent(t *testing.T) {
	for _, mode := range []string{"changed ID", "changed reviewed content", "changed PS3 serial"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			platform := "Console"
			id, release, review := "20", "Orbit (Japan)", ""
			if mode == "changed reviewed content" {
				id = "10"
				release += " (DLC)"
				review = "Previously checked regional releases"
			}
			if mode == "changed PS3 serial" {
				platform = "Sony - PlayStation 3"
				writeSerialGame(t, root, platform, "Full", "USA", "BLUS30198", "", "", serialProvider("10"))
				writeSerialGame(t, root, platform, "Other", "Japan", "BLJM99999", "", "", serialProvider("10"))
			} else {
				writeFoldGame(t, root, platform, "Full", "Orbit (USA)", "Orbit", "10")
				writeFoldGame(t, root, platform, "Other", release, "Orbit", id)
			}
			before := catalogBytes(t, root)
			group := Group{Platform: platform, Keep: GameRef{Folder: "Full"}, Merge: []GameRef{{Folder: "Other"}}, ReviewReason: review, ReviewAccepted: true}
			if err := applyGroup(root, &group, newRegionRules(nil, nil)); err != nil {
				t.Fatal(err)
			}
			if !group.Skipped || group.SkipReason == "" {
				t.Fatal("stale staged proposal applied", group)
			}
			if !reflect.DeepEqual(before, catalogBytes(t, root)) {
				t.Fatal("staged guard changed files")
			}
		})
	}
}
