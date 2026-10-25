package serveui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogstate"
)

func TestBrowserAPIDiscoveryAndEmbeddedDocs(t *testing.T) {
	h, err := Handler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api", "/docs", "/"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		switch path {
		case "/api":
			var index struct {
				Documentation string `json:"documentation"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &index); err != nil {
				t.Fatal(err)
			}
			if index.Documentation != "/docs" {
				t.Fatal(w.Body.String())
			}
		case "/docs":
			for _, term := range []string{"SwaggerUIBundle", "/openapi.json", "validatorUrl:null", "persistAuthorization:false"} {
				if !strings.Contains(w.Body.String(), term) {
					t.Fatal(term)
				}
			}
		case "/":
			for _, link := range []string{`href="/api"`, `href="/docs"`} {
				if !strings.Contains(w.Body.String(), link) {
					t.Fatal(link)
				}
			}
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 405 {
			t.Fatal(path, w.Code)
		}
	}
}

func TestStatsIsReadOnlyPlatformScopedAndCancellable(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			game := filepath.Join(root, "platforms", "Arcade", "Game")
			writeTestFile(t, filepath.Join(game, "game.yaml"), "title: Game\nreleases: {Game: {}}\nsources: {launchbox: {matches: [{title: Game}]}}\n")
			writeTestFile(t, filepath.Join(game, "fingerprints.yaml"), "invalid: [")
			writeTestFile(t, filepath.Join(game, "media", "box-front.launchbox.png"), "picture")
			writeTestFile(t, filepath.Join(root, "platforms", "Other", "Broken", "game.yaml"), "invalid: [")
			if packed {
				catalogfstest.Pack(t, filepath.Dir(game))
			}
			h, err := Handler(root)
			if err != nil {
				t.Fatal(err)
			}
			var status catalogstate.Status
			requestJSON(t, h, "/api/stats?platform=Arcade", &status)
			if status.GameCount != 1 || status.ReleaseCount != 1 || status.LocalMediaFileCount != 1 || status.GamesWithSourceMatches != 1 {
				t.Fatalf("%+v", status)
			}
			if _, err := os.Stat(filepath.Join(root, ".rommeta")); !os.IsNotExist(err) {
				t.Fatal("stats touched writable cache", err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/api/stats?platform=..", nil))
			if w.Code != http.StatusBadRequest {
				t.Fatal(w.Code)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/api/stats?platform=Arcade", nil).WithContext(ctx))
			if w.Code == 200 {
				t.Fatal("cancelled scan succeeded")
			}
		})
	}
}

func TestStatsStreamHasProgressAndTerminalResultOrError(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "platforms", "Example", "A", "game.yaml"), "title: A\n")
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, broken := range []bool{false, true} {
		if broken {
			writeTestFile(t, filepath.Join(root, "platforms", "Example", "A", "game.yaml"), "broken: [")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/stats?platform=Example&stream=1", nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/x-ndjson") {
			t.Fatal(w.Code, w.Header())
		}
		decoder := json.NewDecoder(w.Body)
		var progress map[string]json.RawMessage
		if err := decoder.Decode(&progress); err != nil {
			t.Fatal(err)
		}
		if progress["progress"] == nil {
			t.Fatal(progress)
		}
		var terminal map[string]json.RawMessage
		for decoder.More() {
			if err := decoder.Decode(&terminal); err != nil {
				t.Fatal(err)
			}
		}
		if broken {
			if terminal["error"] == nil {
				t.Fatal(terminal)
			}
		} else {
			if terminal["result"] == nil {
				t.Fatal(terminal)
			}
		}
	}
}
