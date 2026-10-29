package launchbox

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
			before := []byte("title: Example\nfetch: {preserve_sources: [launchbox]}\nreleases:\n  Example (USA): {}\nsources:\n  launchbox:\n    " + stanza + "\n")
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "fingerprints.yaml"), []byte("items: [{release: Example (USA), file: {size: 4, md5: 0123456789abcdef0123456789abcdef}}]\n"), 0600); err != nil {
				t.Fatal(err)
			}

			for pass := 0; pass < 2; pass++ {
				got, err := Import(ImportOptions{DataRoot: root, Apply: true, ForceOverwrite: true, ZipPath: filepath.Join(root, "missing.zip")})
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
