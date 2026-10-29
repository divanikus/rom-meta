package screenscraper

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
		score := scoreCandidate(gameMetadata{Title: tc.source}, game, searchTerms(game))
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
		if r.URL.Path == "/systemesListe.php" {
			w.Write([]byte(`{"response":{"systemes":[{"id":22,"nom":"Sega Saturn"}]}}`))
			return
		}
		if corrected.Load() {
			w.Write([]byte(`{"response":{"jeu":{"id":2,"noms":[{"region":"jp","text":"Street Fighter Zero 2'"}]}}}`))
		} else {
			w.Write([]byte(`{"response":{"jeu":{"id":1,"noms":[{"region":"jp","text":"Street Fighter Zero 2"}]}}}`))
		}
	}))
	defer server.Close()
	writeSourcesConfig(t, root, server.URL)
	writeSecrets(t, root, map[string]string{"username": "fixture", "password": "fixture", "dev_id": "fixture", "dev_password": "fixture"})
	writeGame(t, root, "Sega - Saturn", "Prime", "title: Street Fighter Zero 2'\nowner_note: keep\nreleases:\n  Street Fighter Zero 2' (Japan): {}\nsources:\n  screenscraper: {matches: [{screenscraper_id: 1}]}\n")
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
		if pass == 2 && !strings.Contains(string(bytes), `screenscraper_id: "2"`) {
			t.Fatalf("new edition not accepted: %s", bytes)
		}
	}
}
