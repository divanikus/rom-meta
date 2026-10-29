package screenscraper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"rommeta/internal/appinfo"
	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/fingerprintselect"
	"rommeta/internal/metadate"
	"rommeta/internal/platformnames"
	"rommeta/internal/sourceurl"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"rommeta/internal/titlematch"
)

// ImportOptions controls ScreenScraper metadata enrichment.
type ImportOptions struct {
	DataRoot         string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	ForceOverwrite   bool
	Limit            int
	Client           *http.Client
	Progress         ProgressFunc
}

// ProgressFunc receives progress updates during applied ScreenScraper searches.
type ProgressFunc func(ImportProgress)

// ImportProgress describes a ScreenScraper search progress update.
type ImportProgress struct {
	Current int
	Total   int
	Stage   string
	Game    string
	Queries int
	Matches int
}

// ImportResult describes a dry-run or applied ScreenScraper enrichment.
type ImportResult struct {
	DataRoot             string
	APIBaseURL           string
	PlatformCount        int
	GameBundleCount      int
	SelectedGameCount    int
	SkippedByLimitCount  int
	PlannedSearchCount   int
	QueriedSearchCount   int
	MatchedGameCount     int
	GameFileCount        int
	NotFoundGameCount    int
	WrittenFileCount     int
	SkippedExistingCount int
	FailedSearchCount    int
	SkippedUnmappedCount int
	UnmappedPlatforms    map[string]int
	Failures             []ImportFailure
	RateLimitPerSecond   int
	PlatformListCount    int
	UsedCachedPlatforms  bool
	Limit                int
	Apply                bool
}

// ImportFailure is a failed ScreenScraper search that did not stop the whole run.
type ImportFailure struct {
	Game  string
	Query string
	Error string
}

type catalogGame struct {
	MAMEReleases   []catalogcache.Release
	Platform       string
	Folder         string
	Path           string
	Title          string
	DefaultRelease string
	Releases       []string
	ReleaseTitles  []string
	Aliases        []string
	Sources        []string
	Fingerprints   []catalogcache.Fingerprint
	UnofficialOnly bool
}

type sourceSettings struct {
	Username           string
	Password           string
	DevID              string
	DevPassword        string
	SoftName           string
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
	ID              string
	Title           string
	ReleaseDate     string
	Developer       string
	Publisher       string
	Genres          []string
	Players         string
	Rating          string
	Descriptions    map[string]string
	AlternateTitles []string
	TitleCandidates []sourceyaml.TitleCandidate
	MediaURLs       map[string][]sourceyaml.MediaCandidate
	MatchedROM      string
	SystemID        string
	SystemParentID  string
	ROMs            []screenScraperROM
}

type rawMedia struct {
	Key        string
	URL        string
	Region     string
	Language   string
	Format     string
	SourceType string
	CRC32      string
	MD5        string
	SHA1       string
}

type screenScraperROM struct {
	Filename string
	MD5      string
	Size     int64
}

type screenScraperQuery struct {
	Term    string
	Release string
	MD5     string
	Size    int64
	ROMName string
}

type screenScraperSystem struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Names []string `json:"names,omitempty"`
}

type systemsCache struct {
	APIBaseURL string                `json:"api_base_url"`
	FetchedAt  string                `json:"fetched_at"`
	Systems    []screenScraperSystem `json:"systems"`
}

const (
	defaultAPIBaseURL   = "https://api.screenscraper.fr/api2"
	defaultRateLimitRPS = 1
	maxSearchTerms      = 16
	maxHashSearches     = 2
	maxMediaPerKind     = 5
	systemsCacheMaxAge  = 7 * 24 * time.Hour
	minimumMatchScore   = 100
)

// Import searches ScreenScraper for one candidate per catalog game and
// optionally writes a source-specific stanza into game.yaml.
func Import(options ImportOptions) (ImportResult, error) {
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
	settings, err := screenScraperSettings(config)
	if err != nil {
		return ImportResult{}, err
	}

	result := ImportResult{
		DataRoot:           dataRoot,
		APIBaseURL:         settings.APIBaseURL,
		RateLimitPerSecond: settings.RateLimitPerSecond,
		Apply:              options.Apply,
		Limit:              options.Limit,
		UnmappedPlatforms:  make(map[string]int),
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}

	var systems []screenScraperSystem
	pacer := newSearchPacer(settings.RateLimitPerSecond)
	setupDone := false
	setupApply := func() error {
		if setupDone || !options.Apply {
			setupDone = true
			return nil
		}
		reportStage(options.Progress, options.Apply, "checking credentials", 0, 0, "ScreenScraper")
		if err := ensureCredentials(settings); err != nil {
			return err
		}

		reportStage(options.Progress, options.Apply, "loading platform list", 0, 0, "ScreenScraper")
		var cached bool
		var err error
		systems, cached, err = screenScraperSystems(client, dataRoot, settings, options.ForceOverwrite)
		if err != nil {
			return err
		}
		if !cached {
			pacer.last = time.Now()
		}
		result.PlatformListCount = len(systems)
		result.UsedCachedPlatforms = cached
		setupDone = true
		return nil
	}

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
	platformCount, gameCount, err := streamCatalogGames(dataRoot, options.Platform, "screenscraper", options.ForceOverwrite, streamProgress, func(game catalogGame) error {
		if !options.ForceOverwrite && catalogcache.HasSource(game.Sources, "screenscraper") {
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
		terms := searchTerms(game)
		plannedQueries := len(fingerprintQueries(game)) + len(terms)
		if len(game.MAMEReleases) > 0 {
			plannedQueries = len(mameQueries(game))
		}
		if !options.Apply {
			result.PlannedSearchCount += plannedQueries
			return nil
		}
		if err := setupApply(); err != nil {
			return err
		}

		systemID, mapped := screenScraperSystemID(game.Platform, systems, options.PlatformRegistry)
		if !mapped {
			result.SkippedUnmappedCount++
			result.UnmappedPlatforms[game.Platform]++
			return nil
		}
		result.PlannedSearchCount += plannedQueries
		match, queries, ok, err := searchBestMatch(client, settings, game, terms, systemID, pacer, func(stage string, queries int) {
			if options.Progress != nil {
				options.Progress(ImportProgress{Stage: stage, Current: current, Total: total,
					Game: game.Platform + " / " + game.Title, Queries: result.QueriedSearchCount + queries, Matches: result.MatchedGameCount})
			}
		})
		result.QueriedSearchCount += queries
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
			if err := sourceyaml.WriteSourceMetadata(out, "screenscraper", renderNotFoundMetadata()); err != nil {
				return err
			}
			result.WrittenFileCount++
			return nil
		}
		result.MatchedGameCount++
		result.GameFileCount++
		out := filepath.Join(game.Path, "game.yaml")
		if err := sourceyaml.WriteSourceMetadata(out, "screenscraper", renderMetadata(match)); err != nil {
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

func screenScraperSettings(config store.SourcesConfig) (sourceSettings, error) {
	var source store.Source
	found := false
	for _, candidate := range config.Sources {
		if candidate.Name == "screenscraper" {
			source = candidate
			found = true
			break
		}
	}
	if !found {
		return sourceSettings{}, fmt.Errorf("source screenscraper not found in %s", config.Path)
	}

	settings := sourceSettings{
		SoftName:           appinfo.DisplayName(),
		APIBaseURL:         strings.TrimRight(firstNonEmpty(source.APIBaseURL, defaultAPIBaseURL), "/"),
		RateLimitPerSecond: source.RateLimitPerSecond,
	}
	if settings.RateLimitPerSecond <= 0 {
		settings.RateLimitPerSecond = defaultRateLimitRPS
	}
	if value, ok := store.CredentialValue(config, source, "username"); ok {
		settings.Username = value
	}
	if value, ok := store.CredentialValue(config, source, "password"); ok {
		settings.Password = value
	}
	if value, ok := store.CredentialValue(config, source, "dev_id"); ok {
		settings.DevID = value
	} else if config.Secrets.Exists && config.Secrets.Err == nil {
		settings.DevID = config.Secrets.Values["screenscraper"]["dev_id"]
	}
	if value, ok := store.CredentialValue(config, source, "dev_password"); ok {
		settings.DevPassword = value
	} else if config.Secrets.Exists && config.Secrets.Err == nil {
		settings.DevPassword = config.Secrets.Values["screenscraper"]["dev_password"]
	}
	return settings, nil
}

func ensureCredentials(settings sourceSettings) error {
	var missing []string
	if strings.TrimSpace(settings.Username) == "" {
		missing = append(missing, "sources.screenscraper.username")
	}
	if strings.TrimSpace(settings.Password) == "" {
		missing = append(missing, "sources.screenscraper.password")
	}
	if strings.TrimSpace(settings.DevID) == "" {
		missing = append(missing, "sources.screenscraper.dev_id")
	}
	if strings.TrimSpace(settings.DevPassword) == "" {
		missing = append(missing, "sources.screenscraper.dev_password")
	}
	if len(missing) > 0 {
		return fmt.Errorf("ScreenScraper credentials are missing: fill %s in config/secrets.yaml", strings.Join(missing, ", "))
	}
	return nil
}

func searchBestMatch(client *http.Client, settings sourceSettings, game catalogGame, terms []string, systemID string, pacer *searchPacer, progress func(string, int)) (matchedGame, int, bool, error) {
	var best matchedGame
	queries := 0
	runQuery := func(query screenScraperQuery) (gameMetadata, bool, error) {
		for attempt := 0; ; attempt++ {
			pacer.wait()
			queries++
			if progress != nil {
				progress("querying", queries)
			}
			metadata, found, err := queryGameWithFingerprint(client, settings, systemID, query)
			if attempt > 0 || !isTimeout(err) {
				return metadata, found, err
			}
			if progress != nil {
				progress("retrying timeout in 2s", queries)
			}
			time.Sleep(2 * time.Second)
		}
	}

	if len(game.MAMEReleases) > 0 {
		// Exact set evidence is required; never search another card's clone parent.
		for _, query := range mameQueries(game) {
			metadata, found, err := runQuery(query)
			if err != nil {
				if isNotFoundError(err) {
					continue
				}
				return matchedGame{}, queries, false, err
			}
			if !found || isNonGameMetadata(metadata) || !metadataMatchesMAMESet(metadata, query, systemID) {
				continue
			}
			return matchedGame{Release: query.Release, MatchedQuery: query.ROMName, Score: 200, Metadata: metadata}, queries, true, nil
		}
		return matchedGame{}, queries, false, nil
	}
	fingerprintMismatch := false
	nonGameResponse := false
	for _, query := range fingerprintQueries(game) {
		metadata, found, err := runQuery(query)
		if err != nil {
			if isNotFoundError(err) {
				continue
			}
			return matchedGame{}, queries, false, err
		}
		if !found {
			continue
		}
		if isNonGameMetadata(metadata) {
			if query.MD5 == "" || len(metadata.ROMs) == 0 || !namedNonGameMatchesCatalog(metadata, game, query.Release) {
				nonGameResponse = true
				continue
			}
		}
		if !metadataMatchesFingerprint(metadata, query) {
			fingerprintMismatch = true
			continue
		}
		return matchedGame{
			Release:      firstNonEmpty(query.Release, game.DefaultRelease, game.Title),
			MatchedQuery: query.Label(),
			Score:        200,
			Metadata:     metadata,
		}, queries, true, nil
	}
	// A service bucket or a different ROM is not evidence for a title fallback.
	if fingerprintMismatch || nonGameResponse {
		return matchedGame{}, queries, false, nil
	}

	for _, term := range terms {
		metadata, found, err := runQuery(screenScraperQuery{Term: term})
		if err != nil {
			if isNotFoundError(err) {
				continue
			}
			return matchedGame{}, queries, false, err
		}
		if !found {
			continue
		}
		if isNonGameMetadata(metadata) {
			continue
		}
		score := scoreCandidate(metadata, game, terms)
		if score < minimumMatchScore || score <= best.Score {
			continue
		}
		best = matchedGame{
			Release:      firstNonEmpty(game.DefaultRelease, game.Title),
			MatchedQuery: term,
			Score:        score,
			Metadata:     metadata,
		}
		if best.Score >= 140 {
			break
		}
	}
	if best.Score < minimumMatchScore {
		return matchedGame{}, queries, false, nil
	}
	return best, queries, true, nil
}

// ScreenScraper uses shared non-game buckets for unrelated ROMs. They must
// never become game identities, even when their ROM list contains our hash.
func isNonGameMetadata(metadata gameMetadata) bool {
	isBucket := func(title string) bool {
		title = strings.ToLower(strings.TrimSpace(title))
		return title == "#nongame" || strings.HasPrefix(title, "zzz(notgame):")
	}
	if isBucket(metadata.Title) {
		return true
	}
	for _, title := range metadata.AlternateTitles {
		if isBucket(title) {
			return true
		}
	}
	return false
}

// Named utilities/demos may have useful artwork despite ScreenScraper's marker.
// Accept only a catalog-backed name on a fingerprint lookup. Keep the original
// marker in saved metadata so this exception cannot enable service-ID folding.
func namedNonGameMatchesCatalog(metadata gameMetadata, game catalogGame, release string) bool {
	const prefix = "zzz(notgame):"
	title := strings.TrimSpace(metadata.Title)
	if !strings.HasPrefix(strings.ToLower(title), prefix) {
		return false
	}
	for _, candidate := range append([]string{title}, metadata.AlternateTitles...) {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(strings.ToLower(candidate), prefix) {
			candidate = strings.TrimSpace(candidate[len(prefix):])
		}
		if strings.HasPrefix(candidate, "#") || strings.EqualFold(candidate, "pas un jeu") || strings.EqualFold(candidate, "demo disc") {
			return false
		}
	}
	name := compactTitleKey(title[len(prefix):])
	if name == "" {
		return false
	}
	catalogNames := append([]string{game.Title, stripTags(release)}, game.Aliases...)
	for _, candidate := range catalogNames {
		if name == compactTitleKey(candidate) {
			return true
		}
	}
	return false
}

func screenScraperSystems(client *http.Client, dataRoot string, settings sourceSettings, refresh bool) ([]screenScraperSystem, bool, error) {
	cachePath := screenScraperSystemsCachePath(dataRoot)
	if !refresh {
		if systems, ok := readSystemsCache(cachePath, settings.APIBaseURL); ok {
			return systems, true, nil
		}
	}
	systems, err := querySystems(client, settings)
	if err != nil {
		return nil, false, err
	}
	if len(systems) == 0 {
		return nil, false, fmt.Errorf("ScreenScraper platform list returned no systems")
	}
	if err := writeSystemsCache(cachePath, settings.APIBaseURL, systems); err != nil {
		return nil, false, err
	}
	return systems, false, nil
}

func screenScraperSystemsCachePath(dataRoot string) string {
	return filepath.Join(dataRoot, ".rommeta", "sources", "screenscraper", "systems.json")
}

func readSystemsCache(path, apiBaseURL string) ([]screenScraperSystem, bool) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cache systemsCache
	if err := json.Unmarshal(contents, &cache); err != nil {
		return nil, false
	}
	if cache.APIBaseURL != apiBaseURL || len(cache.Systems) == 0 {
		return nil, false
	}
	fetchedAt, err := time.Parse(time.RFC3339, cache.FetchedAt)
	if err != nil || time.Since(fetchedAt) >= systemsCacheMaxAge {
		return nil, false
	}
	return cache.Systems, true
}

func writeSystemsCache(path, apiBaseURL string, systems []screenScraperSystem) error {
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cache := systemsCache{
		APIBaseURL: apiBaseURL,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		Systems:    systems,
	}
	contents, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	return catalogfs.WriteFile(path, contents, 0o644)
}

func querySystems(client *http.Client, settings sourceSettings) ([]screenScraperSystem, error) {
	params := url.Values{}
	if strings.TrimSpace(settings.DevID) != "" {
		params.Set("devid", settings.DevID)
	}
	if strings.TrimSpace(settings.DevPassword) != "" {
		params.Set("devpassword", settings.DevPassword)
	}
	params.Set("softname", settings.SoftName)
	params.Set("output", "json")
	params.Set("ssid", settings.Username)
	params.Set("sspassword", settings.Password)

	endpoint := strings.TrimRight(settings.APIBaseURL, "/") + "/systemesListe.php?" + params.Encode()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ScreenScraper platform list request could not be created: %w", withoutRequestURL(err))
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ScreenScraper platform list request failed: %w", withoutRequestURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode == 430 {
		return nil, ErrDailyQuota
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("ScreenScraper platform list response could not be read: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if response.StatusCode == http.StatusForbidden && isDeveloperCredentialMessage(message) {
			return nil, fmt.Errorf("ScreenScraper platform list returned %s: developer credentials appear to be required for RomMeta API access", response.Status)
		}
		return nil, fmt.Errorf("ScreenScraper platform list returned %s: %s", response.Status, message)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("ScreenScraper platform list returned: %s", trimmed)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ScreenScraper platform list response could not be decoded: %w", err)
	}
	systems := systemsFromResponse(parsed)
	sort.Slice(systems, func(i, j int) bool {
		return systems[i].Name < systems[j].Name
	})
	return systems, nil
}

func systemsFromResponse(parsed map[string]any) []screenScraperSystem {
	root := any(parsed)
	if response, ok := parsed["response"]; ok {
		root = response
	}
	var systems []screenScraperSystem
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if system, ok := systemFromObject(typed); ok {
				systems = appendUniqueSystem(systems, system)
				return
			}
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(root)
	return systems
}

func systemFromObject(object map[string]any) (screenScraperSystem, bool) {
	id := firstNonEmpty(
		asString(object["id"]),
		asString(object["idsysteme"]),
		asString(object["idSysteme"]),
		asString(object["systemeid"]),
		asString(object["systemeId"]),
		asString(object["systeme_id"]),
		asString(object["id_systeme"]),
	)
	names := systemNames(object)
	name := firstNonEmpty(names...)
	if id == "" || name == "" {
		return screenScraperSystem{}, false
	}
	return screenScraperSystem{ID: id, Name: name, Names: names}, true
}

func systemNames(object map[string]any) []string {
	var names []string
	for _, key := range []string{"nom", "name", "systeme", "system", "fullname"} {
		names = appendSystemNames(names, asString(object[key]))
	}
	names = appendSystemNames(names, nestedText(object["nomcourt"]))
	names = appendSystemNamesFromValue(names, object["noms"])
	return names
}

func appendSystemNamesFromValue(names []string, value any) []string {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			names = appendSystemNamesFromValue(names, item)
		}
	case map[string]any:
		for _, key := range []string{
			"nom_eu",
			"nom_us",
			"nom_jp",
			"nom_wor",
			"nom_recalbox",
			"nom_retropie",
			"nom_launchbox",
			"nom_hyperspin",
			"noms_commun",
			"text",
			"nom",
			"name",
		} {
			names = appendSystemNames(names, asString(typed[key]))
		}
	default:
		names = appendSystemNames(names, asString(value))
	}
	return names
}

func appendSystemNames(names []string, value string) []string {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		names = appendUnique(names, part)
	}
	return names
}

func appendUniqueSystem(systems []screenScraperSystem, next screenScraperSystem) []screenScraperSystem {
	for index, system := range systems {
		if system.ID == next.ID {
			for _, name := range next.Names {
				systems[index].Names = appendUnique(systems[index].Names, name)
			}
			if systems[index].Name == "" {
				systems[index].Name = firstNonEmpty(systems[index].Names...)
			}
			return systems
		}
	}
	return append(systems, next)
}

func queryGame(client *http.Client, settings sourceSettings, systemID, term string) (gameMetadata, bool, error) {
	return queryGameWithFingerprint(client, settings, systemID, screenScraperQuery{Term: term})
}

func queryGameWithFingerprint(client *http.Client, settings sourceSettings, systemID string, query screenScraperQuery) (gameMetadata, bool, error) {
	params := url.Values{}
	if strings.TrimSpace(settings.DevID) != "" {
		params.Set("devid", settings.DevID)
	}
	if strings.TrimSpace(settings.DevPassword) != "" {
		params.Set("devpassword", settings.DevPassword)
	}
	params.Set("softname", settings.SoftName)
	params.Set("output", "json")
	params.Set("ssid", settings.Username)
	params.Set("sspassword", settings.Password)
	params.Set("systemeid", systemID)
	params.Set("romnom", cleanSearchTerm(query.Term))
	if query.ROMName != "" {
		params.Set("romnom", query.ROMName)
		params.Set("romtype", "rom")
	}
	if query.MD5 != "" && query.Size > 0 {
		// jeuInfos expects a ROM filename with its extension, not a cleaned title.
		if query.ROMName != "" {
			params.Set("romnom", query.ROMName)
		}
		params.Set("romtype", "rom")
		params.Set("md5", query.MD5)
		params.Set("romtaille", strconv.FormatInt(query.Size, 10))
	}

	endpoint := strings.TrimRight(settings.APIBaseURL, "/") + "/jeuInfos.php?" + params.Encode()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return gameMetadata{}, false, fmt.Errorf("ScreenScraper search request could not be created: %w", withoutRequestURL(err))
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return gameMetadata{}, false, fmt.Errorf("ScreenScraper search request failed: %w", withoutRequestURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode == 430 {
		return gameMetadata{}, false, ErrDailyQuota
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return gameMetadata{}, false, fmt.Errorf("ScreenScraper search response could not be read: %w", err)
	}
	if response.StatusCode == 431 && strings.Contains(strings.ToLower(string(body)), "repassez demain") {
		return gameMetadata{}, false, ErrDailyQuota
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if response.StatusCode == http.StatusForbidden && isDeveloperCredentialMessage(message) {
			return gameMetadata{}, false, &searchResponseError{statusCode: response.StatusCode, message: "developer credentials appear to be required for RomMeta API access; ordinary user credentials alone were rejected"}
		}
		return gameMetadata{}, false, &searchResponseError{statusCode: response.StatusCode, message: message}
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return gameMetadata{}, false, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		if isScreenScraperNotFoundMessage(trimmed) {
			return gameMetadata{}, false, nil
		}
		return gameMetadata{}, false, &searchResponseError{statusCode: response.StatusCode, message: trimmed}
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return gameMetadata{}, false, fmt.Errorf("ScreenScraper search response could not be decoded: %w", err)
	}
	jeu := objectAt(parsed, "response", "jeu")
	if len(jeu) == 0 {
		return gameMetadata{}, false, nil
	}
	metadata := metadataFromGame(jeu)
	// The API can return a global name match despite systemeid. An explicit
	// different system cannot establish this platform's game or ROM identity.
	if metadata.SystemID != "" && metadata.SystemID != systemID && metadata.SystemParentID != systemID {
		return gameMetadata{}, false, nil
	}
	if metadata.ID == "" && metadata.Title == "" {
		return gameMetadata{}, false, nil
	}
	return metadata, true, nil
}

func (q screenScraperQuery) Label() string {
	if q.MD5 == "" {
		return q.Term
	}
	hash := q.MD5
	if len(hash) > 8 {
		hash = hash[:8]
	}
	return "md5:" + hash
}

func isScreenScraperNotFoundMessage(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "not found") ||
		strings.Contains(value, "non trouv") ||
		strings.Contains(value, "introuvable")
}

func isDeveloperCredentialMessage(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "identifiants développeur") ||
		strings.Contains(value, "identifiants developpeur") ||
		strings.Contains(value, "developer")
}

func scoreCandidate(metadata gameMetadata, game catalogGame, terms []string) int {
	targetKeys := map[string]bool{}
	for _, term := range terms {
		for _, key := range titleMatchKeys(term) {
			if key == "" {
				continue
			}
			targetKeys[key] = true
		}
	}
	gameTitleKeys := map[string]bool{}
	for _, key := range titleMatchKeys(game.Title) {
		gameTitleKeys[key] = true
	}
	if game.UnofficialOnly && !metadataHasUnofficialMarker(metadata) {
		return 0
	}

	score := 0
	for _, key := range titleMatchKeys(metadata.Title) {
		switch {
		case gameTitleKeys[key]:
			score = max(score, 125)
		case targetKeys[key]:
			score = max(score, 110)
		}
	}
	for _, alternative := range metadata.AlternateTitles {
		for _, key := range titleMatchKeys(alternative) {
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
	score += 20
	if metadata.ReleaseDate != "" {
		score += 1
	}
	return score
}

func metadataHasUnofficialMarker(metadata gameMetadata) bool {
	if hasUnofficialTitleMarker(metadata.Title) {
		return true
	}
	for _, alternative := range metadata.AlternateTitles {
		if hasUnofficialTitleMarker(alternative) {
			return true
		}
	}
	return false
}

func metadataFromGame(jeu map[string]any) gameMetadata {
	rom, _ := jeu["rom"].(map[string]any)
	system, _ := jeu["systeme"].(map[string]any)
	metadata := gameMetadata{
		MatchedROM:      asString(rom["romfilename"]),
		SystemID:        asString(system["id"]),
		SystemParentID:  asString(system["parentid"]),
		ID:              firstNonEmpty(asString(jeu["id"]), asString(jeu["idjeu"])),
		Title:           firstNonEmpty(asString(jeu["nom"]), preferredText(jeu["noms"], "us", "wor", "eu", "jp")),
		ReleaseDate:     metadate.Normalize(preferredText(jeu["dates"], "us", "wor", "eu", "jp")),
		Developer:       nestedText(jeu["developpeur"]),
		Publisher:       nestedText(jeu["editeur"]),
		Genres:          textList(jeu["genres"]),
		Players:         nestedText(jeu["joueurs"]),
		Rating:          nestedText(jeu["note"]),
		Descriptions:    localizedTextMap(jeu["synopsis"]),
		AlternateTitles: textList(jeu["noms"]),
		TitleCandidates: titleCandidates(jeu["noms"]),
		MediaURLs:       mediaURLs(jeu),
		ROMs:            romsFromGame(jeu),
	}
	if metadata.Title == "" {
		metadata.Title = firstNonEmpty(metadata.AlternateTitles...)
	}
	return metadata
}

func romsFromGame(jeu map[string]any) []screenScraperROM {
	var roms []screenScraperROM
	for _, item := range arrayOfObjects(jeu["roms"]) {
		md5 := strings.ToLower(firstNonEmpty(
			asString(item["rommd5"]),
			asString(item["md5"]),
			asString(item["rom_md5"]),
		))
		size := int64Value(firstNonEmpty(
			asString(item["romsize"]),
			asString(item["romtaille"]),
			asString(item["size"]),
			asString(item["taille"]),
		))
		filename := asString(item["romfilename"])
		if md5 == "" && size == 0 && filename == "" {
			continue
		}
		roms = append(roms, screenScraperROM{Filename: filename, MD5: md5, Size: size})
	}
	return roms
}

func metadataMatchesFingerprint(metadata gameMetadata, query screenScraperQuery) bool {
	md5 := strings.ToLower(strings.TrimSpace(query.MD5))
	if md5 == "" {
		return true
	}
	if len(metadata.ROMs) == 0 {
		return true
	}
	for _, rom := range metadata.ROMs {
		if strings.ToLower(strings.TrimSpace(rom.MD5)) != md5 {
			continue
		}
		if query.Size > 0 && rom.Size > 0 && query.Size != rom.Size {
			continue
		}
		return true
	}
	return false
}

func int64Value(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func mediaURLs(jeu map[string]any) map[string][]sourceyaml.MediaCandidate {
	result := map[string][]sourceyaml.MediaCandidate{}
	var medias []rawMedia
	for _, item := range arrayOfObjects(jeu["medias"]) {
		sourceType := asString(item["type"])
		key := mediaKey(sourceType)
		if key == "" {
			continue
		}
		mediaURL := sourceurl.StripSecrets(asString(item["url"]))
		if !strings.HasPrefix(mediaURL, "http://") && !strings.HasPrefix(mediaURL, "https://") {
			continue
		}
		medias = append(medias, rawMedia{
			Key:        key,
			URL:        mediaURL,
			Region:     firstNonEmpty(asString(item["region"]), asString(item["regionid"])),
			Language:   firstNonEmpty(asString(item["langue"]), asString(item["langueid"]), asString(item["language"])),
			Format:     asString(item["format"]),
			SourceType: sourceType,
			CRC32:      asString(item["crc"]),
			MD5:        asString(item["md5"]),
			SHA1:       asString(item["sha1"]),
		})
	}
	addMediaURLs(result, medias)
	return result
}

func addMediaURLs(target map[string][]sourceyaml.MediaCandidate, medias []rawMedia) {
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, media := range medias {
		if media.Key == "" || media.URL == "" {
			continue
		}
		if seen[media.URL] {
			continue
		}
		seen[media.URL] = true
		counts[media.Key]++
		if counts[media.Key] > maxMediaPerKind {
			continue
		}
		target[media.Key] = append(target[media.Key], sourceyaml.MediaCandidate{
			URL:        media.URL,
			Region:     media.Region,
			Language:   media.Language,
			Format:     media.Format,
			SourceType: media.SourceType,
			CRC32:      media.CRC32,
			MD5:        media.MD5,
			SHA1:       media.SHA1,
		})
	}
}

func mediaKey(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	key = strings.NewReplacer(" ", "-", "_", "-", ".", "-", "/", "-", "\\", "-").Replace(key)
	key = repeatedDash.ReplaceAllString(key, "-")
	switch key {
	case "box-2d", "box2d", "box-front", "box-front-2d", "cover", "cover-2d":
		return "box_front"
	case "box-2d-back", "box-back", "back", "cover-back":
		return "box_back"
	case "box-3d", "box3d", "cover-3d":
		return "box_3d"
	case "wheel", "wheel-hd", "logo":
		return "logo"
	case "marquee", "screenmarquee":
		return "banner"
	case "ss", "screenshot", "snap":
		return "screenshot"
	case "sstitle", "title", "title-screen":
		return "title_screen"
	case "fanart", "fan-art":
		return "fanart"
	case "cart", "cart-front", "cartridge", "support-2d":
		return "cart_front"
	case "cart-3d", "support-3d":
		return "cart_3d"
	default:
		return ""
	}
}

func preferredText(value any, priorities ...string) string {
	objects := arrayOfObjects(value)
	if len(objects) == 0 {
		return firstNonEmpty(asString(value), nestedText(value))
	}
	for _, priority := range priorities {
		for _, object := range objects {
			if regionLanguageMatches(object, priority) {
				if text := objectText(object); text != "" {
					return text
				}
			}
		}
	}
	for _, object := range objects {
		if text := objectText(object); text != "" {
			return text
		}
	}
	return ""
}

func localizedTextMap(value any) map[string]string {
	result := map[string]string{}
	objects := arrayOfObjects(value)
	if len(objects) == 0 {
		if text := firstNonEmpty(asString(value), nestedText(value)); text != "" {
			result["und"] = text
		}
		return result
	}
	for _, object := range objects {
		text := objectText(object)
		if text == "" {
			continue
		}
		language := objectLanguage(object)
		if language == "" {
			language = "und"
		}
		if _, exists := result[language]; !exists {
			result[language] = text
		}
	}
	return result
}

func titleCandidates(value any) []sourceyaml.TitleCandidate {
	var result []sourceyaml.TitleCandidate
	for _, object := range arrayOfObjects(value) {
		title := objectText(object)
		if title == "" {
			continue
		}
		region := firstNonEmpty(asString(object["region"]), asString(object["regionid"]))
		language := objectLanguage(object)
		if region == "" && language == "" {
			continue
		}
		result = append(result, sourceyaml.TitleCandidate{
			Title:    title,
			Region:   region,
			Language: language,
		})
	}
	return result
}

func objectLanguage(object map[string]any) string {
	for _, key := range []string{"langue", "langueid", "language", "languageid", "lang"} {
		if value := strings.TrimSpace(asString(object[key])); value != "" {
			return value
		}
	}
	return ""
}

func regionLanguageMatches(object map[string]any, priority string) bool {
	priority = strings.ToLower(priority)
	for _, key := range []string{"region", "regionid", "langue", "langueid"} {
		value := strings.ToLower(asString(object[key]))
		if value == priority {
			return true
		}
	}
	return false
}

func textList(value any) []string {
	var result []string
	for _, object := range arrayOfObjects(value) {
		result = appendUnique(result, objectText(object))
	}
	if len(result) == 0 {
		if text := firstNonEmpty(asString(value), nestedText(value)); text != "" {
			result = appendUnique(result, text)
		}
	}
	return result
}

func nestedText(value any) string {
	if object, ok := value.(map[string]any); ok {
		return objectText(object)
	}
	return asString(value)
}

func objectText(object map[string]any) string {
	for _, key := range []string{"text", "nom", "name", "value", "date"} {
		if text := asString(object[key]); text != "" {
			return text
		}
	}
	return ""
}

func arrayOfObjects(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		var result []map[string]any
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

func objectAt(root map[string]any, path ...string) map[string]any {
	current := any(root)
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	object, _ := current.(map[string]any)
	return object
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func screenScraperSystemID(platform string, systems []screenScraperSystem, registry *platformnames.Registry) (string, bool) {
	if registry != nil {
		if id, ok := registry.SourceID(platform, platformnames.SourceScreenScraper); ok {
			return id, true
		}
	} else {
		defaultRegistry := platformnames.DefaultRegistry()
		if id, ok := defaultRegistry.SourceID(platform, platformnames.SourceScreenScraper); ok {
			return id, true
		}
	}
	if len(systems) > 0 {
		if id, ok := resolveScreenScraperSystemID(platform, systems, registry); ok {
			return id, true
		}
	}
	key := looseTitleKey(displayPlatformName(platform, registry))
	switch key {
	case "nintendo 64":
		return "14", true
	case "super nintendo":
		return "4", true
	case "pc engine":
		return "31", true
	case "pc engine cd":
		return "114", true
	default:
		return "", false
	}
}

func resolveScreenScraperSystemID(platform string, systems []screenScraperSystem, registry *platformnames.Registry) (string, bool) {
	byKey := map[string]string{}
	ambiguous := map[string]bool{}
	for _, system := range systems {
		if system.ID == "" || (system.Name == "" && len(system.Names) == 0) {
			continue
		}
		names := system.Names
		if len(names) == 0 {
			names = []string{system.Name}
		}
		for _, name := range names {
			key := looseTitleKey(name)
			if key == "" {
				continue
			}
			if existing, ok := byKey[key]; ok && existing != system.ID {
				ambiguous[key] = true
				continue
			}
			byKey[key] = system.ID
		}
	}
	candidates := platformnames.CandidatesForSource(platform, platformnames.SourceScreenScraper)
	if registry != nil {
		candidates = registry.CandidatesForSource(platform, platformnames.SourceScreenScraper)
	}
	for _, candidate := range candidates {
		key := looseTitleKey(candidate)
		if key == "" || ambiguous[key] {
			continue
		}
		if id, ok := byKey[key]; ok {
			return id, true
		}
	}
	return "", false
}

func streamCatalogGames(dataRoot, platform, source string, forceOverwrite bool, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	return catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.MetadataReadOptions(platformFilters(platform), source, forceOverwrite), progress, func(cached catalogcache.Game) error {
		return fn(catalogGameFromCached(cached))
	})
}

func catalogGameFromCached(cached catalogcache.Game) catalogGame {
	game := catalogGame{
		Platform:       cached.Platform,
		Folder:         cached.Folder,
		Path:           cached.Path,
		Title:          cached.Title,
		DefaultRelease: cached.DefaultRelease,
		Aliases:        cached.Aliases,
		Sources:        cached.Sources,
		Fingerprints:   cached.Fingerprints,
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

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func searchTerms(game catalogGame) []string {
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

func fingerprintQueries(game catalogGame) []screenScraperQuery {
	type candidate struct {
		query    screenScraperQuery
		release  string
		priority int
		order    int
	}
	releaseCounts := map[string]int{}
	for _, fingerprint := range game.Fingerprints {
		releaseCounts[fingerprint.Release]++
	}
	var candidates []candidate
	for index, fingerprint := range game.Fingerprints {
		if !fingerprintselect.UsableForHashLookup(fingerprint, releaseCounts[fingerprint.Release]) {
			continue
		}
		md5 := strings.ToLower(strings.TrimSpace(fingerprint.MD5))
		if len(md5) != 32 || fingerprint.Size <= 0 {
			continue
		}
		release := strings.TrimSpace(fingerprint.Release)
		queryTerm := firstNonEmpty(release, strings.TrimSpace(fingerprint.Name), game.DefaultRelease, game.Title)
		candidates = append(candidates, candidate{
			query: screenScraperQuery{
				Term:    stripTags(queryTerm),
				Release: release,
				MD5:     md5,
				Size:    fingerprint.Size,
				ROMName: strings.TrimSpace(fingerprint.Name),
			},
			release:  release,
			priority: fingerprintselect.Priority(fingerprint, game.DefaultRelease, releaseCounts[release]),
			order:    index,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].order < candidates[j].order
	})
	var queries []screenScraperQuery
	seenRelease := map[string]bool{}
	seenHash := map[string]bool{}
	for _, candidate := range candidates {
		hashKey := candidate.query.MD5 + "/" + strconv.FormatInt(candidate.query.Size, 10)
		if seenHash[hashKey] {
			continue
		}
		releaseKey := queryTermKey(candidate.release)
		if releaseKey != "" && seenRelease[releaseKey] {
			continue
		}
		queries = append(queries, candidate.query)
		seenHash[hashKey] = true
		if releaseKey != "" {
			seenRelease[releaseKey] = true
		}
		if len(queries) >= maxHashSearches {
			break
		}
	}
	return queries
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

func displayPlatformName(name string, registry *platformnames.Registry) string {
	if registry != nil {
		return registry.ForSource(name, platformnames.SourceScreenScraper)
	}
	return platformnames.ForSource(name, platformnames.SourceScreenScraper)
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

func looseTitleKey(value string) string { return titlematch.Key(value) }

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
	if m.ID != "" {
		fmt.Fprintf(&b, "        screenscraper_id: %s\n", yamlString(m.ID))
	}
	if m.Title != "" {
		fmt.Fprintf(&b, "        title: %s\n", yamlString(m.Title))
	}
	sourceyaml.RenderTitles(&b, "        ", m.TitleCandidates)
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
	if m.Players != "" {
		fmt.Fprintf(&b, "        players: %s\n", yamlString(m.Players))
	}
	if m.Rating != "" {
		fmt.Fprintf(&b, "        rating: %s\n", yamlString(m.Rating))
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
	sourceyaml.RenderDescriptions(&b, "        ", m.Descriptions)
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
var repeatedDash = regexp.MustCompile(`-+`)
