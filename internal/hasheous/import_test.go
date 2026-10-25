package hasheous

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogcache"
)

func TestFingerprintLookupRequestsPreferDefaultReleaseAndDeduplicate(t *testing.T) {
	game := catalogGame{
		Title:          "Doom",
		DefaultRelease: "Doom (USA)",
		Fingerprints: []catalogcache.Fingerprint{
			{Release: "Doom (Europe)", Name: "Doom (Europe).32x", MD5: "11111111111111111111111111111111", SHA1: "2222222222222222222222222222222222222222", Format: "32x"},
			{Release: "Doom (USA)", Name: "Doom (USA).32x", MD5: "33333333333333333333333333333333", SHA1: "4444444444444444444444444444444444444444", Format: "32x"},
			{Release: "Doom (USA)", Name: "Doom (USA) duplicate.32x", MD5: "33333333333333333333333333333333", SHA1: "4444444444444444444444444444444444444444", Format: "32x"},
		},
	}

	requests := fingerprintLookupRequests(game)

	if len(requests) != 2 {
		t.Fatalf("expected two unique release lookup requests, got %#v", requests)
	}
	if requests[0].Release != "Doom (USA)" {
		t.Fatalf("expected default release first, got %#v", requests)
	}
	if len(requests[0].Inputs) != 1 || requests[0].Inputs[0].MD5 != "33333333333333333333333333333333" {
		t.Fatalf("unexpected first request: %#v", requests[0])
	}
}

func TestFingerprintLookupRequestsUseLikelyDataDiscFingerprint(t *testing.T) {
	game := catalogGame{
		Title:          "Golden Axe",
		DefaultRelease: "Golden Axe (Japan)",
		Fingerprints: []catalogcache.Fingerprint{
			{Release: "Golden Axe (Japan)", Name: "Golden Axe (Japan).cue", Size: 7000, MD5: "11111111111111111111111111111111", Format: "cue"},
			{Release: "Golden Axe (Japan)", Name: "Golden Axe (Japan) (Track 01).bin", Size: 8000000, MD5: "22222222222222222222222222222222", Format: "bin"},
			{Release: "Golden Axe (Japan)", Name: "Golden Axe (Japan) (Track 02).bin", Size: 48000000, MD5: "33333333333333333333333333333333", Format: "bin"},
		},
	}

	requests := fingerprintLookupRequests(game)

	if len(requests) != 1 {
		t.Fatalf("expected one release lookup request, got %#v", requests)
	}
	if requests[0].Release != "Golden Axe (Japan)" || len(requests[0].Inputs) != 1 {
		t.Fatalf("expected one representative disc file in release request, got %#v", requests[0])
	}
	if requests[0].Inputs[0].MD5 != "33333333333333333333333333333333" {
		t.Fatalf("expected largest bin track as representative request, got %#v", requests[0])
	}
}

func TestFingerprintLookupRequestsKeepTrackOneWhenItLooksLikeData(t *testing.T) {
	game := catalogGame{
		Title:          "Sewer Shark",
		DefaultRelease: "Sewer Shark (USA)",
		Fingerprints: []catalogcache.Fingerprint{
			{Release: "Sewer Shark (USA)", Name: "Sewer Shark (USA).cue", Size: 6000, MD5: "11111111111111111111111111111111", Format: "cue"},
			{Release: "Sewer Shark (USA)", Name: "Sewer Shark (USA) (Track 01).bin", Size: 30000000, MD5: "22222222222222222222222222222222", Format: "bin"},
			{Release: "Sewer Shark (USA)", Name: "Sewer Shark (USA) (Track 02).bin", Size: 6000000, MD5: "33333333333333333333333333333333", Format: "bin"},
		},
	}

	requests := fingerprintLookupRequests(game)

	if len(requests) != 1 {
		t.Fatalf("expected one release lookup request, got %#v", requests)
	}
	if requests[0].Inputs[0].MD5 != "22222222222222222222222222222222" {
		t.Fatalf("expected largest track-one bin as representative request, got %#v", requests[0])
	}
}

func TestLookupBestMatchesMapsIGDBID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/Lookup/ByHash" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("returnAllSources"); got != "true" {
			t.Fatalf("unexpected returnAllSources: %q", got)
		}
		body := readRequestBody(t, r)
		if !strings.Contains(body, `"MD5":"33333333333333333333333333333333"`) {
			t.Fatalf("expected MD5 in lookup body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"name": "Doom",
			"signatures": {
				"NoIntros": [
					{
						"game": { "name": "Doom" },
						"rom": { "md5": "33333333333333333333333333333333", "sha1": "4444444444444444444444444444444444444444" }
					}
				]
			},
			"metadata": [
				{ "source": "IGDB", "id": "1927", "status": "Mapped", "link": "https://www.igdb.com/games/doom" },
				{ "source": "TheGamesDB", "id": "123", "status": "Mapped", "link": "https://thegamesdb.net/game.php?id=123" }
			]
		}`))
	}))
	defer server.Close()

	inputs := []lookupInput{{
		Release: "Doom (USA)",
		Label:   "md5:33333333",
		MD5:     "33333333333333333333333333333333",
		SHA1:    "4444444444444444444444444444444444444444",
	}}
	matches, queries, err := lookupBestMatches(context.Background(), server.Client(), newRateLimiter(1000), sourceSettings{
		APIBaseURL:         server.URL,
		RateLimitPerSecond: 1000,
	}, catalogGame{Title: "Doom", DefaultRelease: "Doom (USA)"}, []lookupRequest{{Release: "Doom (USA)", Inputs: inputs}}, nil)
	if err != nil {
		t.Fatalf("lookupBestMatches returned error: %v", err)
	}
	if queries != 1 {
		t.Fatalf("expected one lookup query, got %d", queries)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %#v", matches)
	}
	match := matches[0]
	if match.IGDBID != 1927 || match.TheGamesDBID != "123" || match.Release != "Doom (USA)" {
		t.Fatalf("unexpected match: %#v", match)
	}
	if match.MatchedQuery != "md5:33333333" || match.Score != 220 {
		t.Fatalf("unexpected match proof: %#v", match)
	}
}

func TestLookupBestMatchesSkipsConflictingMetadataBridge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 257387,
			"name": "Fighting Street",
			"signatures": {
				"Redump": [
					{
						"game": { "name": "Bikkuriman Daijikai" },
						"rom": { "md5": "b0743b2a5dfaea3eb15a1fcb47da1b63" }
					}
				]
			},
			"metadata": [
				{ "source": "IGDB", "id": "37589", "status": "Mapped", "link": "https://www.igdb.com/games/fighting-street" }
			]
		}`))
	}))
	defer server.Close()

	inputs := []lookupInput{{
		Release: "Bikkuriman Daijikai (Japan)",
		Label:   "md5:b0743b2a",
		MD5:     "b0743b2a5dfaea3eb15a1fcb47da1b63",
	}}
	matches, _, err := lookupBestMatches(context.Background(), server.Client(), newRateLimiter(1000), sourceSettings{
		APIBaseURL:         server.URL,
		RateLimitPerSecond: 1000,
	}, catalogGame{Title: "Bikkuriman Daijikai", DefaultRelease: "Bikkuriman Daijikai (Japan)"}, []lookupRequest{{Release: "Bikkuriman Daijikai (Japan)", Inputs: inputs}}, nil)
	if err != nil {
		t.Fatalf("lookupBestMatches returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one signature match, got %#v", matches)
	}
	match := matches[0]
	if match.Title != "Bikkuriman Daijikai" {
		t.Fatalf("expected signature title to win, got %#v", match)
	}
	if match.IGDBID != 0 || len(match.References) != 0 || match.Score != 180 {
		t.Fatalf("expected conflicting metadata bridge to be skipped, got %#v", match)
	}
}

func TestLookupBestMatchesSkipsRetryableReleaseFailure(t *testing.T) {
	requestsSeen := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsSeen++
		if requestsSeen == 1 {
			http.Error(w, "Lookup operation is taking too long", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"name": "Doom",
			"signatures": {
				"NoIntros": [
					{
						"game": { "name": "Doom" },
						"rom": { "md5": "33333333333333333333333333333333" }
					}
				]
			},
			"metadata": [
				{ "source": "IGDB", "id": "1927", "status": "Mapped" }
			]
		}`))
	}))
	defer server.Close()

	matches, queries, err := lookupBestMatches(context.Background(), server.Client(), newRateLimiter(1000), sourceSettings{
		APIBaseURL:         server.URL,
		RateLimitPerSecond: 1000,
	}, catalogGame{Title: "Doom", DefaultRelease: "Doom (USA)"}, []lookupRequest{
		{Release: "Doom (Europe)", Inputs: []lookupInput{{Release: "Doom (Europe)", MD5: "11111111111111111111111111111111"}}},
		{Release: "Doom (USA)", Inputs: []lookupInput{{Release: "Doom (USA)", MD5: "33333333333333333333333333333333"}}},
	}, nil)
	if err != nil {
		t.Fatalf("lookupBestMatches returned error: %v", err)
	}
	if queries != 2 {
		t.Fatalf("expected two lookup queries, got %d", queries)
	}
	if len(matches) != 1 || matches[0].IGDBID != 1927 || matches[0].Release != "Doom (USA)" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestLookupBestMatchesReturnsRetryableFailureWhenNoReleaseMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		http.Error(w, "Lookup operation is taking too long", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	matches, queries, err := lookupBestMatches(context.Background(), server.Client(), newRateLimiter(1000), sourceSettings{
		APIBaseURL:         server.URL,
		RateLimitPerSecond: 1000,
	}, catalogGame{Title: "Doom", DefaultRelease: "Doom (USA)"}, []lookupRequest{
		{Release: "Doom (Europe)", Inputs: []lookupInput{{Release: "Doom (Europe)", MD5: "11111111111111111111111111111111"}}},
		{Release: "Doom (USA)", Inputs: []lookupInput{{Release: "Doom (USA)", MD5: "33333333333333333333333333333333"}}},
	}, nil)
	if err == nil {
		t.Fatalf("expected retryable lookup error")
	}
	if !strings.Contains(err.Error(), "retry after 90") {
		t.Fatalf("expected retry-after hint in error, got %v", err)
	}
	if queries != 2 {
		t.Fatalf("expected two lookup queries, got %d", queries)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %#v", matches)
	}
}

func TestImportStreamsCatalogAndSkipsExistingInOneReadPass(t *testing.T) {
	root := t.TempDir()
	writeText(t, filepath.Join(root, "config", "sources.yaml"), `sources:
  hasheous:
    api:
      base_url: http://127.0.0.1
      rate_limit_per_second: 1000
`)
	writeText(t, filepath.Join(root, "platforms", "Sega - 32X", "Already Checked", "game.yaml"), `title: "Already Checked"
default_release: "Already Checked (USA)"
releases:
  "Already Checked (USA)":

sources:
  hasheous:
    status: not_found
`)
	writeText(t, filepath.Join(root, "platforms", "Sega - 32X", "Needs Lookup", "game.yaml"), `title: "Needs Lookup"
default_release: "Needs Lookup (USA)"
releases:
  "Needs Lookup (USA)":
`)

	var stages []string
	var gameProgress []ImportProgress
	result, err := Import(ImportOptions{
		DataRoot: root,
		Apply:    true,
		Progress: func(snapshot ImportProgress) {
			if snapshot.Stage != "" {
				stages = append(stages, snapshot.Stage)
				return
			}
			gameProgress = append(gameProgress, snapshot)
		},
	})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.GameBundleCount != 2 || result.SelectedGameCount != 1 || result.SkippedExistingCount != 1 {
		t.Fatalf("unexpected streaming counts: %#v", result)
	}
	if result.NotFoundGameCount != 1 || result.WrittenFileCount != 1 || result.QueriedLookupCount != 0 {
		t.Fatalf("unexpected lookup/write counts: %#v", result)
	}
	if contains(stages, "checking existing metadata") {
		t.Fatalf("Hasheous should not run a separate existing-metadata scan, stages=%#v", stages)
	}
	if !contains(stages, "reading catalog") {
		t.Fatalf("expected one catalog read stage, got %#v", stages)
	}
	if len(gameProgress) != 1 || gameProgress[0].Current != 2 || gameProgress[0].Total != 2 {
		t.Fatalf("expected lookup progress to use full catalog position, got %#v", gameProgress)
	}
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	return string(data)
}

func writeText(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
