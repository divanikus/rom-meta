package catalogfold

import (
	"fmt"
	"path/filepath"
	"reflect"
	"rommeta/internal/taskprogress"
	"strings"
	"testing"
)

func TestConfirmedDevelopmentReleasesFoldInAllModes(t *testing.T) {
	for _, mode := range []string{"automatic", "interactive", "review plan"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeFoldGame(t, root, "Console", "Main", "Orbit (Japan)", "Orbit", "10")
			writeFoldGame(t, root, "Console", "Early", "Orbit (USA) (Demo)", "Orbit (Demo)", "10")
			writeFoldGame(t, root, "Console", "Beta", "Orbit (Europe) (Beta) (2001-01-01)", "Orbit (Beta)", "10")
			writeFoldFile(t, filepath.Join(root, "platforms", "Console", "Early", "media", "cover.jpg"), "early artwork")
			options := Options{DataRoot: root, Apply: true}
			if mode == "interactive" {
				options.Apply = false
				options.Review = func(ReviewPrompt) (ReviewDecision, error) { return ReviewApply, nil }
			}
			if mode == "review plan" {
				options.ReviewPlan = writeReviewFixture(t, root, reviewedFixture(t, root, "Console", "Main", "Early", "Beta"))
			}
			result, err := Fold(options)
			if err != nil || result.AppliedGroupCount != 1 || result.MergedGameCount != 2 {
				t.Fatal(err, result)
			}
			game := readContentMap(t, filepath.Join(root, "platforms", "Console", "Main", "game.yaml"))
			if len(game["releases"].(map[string]any)) != 3 || game["default_release"] != "Orbit (Japan)" {
				t.Fatal(game)
			}
			if readFoldFile(t, filepath.Join(root, "platforms", "Console", "Main", "media", "cover.jpg")) != "early artwork" {
				t.Fatal("early media lost")
			}
		})
	}
}

func TestReviewedDevelopmentParentRetiresOnlyEarlyBuildKeys(t *testing.T) {
	for _, member := range []string{"Demo", "Beta", "Proto", "Promo", "Special Edition", "DLC", "Bonus Disc", "retail", "unknown"} {
		t.Run(member, func(t *testing.T) {
			root := t.TempDir()
			writeFoldGame(t, root, "Console", "Main", "Orbit (Japan)", "Orbit", "10")
			name := "Orbit (USA) (" + member + ")"
			if member == "retail" {
				name = "Orbit (USA)"
			}
			writeFoldGame(t, root, "Console", "Early", name, "Orbit", "10")
			for _, folder := range []string{"Main", "Early"} {
				path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
				writeFoldFile(t, path, readFoldFile(t, path)+fmt.Sprintf("\nfold: {content_key: %q}\n", folder+"-key"))
				writeFoldFile(t, filepath.Join(root, "platforms", "Console", folder, "media", "cover.jpg"), folder+" art")
			}
			before := catalogBytes(t, root)
			ordinary, err := Fold(Options{DataRoot: root, Apply: true})
			if err != nil || ordinary.AppliedGroupCount != 0 || !reflect.DeepEqual(before, catalogBytes(t, root)) {
				t.Fatal("ordinary fold retired keys", err, ordinary)
			}
			group := reviewedFixture(t, root, "Console", "Main", "Early")
			group.DevelopmentParent = "Main"
			plan := writeReviewFixture(t, root, group)
			result, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true})
			allowed := member == "Demo" || member == "Beta" || member == "Proto" || member == "Promo"
			if !allowed {
				if err == nil || !reflect.DeepEqual(before, catalogBytes(t, root)) {
					t.Fatal("non-development boundary bypassed", err, result)
				}
				return
			}
			if err != nil || result.AppliedGroupCount != 1 {
				t.Fatal(err, result)
			}
			game := readContentMap(t, filepath.Join(root, "platforms", "Console", "Main", "game.yaml"))
			if game["fold"].(map[string]any)["content_key"] != "Main-key" || game["default_release"] != "Orbit (Japan)" {
				t.Fatal(game)
			}
			media := contentFiles(t, filepath.Join(root, "platforms", "Console", "Main", "media"))
			if len(media) != 2 {
				t.Fatal("colliding early artwork lost", media)
			}
			if _, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true}); err == nil {
				t.Fatal("consumed parent plan replayed")
			}
		})
	}
}

func TestReviewedDevelopmentParentIsBoundAndRechecked(t *testing.T) {
	root := t.TempDir()
	writeFoldGame(t, root, "Console", "Main", "Orbit (USA)", "Orbit", "10")
	writeFoldGame(t, root, "Console", "Early", "Orbit (Japan) (DLC)", "Orbit", "10")
	before := catalogBytes(t, root)
	group := Group{Platform: "Console", Keep: GameRef{Folder: "Main"}, Merge: []GameRef{{Folder: "Early"}}, DevelopmentParent: "Main", ReviewReason: "Prior demo review"}
	if err := applyGroup(root, &group, newRegionRules(nil, nil)); err != nil || !group.Skipped || !strings.Contains(group.SkipReason, "development") || !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("staged changed role bypassed parent guard", err, group)
	}
	reviewed := reviewedFixture(t, root, "Console", "Main", "Early")
	reviewed.DevelopmentParent = "Missing"
	if _, err := Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, reviewed), Apply: true}); err == nil {
		t.Fatal("missing parent accepted")
	}
}

func TestReviewedDevelopmentPlanRechecksLiveInputsBeforeCommit(t *testing.T) {
	root := t.TempDir()
	writeFoldGame(t, root, "Console", "Main", "Orbit (USA)", "Orbit", "10")
	writeFoldGame(t, root, "Console", "Early", "Orbit (Japan) (Demo)", "Orbit", "10")
	group := reviewedFixture(t, root, "Console", "Main", "Early")
	group.DevelopmentParent = "Main"
	path := filepath.Join(root, "platforms", "Console", "Early", "game.yaml")
	original := readFoldFile(t, path)
	changed := false
	_, err := Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, group), Apply: true, Progress: func(s taskprogress.Snapshot) {
		if s.Stage == "committing" {
			changed = true
			writeFoldFile(t, path, original+"\ncurated_note: concurrent edit\n")
		}
	}})
	if !changed || err == nil || !strings.Contains(err.Error(), "stale") || readFoldFile(t, path) != original+"\ncurated_note: concurrent edit\n" {
		t.Fatal("live input overwritten", err)
	}
}

func TestReviewedDevelopmentParentCanResolveUnknownManufacturingQualifiers(t *testing.T) {
	root := t.TempDir()
	writeFoldGame(t, root, "Console", "Main", "Orbit (Japan) (Disc 1) (First Part) (1M)", "Orbit", "10")
	writeFoldGame(t, root, "Console", "Early", "Orbit (Japan) (Demo) (2MM1)", "Orbit", "10")
	group := reviewedFixture(t, root, "Console", "Main", "Early")
	group.DevelopmentParent = "Main"
	result, err := Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, group), Apply: true})
	if err != nil || result.AppliedGroupCount != 1 {
		t.Fatal(err, result)
	}
	// Explicit review resolves unknown manufacturing labels, retaining their data;
	// it must still reject a named commercial edition's demo in a different parent.
	root = t.TempDir()
	writeFoldGame(t, root, "Console", "Main", "Orbit (Japan)", "Orbit", "10")
	writeFoldGame(t, root, "Console", "Early", "Orbit (USA) (Demo) (Special Edition)", "Orbit", "10")
	group = reviewedFixture(t, root, "Console", "Main", "Early")
	group.DevelopmentParent = "Main"
	if _, err = Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, group), Apply: true}); err == nil {
		t.Fatal("known edition boundary waived")
	}
}
