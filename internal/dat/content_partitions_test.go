package dat

import (
	"reflect"
	"testing"
)

func TestBundleContentPartitionsAreOrderIndependent(t *testing.T) {
	games := []datGame{{Name: "Orbit (USA)", ID: "1"}, {Name: "Tsuki (Japan)", ID: "2", CloneOf: "1"}, {Name: "Orbit (USA) (Demo)", ID: "3", CloneOf: "1"}, {Name: "Orbit (Japan) (Special Edition)", ID: "4", CloneOf: "1"}, {Name: "Orbit (Europe) (Rev 2)", ID: "5", CloneOf: "1"}}
	expected := bundleGames(games)
	if len(expected) != 2 {
		t.Fatal(expected)
	}
	for i := 0; i < len(games); i++ {
		games = append(games[1:], games[0])
		if actual := bundleGames(games); !reflect.DeepEqual(expected, actual) {
			t.Fatalf("input order changed partition: %+v", actual)
		}
	}
}

func TestReviewedContentAnchorsSurviveChangedDATIDs(t *testing.T) {
	base, err := identifyExistingBundle("Main", []byte("title: Orbit\nfold: {content_key: main-key}\nreleases:\n  Orbit (USA):\n    authority: {source: redump, entry_id: '1', entry_name: Orbit (USA)}\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	edition, err := identifyExistingBundle("Expanded", []byte("title: Orbit\nfold: {content_key: expanded-key}\nreleases:\n  Orbit (Japan):\n    authority: {source: redump, entry_id: '2', entry_name: Orbit (Japan)}\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	incoming := newBundleIdentity{folder: "Incoming", entryIDs: map[string]bool{scopedIdentity("redump", "1"): true, scopedIdentity("redump", "999"): true}, entryNames: map[string]bool{"orbit (usa)": true, "orbit (japan)": true}}
	index := indexExistingBundles([]existingBundle{base, edition})
	if len(strongestCandidates(incoming, index)) != 1 {
		t.Fatal("fixture must reproduce a stronger partial ID match")
	}
	if reviewedAnchorConflict(incoming, index) == "" {
		t.Fatal("changed ID erased reviewed boundary")
	}
	edition.contentKey = "main-key"
	if reason := reviewedAnchorConflict(incoming, indexExistingBundles([]existingBundle{base, edition})); reason != "" {
		t.Fatal("equal keys are not a conflict", reason)
	}
}

func TestReviewedAnchorGuardDoesNotTreatSharedBytesAsConflictingNames(t *testing.T) {
	base := existingBundle{folder: "Main", contentKey: "main-key", entryIDs: map[string]bool{"1": true}, entryNames: map[string]bool{"main": true}, releaseSignatures: map[string]bool{"same-image": true}}
	demo := existingBundle{folder: "Demo", contentKey: "demo-key", entryNames: map[string]bool{"demo": true}, releaseSignatures: map[string]bool{"same-image": true}}
	incoming := newBundleIdentity{folder: "Main", entryIDs: map[string]bool{"1": true}, entryNames: map[string]bool{"main": true}, releaseSignatures: map[string]bool{"same-image": true}}
	if reason := reviewedAnchorConflict(incoming, indexExistingBundles([]existingBundle{base, demo})); reason != "" {
		t.Fatal(reason)
	}
}

func TestDemoCompilationIssuesKeepTheirProductIdentity(t *testing.T) {
	games := []datGame{
		{Name: "Console Magazine - Demo Disk 01-2007 (Europe)", ID: "1"},
		{Name: "Console Magazine - Demo Disk 02-2007 (Europe)", ID: "2", CloneOf: "1"},
	}
	if got := bundleGames(games); len(got) != 2 {
		t.Fatal("different compilation issues merged", got)
	}
}

func TestKnownReviewedDATPartitionsRejectUnownedOrConflictingReleases(t *testing.T) {
	base := existingBundle{folder: "Main", contentKey: "main", entryIDs: map[string]bool{scopedIdentity("redump", "1"): true}, entryNames: map[string]bool{"orbit (usa)": true}}
	other := existingBundle{folder: "Edition", contentKey: "edition", entryIDs: map[string]bool{scopedIdentity("redump", "2"): true}, entryNames: map[string]bool{"orbit (japan)": true}}
	index := indexExistingBundles([]existingBundle{base, other})
	plan := Plan{Authority: "redump"}
	for _, mode := range []string{"known", "changed ID", "conflicting ID", "unknown"} {
		games := []datGame{{Name: "Orbit (USA)", ID: "1"}, {Name: "Orbit (Japan)", ID: "2"}}
		switch mode {
		case "changed ID":
			games[1].ID = "999"
		case "conflicting ID":
			games[1].ID = "1"
		case "unknown":
			games = append(games, datGame{Name: "Orbit (Europe)", ID: "3"})
		}
		got, err := partitionKnownReviewedBundles(plan, []gameBundle{{Title: "Orbit", Folder: "Orbit", Releases: games}}, index)
		if err != nil {
			t.Fatal(err)
		}
		want := 2
		if mode == "conflicting ID" || mode == "unknown" {
			want = 1
		}
		if len(got) != want {
			t.Fatal(mode, got)
		}
	}
}
