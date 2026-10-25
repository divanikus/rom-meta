package screenscraper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rommeta/internal/platformnames"
)

func TestReviewedSystemIDsDisambiguateRelatedPlatforms(t *testing.T) {
	registry := platformnames.DefaultRegistry()
	systems := []screenScraperSystem{
		{ID: "130", Name: "Amiga CD32", Names: []string{"amiga", "amigacd32"}},
		{ID: "139", Name: "Amiga CD32 (hack)", Names: []string{"amiga", "amigacd32"}},
		{ID: "14", Name: "Nintendo 64", Names: []string{"n64"}},
		{ID: "122", Name: "Nintendo 64DD", Names: []string{"n64"}},
		{ID: "253", Name: "FM Towns", Names: []string{"fmtowns", "marty"}},
		{ID: "97", Name: "FM-7", Names: []string{"fm7", "Fujitsu FM-7"}},
		{ID: "250", Name: "Sega Pico"},
	}
	for _, configured := range []*platformnames.Registry{nil, &registry} {
		for _, tc := range []struct{ platform, id string }{
			{"Fujitsu - FM Towns series", "253"},
			{"Fujitsu - FM-Towns", "253"},
			{"Commodore - Amiga CD32", "130"},
			{"Nintendo - Nintendo 64DD", "122"},
			{"Nintendo - Nintendo 64", "14"},
		} {
			if id, ok := screenScraperSystemID(tc.platform, systems, configured); !ok || id != tc.id {
				t.Fatalf("%s resolved to %q, %t; want %s", tc.platform, id, ok, tc.id)
			}
		}
		for _, platform := range []string{"Sega - Beena", "Zeebo - Zeebo"} {
			if id, ok := screenScraperSystemID(platform, systems, configured); ok {
				t.Fatalf("unsupported %s borrowed system %s", platform, id)
			}
		}
	}
}

func TestImportSeparatesUnmappedPlatformsFromFailedSearches(t *testing.T) {
	root := t.TempDir()
	writeSourcesConfig(t, root, "https://example.test")
	writeSecrets(t, root, map[string]string{"username": "test-user", "password": "test-pass", "dev_id": "test-dev", "dev_password": "test-dev-pass"})
	for _, title := range []string{"Game A", "Game B", "Game C"} {
		writeGame(t, root, "Sega - Beena", title, fmt.Sprintf("title: %q\n", title))
	}
	writeGame(t, root, "Nintendo - Nintendo 64", "Known Game", "title: Known Game\n")
	queries := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/systemesListe.php" {
			return testResponse(200, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
		}
		queries++
		return testResponse(200, `{"response":{"jeu":{"id":42,"noms":[{"region":"us","text":"Known Game"}]}}}`), nil
	})}
	result, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if result.FailedSearchCount != 0 || len(result.Failures) != 0 {
		t.Fatalf("unmapped platforms are not failed network searches: %+v", result)
	}
	if result.SkippedUnmappedCount != 3 || len(result.UnmappedPlatforms) != 1 || result.UnmappedPlatforms["Sega - Beena"] != 3 {
		t.Fatalf("expected one grouped platform skip: %+v", result)
	}
	if queries != 1 || result.PlannedSearchCount != 1 || result.QueriedSearchCount != 1 || result.WrittenFileCount != 1 || result.NotFoundGameCount != 0 {
		t.Fatalf("only the mapped game should be queried and written: %+v, queries=%d", result, queries)
	}
	for _, title := range []string{"Game A", "Game B", "Game C"} {
		text := readFile(t, filepath.Join(root, "platforms", "Sega - Beena", title, "game.yaml"))
		if strings.Contains(text, "sources:") {
			t.Fatalf("unmapped game must remain retryable: %s", text)
		}
	}
}

func TestScreenScraperSystemsCacheRefresh(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fetchedAt string
		refresh   bool
	}{
		{"fresh", time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339), false},
		{"expired", time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339), true},
		{"missing_date", "", true},
		{"invalid_date", "invalid", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			settings := sourceSettings{APIBaseURL: "https://example.test"}
			path := screenScraperSystemsCachePath(root)
			if err := writeSystemsCache(path, settings.APIBaseURL, []screenScraperSystem{{ID: "14", Name: "Nintendo 64"}}); err != nil {
				t.Fatal(err)
			}
			var cache systemsCache
			if err := json.Unmarshal([]byte(readFile(t, path)), &cache); err != nil {
				t.Fatal(err)
			}
			cache.FetchedAt = tc.fetchedAt
			data, err := json.Marshal(cache)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/systemesListe.php" {
					t.Fatalf("unexpected request: %s", r.URL.Path)
				}
				requests++
				return testResponse(200, `{"response":{"systemes":[{"id":42,"nom":"New System"}]}}`), nil
			})}
			systems, cached, err := screenScraperSystems(client, root, settings, false)
			if err != nil || cached == tc.refresh || len(systems) != 1 {
				t.Fatalf("systems=%+v cached=%t error=%v", systems, cached, err)
			}
			if tc.refresh && (requests != 1 || systems[0].ID != "42") {
				t.Fatalf("stale mapping was not refreshed: requests=%d systems=%+v", requests, systems)
			}
			if !tc.refresh && requests != 0 {
				t.Fatalf("fresh cache should avoid a request, got %d", requests)
			}
			before := requests
			_, cached, err = screenScraperSystems(client, root, settings, false)
			if err != nil || !cached || requests != before {
				t.Fatalf("subsequent pass must use the refreshed cache: cached=%t requests=%d error=%v", cached, requests, err)
			}
		})
	}
}
