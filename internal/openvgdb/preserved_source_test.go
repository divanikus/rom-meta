package openvgdb

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestForceOverwritePreservesReviewedSource(t *testing.T) {
	for _, stanza := range []string{"status: not_found", "matches: [{title: Reviewed}]"} {
		t.Run(stanza, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Example", "game.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			before := []byte("title: Example\nfetch: {preserve_sources: [openvgdb]}\nreleases:\n  Example (USA): {}\nsources:\n  openvgdb:\n    " + stanza + "\n")
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "fingerprints.yaml"), []byte("items: [{release: Example (USA), file: {size: 4, md5: 0123456789abcdef0123456789abcdef}}]\n"), 0600); err != nil {
				t.Fatal(err)
			}

			for pass := 0; pass < 2; pass++ {
				got, err := Import(ImportOptions{DataRoot: root, Apply: true, ForceOverwrite: true, DBPath: filepath.Join(root, "missing.sqlite")})
				if err != nil || got.SkippedPreservedCount != 1 || got.WrittenFileCount != 0 || got.GameFileCount != 0 || got.NotFoundGameCount != 0 {
					t.Fatalf("pass %d: %#v, %v", pass, got, err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, before) {
					t.Fatalf("reviewed source changed: %s %v", after, err)
				}
			}

		})
	}
}

func TestRemovingPreserveControlAllowsForcedRefresh(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte("title: Star Fox 64\nfetch: {preserve_sources: [openvgdb]}\nreleases: {Star Fox 64 (USA): {}}\nsources: {openvgdb: {status: not_found}}\n")
	if err := os.WriteFile(p, before, 0600); err != nil {
		t.Fatal(err)
	}
	db := createOpenVGDBFixture(t)
	got, err := Import(ImportOptions{DataRoot: root, DBPath: db, Apply: true, ForceOverwrite: true})
	if err != nil || got.SkippedPreservedCount != 1 {
		t.Fatalf("preserved %#v %v", got, err)
	}
	if err := os.WriteFile(p, bytes.ReplaceAll(before, []byte("preserve_sources: [openvgdb]"), []byte("preserve_sources: []")), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = Import(ImportOptions{DataRoot: root, DBPath: db, Apply: true, ForceOverwrite: true})
	if err != nil || got.SkippedPreservedCount != 0 || got.WrittenFileCount != 1 || got.MatchedReleaseCount != 1 {
		t.Fatalf("unlock failed %#v %v", got, err)
	}
}
