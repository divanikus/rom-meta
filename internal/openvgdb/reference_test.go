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
		{"Regional Name III", "Other Regional Name", "other-regional-name", false},
		{"Series III", "Series III", "series-ii", false},
		{"Series II", "Series III", "series-ii", false},
	} {
		candidate := releaseMetadata{Title: tc.title, ReferenceURL: "https://example.test/system/123-" + tc.slug}
		if got := conflictingReference(candidate, []releaseMetadata{candidate, {Title: tc.peer}}); got != tc.reject {
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
		if _, err := db.Exec(`insert into RELEASES (releaseID,romID,releaseTitleName,releaseReferenceURL,releaseCoverFront,releaseDescription) values (?,?,?,'https://example.test/cd/918280-ys-book-i-and-ii','https://example.test/wrong.jpg','Foreign fixture prose')`, id, id, title); err != nil {
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
