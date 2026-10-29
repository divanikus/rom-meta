package igdb

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/store"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTitleScoreKeepsEditionSymbols(t *testing.T) {
	for _, tc := range []struct {
		local, source string
		accept        bool
	}{
		{"Street Fighter Zero 2'", "Street Fighter Zero 2", false},
		{"Street Fighter Zero 2", "Street Fighter Zero 2'", false},
		{"Legacy+", "Legacy", false}, {"Legacy", "Legacy+", false},
		{"Game++", "Game+", false}, {"Zero 2'", "Zero 2′", true},
		{"CyberTiger", "Cyber Tiger", true},
	} {
		game := catalogGame{Platform: "Sega - Saturn", Title: tc.local}
		score := scoreCandidate(apiGame{Name: tc.source, Platforms: []apiNamed{{Name: "Sega Saturn"}}}, game, searchTerms(game))
		if (score >= minimumMatchScore) != tc.accept {
			t.Fatalf("%#v score %d", tc, score)
		}
	}
}

func TestForcedRefreshRejectsBaseAndAcceptsLaterExactEdition(t *testing.T) {
	root := t.TempDir()
	if _, err := store.Init(root, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	var corrected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			w.Write([]byte(`{"access_token":"fixture","expires_in":3600,"token_type":"bearer"}`))
		case "/platforms":
			w.Write([]byte(`[{"name":"Sega Saturn"}]`))
		default:
			if corrected.Load() {
				w.Write([]byte(`[{"id":2,"name":"Street Fighter Zero 2'","platforms":[{"name":"Sega Saturn"}]}]`))
			} else {
				w.Write([]byte(`[{"id":1,"name":"Street Fighter Zero 2","platforms":[{"name":"Sega Saturn"}]}]`))
			}
		}
	}))
	defer server.Close()
	writeText(t, filepath.Join(root, "config", "sources.yaml"), `sources:
  igdb:
    credentials:
      type: secrets
      client_id: client_id
      client_secret: client_secret
    oauth:
      token_url: `+server.URL+`/token
      grant_type: client_credentials
    api:
      base_url: `+server.URL+`
      rate_limit_per_second: 1000
`)
	writeText(t, filepath.Join(root, "config", "secrets.yaml"), `sources:
  igdb:
    client_id: fixture
    client_secret: fixture
`)
	writeText(t, filepath.Join(root, "platforms", "Sega - Saturn", "Prime", "game.yaml"), "title: Street Fighter Zero 2'\nowner_note: keep\nreleases:\n  Street Fighter Zero 2' (Japan): {}\nsources:\n  igdb: {matches: [{igdb_id: 1}]}\n")
	for pass := 0; pass < 3; pass++ {
		corrected.Store(pass == 2)
		got, err := Import(ImportOptions{DataRoot: root, Apply: true, ForceOverwrite: true, Client: server.Client()})
		if err != nil || got.WrittenFileCount != 1 || (got.MatchedGameCount == 1) != (pass == 2) {
			t.Fatalf("pass %d: %#v %v", pass, got, err)
		}
		bytes, err := os.ReadFile(filepath.Join(root, "platforms", "Sega - Saturn", "Prime", "game.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(bytes), "owner_note: keep") {
			t.Fatalf("lost owner data: %s", bytes)
		}
		if pass < 2 && !strings.Contains(string(bytes), "status: not_found") {
			t.Fatalf("foreign binding survived: %s", bytes)
		}
		if pass == 2 && !strings.Contains(string(bytes), `igdb_id: 2`) {
			t.Fatalf("new edition not accepted: %s", bytes)
		}
	}
}
