package serveui

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfstest"
)

func TestReadOnlyFingerprintPagesAndFilters(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			card := filepath.Join(root, "platforms", "Test", "Card")
			writeTestFile(t, filepath.Join(card, "game.yaml"), "title: Example\nreleases: {USA: {}}\nvariants: {Fixed: {kind: hack}}\n")
			var data strings.Builder
			data.WriteString("items:\n")
			for i := 0; i < 205; i++ {
				edition := "release: USA"
				if i >= 200 {
					edition = "variant: Fixed"
				}
				fmt.Fprintf(&data, "  - %s\n    file: {name: 'file%03d.zip', size: %d, crc32: '%08x', md5: '%032x', sha1: '%040x', sha256: '%064x', kind: payload}\n    dump: {format: zip}\n", edition, i, i+1, i+1, i+1, i+1, i+1)
			}
			writeTestFile(t, filepath.Join(card, "fingerprints.yaml"), data.String())
			if packed {
				catalogfstest.Pack(t, filepath.Dir(card))
			}
			h, err := Handler(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				query        string
				count, total int
				first        string
			}{
				{"", 100, 205, "file000.zip"}, {"&offset=100", 100, 205, "file100.zip"}, {"&offset=200&limit=200", 5, 205, "file200.zip"}, {"&offset=999", 0, 205, ""}, {"&release=USA&limit=200", 200, 200, "file000.zip"}, {"&variant=Fixed", 5, 5, "file200.zip"}, {"&q=FILE204", 1, 1, "file204.zip"}, {"&q=" + fmt.Sprintf("%064x", 205), 1, 1, "file204.zip"},
			} {
				var result fingerprintsResponse
				requestJSON(t, h, "/api/fingerprints?platform=Test&game=Card"+tc.query, &result)
				if len(result.Fingerprints) != tc.count || result.Total != tc.total || result.RegisteredTotal != 205 || tc.count > 0 && result.Fingerprints[0].Name != tc.first {
					t.Fatal(tc, result)
				}
			}
			for _, bad := range []string{"&limit=0", "&limit=201", "&offset=-1", "&offset=bad", "&release=USA&variant=Fixed", "&q=" + strings.Repeat("a", 257)} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", "/api/fingerprints?platform=Test&game=Card"+bad, nil))
				if w.Code != 400 {
					t.Fatal(bad, w.Code)
				}
			}
			for _, path := range []string{filepath.Join(root, "catalog.sqlite"), filepath.Join(root, "platforms", "Test", "_index.sqlite")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("browser created writable cache", path, err)
				}
			}
		})
	}
}

func TestMissingMalformedAndUnsafeFingerprintsStayIsolated(t *testing.T) {
	root := t.TempDir()
	card := filepath.Join(root, "platforms", "Test", "Card")
	writeTestFile(t, filepath.Join(card, "game.yaml"), "title: Example\n")
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	var empty fingerprintsResponse
	requestJSON(t, h, "/api/fingerprints?platform=Test&game=Card", &empty)
	if empty.Total != 0 || empty.Fingerprints == nil {
		t.Fatal(empty)
	}
	for _, path := range []string{"/api/fingerprints", "/api/fingerprints?platform=..&game=Card", "/api/fingerprints?platform=Test&game=..", "/api/fingerprints?platform=Test&game=_fold"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	writeTestFile(t, filepath.Join(card, "fingerprints.yaml"), "items: [invalid\n")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/fingerprints?platform=Test&game=Card", nil))
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	var game gameResponse
	requestJSON(t, h, "/api/game?platform=Test&game=Card", &game)
	if game.Game.Title != "Example" {
		t.Fatal(game)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/fingerprints", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/fingerprints?platform=Test&game=Missing", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	if err := os.Remove(filepath.Join(card, "fingerprints.yaml")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.yaml")
	writeTestFile(t, outside, "items: [{file: {name: private-secret}}]\n")
	if err := os.Symlink(outside, filepath.Join(card, "fingerprints.yaml")); err != nil {
		t.Log("Symlink check unavailable", err)
		return
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/fingerprints?"+url.Values{"platform": {"Test"}, "game": {"Card"}}.Encode(), nil))
	if w.Code != 400 || strings.Contains(w.Body.String(), "private-secret") {
		t.Fatal(w.Code, w.Body.String())
	}
}
