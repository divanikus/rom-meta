package catalogcache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/regions"
	"rommeta/internal/taskprogress"

	_ "modernc.org/sqlite"
)

const schemaVersion = 7

// Game is the cached subset of game.yaml used by fetchers.
type Game struct {
	ArcadePlatform string                `json:"arcade_platform,omitempty"`
	Platform       string                `json:"platform"`
	Folder         string                `json:"folder"`
	Path           string                `json:"-"`
	Title          string                `json:"title,omitempty"`
	DefaultRelease string                `json:"default_release,omitempty"`
	Variants       []catalogyaml.Variant `json:"variants,omitempty"`
	Releases       []Release             `json:"releases,omitempty"`
	Aliases        []string              `json:"aliases,omitempty"`
	Sources        []string              `json:"sources,omitempty"`
	SourceStats    []SourceStat          `json:"source_stats,omitempty"`
	MediaURLs      []MediaURL            `json:"media,omitempty"`
	Fingerprints   []Fingerprint         `json:"fingerprints,omitempty"`
}

// Release is the cached release subset used for source matching.
type Release struct {
	MAMESet string   `json:"mame_set,omitempty"`
	Name    string   `json:"name"`
	Title   string   `json:"title,omitempty"`
	Regions []string `json:"regions,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// SourceStat is the cached source stanza coverage used by status reports.
type SourceStat struct {
	Source     string `json:"source"`
	Status     string `json:"status,omitempty"`
	MatchCount int    `json:"match_count,omitempty"`
}

// Fingerprint belongs to one explicit official release or derived variant.
type Fingerprint struct {
	PayloadKind string `json:"payload_kind,omitempty"`
	Variant     string `json:"variant,omitempty"`
	Release     string `json:"release,omitempty"`
	Name        string `json:"name,omitempty"`
	Size        int64  `json:"size,omitempty"`
	CRC32       string `json:"crc32,omitempty"`
	MD5         string `json:"md5,omitempty"`
	SHA1        string `json:"sha1,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Format      string `json:"format,omitempty"`
}

// MediaURL is a source-provided downloadable media URL found in game.yaml.
type MediaURL struct {
	Source     string `json:"source"`
	Release    string `json:"release"`
	MediaType  string `json:"media_type"`
	URL        string `json:"url"`
	Region     string `json:"region,omitempty"`
	Language   string `json:"language,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Format     string `json:"format,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	CRC32      string `json:"crc32,omitempty"`
	MD5        string `json:"md5,omitempty"`
	SHA1       string `json:"sha1,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Order      int    `json:"order,omitempty"`
}

type cache struct {
	root    string
	path    string
	db      *sql.DB
	tx      *sql.Tx
	changed bool
}

// Path returns the disposable runtime SQLite cache path for a data root.
func Path(dataRoot string) string {
	return filepath.Join(dataRoot, ".rommeta", "cache", "cache.sqlite")
}

// ReadOptions controls catalog cache reads.
type ReadOptions struct {
	Platforms      []string
	IgnoredSources []string
	ScanningStage  string
	ReadingStage   string
	// ExistingSource permits a header-only result for an unchanged game that
	// already contains this source. The caller must skip those games.
	ExistingSource string
}

// MetadataReadOptions keeps resume and explicit overwrite behavior consistent
// across metadata connectors. Overwrite always reads the full catalog fields.
func MetadataReadOptions(platforms []string, source string, forceOverwrite bool) ReadOptions {
	options := ReadOptions{Platforms: platforms}
	if forceOverwrite {
		options.IgnoredSources = []string{source}
	} else {
		options.ExistingSource = source
	}
	return options
}

// GameFunc receives one cached catalog game during streaming reads.
type GameFunc func(Game) error

// ReadGames scans the conventional platforms tree and reads game.yaml through
// a disposable SQLite cache in .rommeta/cache/cache.sqlite.
func ReadGames(dataRoot string, progress taskprogress.Func) ([]Game, int, error) {
	return ReadGamesWithOptions(dataRoot, ReadOptions{}, progress)
}

// ReadGamesWithOptions scans the conventional platforms tree and reads game.yaml
// through a disposable SQLite cache in .rommeta/cache/cache.sqlite.
func ReadGamesWithOptions(dataRoot string, options ReadOptions, progress taskprogress.Func) ([]Game, int, error) {
	c, err := open(dataRoot)
	if err != nil {
		return nil, 0, err
	}
	defer c.close()

	if err := c.begin(); err != nil {
		return nil, 0, err
	}
	games, platformCount, err := c.readGames(options, progress)
	if err != nil {
		_ = c.rollback()
		return nil, 0, err
	}
	if err := c.commit(); err != nil {
		return nil, 0, err
	}
	return games, platformCount, nil
}

// ForEachGameWithOptions streams catalog games through a disposable SQLite
// cache without keeping the whole catalog in memory.
func ForEachGameWithOptions(dataRoot string, options ReadOptions, progress taskprogress.Func, fn GameFunc) (int, int, error) {
	c, err := open(dataRoot)
	if err != nil {
		return 0, 0, err
	}
	defer c.close()

	if err := c.begin(); err != nil {
		return 0, 0, err
	}
	platformCount, gameCount, err := c.walkGames(options, progress, fn)
	if err != nil {
		_ = c.rollback()
		return 0, 0, err
	}
	if err := c.commit(); err != nil {
		return 0, 0, err
	}
	return platformCount, gameCount, nil
}

// ReadGameBundle reads one conventional platform/game bundle directly without
// scanning the catalog or opening the disposable SQLite cache.
func ReadGameBundle(dataRoot, platform, folder string) (Game, error) {
	game, _, err := ReadGameDocument(dataRoot, platform, folder)
	if err != nil {
		return Game{}, err
	}
	game.Fingerprints, err = readFingerprintsYAML(filepath.Join(game.Path, "fingerprints.yaml"))
	if err != nil {
		return Game{}, err
	}
	return game, nil
}

// ReadGameDocument reads game.yaml once, without fingerprints or the writable
// SQLite cache. Callers can reuse the parsed document for source profiles.
func ReadGameDocument(dataRoot, platform, folder string) (Game, *catalogyaml.Document, error) {
	platform = strings.TrimSpace(platform)
	folder = strings.TrimSpace(folder)
	if !isSafeCatalogName(platform) {
		return Game{}, nil, fmt.Errorf("invalid platform name %q", platform)
	}
	if !isSafeCatalogName(folder) || catalogrules.IsIgnoredGameFolder(folder) {
		return Game{}, nil, fmt.Errorf("invalid game folder %q", folder)
	}
	root, err := filepath.Abs(firstNonEmpty(dataRoot, "."))
	if err != nil {
		return Game{}, nil, err
	}
	gamePath := filepath.Join(root, "platforms", platform, folder, "game.yaml")
	contents, err := catalogfs.ReadFile(gamePath)
	if err != nil {
		return Game{}, nil, err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return Game{}, nil, fmt.Errorf("parse %s: %w", gamePath, err)
	}
	if _, err := catalogyaml.Variants(document.Root()); err != nil {
		return Game{}, nil, err
	}
	game := gameFromDocument(document)
	game.Platform = platform
	game.Folder = folder
	game.Path = filepath.Dir(gamePath)
	return game, document, nil
}

// HasSource reports whether a cached game contains a source stanza.
func HasSource(sources []string, source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	for _, value := range sources {
		if value == source {
			return true
		}
	}
	return false
}

func open(dataRoot string) (*cache, error) {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, err
	}
	path := Path(root)
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	c := &cache{root: root, path: path, db: db}
	if err := c.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

func (c *cache) init() error {
	if _, err := c.db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	legacyCatalog, err := tableExists(c.db, "catalog_games")
	if err != nil {
		return err
	}
	if _, err := c.db.Exec(`
CREATE TABLE IF NOT EXISTS catalog_cache_meta (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	schema_version INTEGER NOT NULL
);`); err != nil {
		return err
	}
	var version int
	err = c.db.QueryRow(`SELECT schema_version FROM catalog_cache_meta WHERE id = 1`).Scan(&version)
	switch {
	case err == sql.ErrNoRows && legacyCatalog:
		return incompatibleCacheError(c.path, "unversioned catalog cache")
	case err == sql.ErrNoRows:
		if _, err := c.db.Exec(`INSERT INTO catalog_cache_meta (id, schema_version) VALUES (1, ?)`, schemaVersion); err != nil {
			return err
		}
	case err != nil:
		return err
	case version == 1:
		if err := c.migrateFileStamps(); err != nil {
			return err
		}
		if err := c.migrateArcadeIdentity(); err != nil {
			return err
		}
	case version == 2:
		if err := c.migrateArcadeIdentity(); err != nil {
			return err
		}
	case version == 3, version == 4, version == 5, version == 6:
	case version != schemaVersion:
		return incompatibleCacheError(c.path, fmt.Sprintf("schema version %d, expected %d", version, schemaVersion))
	}
	if version > 0 && version < 4 {
		if err := c.migrateVariants(); err != nil {
			return err
		}
	}
	// Version 5 briefly stored fetch preservation controls. Its unused extra
	// column is harmless; retain provider snapshots while retiring that policy.
	// Reparse media ownership after preserving grouped edition boundaries.
	if version > 0 && version < 7 {
		if _, err := c.db.Exec("UPDATE catalog_games SET size = -2"); err != nil {
			return err
		}
	}
	if version > 0 && version < schemaVersion {
		if _, err := c.db.Exec("UPDATE catalog_cache_meta SET schema_version = ? WHERE id = 1", schemaVersion); err != nil {
			return err
		}
	}
	_, err = c.db.Exec(`
CREATE TABLE IF NOT EXISTS catalog_games (
	path TEXT PRIMARY KEY,
	size INTEGER NOT NULL,
	mtime_ns INTEGER NOT NULL,
	fingerprint_size INTEGER NOT NULL,
	fingerprint_mtime_ns INTEGER NOT NULL,
	platform TEXT NOT NULL,
	folder TEXT NOT NULL,
	title TEXT,
	default_release TEXT,
	arcade_platform TEXT NOT NULL DEFAULT '',
	variants_json TEXT NOT NULL DEFAULT '[]',
	releases_json TEXT NOT NULL,
	aliases_json TEXT NOT NULL,
	sources_json TEXT NOT NULL,
	source_stats_json TEXT NOT NULL,
	media_urls_json TEXT NOT NULL,
	fingerprints_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_catalog_games_platform_folder ON catalog_games(platform, folder);
CREATE INDEX IF NOT EXISTS idx_catalog_games_mtime ON catalog_games(mtime_ns);
CREATE INDEX IF NOT EXISTS idx_catalog_games_resume ON catalog_games(path, size, mtime_ns, fingerprint_size, fingerprint_mtime_ns, title, sources_json);
`)
	return err
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count)
	return count > 0, err
}

func incompatibleCacheError(path, reason string) error {
	return fmt.Errorf("incompatible disposable catalog cache (%s): %s; delete %s and rerun", reason, path, path)
}

func (c *cache) begin() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	c.tx = tx
	return nil
}

func (c *cache) commit() error {
	if c.tx == nil {
		return nil
	}
	err := c.tx.Commit()
	c.tx = nil
	return err
}

func (c *cache) rollback() error {
	if c.tx == nil {
		return nil
	}
	err := c.tx.Rollback()
	c.tx = nil
	return err
}

func (c *cache) close() {
	_ = c.rollback()
	_ = c.db.Close()
}

func (c *cache) readGames(options ReadOptions, progress taskprogress.Func) ([]Game, int, error) {
	var games []Game
	platformCount, _, err := c.walkGames(options, progress, func(game Game) error {
		games = append(games, game)
		return nil
	})
	return games, platformCount, err
}

func (c *cache) walkGames(options ReadOptions, progress taskprogress.Func, fn GameFunc) (int, int, error) {
	platformRoot := filepath.Join(c.root, "platforms")
	platforms, err := platformNames(platformRoot, options.Platforms)
	if err != nil {
		return 0, 0, err
	}

	platformCount := 0
	gameCount := 0
	scanningStage := firstNonEmpty(options.ScanningStage, "scanning catalog")
	for index, platform := range platforms {
		progress.Update(scanningStage, index+1, len(platforms), platform)
		if strings.HasPrefix(platform, ".") {
			continue
		}
		platformCount++
		platformPath := filepath.Join(platformRoot, platform)
		entries, err := catalogfs.ReadDir(platformPath)
		if err != nil {
			return 0, 0, err
		}
		for _, entry := range entries {
			if isCatalogGameEntry(entry) {
				gameCount++
			}
		}
	}

	current := 0
	readingStage := firstNonEmpty(options.ReadingStage, "reading catalog")
	for _, platform := range platforms {
		if strings.HasPrefix(platform, ".") {
			continue
		}
		platformPath := filepath.Join(platformRoot, platform)
		entries, err := catalogfs.ReadDir(platformPath)
		if err != nil {
			return 0, 0, err
		}
		for _, entry := range entries {
			if !isCatalogGameEntry(entry) {
				continue
			}
			current++
			progress.Update(readingStage, current, gameCount, platform+" / "+entry.Name())
			game, err := c.readGame(filepath.Join(platformPath, entry.Name(), "game.yaml"), platform, entry.Name(), options)
			if err != nil {
				return 0, 0, err
			}
			game = filterIgnoredSources(game, options.IgnoredSources)
			if fn != nil {
				if err := fn(game); err != nil {
					return 0, 0, err
				}
			}
		}
	}
	return platformCount, gameCount, nil
}

func filterIgnoredSources(game Game, ignored []string) Game {
	if len(ignored) == 0 {
		return game
	}
	ignoredKeys := map[string]bool{}
	for _, source := range ignored {
		source = strings.TrimSpace(source)
		if source != "" {
			ignoredKeys[source] = true
		}
	}
	if len(ignoredKeys) == 0 {
		return game
	}
	game.Sources = filterStrings(game.Sources, ignoredKeys)
	game.SourceStats = filterSourceStats(game.SourceStats, ignoredKeys)
	game.MediaURLs = filterMediaURLs(game.MediaURLs, ignoredKeys)
	return game
}

func filterStrings(values []string, ignored map[string]bool) []string {
	if len(values) == 0 {
		return values
	}
	filtered := values[:0]
	for _, value := range values {
		if !ignored[value] {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterSourceStats(values []SourceStat, ignored map[string]bool) []SourceStat {
	if len(values) == 0 {
		return values
	}
	filtered := values[:0]
	for _, value := range values {
		if !ignored[value.Source] {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterMediaURLs(values []MediaURL, ignored map[string]bool) []MediaURL {
	if len(values) == 0 {
		return values
	}
	filtered := values[:0]
	for _, value := range values {
		if !ignored[value.Source] {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func isCatalogGameEntry(entry os.DirEntry) bool {
	return entry.IsDir() &&
		!strings.HasPrefix(entry.Name(), ".") &&
		!strings.HasPrefix(entry.Name(), "_") &&
		!catalogrules.IsIgnoredGameFolder(entry.Name())
}

func isSafeCatalogName(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, `/\`) {
		return false
	}
	return !strings.HasPrefix(value, ".")
}

func platformNames(platformRoot string, requested []string) ([]string, error) {
	if len(requested) > 0 {
		names := make([]string, 0, len(requested))
		seen := map[string]bool{}
		for _, name := range requested {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
				return nil, fmt.Errorf("invalid platform filter %q", name)
			}
			seen[name] = true
			platformPath := filepath.Join(platformRoot, name)
			info, err := catalogfs.Stat(platformPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			if !info.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}

	entries, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (c *cache) readGame(path, platform, folder string, options ReadOptions) (Game, error) {
	info, err := catalogfs.Stat(path)
	if err != nil {
		return Game{}, err
	}
	stamp := bundleStamp{size: info.Size(), mtimeNS: catalogfs.CacheStamp(info), fingerprintSize: -1}
	fingerprintPath := filepath.Join(filepath.Dir(path), "fingerprints.yaml")
	if fingerprintInfo, err := catalogfs.Stat(fingerprintPath); err == nil {
		stamp.fingerprintSize = fingerprintInfo.Size()
		stamp.fingerprintMTimeNS = catalogfs.CacheStamp(fingerprintInfo)
	} else if !os.IsNotExist(err) {
		return Game{}, err
	}
	key, err := filepath.Rel(c.root, path)
	if err != nil {
		key = path
	}
	key = filepath.ToSlash(key)
	if options.ExistingSource != "" && !HasSource(options.IgnoredSources, options.ExistingSource) {
		if game, ok, err := c.cachedExistingSource(key, path, platform, folder, stamp, options.ExistingSource); err != nil {
			return Game{}, err
		} else if ok {
			return game, nil
		}
	}
	if game, ok, err := c.cachedGame(key, path, platform, folder, stamp); err != nil {
		return Game{}, err
	} else if ok {
		return game, nil
	}

	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return Game{}, err
	}
	game, err := parseGameYAML(contents)
	if err != nil {
		return Game{}, fmt.Errorf("parse %s: %w", path, err)
	}
	fingerprints, err := readFingerprintsYAML(fingerprintPath)
	if err != nil {
		return Game{}, err
	}
	game.Platform = platform
	game.Folder = folder
	game.Path = filepath.Dir(path)
	game.Fingerprints = fingerprints
	if err := c.storeGame(key, stamp, game); err != nil {
		return Game{}, err
	}
	c.changed = true
	return game, nil
}

func (c *cache) cachedGame(key, path, platform, folder string, stamp bundleStamp) (Game, bool, error) {
	row := c.tx.QueryRow(`
SELECT title, default_release, arcade_platform, variants_json, releases_json, aliases_json, sources_json, source_stats_json, media_urls_json, fingerprints_json
FROM catalog_games
WHERE path = ? AND size = ? AND mtime_ns = ? AND fingerprint_size = ? AND fingerprint_mtime_ns = ?`, key, stamp.size, stamp.mtimeNS, stamp.fingerprintSize, stamp.fingerprintMTimeNS)
	var title, defaultRelease, arcadePlatform, variantsJSON, releasesJSON, aliasesJSON, sourcesJSON, sourceStatsJSON, mediaURLsJSON, fingerprintsJSON string
	if err := row.Scan(&title, &defaultRelease, &arcadePlatform, &variantsJSON, &releasesJSON, &aliasesJSON, &sourcesJSON, &sourceStatsJSON, &mediaURLsJSON, &fingerprintsJSON); err != nil {
		if err == sql.ErrNoRows {
			return Game{}, false, nil
		}
		return Game{}, false, err
	}
	game := Game{
		Platform:       platform,
		Folder:         folder,
		Path:           filepath.Dir(path),
		Title:          title,
		DefaultRelease: defaultRelease,
		ArcadePlatform: arcadePlatform,
	}
	if err := decodeJSON(variantsJSON, &game.Variants); err != nil {
		return Game{}, false, err
	}
	if err := decodeJSON(releasesJSON, &game.Releases); err != nil {
		return Game{}, false, err
	}
	if err := decodeJSON(aliasesJSON, &game.Aliases); err != nil {
		return Game{}, false, err
	}
	if err := decodeJSON(sourcesJSON, &game.Sources); err != nil {
		return Game{}, false, err
	}
	if err := decodeJSON(sourceStatsJSON, &game.SourceStats); err != nil {
		return Game{}, false, err
	}
	if err := decodeJSON(mediaURLsJSON, &game.MediaURLs); err != nil {
		return Game{}, false, err
	}
	if err := decodeJSON(fingerprintsJSON, &game.Fingerprints); err != nil {
		return Game{}, false, err
	}
	return game, true, nil
}

const cachedExistingSourceSQL = `
SELECT title, sources_json FROM catalog_games INDEXED BY idx_catalog_games_resume
WHERE path = ? AND size = ? AND mtime_ns = ? AND fingerprint_size = ? AND fingerprint_mtime_ns = ?`

func (c *cache) cachedExistingSource(key, path, platform, folder string, stamp bundleStamp, source string) (Game, bool, error) {
	// This covering index avoids fetching media/fingerprint overflow pages from
	// SQLite, which is particularly expensive when the cache itself is on SMB.
	row := c.tx.QueryRow(cachedExistingSourceSQL, key, stamp.size, stamp.mtimeNS, stamp.fingerprintSize, stamp.fingerprintMTimeNS)
	var title, sourcesJSON string
	if err := row.Scan(&title, &sourcesJSON); err != nil {
		if err == sql.ErrNoRows {
			return Game{}, false, nil
		}
		return Game{}, false, err
	}
	var sources []string
	if err := decodeJSON(sourcesJSON, &sources); err != nil {
		return Game{}, false, err
	}
	if !HasSource(sources, source) {
		return Game{}, false, nil
	}
	return Game{Platform: platform, Folder: folder, Path: filepath.Dir(path), Title: title, Sources: sources}, true, nil
}

func (c *cache) storeGame(key string, stamp bundleStamp, game Game) error {
	variantsJSON, err := encodeJSON(game.Variants)
	if err != nil {
		return err
	}
	releasesJSON, err := encodeJSON(game.Releases)
	if err != nil {
		return err
	}
	aliasesJSON, err := encodeJSON(game.Aliases)
	if err != nil {
		return err
	}
	sourcesJSON, err := encodeJSON(game.Sources)
	if err != nil {
		return err
	}
	sourceStatsJSON, err := encodeJSON(game.SourceStats)
	if err != nil {
		return err
	}
	mediaURLsJSON, err := encodeJSON(game.MediaURLs)
	if err != nil {
		return err
	}
	fingerprintsJSON, err := encodeJSON(game.Fingerprints)
	if err != nil {
		return err
	}
	_, err = c.tx.Exec(`
INSERT INTO catalog_games (
	path, size, mtime_ns, fingerprint_size, fingerprint_mtime_ns, platform, folder, title, default_release, arcade_platform,
	variants_json, releases_json, aliases_json, sources_json, source_stats_json, media_urls_json, fingerprints_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
	size = excluded.size,
	mtime_ns = excluded.mtime_ns,
	fingerprint_size = excluded.fingerprint_size,
	fingerprint_mtime_ns = excluded.fingerprint_mtime_ns,
	platform = excluded.platform,
	folder = excluded.folder,
	title = excluded.title,
	default_release = excluded.default_release,
	arcade_platform = excluded.arcade_platform,
	variants_json = excluded.variants_json,
	releases_json = excluded.releases_json,
	aliases_json = excluded.aliases_json,
	sources_json = excluded.sources_json,
	source_stats_json = excluded.source_stats_json,
	media_urls_json = excluded.media_urls_json,
	fingerprints_json = excluded.fingerprints_json`,
		key, stamp.size, stamp.mtimeNS, stamp.fingerprintSize, stamp.fingerprintMTimeNS, game.Platform, game.Folder, game.Title, game.DefaultRelease, game.ArcadePlatform,
		variantsJSON, releasesJSON, aliasesJSON, sourcesJSON, sourceStatsJSON, mediaURLsJSON, fingerprintsJSON)
	return err
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeJSON(value string, target any) error {
	if strings.TrimSpace(value) == "" {
		value = "null"
	}
	return json.Unmarshal([]byte(value), target)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ReadFingerprints reads a logical fingerprint document without opening the writable cache.
func ReadFingerprints(path string) ([]Fingerprint, error) { return readFingerprintsYAML(path) }

func readFingerprintsYAML(path string) ([]Fingerprint, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseFingerprintsYAML(contents)
}

func parseFingerprintsYAML(contents []byte) ([]Fingerprint, error) {
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return nil, err
	}
	var fingerprints []Fingerprint
	items := catalogyaml.Value(document.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(document.Root(), "roms")
	}
	if err := catalogyaml.Sequence(items, func(item *yaml.Node) error {
		file := catalogyaml.Value(item, "file")
		dump := catalogyaml.Value(item, "dump")
		fingerprint := Fingerprint{
			PayloadKind: catalogyaml.Scalar(catalogyaml.Value(file, "kind")),
			Variant:     catalogyaml.Scalar(catalogyaml.Value(item, "variant")),
			Release:     catalogyaml.Scalar(catalogyaml.Value(item, "release")),
			Name:        catalogyaml.Scalar(catalogyaml.Value(file, "name")),
			Size:        catalogyaml.Int64(catalogyaml.Value(file, "size")),
			CRC32:       strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, "crc32"))),
			MD5:         strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, "md5"))),
			SHA1:        strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, "sha1"))),
			SHA256:      strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, "sha256"))),
			Format:      strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(dump, "format"))),
		}
		if fingerprint.Release != "" && fingerprint.Variant != "" {
			return fmt.Errorf("fingerprint must not contain both release and variant")
		}
		if fingerprint.Release != "" || fingerprint.Variant != "" || fingerprint.Name != "" || fingerprint.Size > 0 || fingerprint.CRC32 != "" || fingerprint.MD5 != "" || fingerprint.SHA1 != "" || fingerprint.SHA256 != "" {
			fingerprints = append(fingerprints, fingerprint)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return fingerprints, nil
}

func appendInlineAliases(aliases []string, rawList string) []string {
	for _, alias := range parseInlineList(rawList) {
		aliases = appendUnique(aliases, alias)
	}
	return aliases
}

func setMediaURLField(media *MediaURL, key, value string) {
	switch strings.TrimSpace(key) {
	case "region":
		media.Region = regions.NormalizeDisplay(value)
	case "language":
		media.Language = value
	case "quality":
		media.Quality = value
	case "format":
		media.Format = value
	case "source_id":
		media.SourceID = value
	case "source_file":
		media.SourceFile = value
	case "source_type":
		media.SourceType = value
	case "crc32":
		media.CRC32 = value
	case "md5":
		media.MD5 = value
	case "sha1":
		media.SHA1 = value
	case "size":
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			media.Size = parsed
		}
	}
}

func splitKeyValue(value string) (string, string, bool) {
	key, raw, ok := strings.Cut(value, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(raw), true
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
	}
	return value
}

func parseInlineList(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil
	}
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if value == "" {
		return nil
	}
	var result []string
	for _, part := range splitInlineList(value) {
		parsed := parseScalar(part)
		if parsed != "" {
			result = appendUnique(result, parsed)
		}
	}
	return result
}

func splitInlineList(value string) []string {
	var result []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\' && inDouble:
			b.WriteRune(r)
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteRune(r)
		case r == ',' && !inSingle && !inDouble:
			result = append(result, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	result = append(result, strings.TrimSpace(b.String()))
	return result
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func regionTags(value string) []string {
	var regions []string
	for _, match := range tagValuePattern.FindAllStringSubmatch(value, -1) {
		if len(match) < 2 {
			continue
		}
		for _, part := range strings.Split(match[1], ",") {
			part = strings.TrimSpace(part)
			if isRegionTag(part) {
				regions = appendUnique(regions, part)
			}
		}
	}
	return regions
}

func isRegionTag(value string) bool {
	switch strings.ToLower(value) {
	case "world", "usa", "europe", "japan", "france", "germany", "spain", "italy", "australia", "korea", "china", "brazil":
		return true
	default:
		return false
	}
}

func releaseTags(name string, tags []string) []string {
	result := append([]string(nil), tags...)
	for _, tag := range catalogrules.UnofficialTagsFromName(name) {
		result = appendUnique(result, tag)
	}
	return result
}

var tagValuePattern = regexp.MustCompile(`[\(\[]([^\)\]]+)[\)\]]`)
