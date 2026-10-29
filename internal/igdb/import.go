package igdb

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/platformnames"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"

	_ "modernc.org/sqlite"
)

// ImportOptions controls IGDB metadata enrichment.
type ImportOptions struct {
	DataRoot         string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	ForceOverwrite   bool
	RefreshRelations bool
	RefreshLinks     bool
	LinkReview       string
	Limit            int
	Client           *http.Client
	Progress         ProgressFunc
}

// ProgressFunc receives progress updates during applied IGDB searches.
type ProgressFunc func(ImportProgress)

// ImportProgress describes an IGDB search progress update.
type ImportProgress struct {
	Current int
	Total   int
	Stage   string
	Game    string
	Queries int
	Matches int
}

// ImportResult describes a dry-run or applied IGDB enrichment.
type ImportResult struct {
	DataRoot              string
	LinkRows              []string
	APIBaseURL            string
	TokenCachePath        string
	PlatformCount         int
	GameBundleCount       int
	SelectedGameCount     int
	SkippedByLimitCount   int
	PlannedSearchCount    int
	QueriedSearchCount    int
	CachedSearchCount     int
	MatchedGameCount      int
	GameFileCount         int
	NotFoundGameCount     int
	WrittenFileCount      int
	SkippedPreservedCount int
	SkippedExistingCount  int
	FailedSearchCount     int
	Failures              []ImportFailure
	TokenChecked          bool
	UsedCachedToken       bool
	UsedCachedPlatforms   bool
	RateLimitPerSecond    int
	Limit                 int
	Apply                 bool
}

// ImportFailure is a failed IGDB search that did not stop the whole run.
type ImportFailure struct {
	Game  string
	Query string
	Error string
}

type catalogGame struct {
	PreservedSources []string
	MAMEReleases     []catalogcache.Release
	ArcadePlatform   string
	Platform         string
	Folder           string
	Path             string
	Title            string
	DefaultRelease   string
	Releases         []string
	ReleaseTitles    []string
	Aliases          []string
	Sources          []string
	HasheousIGDBIDs  []int
	UnofficialOnly   bool
}

type sourceSettings struct {
	ClientID           string
	ClientSecret       string
	TokenURL           string
	GrantType          string
	APIBaseURL         string
	RateLimitPerSecond int
}

type matchedGame struct {
	Release      string
	MatchedQuery string
	Score        int
	Metadata     gameMetadata
}

type gameMetadata struct {
	ID              int
	Title           string
	Slug            string
	URL             string
	ReleaseDate     string
	Developer       string
	Publisher       string
	Genres          []string
	Themes          []string
	Collections     []string
	Franchises      []string
	Platforms       []string
	Rating          string
	RatingCount     int
	Description     string
	AlternateTitles []string
	MediaURLs       map[string][]sourceyaml.MediaCandidate
	Relationships   []sourceyaml.Relationship
	GameType        string
}

type accessToken struct {
	Value     string
	Type      string
	ExpiresAt time.Time
	FromCache bool
	CachePath string
}

type tokenCache struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type apiGame struct {
	VersionParent int `json:"version_parent"`
	ParentGame    int `json:"parent_game"`
	GameType      struct {
		Type string `json:"type"`
	} `json:"game_type"`

	ID                int                  `json:"id"`
	Name              string               `json:"name"`
	Slug              string               `json:"slug"`
	URL               string               `json:"url"`
	Summary           string               `json:"summary"`
	Storyline         string               `json:"storyline"`
	FirstReleaseDate  int64                `json:"first_release_date"`
	TotalRating       float64              `json:"total_rating"`
	TotalRatingCount  int                  `json:"total_rating_count"`
	Genres            []apiNamed           `json:"genres"`
	Themes            []apiNamed           `json:"themes"`
	Collections       []apiNamed           `json:"collections"`
	Franchise         apiNamed             `json:"franchise"`
	Franchises        []apiNamed           `json:"franchises"`
	Platforms         []apiNamed           `json:"platforms"`
	Cover             apiImage             `json:"cover"`
	Artworks          []apiImage           `json:"artworks"`
	Screenshots       []apiImage           `json:"screenshots"`
	AlternativeNames  []apiNamed           `json:"alternative_names"`
	InvolvedCompanies []apiInvolvedCompany `json:"involved_companies"`
}

type apiNamed struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type apiImage struct {
	ID      int    `json:"id"`
	ImageID string `json:"image_id"`
}

type apiInvolvedCompany struct {
	Developer bool     `json:"developer"`
	Publisher bool     `json:"publisher"`
	Company   apiNamed `json:"company"`
}

const (
	defaultTokenURL     = "https://id.twitch.tv/oauth2/token"
	defaultGrantType    = "client_credentials"
	defaultAPIBaseURL   = "https://api.igdb.com/v4"
	defaultRateLimitRPS = 4
	maxSearchTerms      = 16
	maxMediaPerKind     = 5
	minimumMatchScore   = 100
	igdbCacheVersion    = "igdb-v3"
	igdbFullFrameImage  = "1080p"
	igdbGameFields      = "version_parent,parent_game,game_type.type,id,name,slug,url,summary,storyline,first_release_date,total_rating,total_rating_count,genres.name,themes.name,collections.name,franchise.name,franchises.name,platforms.name,cover.image_id,artworks.image_id,screenshots.image_id,alternative_names.name,involved_companies.developer,involved_companies.publisher,involved_companies.company.name"
)

// Import searches IGDB for one candidate per catalog game and optionally writes
// a source-specific stanza into game.yaml.
func Import(options ImportOptions) (ImportResult, error) {
	if options.RefreshLinks {
		return refreshLinks(options)
	}
	if options.RefreshRelations {
		return refreshRelationships(options)
	}
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}

	reportStage(options.Progress, options.Apply, "loading config", 0, 0, filepath.Join(dataRoot, "config", "sources.yaml"))
	config, err := store.LoadSources(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}
	settings, err := igdbSettings(config)
	if err != nil {
		return ImportResult{}, err
	}

	result := ImportResult{
		DataRoot:           dataRoot,
		APIBaseURL:         settings.APIBaseURL,
		TokenCachePath:     tokenCachePath(dataRoot),
		RateLimitPerSecond: settings.RateLimitPerSecond,
		Apply:              options.Apply,
		Limit:              options.Limit,
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	limiter := newRateLimiter(settings.RateLimitPerSecond)

	var token accessToken
	var cache *queryCache
	var sourcePlatforms []string
	setupDone := false
	setupApply := func() error {
		if setupDone || !options.Apply {
			setupDone = true
			return nil
		}
		reportStage(options.Progress, options.Apply, "authenticating", 0, 0, "IGDB")
		var err error
		token, err = ensureAccessToken(client, dataRoot, settings)
		if err != nil {
			return err
		}
		result.TokenChecked = true
		result.UsedCachedToken = token.FromCache
		result.TokenCachePath = token.CachePath

		reportStage(options.Progress, options.Apply, "opening cache", 0, 0, queryCachePath(dataRoot))
		cache, err = openQueryCache(dataRoot, options.ForceOverwrite)
		if err != nil {
			return err
		}

		var cached bool
		reportStage(options.Progress, options.Apply, "loading platform list", 0, 0, "IGDB")
		sourcePlatforms, cached, err = queryPlatformNamesCached(client, cache, limiter, settings, token)
		if err != nil {
			return err
		}
		result.UsedCachedPlatforms = cached
		setupDone = true
		return nil
	}
	defer func() {
		if cache != nil {
			cache.close()
		}
	}()

	reportStage(options.Progress, options.Apply, "opening catalog cache", 0, 0, catalogcache.Path(dataRoot))
	catalogCurrent := 0
	catalogTotal := 0
	progress := catalogProgress(options.Progress, options.Apply)
	streamProgress := taskprogress.Func(func(snapshot taskprogress.Snapshot) {
		if snapshot.Stage == "reading catalog" {
			catalogCurrent = snapshot.Current
			catalogTotal = snapshot.Total
		}
		if progress != nil {
			progress(snapshot)
		}
	})
	platformCount, gameCount, err := streamCatalogGames(dataRoot, options.Platform, "igdb", options.ForceOverwrite, streamProgress, func(game catalogGame) error {
		if catalogcache.HasSource(game.PreservedSources, "igdb") {
			result.SkippedPreservedCount++
			return nil
		}
		if !options.ForceOverwrite && catalogcache.HasSource(game.Sources, "igdb") {
			result.SkippedExistingCount++
			return nil
		}
		if options.Limit > 0 && result.SelectedGameCount >= options.Limit {
			result.SkippedByLimitCount++
			return nil
		}
		result.SelectedGameCount++
		current := catalogCurrent
		total := catalogTotal
		if current == 0 {
			current = result.SelectedGameCount
		}
		reportProgress(options.Progress, options.Apply, current, total, game.Platform+" / "+game.Title, result.QueriedSearchCount, result.MatchedGameCount)
		game.HasheousIGDBIDs = readHasheousIGDBIDs(filepath.Join(game.Path, "game.yaml"))
		terms := searchTerms(game)
		result.PlannedSearchCount += len(game.HasheousIGDBIDs) + len(terms)
		if !options.Apply {
			return nil
		}
		if err := setupApply(); err != nil {
			return err
		}

		match, queries, cached, ok, err := searchBestMatch(client, cache, limiter, settings, token, game, terms, sourcePlatforms, options.PlatformRegistry)
		result.QueriedSearchCount += queries
		result.CachedSearchCount += cached
		if err != nil {
			if isFatalSearchError(err) {
				return err
			}
			result.FailedSearchCount++
			if len(result.Failures) < 10 {
				result.Failures = append(result.Failures, ImportFailure{
					Game:  game.Title,
					Query: firstNonEmpty(terms...),
					Error: err.Error(),
				})
			}
			return nil
		}
		if !ok {
			result.NotFoundGameCount++
			out := filepath.Join(game.Path, "game.yaml")
			if err := sourceyaml.WriteSourceMetadata(out, "igdb", renderNotFoundMetadata()); err != nil {
				return err
			}
			result.WrittenFileCount++
			return nil
		}
		result.MatchedGameCount++
		result.GameFileCount++
		out := filepath.Join(game.Path, "game.yaml")
		if err := sourceyaml.WriteSourceMetadata(out, "igdb", renderMetadata(match)); err != nil {
			return err
		}
		result.WrittenFileCount++
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	result.PlatformCount = platformCount
	result.GameBundleCount = gameCount
	return result, nil
}

func reportProgress(progress ProgressFunc, apply bool, current, total int, game string, queries, matches int) {
	if !apply || progress == nil {
		return
	}
	progress(ImportProgress{
		Current: current,
		Total:   total,
		Game:    game,
		Queries: queries,
		Matches: matches,
	})
}

func reportStage(progress ProgressFunc, apply bool, stage string, current, total int, label string) {
	if !apply || progress == nil {
		return
	}
	progress(ImportProgress{
		Current: current,
		Total:   total,
		Stage:   stage,
		Game:    label,
	})
}

func catalogProgress(progress ProgressFunc, apply bool) taskprogress.Func {
	if !apply || progress == nil {
		return nil
	}
	return func(snapshot taskprogress.Snapshot) {
		reportStage(progress, apply, snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
}

func igdbSettings(config store.SourcesConfig) (sourceSettings, error) {
	var source store.Source
	found := false
	for _, candidate := range config.Sources {
		if candidate.Name == "igdb" {
			source = candidate
			found = true
			break
		}
	}
	if !found {
		return sourceSettings{}, fmt.Errorf("source igdb not found in %s", config.Path)
	}

	settings := sourceSettings{
		TokenURL:           firstNonEmpty(source.OAuthTokenURL, defaultTokenURL),
		GrantType:          firstNonEmpty(source.OAuthGrantType, defaultGrantType),
		APIBaseURL:         strings.TrimRight(firstNonEmpty(source.APIBaseURL, defaultAPIBaseURL), "/"),
		RateLimitPerSecond: source.RateLimitPerSecond,
	}
	if settings.RateLimitPerSecond <= 0 {
		settings.RateLimitPerSecond = defaultRateLimitRPS
	}
	if value, ok := store.CredentialValue(config, source, "client_id"); ok {
		settings.ClientID = value
	}
	if value, ok := store.CredentialValue(config, source, "client_secret"); ok {
		settings.ClientSecret = value
	}
	return settings, nil
}

func ensureAccessToken(client *http.Client, dataRoot string, settings sourceSettings) (accessToken, error) {
	if strings.TrimSpace(settings.ClientID) == "" || strings.TrimSpace(settings.ClientSecret) == "" {
		return accessToken{}, fmt.Errorf("IGDB credentials are missing: fill sources.igdb.client_id and sources.igdb.client_secret in config/secrets.yaml")
	}

	cachePath := tokenCachePath(dataRoot)
	if cached, ok := readCachedToken(cachePath); ok {
		return accessToken{
			Value:     cached.AccessToken,
			Type:      firstNonEmpty(cached.TokenType, "Bearer"),
			ExpiresAt: cached.ExpiresAt,
			FromCache: true,
			CachePath: cachePath,
		}, nil
	}

	form := url.Values{}
	form.Set("client_id", settings.ClientID)
	form.Set("client_secret", settings.ClientSecret)
	form.Set("grant_type", firstNonEmpty(settings.GrantType, defaultGrantType))

	request, err := http.NewRequest(http.MethodPost, settings.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return accessToken{}, fmt.Errorf("IGDB token request could not be created: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return accessToken{}, fmt.Errorf("IGDB token request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return accessToken{}, fmt.Errorf("IGDB token request returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var parsed tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return accessToken{}, fmt.Errorf("IGDB token response could not be decoded: %w", err)
	}
	if parsed.AccessToken == "" {
		return accessToken{}, fmt.Errorf("IGDB token response did not include access_token")
	}
	expiresAt := time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	if parsed.ExpiresIn <= 0 {
		expiresAt = time.Now().Add(time.Hour)
	}
	tokenType := firstNonEmpty(parsed.TokenType, "Bearer")
	cache := tokenCache{AccessToken: parsed.AccessToken, TokenType: tokenType, ExpiresAt: expiresAt}
	if err := writeCachedToken(cachePath, cache); err != nil {
		return accessToken{}, err
	}
	return accessToken{
		Value:     parsed.AccessToken,
		Type:      tokenType,
		ExpiresAt: expiresAt,
		CachePath: cachePath,
	}, nil
}

func readCachedToken(path string) (tokenCache, bool) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return tokenCache{}, false
	}
	var cache tokenCache
	if err := json.Unmarshal(contents, &cache); err != nil {
		return tokenCache{}, false
	}
	if cache.AccessToken == "" || time.Now().Add(5*time.Minute).After(cache.ExpiresAt) {
		return tokenCache{}, false
	}
	return cache, true
}

func writeCachedToken(path string, cache tokenCache) error {
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := catalogfs.WriteFile(tmp, append(contents, '\n'), 0o600); err != nil {
		return err
	}
	return catalogfs.Rename(tmp, path)
}

func tokenCachePath(dataRoot string) string {
	return filepath.Join(dataRoot, ".rommeta", "sources", "igdb", "token.json")
}

func searchBestMatch(client *http.Client, cache *queryCache, limiter *rateLimiter, settings sourceSettings, token accessToken, game catalogGame, terms []string, sourcePlatforms []string, registry *platformnames.Registry) (matchedGame, int, int, bool, error) {
	var best matchedGame
	queries := 0
	cachedQueries := 0

	for _, id := range game.HasheousIGDBIDs {
		candidates, cached, err := queryGameByIDCached(client, cache, limiter, settings, token, id)
		if cached {
			cachedQueries++
		} else {
			queries++
		}
		if err != nil {
			return matchedGame{}, queries, cachedQueries, false, err
		}
		for _, candidate := range candidates {
			score := scoreHasheousCandidate(candidate, game, terms, sourcePlatforms, registry)
			if score < minimumMatchScore || score <= best.Score {
				continue
			}
			best = matchedGame{
				Release:      firstNonEmpty(game.DefaultRelease, game.Title),
				MatchedQuery: fmt.Sprintf("hasheous.igdb_id=%d", id),
				Score:        score,
				Metadata:     metadataFromGame(candidate),
			}
		}
		if best.Score >= 200 {
			return best, queries, cachedQueries, true, nil
		}
	}

	for _, term := range terms {
		candidates, cached, err := queryGamesCached(client, cache, limiter, settings, token, term)
		if cached {
			cachedQueries++
		} else {
			queries++
		}
		if err != nil {
			return matchedGame{}, queries, cachedQueries, false, err
		}
		for _, candidate := range candidates {
			score := scoreCandidateWithPlatforms(candidate, game, terms, sourcePlatforms, registry)
			if score < minimumMatchScore || score <= best.Score {
				continue
			}
			best = matchedGame{
				Release:      firstNonEmpty(game.DefaultRelease, game.Title),
				MatchedQuery: term,
				Score:        score,
				Metadata:     metadataFromGame(candidate),
			}
		}
		if best.Score >= 140 {
			break
		}
	}
	if best.Score < minimumMatchScore {
		return matchedGame{}, queries, cachedQueries, false, nil
	}
	return best, queries, cachedQueries, true, nil
}

type rateLimiter struct {
	delay time.Duration
	last  time.Time
}

func newRateLimiter(rps int) *rateLimiter {
	if rps <= 0 {
		rps = defaultRateLimitRPS
	}
	return &rateLimiter{delay: time.Second / time.Duration(rps)}
}

func (limiter *rateLimiter) wait() {
	if limiter == nil || limiter.delay <= 0 {
		return
	}
	if limiter.last.IsZero() {
		limiter.last = time.Now()
		return
	}
	next := limiter.last.Add(limiter.delay)
	if sleep := time.Until(next); sleep > 0 {
		time.Sleep(sleep)
	}
	limiter.last = time.Now()
}

type queryCache struct {
	db     *sql.DB
	bypass bool
}

func openQueryCache(dataRoot string, bypass bool) (*queryCache, error) {
	path := queryCachePath(dataRoot)
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	cache := &queryCache{db: db, bypass: bypass}
	if err := cache.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return cache, nil
}

func queryCachePath(dataRoot string) string {
	return filepath.Join(dataRoot, ".rommeta", "cache", "igdb.sqlite")
}

func (cache *queryCache) init() error {
	if _, err := cache.db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	_, err := cache.db.Exec(`
CREATE TABLE IF NOT EXISTS igdb_query_cache (
	cache_key TEXT PRIMARY KEY,
	api_base_url TEXT NOT NULL,
	query TEXT NOT NULL,
	response_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_igdb_query_cache_query ON igdb_query_cache(query);
CREATE TABLE IF NOT EXISTS igdb_platform_cache (
	cache_key TEXT PRIMARY KEY,
	api_base_url TEXT NOT NULL,
	response_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
`)
	return err
}

func (cache *queryCache) close() {
	if cache != nil && cache.db != nil {
		_ = cache.db.Close()
	}
}

func queryPlatformNamesCached(client *http.Client, cache *queryCache, limiter *rateLimiter, settings sourceSettings, token accessToken) ([]string, bool, error) {
	key := cacheKey("platforms", settings.APIBaseURL, "")
	if cache != nil && !cache.bypass {
		var responseJSON string
		row := cache.db.QueryRow(`SELECT response_json FROM igdb_platform_cache WHERE cache_key = ?`, key)
		if err := row.Scan(&responseJSON); err == nil {
			var platforms []string
			if err := json.Unmarshal([]byte(responseJSON), &platforms); err == nil {
				return platforms, true, nil
			}
		} else if err != sql.ErrNoRows {
			return nil, false, err
		}
	}
	limiter.wait()
	platforms, err := queryPlatformNames(client, settings.APIBaseURL, settings.ClientID, token)
	if err != nil {
		return nil, false, err
	}
	if cache != nil {
		if err := cache.storePlatforms(key, settings.APIBaseURL, platforms); err != nil {
			return nil, false, err
		}
	}
	return platforms, false, nil
}

func (cache *queryCache) storePlatforms(key, apiBaseURL string, platforms []string) error {
	responseJSON, err := json.Marshal(platforms)
	if err != nil {
		return err
	}
	_, err = cache.db.Exec(`
INSERT INTO igdb_platform_cache (cache_key, api_base_url, response_json, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
	api_base_url = excluded.api_base_url,
	response_json = excluded.response_json,
	created_at = excluded.created_at`,
		key, apiBaseURL, string(responseJSON), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func queryGamesCached(client *http.Client, cache *queryCache, limiter *rateLimiter, settings sourceSettings, token accessToken, term string) ([]apiGame, bool, error) {
	term = cleanSearchTerm(term)
	key := cacheKey("games", settings.APIBaseURL, term)
	if cache != nil && !cache.bypass {
		var responseJSON string
		row := cache.db.QueryRow(`SELECT response_json FROM igdb_query_cache WHERE cache_key = ?`, key)
		if err := row.Scan(&responseJSON); err == nil {
			var games []apiGame
			if err := json.Unmarshal([]byte(responseJSON), &games); err == nil {
				return games, true, nil
			}
		} else if err != sql.ErrNoRows {
			return nil, false, err
		}
	}
	limiter.wait()
	games, err := queryGames(client, settings.APIBaseURL, settings.ClientID, token, term)
	if err != nil {
		return nil, false, err
	}
	if cache != nil {
		if err := cache.storeGames(key, settings.APIBaseURL, term, games); err != nil {
			return nil, false, err
		}
	}
	return games, false, nil
}

func queryGameByIDCached(client *http.Client, cache *queryCache, limiter *rateLimiter, settings sourceSettings, token accessToken, id int) ([]apiGame, bool, error) {
	if id <= 0 {
		return nil, false, nil
	}
	query := strconv.Itoa(id)
	key := cacheKey("game-id", settings.APIBaseURL, query)
	if cache != nil && !cache.bypass {
		var responseJSON string
		row := cache.db.QueryRow(`SELECT response_json FROM igdb_query_cache WHERE cache_key = ?`, key)
		if err := row.Scan(&responseJSON); err == nil {
			var games []apiGame
			if err := json.Unmarshal([]byte(responseJSON), &games); err == nil {
				return games, true, nil
			}
		} else if err != sql.ErrNoRows {
			return nil, false, err
		}
	}
	limiter.wait()
	games, err := queryGameByID(client, settings.APIBaseURL, settings.ClientID, token, id)
	if err != nil {
		return nil, false, err
	}
	if cache != nil {
		if err := cache.storeGames(key, settings.APIBaseURL, "id:"+query, games); err != nil {
			return nil, false, err
		}
	}
	return games, false, nil
}

func (cache *queryCache) storeGames(key, apiBaseURL, term string, games []apiGame) error {
	responseJSON, err := json.Marshal(games)
	if err != nil {
		return err
	}
	_, err = cache.db.Exec(`
INSERT INTO igdb_query_cache (cache_key, api_base_url, query, response_json, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
	api_base_url = excluded.api_base_url,
	query = excluded.query,
	response_json = excluded.response_json,
	created_at = excluded.created_at`,
		key, apiBaseURL, term, string(responseJSON), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func cacheKey(kind, apiBaseURL, value string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{igdbCacheVersion, kind, strings.TrimRight(apiBaseURL, "/"), value}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func queryPlatformNames(client *http.Client, apiBaseURL, clientID string, token accessToken) ([]string, error) {
	body := `fields name;
limit 500;`

	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBaseURL, "/")+"/platforms", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("IGDB platform request could not be created: %w", err)
	}
	request.Header.Set("Client-ID", clientID)
	request.Header.Set("Authorization", "Bearer "+token.Value)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "text/plain")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("IGDB platform request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, fmt.Errorf("IGDB platforms returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var platforms []apiNamed
	if err := json.NewDecoder(response.Body).Decode(&platforms); err != nil {
		return nil, fmt.Errorf("IGDB platform response could not be decoded: %w", err)
	}
	return namedList(platforms), nil
}

func queryGames(client *http.Client, apiBaseURL, clientID string, token accessToken, term string) ([]apiGame, error) {
	body := fmt.Sprintf(`search %s;
fields %s;
limit 10;`, strconv.Quote(cleanSearchTerm(term)), igdbGameFields)

	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBaseURL, "/")+"/games", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("IGDB search request could not be created: %w", err)
	}
	request.Header.Set("Client-ID", clientID)
	request.Header.Set("Authorization", "Bearer "+token.Value)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "text/plain")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("IGDB search request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, fmt.Errorf("IGDB search returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var games []apiGame
	if err := json.NewDecoder(response.Body).Decode(&games); err != nil {
		return nil, fmt.Errorf("IGDB search response could not be decoded: %w", err)
	}
	return games, nil
}

func queryGameByID(client *http.Client, apiBaseURL, clientID string, token accessToken, id int) ([]apiGame, error) {
	body := fmt.Sprintf(`fields %s;
where id = %d;
limit 1;`, igdbGameFields, id)

	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBaseURL, "/")+"/games", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("IGDB game-id request could not be created: %w", err)
	}
	request.Header.Set("Client-ID", clientID)
	request.Header.Set("Authorization", "Bearer "+token.Value)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "text/plain")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("IGDB game-id request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, fmt.Errorf("IGDB game-id returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var games []apiGame
	if err := json.NewDecoder(response.Body).Decode(&games); err != nil {
		return nil, fmt.Errorf("IGDB game-id response could not be decoded: %w", err)
	}
	return games, nil
}

func isFatalSearchError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "401") || strings.Contains(message, "403")
}

func scoreCandidate(candidate apiGame, game catalogGame, terms []string) int {
	return scoreCandidateWithPlatforms(candidate, game, terms, nil, nil)
}

func scoreHasheousCandidate(candidate apiGame, game catalogGame, terms []string, sourcePlatforms []string, registry *platformnames.Registry) int {
	if scoreCandidateWithPlatforms(candidate, game, terms, sourcePlatforms, registry) < minimumMatchScore {
		return 0
	}
	return 220
}

func scoreCandidateWithPlatforms(candidate apiGame, game catalogGame, terms []string, sourcePlatforms []string, registry *platformnames.Registry) int {
	matchesPlatform := platformMatches(game.Platform, candidate.Platforms, sourcePlatforms, registry)
	if len(game.MAMEReleases) > 0 {
		matchesPlatform = arcadePlatformMatches(game, candidate)
		if !arcadeCandidateAllowed(game, candidate) {
			return 0
		}
	}
	// An unscoped homonym is not enough to select artwork for this platform.
	if !matchesPlatform {
		return 0
	}
	if game.UnofficialOnly && len(game.MAMEReleases) == 0 && !apiGameHasUnofficialMarker(candidate) {
		return 0
	}

	targetKeys := map[string]bool{}
	for _, term := range terms {
		for _, key := range titleMatchKeys(term) {
			targetKeys[key] = true
		}
	}
	gameTitleKeys := map[string]bool{}
	for _, key := range titleMatchKeys(game.Title) {
		gameTitleKeys[key] = true
	}

	score := 0
	for _, key := range titleMatchKeys(candidate.Name) {
		switch {
		case gameTitleKeys[key]:
			score = max(score, 125)
		case targetKeys[key]:
			score = max(score, 110)
		}
	}
	for _, alternative := range candidate.AlternativeNames {
		for _, key := range titleMatchKeys(alternative.Name) {
			switch {
			case gameTitleKeys[key]:
				score = max(score, 120)
			case targetKeys[key]:
				score = max(score, 100)
			}
		}
	}
	if score == 0 {
		return 0
	}
	if matchesPlatform {
		score += 20
	}
	if candidate.FirstReleaseDate > 0 {
		score += 1
	}
	return score
}

func apiGameHasUnofficialMarker(candidate apiGame) bool {
	if hasUnofficialTitleMarker(candidate.Name) {
		return true
	}
	for _, alternative := range candidate.AlternativeNames {
		if hasUnofficialTitleMarker(alternative.Name) {
			return true
		}
	}
	return false
}

func metadataFromGame(game apiGame) gameMetadata {
	metadata := gameMetadata{
		ID:              game.ID,
		Relationships:   relationshipsFromGame(game),
		GameType:        game.GameType.Type,
		Title:           strings.TrimSpace(game.Name),
		Slug:            strings.TrimSpace(game.Slug),
		URL:             strings.TrimSpace(game.URL),
		ReleaseDate:     releaseDate(game.FirstReleaseDate),
		Genres:          namedList(game.Genres),
		Themes:          namedList(game.Themes),
		Collections:     namedList(game.Collections),
		Franchises:      namedListWithPrimary(game.Franchise, game.Franchises),
		Platforms:       namedList(game.Platforms),
		Rating:          ratingString(game.TotalRating),
		RatingCount:     game.TotalRatingCount,
		Description:     strings.TrimSpace(firstNonEmpty(game.Summary, game.Storyline)),
		AlternateTitles: namedList(game.AlternativeNames),
		MediaURLs:       mediaURLs(game),
	}
	for _, company := range game.InvolvedCompanies {
		name := strings.TrimSpace(company.Company.Name)
		if name == "" {
			continue
		}
		if company.Developer && metadata.Developer == "" {
			metadata.Developer = name
		}
		if company.Publisher && metadata.Publisher == "" {
			metadata.Publisher = name
		}
	}
	return metadata
}

func releaseDate(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).UTC().Format("2006-01-02")
}

func ratingString(value float64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func namedList(values []apiNamed) []string {
	var result []string
	for _, value := range values {
		result = appendUnique(result, value.Name)
	}
	return result
}

func namedListWithPrimary(primary apiNamed, values []apiNamed) []string {
	result := appendUnique(nil, primary.Name)
	for _, value := range values {
		result = appendUnique(result, value.Name)
	}
	return result
}

func mediaURLs(game apiGame) map[string][]sourceyaml.MediaCandidate {
	result := map[string][]sourceyaml.MediaCandidate{}
	if game.Cover.ImageID != "" {
		result["box_front"] = append(result["box_front"], imageCandidate(igdbFullFrameImage, game.Cover))
	}
	addImages(result, "fanart", igdbFullFrameImage, game.Artworks)
	addImages(result, "screenshot", igdbFullFrameImage, game.Screenshots)
	return result
}

func addImages(target map[string][]sourceyaml.MediaCandidate, key, size string, images []apiImage) {
	seen := map[string]bool{}
	for _, image := range images {
		if image.ImageID == "" || seen[image.ImageID] {
			continue
		}
		seen[image.ImageID] = true
		if len(target[key]) >= maxMediaPerKind {
			return
		}
		target[key] = append(target[key], imageCandidate(size, image))
	}
}

func imageCandidate(size string, image apiImage) sourceyaml.MediaCandidate {
	return sourceyaml.MediaCandidate{
		URL:      imageURL(size, image.ImageID),
		SourceID: image.ImageID,
	}
}

func imageURL(size, imageID string) string {
	return fmt.Sprintf("https://images.igdb.com/igdb/image/upload/t_%s/%s.jpg", size, imageID)
}

func streamCatalogGames(dataRoot, platform, source string, forceOverwrite bool, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	return catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.MetadataReadOptions(platformFilters(platform), source, forceOverwrite), progress, func(cached catalogcache.Game) error {
		return fn(catalogGameFromCached(cached))
	})
}

func catalogGameFromCached(cached catalogcache.Game) catalogGame {
	game := catalogGame{
		ArcadePlatform:   cached.ArcadePlatform,
		Platform:         cached.Platform,
		Folder:           cached.Folder,
		Path:             cached.Path,
		Title:            cached.Title,
		DefaultRelease:   cached.DefaultRelease,
		Aliases:          cached.Aliases,
		Sources:          cached.Sources,
		PreservedSources: cached.PreservedSources,
	}
	releaseCount := 0
	unofficialReleaseCount := 0
	for _, release := range cached.Releases {
		if release.MAMESet != "" {
			game.MAMEReleases = append(game.MAMEReleases, release)
		}
		releaseCount++
		if catalogrules.HasUnofficialTag(release.Tags) {
			unofficialReleaseCount++
		}
		game.Releases = appendUnique(game.Releases, release.Name)
		game.ReleaseTitles = appendUnique(game.ReleaseTitles, release.Title)
	}
	game.UnofficialOnly = releaseCount > 0 && releaseCount == unofficialReleaseCount
	return game
}

func readHasheousIGDBIDs(path string) []int {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return nil
	}
	var ids []int
	inSources := false
	inHasheous := false
	inMatches := false
	for _, raw := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		line := trimComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := countIndent(line)
		value := strings.TrimSpace(line)
		if indent == 0 {
			inSources = value == "sources:"
			inHasheous = false
			inMatches = false
			continue
		}
		if !inSources {
			continue
		}
		if indent == 2 {
			inHasheous = value == "hasheous:"
			inMatches = false
			continue
		}
		if !inHasheous {
			continue
		}
		if indent == 4 {
			inMatches = value == "matches:"
			continue
		}
		if inMatches && indent >= 8 && strings.HasPrefix(value, "igdb_id:") {
			id := parseScalar(strings.TrimSpace(strings.TrimPrefix(value, "igdb_id:")))
			if parsed, err := strconv.Atoi(id); err == nil && parsed > 0 {
				ids = appendUniqueInt(ids, parsed)
			}
		}
	}
	return ids
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func searchTerms(game catalogGame) []string {
	if len(game.MAMEReleases) > 0 {
		return arcadeSearchTerms(game)
	}
	if game.UnofficialOnly {
		return unofficialSearchTerms(game)
	}
	var terms []string
	for _, value := range []string{
		game.Title,
		titleShape(stripTags(game.Title)),
	} {
		terms = appendTermVariants(terms, value)
	}
	for _, alias := range game.Aliases {
		terms = appendTermVariants(terms, alias)
	}
	terms = appendTermVariants(terms, stripTags(game.DefaultRelease))
	for _, title := range game.ReleaseTitles {
		terms = appendTermVariants(terms, title)
	}
	for _, release := range game.Releases {
		terms = appendTermVariants(terms, stripTags(release))
	}
	if len(terms) > maxSearchTerms {
		return terms[:maxSearchTerms]
	}
	return terms
}

func unofficialSearchTerms(game catalogGame) []string {
	var terms []string
	for _, value := range []string{
		game.DefaultRelease,
	} {
		terms = appendUnofficialTermVariants(terms, value)
	}
	for _, release := range game.Releases {
		terms = appendUnofficialTermVariants(terms, release)
	}
	for _, title := range game.ReleaseTitles {
		terms = appendUnofficialTermVariants(terms, title)
	}
	for _, alias := range game.Aliases {
		terms = appendUnofficialTermVariants(terms, alias)
	}
	if len(terms) > maxSearchTerms {
		return terms[:maxSearchTerms]
	}
	return terms
}

func appendUnofficialTermVariants(values []string, value string) []string {
	if !hasUnofficialTitleMarker(value) {
		return values
	}
	return appendTermVariants(values, value)
}

func appendTermVariants(values []string, value string) []string {
	for _, variant := range searchTermVariants(value) {
		values = appendTerm(values, variant)
	}
	return values
}

func hasUnofficialTitleMarker(value string) bool {
	return catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(value))
}

func searchTermVariants(value string) []string {
	value = strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " "))
	if value == "" {
		return nil
	}
	var variants []string
	for _, variant := range []string{
		value,
		splitCamelCaseWords(value),
		slashNumberVariant(value),
		slashNumberVariant(splitCamelCaseWords(value)),
	} {
		if strings.TrimSpace(variant) != "" {
			variants = appendUnique(variants, strings.TrimSpace(repeatedSpace.ReplaceAllString(variant, " ")))
		}
	}
	return variants
}

func appendTerm(values []string, value string) []string {
	value = strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " "))
	if value == "" {
		return values
	}
	key := queryTermKey(value)
	for _, existing := range values {
		if queryTermKey(existing) == key {
			return values
		}
	}
	return append(values, value)
}

func platformMatches(platform string, candidates []apiNamed, sourcePlatforms []string, registry *platformnames.Registry) bool {
	name := displayPlatformName(platform, registry)
	if len(sourcePlatforms) > 0 {
		if registry != nil {
			name = registry.ResolveForSource(platform, platformnames.SourceIGDB, sourcePlatforms)
		} else {
			name = platformnames.ResolveForSource(platform, platformnames.SourceIGDB, sourcePlatforms)
		}
	}
	keys := platformMatchKeys(name)
	for _, candidate := range candidates {
		if keys[looseTitleKey(candidate.Name)] {
			return true
		}
	}
	return false
}

func platformMatchKeys(name string) map[string]bool {
	keys := map[string]bool{}
	addPlatformMatchKey(keys, name)
	for _, part := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		addPlatformMatchKey(keys, part)
	}
	return keys
}

func addPlatformMatchKey(keys map[string]bool, name string) {
	if key := looseTitleKey(name); key != "" {
		keys[key] = true
	}
}

func displayPlatformName(name string, registry *platformnames.Registry) string {
	if registry != nil {
		return registry.ForSource(name, platformnames.SourceIGDB)
	}
	return platformnames.ForSource(name, platformnames.SourceIGDB)
}

func titleMatchKeys(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var keys []string
	for _, variant := range []string{
		value,
		stripTags(value),
		titleShape(stripTags(value)),
		strings.ReplaceAll(titleShape(stripTags(value)), ": ", " - "),
	} {
		for _, key := range []string{looseTitleKey(variant), compactTitleKey(variant)} {
			if key != "" {
				keys = appendUnique(keys, key)
			}
		}
	}
	return keys
}

func stripTags(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " "))
}

func titleShape(value string) string {
	parts := strings.Split(value, " - ")
	for i, part := range parts {
		parts[i] = moveTrailingArticle(part)
	}
	return strings.Join(parts, ": ")
}

func moveTrailingArticle(value string) string {
	for _, article := range []string{", The", ", A", ", An"} {
		if strings.HasSuffix(value, article) {
			base := strings.TrimSpace(strings.TrimSuffix(value, article))
			return strings.TrimSpace(strings.TrimPrefix(article, ", ")) + " " + base
		}
	}
	return value
}

func looseTitleKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '&':
			b.WriteString(" and ")
		default:
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(repeatedSpace.ReplaceAllString(b.String(), " "))
}

func compactTitleKey(value string) string {
	return strings.ReplaceAll(looseTitleKey(value), " ", "")
}

func splitCamelCaseWords(value string) string {
	var b strings.Builder
	var previous rune
	for index, r := range value {
		if index > 0 && unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		previous = r
	}
	return b.String()
}

func slashNumberVariant(value string) string {
	return numberDashPattern.ReplaceAllString(value, "$1/$2")
}

func cleanSearchTerm(value string) string {
	value = strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " "))
	value = strings.ReplaceAll(value, ";", " ")
	return value
}

func queryTermKey(value string) string {
	return strings.ToLower(strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " ")))
}

func renderMetadata(match matchedGame) string {
	var b bytes.Buffer
	m := match.Metadata
	fmt.Fprintf(&b, "    matches:\n")
	fmt.Fprintf(&b, "      - release: %s\n", yamlString(match.Release))
	if m.ID > 0 {
		fmt.Fprintf(&b, "        igdb_id: %d\n", m.ID)
	}
	if m.Title != "" {
		fmt.Fprintf(&b, "        title: %s\n", yamlString(m.Title))
	}
	if m.Slug != "" {
		fmt.Fprintf(&b, "        slug: %s\n", yamlString(m.Slug))
	}
	if m.URL != "" {
		fmt.Fprintf(&b, "        references:\n")
		fmt.Fprintf(&b, "          page: %s\n", yamlString(m.URL))
	}
	if m.ReleaseDate != "" {
		fmt.Fprintf(&b, "        release_date: %s\n", yamlString(m.ReleaseDate))
	}
	if m.Developer != "" {
		fmt.Fprintf(&b, "        developer: %s\n", yamlString(m.Developer))
	}
	if m.Publisher != "" {
		fmt.Fprintf(&b, "        publisher: %s\n", yamlString(m.Publisher))
	}
	if len(m.Genres) > 0 {
		fmt.Fprintf(&b, "        genres: %s\n", yamlStringList(m.Genres))
	}
	if len(m.Themes) > 0 {
		fmt.Fprintf(&b, "        themes: %s\n", yamlStringList(m.Themes))
	}
	if len(m.Collections) > 0 {
		fmt.Fprintf(&b, "        collections: %s\n", yamlStringList(m.Collections))
	}
	if len(m.Franchises) > 0 {
		fmt.Fprintf(&b, "        franchises: %s\n", yamlStringList(m.Franchises))
	}
	if len(m.Platforms) > 0 {
		fmt.Fprintf(&b, "        platforms: %s\n", yamlStringList(m.Platforms))
	}
	if m.Rating != "" {
		fmt.Fprintf(&b, "        rating: %s\n", yamlString(m.Rating))
	}
	if m.RatingCount > 0 {
		fmt.Fprintf(&b, "        rating_count: %d\n", m.RatingCount)
	}
	if len(m.AlternateTitles) > 0 {
		fmt.Fprintf(&b, "        alternate_titles: %s\n", yamlStringList(m.AlternateTitles))
	}
	if match.MatchedQuery != "" {
		fmt.Fprintf(&b, "        matched_query: %s\n", yamlString(match.MatchedQuery))
	}
	if match.Score > 0 {
		fmt.Fprintf(&b, "        match_score: %d\n", match.Score)
	}
	sourceyaml.RenderRelationships(&b, "        ", m.Relationships)
	if m.GameType != "" {
		fmt.Fprintf(&b, "        game_type: %s\n", yamlString(m.GameType))
	}
	sourceyaml.RenderEnglishDescription(&b, "        ", m.Description)
	sourceyaml.RenderMedia(&b, "        ", m.MediaURLs)
	return b.String()
}

func renderNotFoundMetadata() string {
	return fmt.Sprintf("    status: not_found\n    checked_at: %s\n", yamlString(time.Now().UTC().Format(time.RFC3339)))
}

func trimComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}

func parseScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		if value[0] == '"' && value[len(value)-1] == '"' {
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func splitKV(value string) (string, string, bool) {
	key, raw, ok := strings.Cut(value, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(raw), true
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func yamlStringList(values []string) string {
	var quoted []string
	for _, value := range values {
		quoted = append(quoted, yamlString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func appendUnique(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

func appendUniqueInt(values []int, next int) []int {
	if next <= 0 {
		return values
	}
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var tagPattern = regexp.MustCompile(`\s*[\(\[].*?[\)\]]\s*`)
var numberDashPattern = regexp.MustCompile(`\b([0-9]+)-([0-9]+)\b`)
var repeatedSpace = regexp.MustCompile(`\s+`)
