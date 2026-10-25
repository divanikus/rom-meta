package hasheous

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rommeta/internal/catalogcache"
)

func TestHasLookupEvidence(t *testing.T) {
	valid := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name string
		game catalogcache.Game
		want bool
	}{
		{name: "list card"},
		{name: "invalid hash", game: catalogcache.Game{Fingerprints: []catalogcache.Fingerprint{{MD5: "bad"}}}},
		{name: "ROM hash", game: catalogcache.Game{Fingerprints: []catalogcache.Fingerprint{{SHA1: valid}}}, want: true},
		{name: "component without set", game: catalogcache.Game{Fingerprints: []catalogcache.Fingerprint{{SHA1: valid, PayloadKind: "rom-component"}}}},
		{name: "set without component hash", game: catalogcache.Game{Releases: []catalogcache.Release{{Name: "game", MAMESet: "game"}}}},
		{name: "MAME evidence", game: catalogcache.Game{Releases: []catalogcache.Release{{Name: "game", MAMESet: "game"}}, Fingerprints: []catalogcache.Fingerprint{{Release: "game", PayloadKind: "rom-component", SHA1: valid}}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasLookupEvidence(tc.game); got != tc.want {
				t.Fatalf("evidence=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestAutomaticImportSkipsEvidenceFreeCardsWithoutConsumingLimit(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			root := t.TempDir()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`null`))
			}))
			defer server.Close()
			writeText(t, filepath.Join(root, "config", "sources.yaml"), "sources:\n  hasheous:\n    enabled: true\n    api:\n      base_url: "+server.URL+"\n      rate_limit_per_second: 1000\n")
			original := "title: Game\ndefault_release: Game\nreleases:\n  Game:\n"
			for _, name := range []string{"A no hashes", "B valid hash"} {
				writeText(t, filepath.Join(root, "platforms", "ScummVM", name, "game.yaml"), original)
			}
			writeText(t, filepath.Join(root, "platforms", "ScummVM", "B valid hash", "fingerprints.yaml"), "items:\n  - release: Game\n    file:\n      name: Game.rom\n      md5: 11111111111111111111111111111111\n")
			result, err := Import(ImportOptions{DataRoot: root, Apply: apply, SkipWithoutEvidence: true, ForceOverwrite: true, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if result.SelectedGameCount != 1 || result.SkippedWithoutEvidenceCount != 1 || result.PlannedLookupCount != 1 || result.SkippedByLimitCount != 0 {
				t.Fatalf("wrong selection: %#v", result)
			}
			got, err := os.ReadFile(filepath.Join(root, "platforms", "ScummVM", "A no hashes", "game.yaml"))
			if err != nil || string(got) != original {
				t.Fatalf("unqueryable card changed: %s, %v", got, err)
			}
			if apply {
				if requests.Load() != 1 || result.NotFoundGameCount != 1 || result.WrittenFileCount != 1 {
					t.Fatalf("real miss should still be saved: %#v; requests=%d", result, requests.Load())
				}
			} else if requests.Load() != 0 || result.WrittenFileCount != 0 {
				t.Fatalf("preview mutated/queried: %#v", result)
			}
		})
	}
}
