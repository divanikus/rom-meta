package igdb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogcache"
)

func TestSearchTermsPreferCatalogTitleAndAliases(t *testing.T) {
	game := catalogGame{
		Title:          "Legend of Zelda, The - Ocarina of Time",
		DefaultRelease: "Legend of Zelda, The - Ocarina of Time (USA)",
		ReleaseTitles:  []string{"Zelda no Densetsu - Toki no Ocarina"},
		Aliases:        []string{"The Legend of Zelda: Ocarina of Time"},
		Releases:       []string{"Legend of Zelda, The - Ocarina of Time (USA) (Rev 1)"},
	}

	terms := searchTerms(game)

	if len(terms) == 0 || terms[0] != "Legend of Zelda, The - Ocarina of Time" {
		t.Fatalf("unexpected leading terms: %#v", terms)
	}
	if !contains(terms, "The Legend of Zelda: Ocarina of Time") {
		t.Fatalf("expected alias in search terms: %#v", terms)
	}
	if len(terms) > maxSearchTerms {
		t.Fatalf("too many terms: %#v", terms)
	}
}

func TestSearchTermsForUnofficialGameAvoidCleanTitle(t *testing.T) {
	game := catalogGame{
		Title:          "Aladdin",
		DefaultRelease: "Aladdin (World) (Unl) (Pirate)",
		Releases:       []string{"Aladdin (World) (Unl) (Pirate)"},
		UnofficialOnly: true,
	}

	terms := searchTerms(game)

	if contains(terms, "Aladdin") {
		t.Fatalf("unofficial game should not search the clean official title: %#v", terms)
	}
	if !contains(terms, "Aladdin (World) (Unl) (Pirate)") {
		t.Fatalf("expected tagged release search term: %#v", terms)
	}
}

func TestSearchTermsIncludeDeterministicTitleVariants(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected string
	}{
		{name: "camel case", title: "CyberTiger", expected: "Cyber Tiger"},
		{name: "filesystem slash", title: "Clay Fighter 63 1-3", expected: "Clay Fighter 63 1/3"},
		{name: "camel case and filesystem slash", title: "ClayFighter 63 1-3", expected: "Clay Fighter 63 1/3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terms := searchTerms(catalogGame{Title: tt.title})
			if !contains(terms, tt.expected) {
				t.Fatalf("expected %q in terms %#v", tt.expected, terms)
			}
		})
	}
}

func TestScoreCandidateMatchesArticleShapeAndPlatform(t *testing.T) {
	game := catalogGame{Platform: "Nintendo - Nintendo 64", Title: "Legend of Zelda, The - Ocarina of Time"}
	candidate := apiGame{
		Name:             "The Legend of Zelda: Ocarina of Time",
		FirstReleaseDate: 911692800,
		Platforms:        []apiNamed{{Name: "Nintendo 64"}},
	}

	score := scoreCandidate(candidate, game, searchTerms(game))

	if score < 140 {
		t.Fatalf("expected strong score, got %d", score)
	}
}

func TestScoreCandidateMatchesOnlySpaceDifferences(t *testing.T) {
	game := catalogGame{Platform: "Nintendo - Nintendo 64", Title: "CyberTiger"}
	candidate := apiGame{
		Name:      "Cyber Tiger",
		Platforms: []apiNamed{{Name: "Nintendo 64"}},
	}

	score := scoreCandidate(candidate, game, searchTerms(game))

	if score < 140 {
		t.Fatalf("expected compact-space title score, got %d", score)
	}
}

func TestScoreCandidateMatchesAlternativeTitle(t *testing.T) {
	game := catalogGame{Platform: "Nintendo - Nintendo 64", Title: "Zelda no Densetsu - Toki no Ocarina"}
	candidate := apiGame{
		Name:             "The Legend of Zelda: Ocarina of Time",
		AlternativeNames: []apiNamed{{Name: "Zelda no Densetsu: Toki no Ocarina"}},
		Platforms:        []apiNamed{{Name: "Nintendo 64"}},
	}

	score := scoreCandidate(candidate, game, searchTerms(game))

	if score < 120 {
		t.Fatalf("expected alternative title score, got %d", score)
	}
}

func TestScoreCandidateRejectsDifferentPlatform(t *testing.T) {
	game := catalogGame{Platform: "Nintendo - Nintendo 64", Title: "007 - The World Is Not Enough"}
	candidate := apiGame{
		Name:      "James Bond 007: The World Is Not Enough",
		Platforms: []apiNamed{{Name: "Game Boy Color"}},
	}

	score := scoreCandidate(candidate, game, searchTerms(game))

	if score != 0 {
		t.Fatalf("expected different platform to be rejected, got score %d", score)
	}
}

func TestScoreCandidateRejectsOfficialForUnofficialGame(t *testing.T) {
	game := catalogGame{
		Platform:       "Nintendo - Nintendo 64",
		Title:          "Aladdin",
		DefaultRelease: "Aladdin (World) (Unl) (Pirate)",
		Releases:       []string{"Aladdin (World) (Unl) (Pirate)"},
		UnofficialOnly: true,
	}
	official := apiGame{
		Name:      "Aladdin",
		Platforms: []apiNamed{{Name: "Nintendo 64"}},
	}
	unofficial := apiGame{
		Name:      "Aladdin (Unl)",
		Platforms: []apiNamed{{Name: "Nintendo 64"}},
	}

	if score := scoreCandidate(official, game, searchTerms(game)); score != 0 {
		t.Fatalf("expected official candidate to be rejected, got score %d", score)
	}
	if score := scoreCandidate(unofficial, game, searchTerms(game)); score == 0 {
		t.Fatalf("expected tagged candidate to match")
	}
}

func TestScoreCandidateMatchesCompositePlatformAlias(t *testing.T) {
	game := catalogGame{Platform: "NEC - PC Engine - TurboGrafx-16", Title: "Bonk's Adventure"}
	tests := []apiGame{
		{
			Name:      "Bonk's Adventure",
			Platforms: []apiNamed{{Name: "TurboGrafx-16/PC Engine"}},
		},
		{
			Name:      "Bonk's Adventure",
			Platforms: []apiNamed{{Name: "TurboGrafx-16"}},
		},
	}

	for _, candidate := range tests {
		score := scoreCandidate(candidate, game, searchTerms(game))
		if score < 140 {
			t.Fatalf("expected composite platform alias to match %#v, got score %d", candidate.Platforms, score)
		}
	}
}

func TestCatalogGameFromCachedCollectsReleasesAndCatalogAliases(t *testing.T) {
	root := t.TempDir()
	gamePath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Animal Forest")
	if err := os.MkdirAll(gamePath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(gamePath, "game.yaml")
	if err := os.WriteFile(path, []byte(`title: "Animal Forest"
default_release: "Doubutsu no Mori (Japan)"
alternate_titles: ["Animal Crossing Prototype"]

releases:
  "Doubutsu no Mori (Japan)":
    title: "Doubutsu no Mori"

sources:
  launchbox:
    matches:
      - release: "Doubutsu no Mori (Japan)"
        alternate_titles: ["Animal Forest", "Doubutsu no Mori"]
      - release: "Doubutsu no Mori (Japan)"
        title: "Animal Forest Source Title"
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	var game catalogGame
	_, _, err := catalogcache.ForEachGameWithOptions(root, catalogcache.ReadOptions{}, nil, func(cached catalogcache.Game) error {
		game = catalogGameFromCached(cached)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachGameWithOptions returned error: %v", err)
	}

	if game.Title != "Animal Forest" || game.DefaultRelease != "Doubutsu no Mori (Japan)" {
		t.Fatalf("unexpected game header: %#v", game)
	}
	if !contains(game.Releases, "Doubutsu no Mori (Japan)") {
		t.Fatalf("release key not found: %#v", game.Releases)
	}
	if !contains(game.ReleaseTitles, "Doubutsu no Mori") {
		t.Fatalf("release title not found: %#v", game.ReleaseTitles)
	}
	if !contains(game.Aliases, "Animal Crossing Prototype") {
		t.Fatalf("alias not found: %#v", game.Aliases)
	}
	if contains(game.Aliases, "Animal Forest") || contains(game.Aliases, "Animal Forest Source Title") {
		t.Fatalf("source-derived aliases should not be used for IGDB matching: %#v", game.Aliases)
	}
}

func TestReadHasheousIGDBIDs(t *testing.T) {
	root := t.TempDir()
	gamePath := filepath.Join(root, "platforms", "Sega - 32X", "Doom")
	if err := os.MkdirAll(gamePath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(gamePath, "game.yaml")
	if err := os.WriteFile(path, []byte(`title: "Doom"
default_release: "Doom (USA)"

sources:
  hasheous:
    matches:
      - release: "Doom (USA)"
        igdb_id: 1927
      - release: "Doom (Europe)"
        igdb_id: 1927
      - release: "Doom (Japan)"
        igdb_id: 451
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	ids := readHasheousIGDBIDs(path)
	if len(ids) != 2 || ids[0] != 1927 || ids[1] != 451 {
		t.Fatalf("unexpected Hasheous IGDB ids: %#v", ids)
	}
}

func TestSearchBestMatchUsesHasheousIGDBIDBeforeNameSearch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/games" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body := readRequestBody(t, r)
		if strings.Contains(body, "search ") {
			t.Fatalf("unexpected name search after Hasheous id: %s", body)
		}
		if !strings.Contains(body, "where id = 1927;") {
			t.Fatalf("expected id query, got: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1927,"name":"Doom","platforms":[{"name":"Sega 32X"}]}]`))
	}))
	defer server.Close()

	settings := sourceSettings{APIBaseURL: server.URL, ClientID: "test-client", RateLimitPerSecond: 1000}
	match, queries, cached, ok, err := searchBestMatch(server.Client(), nil, newRateLimiter(1000), settings, accessToken{Value: "test-token"}, catalogGame{
		Platform:        "Sega - 32X",
		Title:           "Doom (regional title)",
		DefaultRelease:  "Doom (USA)",
		HasheousIGDBIDs: []int{1927},
	}, []string{"Doom (regional title)"}, []string{"Sega 32X"}, nil)
	if err != nil {
		t.Fatalf("searchBestMatch returned error: %v", err)
	}
	if !ok || match.Metadata.ID != 1927 || match.MatchedQuery != "hasheous.igdb_id=1927" {
		t.Fatalf("unexpected match: ok=%v match=%#v", ok, match)
	}
	if queries != 1 || cached != 0 || requests != 1 {
		t.Fatalf("unexpected query counts: queries=%d cached=%d requests=%d", queries, cached, requests)
	}
}

func TestSearchBestMatchRejectsMismatchedHasheousIGDBID(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/games" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body := readRequestBody(t, r)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(body, "where id = 42108;"):
			_, _ = w.Write([]byte(`[{"id":42108,"name":"Drop Off","platforms":[{"name":"TurboGrafx-16/PC Engine"}]}]`))
		case strings.Contains(body, `search "Off the Wall";`):
			_, _ = w.Write([]byte(`[{"id":999,"name":"Off the Wall","platforms":[{"name":"TurboGrafx-16/PC Engine"}]}]`))
		default:
			t.Fatalf("unexpected request body: %s", body)
		}
	}))
	defer server.Close()

	settings := sourceSettings{APIBaseURL: server.URL, ClientID: "test-client", RateLimitPerSecond: 1000}
	game := catalogGame{
		Platform:        "NEC - PC Engine - TurboGrafx-16",
		Title:           "Off the Wall",
		DefaultRelease:  "Off the Wall (USA) (Proto)",
		HasheousIGDBIDs: []int{42108},
	}
	match, queries, cached, ok, err := searchBestMatch(server.Client(), nil, newRateLimiter(1000), settings, accessToken{Value: "test-token"}, game, searchTerms(game), nil, nil)
	if err != nil {
		t.Fatalf("searchBestMatch returned error: %v", err)
	}
	if !ok || match.Metadata.ID != 999 || match.MatchedQuery != "Off the Wall" {
		t.Fatalf("expected fallback name search match, ok=%v match=%#v", ok, match)
	}
	if queries != 2 || cached != 0 || len(bodies) != 2 {
		t.Fatalf("unexpected query counts: queries=%d cached=%d bodies=%d", queries, cached, len(bodies))
	}
}

func TestQueryGamesUsesIGDBHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Client-ID"); got != "test-client" {
			t.Fatalf("unexpected Client-ID header: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		body := readRequestBody(t, r)
		if !strings.Contains(body, `search "Zelda";`) {
			t.Fatalf("unexpected request body: %s", body)
		}
		for _, expected := range []string{"collections.name", "franchise.name", "franchises.name"} {
			if !strings.Contains(body, expected) {
				t.Fatalf("expected %q in request body: %s", expected, body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1020,"name":"Zelda","platforms":[{"name":"Nintendo 64"}]}]`))
	}))
	defer server.Close()

	games, err := queryGames(server.Client(), server.URL, "test-client", accessToken{Value: "test-token"}, "Zelda")
	if err != nil {
		t.Fatalf("queryGames returned error: %v", err)
	}
	if len(games) != 1 || games[0].ID != 1020 {
		t.Fatalf("unexpected games: %#v", games)
	}
}

func TestQueryPlatformNamesUsesIGDBHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/platforms" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Client-ID"); got != "test-client" {
			t.Fatalf("unexpected Client-ID header: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		body := readRequestBody(t, r)
		if !strings.Contains(body, "fields name;") {
			t.Fatalf("unexpected request body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":86,"name":"TurboGrafx-16/PC Engine"}]`))
	}))
	defer server.Close()

	platforms, err := queryPlatformNames(server.Client(), server.URL, "test-client", accessToken{Value: "test-token"})
	if err != nil {
		t.Fatalf("queryPlatformNames returned error: %v", err)
	}
	if len(platforms) != 1 || platforms[0] != "TurboGrafx-16/PC Engine" {
		t.Fatalf("unexpected platforms: %#v", platforms)
	}
}

func TestEnsureAccessTokenExplainsRejectedCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid client secret", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := ensureAccessToken(server.Client(), t.TempDir(), sourceSettings{
		ClientID:     "bad-client",
		ClientSecret: "bad-secret",
		TokenURL:     server.URL,
		GrantType:    "client_credentials",
	})
	if err == nil {
		t.Fatalf("expected token error")
	}
	if !strings.Contains(err.Error(), "IGDB token request returned 401 Unauthorized") || !strings.Contains(err.Error(), "invalid client secret") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureAccessTokenExplainsUnavailableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	_, err := ensureAccessToken(server.Client(), t.TempDir(), sourceSettings{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     url,
		GrantType:    "client_credentials",
	})
	if err == nil {
		t.Fatalf("expected token request error")
	}
	if !strings.Contains(err.Error(), "IGDB token request failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryGamesExplainsUnavailableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	_, err := queryGames(server.Client(), url, "client", accessToken{Value: "token"}, "Zelda")
	if err == nil {
		t.Fatalf("expected search request error")
	}
	if !strings.Contains(err.Error(), "IGDB search request failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryGamesCachedReusesStoredSearchResponse(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/games" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1020,"name":"Zelda","platforms":[{"name":"Nintendo 64"}]}]`))
	}))
	defer server.Close()

	cache, err := openQueryCache(t.TempDir(), false)
	if err != nil {
		t.Fatalf("openQueryCache returned error: %v", err)
	}
	defer cache.close()
	settings := sourceSettings{APIBaseURL: server.URL, ClientID: "test-client", RateLimitPerSecond: 1000}
	token := accessToken{Value: "test-token"}

	games, cached, err := queryGamesCached(server.Client(), cache, newRateLimiter(1000), settings, token, "Zelda")
	if err != nil {
		t.Fatalf("initial queryGamesCached returned error: %v", err)
	}
	if cached || len(games) != 1 || requests != 1 {
		t.Fatalf("unexpected initial cache state: cached=%v games=%#v requests=%d", cached, games, requests)
	}

	games, cached, err = queryGamesCached(server.Client(), cache, newRateLimiter(1000), settings, token, "Zelda")
	if err != nil {
		t.Fatalf("cached queryGamesCached returned error: %v", err)
	}
	if !cached || len(games) != 1 || requests != 1 {
		t.Fatalf("expected cached response without another request: cached=%v games=%#v requests=%d", cached, games, requests)
	}
}

func TestQueryGamesCachedCanBypassStoredSearchResponse(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1020,"name":"Zelda"}]`))
	}))
	defer server.Close()

	root := t.TempDir()
	cache, err := openQueryCache(root, false)
	if err != nil {
		t.Fatalf("openQueryCache returned error: %v", err)
	}
	settings := sourceSettings{APIBaseURL: server.URL, ClientID: "test-client", RateLimitPerSecond: 1000}
	token := accessToken{Value: "test-token"}
	if _, _, err := queryGamesCached(server.Client(), cache, newRateLimiter(1000), settings, token, "Zelda"); err != nil {
		t.Fatalf("initial queryGamesCached returned error: %v", err)
	}
	cache.close()

	bypassCache, err := openQueryCache(root, true)
	if err != nil {
		t.Fatalf("openQueryCache bypass returned error: %v", err)
	}
	defer bypassCache.close()
	_, cached, err := queryGamesCached(server.Client(), bypassCache, newRateLimiter(1000), settings, token, "Zelda")
	if err != nil {
		t.Fatalf("bypass queryGamesCached returned error: %v", err)
	}
	if cached || requests != 2 {
		t.Fatalf("expected bypass to make another request, cached=%v requests=%d", cached, requests)
	}
}

func TestQueryPlatformNamesCachedReusesStoredPlatformList(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/platforms" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":86,"name":"TurboGrafx-16/PC Engine"}]`))
	}))
	defer server.Close()

	cache, err := openQueryCache(t.TempDir(), false)
	if err != nil {
		t.Fatalf("openQueryCache returned error: %v", err)
	}
	defer cache.close()
	settings := sourceSettings{APIBaseURL: server.URL, ClientID: "test-client", RateLimitPerSecond: 1000}
	token := accessToken{Value: "test-token"}

	platforms, cached, err := queryPlatformNamesCached(server.Client(), cache, newRateLimiter(1000), settings, token)
	if err != nil {
		t.Fatalf("initial queryPlatformNamesCached returned error: %v", err)
	}
	if cached || len(platforms) != 1 || requests != 1 {
		t.Fatalf("unexpected initial platform cache state: cached=%v platforms=%#v requests=%d", cached, platforms, requests)
	}

	platforms, cached, err = queryPlatformNamesCached(server.Client(), cache, newRateLimiter(1000), settings, token)
	if err != nil {
		t.Fatalf("cached queryPlatformNamesCached returned error: %v", err)
	}
	if !cached || len(platforms) != 1 || requests != 1 {
		t.Fatalf("expected cached platform response without another request: cached=%v platforms=%#v requests=%d", cached, platforms, requests)
	}
}

func TestImportApplyWritesMatchedGamesIncrementally(t *testing.T) {
	root := t.TempDir()
	writeText(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"
default_release: "Star Fox 64 (USA)"
releases:
  Star Fox 64 (USA):
`)
	writeText(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Pilotwings 64", "game.yaml"), `title: "Pilotwings 64"
default_release: "Pilotwings 64 (USA)"
releases:
  Pilotwings 64 (USA):
`)
	writeText(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Zulu Missing", "game.yaml"), `title: "Zulu Missing"
default_release: "Zulu Missing (USA)"
releases:
  Zulu Missing (USA):
`)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600,"token_type":"bearer"}`))
		case "/platforms":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":4,"name":"Nintendo 64"}]`))
		case "/games":
			body := readRequestBody(t, r)
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(body, `search "Star Fox 64";`):
				_, _ = w.Write([]byte(`[{"id":1,"name":"Star Fox 64","platforms":[{"name":"Nintendo 64"}]}]`))
			case strings.Contains(body, `search "Pilotwings 64";`):
				_, _ = w.Write([]byte(`[{"id":2,"name":"Pilotwings 64","platforms":[{"name":"Nintendo 64"}]}]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
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
    client_id: test-client
    client_secret: test-secret
`)

	var stages []string
	result, err := Import(ImportOptions{
		DataRoot: root,
		Apply:    true,
		Client:   server.Client(),
		Progress: func(progress ImportProgress) {
			if progress.Stage != "" {
				stages = appendUniqueString(stages, progress.Stage)
			}
		},
	})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.WrittenFileCount != 3 || result.MatchedGameCount != 2 || result.NotFoundGameCount != 1 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	for _, game := range []string{"Star Fox 64", "Pilotwings 64"} {
		contents, err := os.ReadFile(filepath.Join(root, "platforms", "Nintendo - Nintendo 64", game, "game.yaml"))
		if err != nil {
			t.Fatalf("ReadFile %s failed: %v", game, err)
		}
		if !strings.Contains(string(contents), "  igdb:") {
			t.Fatalf("%s was not written incrementally:\n%s", game, contents)
		}
	}
	missing, err := os.ReadFile(filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Zulu Missing", "game.yaml"))
	if err != nil {
		t.Fatalf("ReadFile missing failed: %v", err)
	}
	if !strings.Contains(string(missing), "  igdb:") || !strings.Contains(string(missing), "status: not_found") || !strings.Contains(string(missing), "checked_at:") {
		t.Fatalf("missing game was not marked as not_found:\n%s", missing)
	}
	for _, expected := range []string{"loading config", "opening catalog cache", "scanning catalog", "reading catalog", "authenticating", "opening cache", "loading platform list"} {
		if !contains(stages, expected) {
			t.Fatalf("expected progress stage %q in %#v", expected, stages)
		}
	}
	if contains(stages, "checking existing metadata") {
		t.Fatalf("IGDB should not run a separate existing-metadata scan, stages=%#v", stages)
	}
	second, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: server.Client()})
	if err != nil {
		t.Fatalf("second Import returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 3 {
		t.Fatalf("expected matched and not_found games to be skipped on second import, got %#v", second)
	}
}

func TestRenderMetadataWritesCollectionsAndFranchises(t *testing.T) {
	body := renderMetadata(matchedGame{
		Release: "Shining Force (USA)",
		Metadata: gameMetadata{
			ID:          1020,
			Title:       "Shining Force",
			Collections: []string{"Shining Force"},
			Franchises:  []string{"Shining"},
		},
	})

	for _, expected := range []string{
		"        collections: [\"Shining Force\"]",
		"        franchises: [\"Shining\"]",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered metadata:\n%s", expected, body)
		}
	}
}

func TestMetadataFromGamePreservesCollectionsAndFranchises(t *testing.T) {
	metadata := metadataFromGame(apiGame{
		Name:        "Shining Force",
		Collections: []apiNamed{{Name: "Shining Force"}},
		Franchise:   apiNamed{Name: "Shining"},
		Franchises:  []apiNamed{{Name: "Shining"}, {Name: "Sega RPGs"}},
	})

	if got := strings.Join(metadata.Collections, ","); got != "Shining Force" {
		t.Fatalf("unexpected collections: %#v", metadata.Collections)
	}
	if got := strings.Join(metadata.Franchises, ","); got != "Shining,Sega RPGs" {
		t.Fatalf("unexpected franchises: %#v", metadata.Franchises)
	}
}

func TestMediaURLsUseFullFrameIGDBImagePreset(t *testing.T) {
	media := mediaURLs(apiGame{
		Cover:       apiImage{ImageID: "co1234"},
		Artworks:    []apiImage{{ImageID: "ar1234"}},
		Screenshots: []apiImage{{ImageID: "sc1234"}},
	})

	expected := map[string]string{
		"box_front":  "https://images.igdb.com/igdb/image/upload/t_1080p/co1234.jpg",
		"fanart":     "https://images.igdb.com/igdb/image/upload/t_1080p/ar1234.jpg",
		"screenshot": "https://images.igdb.com/igdb/image/upload/t_1080p/sc1234.jpg",
	}
	for kind, url := range expected {
		candidates := media[kind]
		if len(candidates) != 1 || candidates[0].URL != url {
			t.Fatalf("unexpected %s candidates: %#v", kind, candidates)
		}
	}
	for kind, candidates := range media {
		for _, candidate := range candidates {
			if strings.Contains(candidate.URL, "cover_big") || strings.Contains(candidate.URL, "screenshot_big") {
				t.Fatalf("%s should use the full-frame preset, got %s", kind, candidate.URL)
			}
		}
	}
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	return string(body)
}

func writeText(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
