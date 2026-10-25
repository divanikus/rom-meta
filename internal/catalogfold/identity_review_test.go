package catalogfold

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/dat"
	"rommeta/internal/taskprogress"
)

func identityReviewFixture(t *testing.T, root string) ReviewedGroup {
	t.Helper()
	for _, item := range []struct{ folder, name, title string }{{"Main", "Orbit - Special Edition (USA)", "Orbit - Special Edition"}, {"Regional", "Hoshi (Japan) (Kanzenban)", "Hoshi"}} {
		writeFoldGame(t, root, "Console", item.folder, item.name, item.title, "10")
		path := filepath.Join(root, "platforms", "Console", item.folder, "game.yaml")
		writeFoldFile(t, path, readFoldFile(t, path)+fmt.Sprintf("\nfold: {content_key: %q}\n", item.folder+"-key"))
		writeFoldFile(t, filepath.Join(root, "platforms", "Console", item.folder, "media", "cover.jpg"), item.folder+" original artwork")
	}
	g := reviewedFixture(t, root, "Console", "Main", "Regional")
	g.PrimaryCandidates = []string{"Main"}
	g.IdentityReview = &ReviewedIdentity{ContentKey: "reviewed." + strings.Repeat("a", 64), PreviousKeys: map[string]string{"Main": "Main-key", "Regional": "Regional-key"}, Releases: []string{"Orbit - Special Edition (USA)", "Hoshi (Japan) (Kanzenban)"}, References: []string{"https://example.org/publisher/edition-record"}}
	return g
}

func TestExplicitIdentityReviewPreservesPayloadsAndMedia(t *testing.T) {
	root := t.TempDir()
	g := identityReviewFixture(t, root)
	before := catalogBytes(t, root)
	ordinary, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || ordinary.AppliedGroupCount != 0 || !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("ordinary fold waived content boundaries", err, ordinary)
	}
	plan := writeReviewFixture(t, root, g)
	result, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true})
	if err != nil || result.AppliedGroupCount != 1 || result.MergedGameCount != 1 {
		t.Fatal(err, result)
	}
	game := readContentMap(t, filepath.Join(root, "platforms", "Console", "Main", "game.yaml"))
	if len(game["releases"].(map[string]any)) != 2 || game["fold"].(map[string]any)["content_key"] != g.IdentityReview.ContentKey {
		t.Fatal(game)
	}
	fp := readContentMap(t, filepath.Join(root, "platforms", "Console", "Main", "fingerprints.yaml"))
	if len(fp["items"].([]any)) != 2 {
		t.Fatal("fingerprints lost", fp)
	}
	media := contentFiles(t, filepath.Join(root, "platforms", "Console", "Main", "media"))
	if len(media) != 2 {
		t.Fatal("different colliding media lost", media)
	}
	for _, text := range []string{"Main original artwork", "Regional original artwork"} {
		found := false
		for _, value := range media {
			if value == sha256.Sum256([]byte(text)) {
				found = true
			}
		}
		if !found {
			t.Fatal("original artwork bytes lost", text, media)
		}
	}
	if _, err = Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true}); err == nil {
		t.Fatal("consumed identity review replayed")
	}
}

func TestIdentityReviewRequiresCompleteAcknowledgedEvidence(t *testing.T) {
	for _, bad := range []string{"missing release", "unknown release", "duplicate release", "missing old key", "changed old key", "extra old key", "no references", "invalid reference", "invalid key", "both review modes"} {
		t.Run(bad, func(t *testing.T) {
			root := t.TempDir()
			g := identityReviewFixture(t, root)
			r := g.IdentityReview
			switch bad {
			case "missing release":
				r.Releases = r.Releases[:1]
			case "unknown release":
				r.Releases = append(r.Releases, "unreviewed")
			case "duplicate release":
				r.Releases = append(r.Releases, r.Releases[0])
			case "missing old key":
				delete(r.PreviousKeys, "Main")
			case "changed old key":
				r.PreviousKeys["Regional"] = "different-key"
			case "extra old key":
				r.PreviousKeys["Unreviewed"] = ""
			case "no references":
				r.References = nil
			case "invalid reference":
				r.References = []string{"file:///unverified"}
			case "invalid key":
				r.ContentKey = "ordinary-key"
			case "both review modes":
				g.DevelopmentParent = "Main"
			}
			before := catalogBytes(t, root)
			if _, err := Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, g), Apply: true}); err == nil || !reflect.DeepEqual(before, catalogBytes(t, root)) {
				t.Fatal("incomplete identity decision wrote data", bad, err)
			}
		})
	}
}

func TestIdentityReviewStillSeparatesAddonsAndSupplements(t *testing.T) {
	for _, role := range []string{"DLC", "Expansion", "Bonus Disc"} {
		t.Run(role, func(t *testing.T) {
			root := t.TempDir()
			g := identityReviewFixture(t, root)
			name := "Orbit (Japan) (" + role + ")"
			writeFoldGame(t, root, "Console", "Regional", name, "Orbit", "10")
			g = reviewedFixture(t, root, "Console", "Main", "Regional")
			g.IdentityReview = &ReviewedIdentity{ContentKey: "reviewed." + strings.Repeat("b", 64), PreviousKeys: map[string]string{"Main": "Main-key", "Regional": ""}, Releases: []string{"Orbit - Special Edition (USA)", name}, References: []string{"https://example.org/evidence"}}
			before := catalogBytes(t, root)
			if _, err := Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, g), Apply: true}); err == nil || !reflect.DeepEqual(before, catalogBytes(t, root)) {
				t.Fatal("separate product absorbed", role, err)
			}
		})
	}
}

func TestIdentityReviewRechecksStagedMembershipAndLiveHashes(t *testing.T) {
	root := t.TempDir()
	g := identityReviewFixture(t, root)
	group := Group{Platform: "Console", Keep: GameRef{Folder: "Main"}, Merge: []GameRef{{Folder: "Regional"}}, IdentityReview: g.IdentityReview}
	before := catalogBytes(t, root)
	if err := applyGroup(root, &group, newRegionRules(nil, nil)); err != nil || !group.Skipped || !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("unreviewed group used identity override", err, group)
	}
	plan := writeReviewFixture(t, root, g)
	path := filepath.Join(root, "platforms", "Console", "Regional", "game.yaml")
	original := readFoldFile(t, path)
	changed := false
	_, err := Fold(Options{DataRoot: root, ReviewPlan: plan, Apply: true, Progress: func(s taskprogress.Snapshot) {
		if s.Stage == "committing" {
			changed = true
			writeFoldFile(t, path, original+"\ncurated_note: concurrent edit\n")
		}
	}})
	if !changed || err == nil || !strings.Contains(err.Error(), "stale") || readFoldFile(t, path) != original+"\ncurated_note: concurrent edit\n" {
		t.Fatal("concurrent live edit lost", err)
	}
}

func TestIdentityReviewSurvivesImportWithoutAdmittingUnknownEditions(t *testing.T) {
	root := t.TempDir()
	names := []string{"Orbit (USA) (Special Edition)", "Tsuki (Japan) (Kanzenban)"}
	path := contentDAT(t, names...)
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	if _, err := dat.Import(path, options); err != nil {
		t.Fatal(err)
	}
	platform := "Sony - PlayStation 2"
	dir := filepath.Join(root, "platforms", platform)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var folders []string
	expected := map[string]any{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		folder := entry.Name()
		folders = append(folders, folder)
		gp := filepath.Join(dir, folder, "game.yaml")
		game := readContentMap(t, gp)
		for name, raw := range game["releases"].(map[string]any) {
			raw.(map[string]any)["curated_note"] = "preserve exact " + name
			expected[name] = raw
		}
		writeContentMap(t, gp, game)
		writeFoldFile(t, filepath.Join(dir, folder, "media", "cover.jpg"), folder+" artwork")
	}
	if len(folders) != 2 {
		t.Fatal(folders)
	}
	group := reviewedFixture(t, root, platform, folders...)
	group.PrimaryCandidates = []string{folders[0]}
	group.IdentityReview = &ReviewedIdentity{ContentKey: "reviewed." + strings.Repeat("c", 64), PreviousKeys: map[string]string{folders[0]: "", folders[1]: ""}, Releases: names, References: []string{"https://example.org/independent-regional-edition-proof"}}
	result, err := Fold(Options{DataRoot: root, Apply: true, ReviewPlan: writeReviewFixture(t, root, group)})
	if err != nil || result.AppliedGroupCount != 1 {
		t.Fatal(err, result)
	}
	keep := filepath.Join(dir, folders[0])
	beforeGame := readContentMap(t, filepath.Join(keep, "game.yaml"))
	beforeFP := readContentMap(t, filepath.Join(keep, "fingerprints.yaml"))
	beforeMedia := contentFiles(t, filepath.Join(keep, "media"))
	for i := 0; i < 2; i++ {
		if _, err = dat.Import(path, options); err != nil {
			t.Fatal("known reviewed regional edition could not refresh", err)
		}
		game := readContentMap(t, filepath.Join(keep, "game.yaml"))
		fp := readContentMap(t, filepath.Join(keep, "fingerprints.yaml"))
		if !reflect.DeepEqual(game["releases"], expected) || !reflect.DeepEqual(game["fold"], beforeGame["fold"]) || !reflect.DeepEqual(fp, beforeFP) || !reflect.DeepEqual(contentFiles(t, filepath.Join(keep, "media")), beforeMedia) {
			t.Fatal("reviewed import lost release/fingerprint/media payload")
		}
	}
	before := catalogBytes(t, root)
	newPath := contentDAT(t, append(names, "Orbit (Europe) (Special Edition)")...)
	if _, err = dat.Import(newPath, options); err == nil || !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("unreviewed new edition was admitted", err)
	}
}

func TestImportCompleteEditionRetainsBothDiscsAndSeparateStandaloneAddon(t *testing.T) {
	root := t.TempDir()
	names := []string{"Orbit - Game of the Year Edition (USA) (Disc 1)", "Orbit - Game of the Year Edition (USA) (Add-On Content Disc)", "Orbit - Game of the Year Edition (Japan) (Tsuika Contents Disc)", "Orbit (USA)", "Orbit - Add-On Content Pack (USA)"}
	path := contentDAT(t, names...)
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	result, err := dat.Import(path, options)
	if err != nil || result.GameBundleCount != 3 {
		t.Fatal(err, result)
	}
	dir := filepath.Join(root, "platforms", "Sony - PlayStation 2", "Orbit - Game of the Year Edition")
	game := readContentMap(t, filepath.Join(dir, "game.yaml"))
	if len(game["releases"].(map[string]any)) != 3 {
		t.Fatal(game)
	}
	writeFoldFile(t, filepath.Join(dir, "media", "art.png"), "retained art")
	beforeGame := game["releases"]
	beforeFP := readContentMap(t, filepath.Join(dir, "fingerprints.yaml"))
	beforeMedia := contentFiles(t, filepath.Join(dir, "media"))
	for i := 0; i < 2; i++ {
		if _, err = dat.Import(path, options); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(readContentMap(t, filepath.Join(dir, "game.yaml"))["releases"], beforeGame) || !reflect.DeepEqual(readContentMap(t, filepath.Join(dir, "fingerprints.yaml")), beforeFP) || !reflect.DeepEqual(contentFiles(t, filepath.Join(dir, "media")), beforeMedia) {
			t.Fatal("package repeat import lost payload")
		}
	}
}
