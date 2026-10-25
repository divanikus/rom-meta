package catalogfold

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func reviewedFixture(t *testing.T, root, platform string, folders ...string) ReviewedGroup {
	t.Helper()
	group := ReviewedGroup{Platform: platform, Reason: "Reviewed ownership"}
	for _, folder := range folders {
		hash := func(file string) string {
			return fmt.Sprintf("%x", sha256.Sum256([]byte(readFoldFile(t, filepath.Join(root, "platforms", platform, folder, file)))))
		}
		group.Members = append(group.Members, ReviewedMember{Folder: folder, GameSHA256: hash("game.yaml"), FingerprintsSHA256: hash("fingerprints.yaml")})
	}
	return group
}
func writeReviewFixture(t *testing.T, root string, groups ...ReviewedGroup) string {
	t.Helper()
	contents, err := json.Marshal(ReviewPlan{Version: 1, Groups: groups})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "review.json")
	writeFoldFile(t, path, string(contents))
	return path
}
func TestReviewedFoldPartitionsPrimaryAndPreservation(t *testing.T) {
	root := t.TempDir()
	for _, f := range []struct{ folder, release, id string }{{"Main", "Game (Japan)", "1"}, {"Artbook", "Artbook (USA)", "2"}, {"Other", "Other (USA)", "1"}} {
		writeFoldGame(t, root, "Console", f.folder, f.release, f.folder, f.id)
	}
	writeFoldFile(t, filepath.Join(root, "platforms", "Console", "Artbook", "notes.txt"), "supplement")
	other := readFoldFile(t, filepath.Join(root, "platforms", "Console", "Other", "game.yaml"))
	group := reviewedFixture(t, root, "Console", "Main", "Artbook")
	group.PrimaryCandidates = []string{"Main"}
	plan := writeReviewFixture(t, root, group)
	dry, err := Fold(Options{DataRoot: root, ReviewPlan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if dry.GameCount != 2 || dry.CandidateGroupCount != 1 || dry.Groups[0].Keep.Folder != "Main" {
		t.Fatalf("unexpected plan: %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", "Console", "Artbook")); err != nil {
		t.Fatal("dry run mutated folders")
	}
	result, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.AppliedGroupCount != 1 || result.MergedGameCount != 1 {
		t.Fatalf("unexpected apply: %+v", result)
	}
	main := readFoldFile(t, filepath.Join(root, "platforms", "Console", "Main", "game.yaml"))
	for _, text := range []string{`title: "Main"`, `default_release: "Game (Japan)"`, `Artbook (USA)`, `database_id: "1"`, `database_id: "2"`} {
		if !strings.Contains(main, text) {
			t.Fatalf("lost %s in %s", text, main)
		}
	}
	fp := readFoldFile(t, filepath.Join(root, "platforms", "Console", "Main", "fingerprints.yaml"))
	if !strings.Contains(fp, "Game (Japan)") || !strings.Contains(fp, "Artbook (USA)") {
		t.Fatal("lost fingerprints")
	}
	if readFoldFile(t, filepath.Join(root, "platforms", "Console", "Main", "notes.txt")) != "supplement" {
		t.Fatal("lost extra file")
	}
	if readFoldFile(t, filepath.Join(root, "platforms", "Console", "Other", "game.yaml")) != other {
		t.Fatal("unselected shared-ID card changed")
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", "Console", "Artbook")); !os.IsNotExist(err) {
		t.Fatal("merge folder remains")
	}
}
func TestReviewedFoldRejectsInvalidOrStalePlanBeforeWriting(t *testing.T) {
	for _, failure := range []string{"late game change", "late fingerprints change", "duplicate", "traversal", "unknown primary", "manual field", "conflicting release"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			for _, p := range []string{"A", "Z"} {
				for _, f := range []string{"One", "Two"} {
					writeFoldGame(t, root, p, f, f+" (USA)", f, "1")
				}
			}
			first := reviewedFixture(t, root, "A", "One", "Two")
			latePath := filepath.Join(root, "platforms", "Z", "Two", "game.yaml")
			if failure == "manual field" {
				writeFoldFile(t, latePath, readFoldFile(t, latePath)+"\nnotes: preserve me\n")
			}
			if failure == "conflicting release" {
				writeFoldGame(t, root, "Z", "Two", "One (USA)", "Different payload", "1")
			}
			last := reviewedFixture(t, root, "Z", "One", "Two")
			switch failure {
			case "late game change":
				writeFoldFile(t, latePath, readFoldFile(t, latePath)+"\n# changed\n")
			case "late fingerprints change":
				writeFoldFile(t, filepath.Join(root, "platforms", "Z", "Two", "fingerprints.yaml"), "items: []\n")
			case "duplicate":
				last = first
			case "traversal":
				last.Members[0].Folder = ".."
			case "unknown primary":
				last.PrimaryCandidates = []string{"Missing"}
			}
			plan := writeReviewFixture(t, root, first, last)
			before := readFoldFile(t, filepath.Join(root, "platforms", "A", "One", "game.yaml"))
			if _, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true}); err == nil {
				t.Fatal("invalid plan accepted")
			}
			if readFoldFile(t, filepath.Join(root, "platforms", "A", "One", "game.yaml")) != before {
				t.Fatal("earlier platform changed")
			}
			if _, err := os.Stat(filepath.Join(root, "platforms", "A", "Two")); err != nil {
				t.Fatal("earlier member removed")
			}
		})
	}
}
func TestReviewPlanStrictJSONAndExclusiveOptions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plan.json")
	for _, contents := range []string{`{"version":2,"groups":[]}`, `{"version":1,"groups":[],"typo":true}`, `{} {}`} {
		writeFoldFile(t, path, contents)
		if _, err := readReviewPlan(path); err == nil {
			t.Fatalf("accepted %s", contents)
		}
	}
	for _, o := range []Options{{Platform: "P"}, {Match: "M"}, {EditionsOnly: true}, {Review: func(ReviewPrompt) (ReviewDecision, error) { return ReviewSkip, nil }}} {
		o.DataRoot = root
		o.ReviewPlan = path
		if _, err := Fold(o); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestReviewedFoldRechecksStagedInputs(t *testing.T) {
	root := t.TempDir()
	for _, folder := range []string{"One", "Two"} {
		writeFoldGame(t, root, "Console", folder, folder+" (USA)", folder, "1")
	}
	path := filepath.Join(root, "platforms", "Console", "One", "game.yaml")
	plan := writeReviewFixture(t, root, reviewedFixture(t, root, "Console", "One", "Two"))
	updated := readFoldFile(t, path) + "\n# changed after preflight\n"
	_, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true, OnPlatformStart: func(int, int, string) { writeFoldFile(t, path, updated) }})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale staged input error, got %v", err)
	}
	if readFoldFile(t, path) != updated {
		t.Fatal("concurrent edit lost")
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", "Console", "Two")); err != nil {
		t.Fatal("merge committed despite changed inputs")
	}
}

func TestReviewedFoldPreservesHasheousReason(t *testing.T) {
	for _, positive := range []bool{false, true} {
		t.Run(fmt.Sprintf("positive=%v", positive), func(t *testing.T) {
			root := t.TempDir()
			for _, folder := range []string{"One", "Two"} {
				writeFoldGame(t, root, "Console", folder, folder+" (USA)", folder, "1")
			}
			first := filepath.Join(root, "platforms", "Console", "One", "game.yaml")
			diagnostic := "  hasheous:\n    status: not_found\n    checked_at: \"2026-01-01T00:00:00Z\"\n    reason: \"no usable fingerprints\"\n"
			writeFoldFile(t, first, readFoldFile(t, first)+diagnostic)
			if positive {
				second := filepath.Join(root, "platforms", "Console", "Two", "game.yaml")
				writeFoldFile(t, second, readFoldFile(t, second)+"  hasheous:\n    matches:\n      - hasheous_id: \"123\"\n        title: \"Two\"\n")
			}
			group := reviewedFixture(t, root, "Console", "One", "Two")
			group.PrimaryCandidates = []string{"One"}
			plan := writeReviewFixture(t, root, group)
			if _, err := Fold(Options{DataRoot: root, ReviewPlan: plan}); err != nil {
				t.Fatal(err)
			}
			result, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true})
			if err != nil || result.AppliedGroupCount != 1 {
				t.Fatalf("reviewed apply failed: %+v, %v", result, err)
			}
			merged := readFoldFile(t, first)
			if positive {
				if !strings.Contains(merged, `hasheous_id: "123"`) || strings.Contains(merged, "no usable fingerprints") {
					t.Fatalf("positive match did not replace obsolete diagnostic: %s", merged)
				}
			} else {
				var document map[string]any
				if err := yaml.Unmarshal([]byte(merged), &document); err != nil {
					t.Fatal(err)
				}
				sources := document["sources"].(map[string]any)
				source := sources["hasheous"].(map[string]any)
				for key, want := range map[string]string{"status": "not_found", "checked_at": "2026-01-01T00:00:00Z", "reason": "no usable fingerprints"} {
					if source[key] != want {
						t.Fatalf("diagnostic %s = %v, want %s", key, source[key], want)
					}
				}
			}
		})
	}
}

func TestReviewedFoldStillRejectsUnknownSourceFields(t *testing.T) {
	for _, stanza := range []string{
		"  hasheous:\n    status: not_found\n    reason: [manual, values]\n",
		"  hasheous:\n    status: not_found\n    private_note: preserve me\n",
		"  launchbox:\n    status: not_found\n    reason: manual field\n",
	} {
		t.Run(stanza, func(t *testing.T) {
			root := t.TempDir()
			for _, folder := range []string{"One", "Two"} {
				writeFoldGame(t, root, "Console", folder, folder+" (USA)", folder, "1")
			}
			path := filepath.Join(root, "platforms", "Console", "One", "game.yaml")
			// Replace the existing sources section to keep the fixture free of duplicate keys.
			original := readFoldFile(t, path)
			offset := strings.Index(original, "sources:\n")
			if offset < 0 {
				t.Fatal("fixture has no sources")
			}
			original = original[:offset] + "sources:\n" + stanza
			writeFoldFile(t, path, original)
			plan := writeReviewFixture(t, root, reviewedFixture(t, root, "Console", "One", "Two"))
			if _, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true}); err == nil {
				t.Fatal("unsupported source field accepted")
			}
			if readFoldFile(t, path) != original {
				t.Fatal("rejected plan modified metadata")
			}
			if _, err := os.Stat(filepath.Join(root, "platforms", "Console", "Two")); err != nil {
				t.Fatal("rejected plan removed a member")
			}
		})
	}
}
