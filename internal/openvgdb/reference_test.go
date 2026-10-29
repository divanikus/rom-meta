package openvgdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConflictingReferenceRequiresIndependentPeerEvidence(t *testing.T) {
	for _, tc := range []struct {
		title, peer, slug string
		reject            bool
	}{
		{"Ys III", "Ys I & II", "ys-book-i-and-ii", true},
		{"Street Fighter Zero 2'", "Street Fighter Zero 2", "street-fighter-alpha-2", true},
		{"Series IV", "Series 4", "series-4", false},
		{"Series II (Disc 1)", "Series II", "series-ii", false},
		{"Regional Name III", "Other Regional Name", "other-regional-name", false},
		{"Legend 2 Episode 2", "Legend 2", "legend-2", false},
		{"Regional Series III", "Different Series II", "different-series-ii", false},
		{"Series III", "Series III", "series-ii", false},
		{"Series II", "Series III", "series-ii", false},
	} {
		candidate := releaseMetadata{Title: tc.title, Region: "Japan", ReferenceURL: "https://example.test/system/123-" + tc.slug}
		if got := conflictingReference(candidate, []releaseMetadata{candidate, {Title: tc.peer, Region: "Japan"}}); got != tc.reject {
			t.Fatalf("%#v: reject=%v", tc, got)
		}
	}
}

func TestForcedRefreshRejectsForeignReferencesBeforeNameAndHash(t *testing.T) {
	root := t.TempDir()
	snapshot := createOpenVGDBFixture(t)
	db, err := sql.Open("sqlite", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, title := range []string{"Ys I & II", "Ys III"} {
		path := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", title, "game.yaml")
		writeFileForTest(t, path, "title: "+title+"\nowner_note: keep\nreleases:\n  "+title+" (Japan): {}\nsources:\n  openvgdb: {matches: [{title: Wrong}]}\n  launchbox: {status: not_found}\n")
	}
	for i, title := range []string{"Ys I & II", "Ys III"} {
		id := 100 + i
		md5 := strings.Repeat("a", 32)
		if i == 0 {
			md5 = strings.Repeat("b", 32)
		}
		if _, err := db.Exec(`insert into ROMs (romID,systemID,romExtensionlessFileName,romFileName,romHashMD5) values (?,23,?,?,?)`, id, title+" (Japan)", title+" (Japan).cue", md5); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`insert into RELEASES (releaseID,romID,releaseTitleName,TEMPregionLocalizedName,releaseReferenceURL,releaseCoverFront,releaseDescription) values (?,?,?,'Japan','https://example.test/cd/918280-ys-book-i-and-ii','https://example.test/wrong.jpg','Foreign fixture prose')`, id, id, title); err != nil {
			t.Fatal(err)
		}
	}
	// Even a hash pointing at a polluted row cannot validate that row's metadata.
	writeFileForTest(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Ys III", "fingerprints.yaml"), "items:\n  - release: Ys III (Japan)\n    file: {md5: "+strings.Repeat("a", 32)+"}\n")
	for pass := 0; pass < 2; pass++ {
		got, err := Import(ImportOptions{DataRoot: root, DBPath: snapshot, Apply: true, ForceOverwrite: true})
		if err != nil || got.MatchedReleaseCount != 1 || got.NotFoundGameCount != 1 {
			t.Fatalf("pass %d: %#v %v", pass, got, err)
		}
		text, err := os.ReadFile(filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Ys III", "game.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(text), "status: not_found") || strings.Contains(string(text), "Foreign fixture") || !strings.Contains(string(text), "owner_note: keep") || !strings.Contains(string(text), "launchbox:") {
			t.Fatalf("foreign row returned: %s", text)
		}
	}
	// A corrected snapshot is accepted on the next forced refresh, without unlocking.
	if _, err := db.Exec(`update RELEASES set releaseReferenceURL='https://example.test/cd/999-ys-iii',releaseDescription='Correct fixture prose' where romID=101`); err != nil {
		t.Fatal(err)
	}
	got, err := Import(ImportOptions{DataRoot: root, DBPath: snapshot, Apply: true, ForceOverwrite: true})
	if err != nil || got.MatchedReleaseCount != 2 {
		t.Fatalf("corrected snapshot: %#v %v", got, err)
	}
}

func TestCatalogReferenceAllowsKnownRegionalNumberingAndReissues(t *testing.T) {
	for _, titles := range [][2]string{{"Series '88", "Series '90"}, {"Regional Series III", "Series II"}, {"Budget 1500 Series Vol. 5 - Series II", "Series II"}} {
		good := releaseMetadata{Title: titles[1], ReferenceURL: "https://example.test/123-series-ii"}
		bad := releaseMetadata{Title: titles[0], ReferenceURL: good.ReferenceURL}
		index := platformReleaseIndex{byName: map[string]releaseMetadata{"known": good}, conflicts: &platformReleaseIndex{byName: map[string]releaseMetadata{"regional": bad}}}
		if _, ok := index.matchCatalogReference("regional", nil, []string{"regional", "known"}); !ok {
			t.Fatalf("catalog association rejected: %v", titles)
		}
		if _, ok := index.matchCatalogReference("regional", nil, []string{"regional"}); ok {
			t.Fatal("foreign card established identity")
		}
	}
	good := releaseMetadata{Title: "Series II", ReferenceURL: "https://example.test/123-series-ii"}
	prime := releaseMetadata{Title: "Series II'", ReferenceURL: good.ReferenceURL}
	index := platformReleaseIndex{byName: map[string]releaseMetadata{"known": good}, conflicts: &platformReleaseIndex{byName: map[string]releaseMetadata{"prime": prime}}}
	if _, ok := index.matchCatalogReference("prime", nil, []string{"prime", "known"}); ok {
		t.Fatal("association erased prime edition")
	}
}

func TestCatalogReferenceRejectsBorrowedRegionalPredecessor(t *testing.T) {
	bad := releaseMetadata{ROMFileName: "Regional Gallery 3 (Japan).gbc", Title: "Regional Gallery 3", ReferenceURL: "https://example.test/123-world-gallery-2"}
	good := releaseMetadata{ROMFileName: "World Gallery 3 (USA).gbc", Title: "World Gallery 3", ReferenceURL: "https://example.test/456-world-gallery-3"}
	index := platformReleaseIndex{byName: map[string]releaseMetadata{"regional": bad, "world": good}, conflicts: &platformReleaseIndex{byName: map[string]releaseMetadata{"regional gallery 3 (japan)": bad}}}
	if !index.cardReferenceConflict(bad, nil, []string{"regional", "world"}) {
		t.Fatal("borrowed predecessor retained")
	}
	if index.cardReferenceConflict(bad, nil, []string{"regional"}) {
		t.Fatal("unproved regional identity rejected")
	}
}

func TestReferenceNumberingAloneCannotRejectRegionalAliases(t *testing.T) {
	c := releaseMetadata{Title: "Series 2", Region: "Japan", ReferenceURL: "https://example.test/123-series-90"}
	for _, region := range []string{"USA", "", "World"} {
		if conflictingReference(c, []releaseMetadata{c, {Title: "Series 90", Region: region}}) {
			t.Fatalf("regional renumbering rejected: %s", region)
		}
	}
}
