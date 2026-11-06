package igdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/platformnames"
	"strings"
	"testing"
	"time"
)

func TestVerifiedPlatformDiscoveryBeforeAnyGameSearch(t *testing.T) {
	for _, mode := range []string{"match", "missing", "ambiguous", "network"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			game := filepath.Join(root, "platforms", "Funtech - Super Acan", "Orbit", "game.yaml")
			original := "title: Orbit\ndefault_release: Orbit (Taiwan)\nreleases:\n  Orbit (Taiwan):\n"
			writeText(t, game, original)
			searches, lists := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					fmt.Fprint(w, `{"access_token":"test-token","expires_in":3600,"token_type":"bearer"}`)
				case "/platforms":
					lists++
					switch mode {
					case "network":
						http.Error(w, "unavailable", 503)
					case "missing":
						fmt.Fprint(w, `[{"id":1,"name":"Unrelated"}]`)
					case "ambiguous":
						fmt.Fprint(w, `[{"id":1,"name":"Super A'Can"},{"id":2,"name":"Super Acan"}]`)
					default:
						fmt.Fprint(w, `[{"id":1,"name":"Super A'Can"}]`)
					}
				case "/games":
					searches++
					fmt.Fprint(w, `[{"id":10,"name":"Orbit","platforms":[{"name":"Super A'Can"}]}]`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			writeText(t, filepath.Join(root, "config", "sources.yaml"), "sources:\n  igdb:\n    credentials:\n      type: secrets\n      client_id: client_id\n      client_secret: client_secret\n    oauth:\n      token_url: "+server.URL+"/token\n    api:\n      base_url: "+server.URL+"\n      rate_limit_per_second: 1000\n")
			writeText(t, filepath.Join(root, "config", "secrets.yaml"), "sources:\n  igdb:\n    client_id: test-client\n    client_secret: test-secret\n")
			options := ImportOptions{DataRoot: root, Apply: true, VerifyPlatform: true, Client: server.Client()}
			result, err := Import(options)
			if mode == "match" {
				if err != nil || result.MatchedGameCount != 1 || searches == 0 {
					t.Fatal(result, err, searches)
				}
				// A new card reuses the cached list; no forced refresh or saved-card overwrite.
				writeText(t, filepath.Join(root, "platforms", "Funtech - Super Acan", "Another", "game.yaml"), "title: Another\n")
				if _, err = Import(options); err != nil || lists != 1 {
					t.Fatal("list not reused", lists, err)
				}
			} else {
				var unresolved *platformnames.ResolutionError
				isResolution := errors.As(err, &unresolved)
				if err == nil || isResolution == (mode == "network") || searches != 0 {
					t.Fatal("wrong failure", err, searches)
				}
				data, _ := os.ReadFile(game)
				if string(data) != original {
					t.Fatal("unresolved platform changed card")
				}
			}
		})
	}
}

func TestPlatformListPaginationAndCacheExpiry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body := readRequestBody(t, r)
		if strings.Contains(body, "offset 0;") {
			rows := make([]apiNamed, 500)
			for i := range rows {
				rows[i] = apiNamed{ID: i + 1, Name: fmt.Sprintf("Synthetic %d", i)}
			}
			json.NewEncoder(w).Encode(rows)
		} else if strings.Contains(body, "offset 500;") {
			fmt.Fprint(w, `[{"id":501,"name":"Last System"}]`)
		} else {
			t.Errorf("unexpected query %s", body)
		}
	}))
	defer server.Close()
	cache, err := openQueryCache(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.close()
	settings := sourceSettings{APIBaseURL: server.URL, ClientID: "test"}
	get := func() ([]string, bool, error) {
		return queryPlatformNamesCached(server.Client(), cache, newRateLimiter(1000), settings, accessToken{Value: "test"})
	}
	names, cached, err := get()
	if err != nil || cached || len(names) != 501 || names[500] != "Last System" || calls != 2 {
		t.Fatal(len(names), cached, err, calls)
	}
	if _, cached, err = get(); err != nil || !cached || calls != 2 {
		t.Fatal(cached, err, calls)
	}
	if _, err = cache.db.Exec("UPDATE igdb_platform_cache SET created_at = ?", time.Now().Add(-8*24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, cached, err = get(); err != nil || cached || calls != 4 {
		t.Fatal(cached, err, calls)
	}
}
