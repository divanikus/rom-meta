package launchbox

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/contentidentity"
	"rommeta/internal/metadate"
	"rommeta/internal/platformnames"
	"rommeta/internal/regions"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/taskprogress"

	_ "modernc.org/sqlite"
)

// ImportOptions controls LaunchBox metadata snapshot enrichment.
type ImportOptions struct {
	DataRoot         string
	ZipPath          string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	ForceOverwrite   bool
	Progress         taskprogress.Func
}

// ImportResult describes a dry-run or applied LaunchBox enrichment.
type ImportResult struct {
	DataRoot              string
	ZipPath               string
	Snapshot              SnapshotResult
	PlatformCount         int
	GameBundleCount       int
	ReleaseCount          int
	MatchedReleaseCount   int
	GameFileCount         int
	NotFoundGameCount     int
	WrittenFileCount      int
	SkippedPreservedCount int
	SkippedExistingCount  int
	Apply                 bool
}

// SnapshotResult describes the LaunchBox snapshot used by a fetch.
type SnapshotResult struct {
	Checked    bool
	Downloaded bool
	URL        string
	ZipPath    string
}

const defaultSnapshotURL = "https://gamesdb.launchbox-app.com/Metadata.zip"

// DefaultSnapshotURL returns the built-in LaunchBox metadata snapshot URL.
func DefaultSnapshotURL() string {
	return defaultSnapshotURL
}

// DefaultSnapshotPath returns the runtime-local LaunchBox metadata zip path.
func DefaultSnapshotPath(dataRoot string) string {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	return filepath.Join(dataRoot, ".rommeta", "sources", "launchbox", "Metadata.zip")
}

// EnsureSnapshotFromURL downloads the LaunchBox snapshot from a configured URL.
func EnsureSnapshotFromURL(dataRoot, url string, progressCallbacks ...taskprogress.Func) (SnapshotResult, error) {
	progress := firstProgress(progressCallbacks)
	if strings.TrimSpace(url) == "" {
		url = defaultSnapshotURL
	}
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return SnapshotResult{}, err
	}
	zipPath := DefaultSnapshotPath(dataRoot)
	result := SnapshotResult{Checked: true, URL: url, ZipPath: zipPath}
	if _, err := catalogfs.Stat(zipPath); err == nil {
		return result, nil
	} else if err != nil && !os.IsNotExist(err) {
		return SnapshotResult{}, err
	}

	if err := catalogfs.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return SnapshotResult{}, err
	}
	if err := downloadFile(url, zipPath, "downloading snapshot", filepath.Base(zipPath), progress); err != nil {
		return SnapshotResult{}, err
	}
	result.Downloaded = true
	return result, nil
}

type catalogRelease struct {
	MAMESet   string
	MAMETitle string
	Name      string
	Title     string
	Regions   []string
	Tags      []string
}

type catalogGame struct {
	PreservedSources []string
	ArcadePlatform   string
	Platform         string
	Folder           string
	Path             string
	Title            string
	Aliases          []string
	Releases         []catalogRelease
	Sources          []string
}

type releaseMetadata struct {
	ReleaseType     string
	DatabaseID      string
	Title           string
	Platform        string
	Region          string
	ReleaseDate     string
	Developer       string
	Publisher       string
	Genres          []string
	MaxPlayers      string
	CommunityRating string
	Description     string
	AlternateTitles []string
	TitleCandidates []sourceyaml.TitleCandidate
	MediaURLs       map[string][]sourceyaml.MediaCandidate
}

type mediaURLCandidate struct {
	URL           string
	Region        string
	SourceFile    string
	SourceType    string
	CRC32         string
	OriginalOrder int
}

type snapshotIndex struct {
	byPlatformTitle map[string]map[string][]releaseMetadata
	alternateIDs    map[string]string
	byID            map[string]releaseMetadata
	imagesByID      map[string][]launchboxImage
}

type launchboxImage struct {
	Type     string
	Region   string
	FileName string
	CRC32    string
}

// Import matches platform release entries against a LaunchBox Metadata.zip
// snapshot and optionally writes source-specific candidate stanzas into game.yaml.
func Import(options ImportOptions) (ImportResult, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}
	zipPath, err := filepath.Abs(options.ZipPath)
	if err != nil {
		return ImportResult{}, err
	}

	progress := options.Progress
	result := ImportResult{
		DataRoot: dataRoot,
		ZipPath:  zipPath,
		Apply:    options.Apply,
	}

	var mameNames map[string]mameName
	var index snapshotIndex
	resolvedPlatforms := map[string]string{}
	setupDone := false
	setupSnapshot := func() error {
		if setupDone {
			return nil
		}
		sourcePlatforms, err := readPlatformNames(dataRoot, zipPath, progress)
		if err != nil {
			return err
		}
		catalogPlatforms, err := catalogPlatformNames(dataRoot, options.Platform)
		if err != nil {
			return err
		}
		platforms := map[string]bool{}
		for _, platform := range catalogPlatforms {
			platformName := launchboxPlatformName(platform, sourcePlatforms, options.PlatformRegistry)
			resolvedPlatforms[platform] = platformName
			platforms[platformName] = true
		}

		if platforms["Arcade"] {
			for _, name := range arcadeLaunchBoxPlatforms {
				for _, available := range sourcePlatforms {
					if name == available {
						platforms[name] = true
					}
				}
			}
			mameNames, err = readMAMENames(zipPath)
			if err != nil {
				return err
			}
		}
		index, err = readSnapshot(dataRoot, zipPath, platforms, progress)
		if err != nil {
			return err
		}
		setupDone = true
		return nil
	}

	catalogCurrent := 0
	catalogTotal := 0
	streamProgress := taskprogress.Func(func(snapshot taskprogress.Snapshot) {
		if snapshot.Stage == "reading catalog" {
			catalogCurrent = snapshot.Current
			catalogTotal = snapshot.Total
		}
		if progress != nil {
			progress(snapshot)
		}
	})
	platformCount, gameCount, err := streamCatalogGames(dataRoot, options.Platform, "launchbox", options.ForceOverwrite, streamProgress, func(game catalogGame) error {
		if catalogcache.HasSource(game.PreservedSources, "launchbox") {
			result.SkippedPreservedCount++
			return nil
		}
		if !options.ForceOverwrite && catalogcache.HasSource(game.Sources, "launchbox") {
			result.SkippedExistingCount++
			return nil
		}
		if err := setupSnapshot(); err != nil {
			return err
		}
		current := catalogCurrent
		total := catalogTotal
		if current == 0 {
			current = result.GameFileCount + result.NotFoundGameCount + 1
		}
		progress.Update("matching", current, total, game.Platform+" / "+game.Folder)
		platformName := resolvedPlatforms[game.Platform]
		if platformName == "" {
			platformName = launchboxPlatformName(game.Platform, nil, options.PlatformRegistry)
		}
		platformIndex := index.byPlatformTitle[platformName]
		var matches []matchedRelease
		for _, release := range game.Releases {
			result.ReleaseCount++

			var metadata releaseMetadata
			var ok bool
			if release.MAMESet != "" {
				metadata, ok = matchArcadeMetadata(index.byPlatformTitle, game, release, mameNames)
			} else {
				metadata, ok = matchMetadata(platformIndex, release, game.Title, game.Aliases...)
			}
			if !ok {
				continue
			}
			result.MatchedReleaseCount++
			matches = append(matches, matchedRelease{
				Release:  release.Name,
				Metadata: metadata,
			})
		}
		if len(matches) == 0 {
			result.NotFoundGameCount++
			if options.Apply {
				out := filepath.Join(game.Path, "game.yaml")
				if err := sourceyaml.WriteSourceMetadata(out, "launchbox", renderNotFoundMetadata()); err != nil {
					return err
				}
				result.WrittenFileCount++
			}
			return nil
		}
		result.GameFileCount++
		if !options.Apply {
			return nil
		}
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].Release < matches[j].Release
		})
		out := filepath.Join(game.Path, "game.yaml")
		if err := sourceyaml.WriteSourceMetadata(out, "launchbox", renderMetadata(matches)); err != nil {
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

func matchMetadata(platformIndex map[string][]releaseMetadata, release catalogRelease, gameTitle string, aliases ...string) (releaseMetadata, bool) {
	if release.MAMESet != "" {
		keys := titleMatchKeys(release.Title)
		if release.MAMETitle != "" {
			for _, title := range strings.Split(release.MAMETitle, " / ") {
				keys = append(keys, titleMatchKeys(title)...)
			}
		}
		return bestLaunchBoxMetadata(platformIndex, keys, release, catalogrules.HasUnofficialTag(release.Tags))
	}
	if catalogrules.HasUnofficialTag(release.Tags) {
		return bestLaunchBoxMetadata(platformIndex, titleMatchKeys(release.Name), release, true)
	}

	keys := append(titleMatchKeys(release.Name), titleMatchKeys(release.Title)...)
	keys = append(keys, titleMatchKeys(gameTitle)...)
	for _, alias := range aliases {
		keys = append(keys, titleMatchKeys(alias)...)
	}
	return bestLaunchBoxMetadata(platformIndex, keys, release, false)
}

func bestLaunchBoxMetadata(platformIndex map[string][]releaseMetadata, keys []string, release catalogRelease, unofficial bool) (releaseMetadata, bool) {
	seen := map[string]bool{}
	var candidates []releaseMetadata
	for _, key := range keys {
		for _, metadata := range platformIndex[key] {
			if metadata.DatabaseID == "" || seen[metadata.DatabaseID] {
				continue
			}
			seen[metadata.DatabaseID] = true
			candidates = append(candidates, metadata)
		}
	}
	if len(candidates) == 0 {
		// Only a unique primary title may bridge word boundaries. Never let a
		// compact fallback override an exact title or resolve a spelling collision.
		for _, key := range keys {
			for _, metadata := range platformIndex[compactPrimaryTitleKey(key)] {
				if metadata.DatabaseID != "" && !seen[metadata.DatabaseID] {
					seen[metadata.DatabaseID] = true
					candidates = append(candidates, metadata)
				}
			}
		}
		if len(candidates) != 1 {
			return releaseMetadata{}, false
		}
		_, sourceProfile := contentidentity.Analyze(candidates[0].Title)
		releaseProfile := contentidentity.Release(release.Name, release.Title, release.Tags...)
		if sourceProfile.Key() != releaseProfile.Key() || metadataHasUnofficialMarker(candidates[0]) != unofficial || !metadataRegionsCompatible(release, candidates[0]) {
			return releaseMetadata{}, false
		}
	}
	filtered := candidates[:0]
	for _, metadata := range candidates {
		if release.MAMESet != "" && !arcadeCandidateAllowed(release, metadata) {
			continue
		}
		if unofficial && !metadataHasUnofficialMarker(metadata) {
			continue
		}
		filtered = append(filtered, metadata)
	}
	candidates = filtered
	if len(candidates) == 0 {
		return releaseMetadata{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}

	bestScore := 0
	var best releaseMetadata
	tied := false
	for _, metadata := range candidates {
		score := launchBoxDisambiguationScore(release, metadata)
		if score > bestScore {
			best = metadata
			bestScore = score
			tied = false
			continue
		}
		if score == bestScore && score > 0 {
			tied = true
		}
	}
	if bestScore == 0 || tied {
		return releaseMetadata{}, false
	}
	return best, true
}

// Unknown regional coverage is not a conflict; known disjoint coverage is.
func metadataRegionsCompatible(release catalogRelease, metadata releaseMetadata) bool {
	collect := func(values []string) map[string]bool {
		result := map[string]bool{}
		for _, value := range values {
			for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '/' }) {
				if normalized, ok := regions.NormalizeDATDisplay(regions.NormalizeDisplay(part)); ok {
					result[normalized] = true
				}
			}
		}
		return result
	}
	actual := collect(release.Regions)
	available := collect(append([]string{metadata.Region}, metadataTitleRegions(metadata)...))
	if len(actual) == 0 || len(available) == 0 || actual["World"] || available["World"] {
		return true
	}
	for region := range actual {
		if available[region] {
			return true
		}
	}
	return false
}

func launchBoxDisambiguationScore(release catalogRelease, metadata releaseMetadata) int {
	score := 0
	if releaseRegionsMatchMetadata(release.Regions, metadata) {
		score += 20
	}
	releaseTokens := releaseDisambiguationTokens(release.Name)
	if release.MAMESet != "" {
		releaseTokens = releaseDisambiguationTokens(release.Title)
	}
	if len(releaseTokens) == 0 {
		return score
	}
	metadataTokens := launchBoxMetadataTokens(metadata)
	for _, token := range releaseTokens {
		if metadataTokens[token] {
			score += 40
		}
	}
	return score
}

func releaseRegionsMatchMetadata(regions []string, metadata releaseMetadata) bool {
	metadataRegions := map[string]bool{}
	for _, value := range append([]string{metadata.Region}, metadataTitleRegions(metadata)...) {
		for _, region := range normalizedRegionAliases(value) {
			metadataRegions[region] = true
		}
	}
	for _, region := range regions {
		for _, normalized := range normalizedRegionAliases(region) {
			if metadataRegions[normalized] {
				return true
			}
		}
	}
	return false
}

func metadataTitleRegions(metadata releaseMetadata) []string {
	var regions []string
	for _, candidate := range metadata.TitleCandidates {
		regions = append(regions, candidate.Region)
	}
	return regions
}

func releaseDisambiguationTokens(name string) []string {
	var tokens []string
	for _, token := range parenthesizedTokens(name) {
		if shouldIgnoreReleaseDisambiguator(token) {
			continue
		}
		for _, key := range titleMatchKeys(token) {
			if key != "" {
				tokens = appendUnique(tokens, key)
			}
		}
	}
	return tokens
}

func shouldIgnoreReleaseDisambiguator(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return true
	}
	normalized := looseTitleKey(token)
	if normalized == "" {
		return true
	}
	if normalizedRegionAliases(token) != nil {
		return true
	}
	if catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(token)) {
		return true
	}
	parts := strings.Split(normalized, " ")
	if len(parts) == 1 && len([]rune(parts[0])) <= 3 {
		return true
	}
	for _, marker := range []string{"rev", "beta", "proto", "demo", "sample", "program", "sega channel", "virtual console"} {
		if normalized == marker || strings.HasPrefix(normalized, marker+" ") {
			return true
		}
	}
	return false
}

func launchBoxMetadataTokens(metadata releaseMetadata) map[string]bool {
	result := map[string]bool{}
	for _, value := range []string{metadata.Title, metadata.Developer, metadata.Publisher} {
		for _, key := range titleMatchKeys(value) {
			if key != "" {
				result[key] = true
			}
		}
	}
	for _, title := range metadata.AlternateTitles {
		for _, key := range titleMatchKeys(title) {
			if key != "" {
				result[key] = true
			}
		}
	}
	for _, candidate := range metadata.TitleCandidates {
		for _, key := range titleMatchKeys(candidate.Title) {
			if key != "" {
				result[key] = true
			}
		}
	}
	return result
}

func appendLaunchBoxMetadata(values []releaseMetadata, next releaseMetadata) []releaseMetadata {
	if next.DatabaseID == "" {
		return values
	}
	for _, existing := range values {
		if existing.DatabaseID == next.DatabaseID {
			return values
		}
	}
	return append(values, next)
}

func updateLaunchBoxMetadata(values []releaseMetadata, next releaseMetadata) []releaseMetadata {
	if next.DatabaseID == "" {
		return values
	}
	for index, existing := range values {
		if existing.DatabaseID == next.DatabaseID {
			values[index] = next
			return values
		}
	}
	return append(values, next)
}

func normalizedRegionAliases(value string) []string {
	key := looseTitleKey(value)
	if key == "" {
		return nil
	}
	switch key {
	case "usa", "us", "u s", "u s a", "united states", "united states of america", "north america":
		return []string{"usa"}
	case "world", "wor":
		return []string{"world"}
	case "europe", "eu", "eur":
		return []string{"europe"}
	case "japan", "jp", "jpn", "ja":
		return []string{"japan"}
	default:
		return nil
	}
}

func parenthesizedTokens(value string) []string {
	var tokens []string
	start := -1
	for index, r := range value {
		switch r {
		case '(':
			start = index + 1
		case ')':
			if start >= 0 && start <= index {
				tokens = append(tokens, strings.TrimSpace(value[start:index]))
			}
			start = -1
		}
	}
	return tokens
}

func metadataHasUnofficialMarker(metadata releaseMetadata) bool {
	switch strings.ToLower(strings.TrimSpace(metadata.ReleaseType)) {
	case "homebrew", "unlicensed":
		return true
	}
	if catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(metadata.Title)) {
		return true
	}
	for _, title := range metadata.AlternateTitles {
		if catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(title)) {
			return true
		}
	}
	for _, candidate := range metadata.TitleCandidates {
		if catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(candidate.Title)) {
			return true
		}
	}
	return false
}

type launchBoxSnapshotCache struct {
	SchemaVersion      int                                     `json:"schema_version"`
	ZipPath            string                                  `json:"zip_path"`
	ZipSize            int64                                   `json:"zip_size"`
	ZipModTimeUnixNano int64                                   `json:"zip_mod_time_unix_nano"`
	PlatformNames      []string                                `json:"platform_names,omitempty"`
	RequestedPlatforms []string                                `json:"requested_platforms,omitempty"`
	ByPlatformTitle    map[string]map[string][]releaseMetadata `json:"by_platform_title,omitempty"`
	AlternateIDs       map[string]string                       `json:"alternate_ids,omitempty"`
	ByID               map[string]releaseMetadata              `json:"by_id,omitempty"`
}

func readPlatformNames(dataRoot, zipPath string, progress taskprogress.Func) ([]string, error) {
	if cache, ok, err := loadLaunchBoxSnapshotCache(dataRoot, zipPath, false); err != nil {
		return nil, err
	} else if ok && len(cache.PlatformNames) > 0 {
		progress.Update("reading cache", 0, 0, "LaunchBox platform list")
		return append([]string(nil), cache.PlatformNames...), nil
	}

	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(filepath.Base(file.Name), "Metadata.xml") {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return nil, err
		}
		stage := "reading platform list"
		label := filepath.Base(file.Name)
		total := int64(file.UncompressedSize64)
		progress.UpdateBytes(stage, 0, total, label)
		platforms, parseErr := parsePlatformNamesXML(taskprogress.NewReader(source, progress, stage, total, label))
		closeErr := source.Close()
		if parseErr != nil {
			return nil, parseErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		cache, err := newLaunchBoxSnapshotCache(dataRoot, zipPath)
		if err != nil {
			return nil, err
		}
		cache.PlatformNames = platforms
		if err := saveLaunchBoxSnapshotCache(dataRoot, cache); err != nil {
			return nil, err
		}
		return platforms, nil
	}
	return nil, fmt.Errorf("LaunchBox zip does not contain Metadata.xml")
}

func parsePlatformNamesXML(source io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(source)
	seen := map[string]bool{}
	var platforms []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return platforms, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Game" {
			continue
		}
		fields, err := readElementFields(decoder, start)
		if err != nil {
			return nil, err
		}
		platform := strings.TrimSpace(fields["Platform"])
		if platform == "" || seen[platform] {
			continue
		}
		seen[platform] = true
		platforms = append(platforms, platform)
	}
}

func readSnapshot(dataRoot, zipPath string, platforms map[string]bool, progress taskprogress.Func) (snapshotIndex, error) {
	requestedPlatforms := sortedPlatformNames(platforms)
	if cache, ok, err := loadLaunchBoxSnapshotCache(dataRoot, zipPath, true); err != nil {
		return snapshotIndex{}, err
	} else if ok && sameStrings(cache.RequestedPlatforms, requestedPlatforms) && len(cache.ByPlatformTitle) > 0 && len(cache.ByID) > 0 {
		progress.Update("reading cache", 0, 0, "LaunchBox metadata")
		return addCompactPrimaryTitleKeys(snapshotIndex{
			byPlatformTitle: cache.ByPlatformTitle,
			alternateIDs:    cache.AlternateIDs,
			byID:            cache.ByID,
			imagesByID:      map[string][]launchboxImage{},
		}), nil
	}

	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return snapshotIndex{}, err
	}
	defer reader.Close()

	index := snapshotIndex{
		byPlatformTitle: map[string]map[string][]releaseMetadata{},
		alternateIDs:    map[string]string{},
		byID:            map[string]releaseMetadata{},
		imagesByID:      map[string][]launchboxImage{},
	}

	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(filepath.Base(file.Name), "Metadata.xml") {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return snapshotIndex{}, err
		}
		defer source.Close()
		stage := "reading metadata"
		label := filepath.Base(file.Name)
		total := int64(file.UncompressedSize64)
		progress.UpdateBytes(stage, 0, total, label)
		if err := parseMetadataXML(taskprogress.NewReader(source, progress, stage, total, label), platforms, &index); err != nil {
			return snapshotIndex{}, err
		}
		for id, images := range index.imagesByID {
			metadata, ok := index.byID[id]
			if !ok {
				continue
			}
			metadata.MediaURLs = mediaCandidates(images)
			index.byID[id] = metadata
			for _, key := range titleMatchKeys(metadata.Title) {
				index.byPlatformTitle[metadata.Platform][key] = updateLaunchBoxMetadata(index.byPlatformTitle[metadata.Platform][key], metadata)
			}
		}
		cache, err := newLaunchBoxSnapshotCache(dataRoot, zipPath)
		if err != nil {
			return snapshotIndex{}, err
		}
		if existing, ok, err := loadLaunchBoxSnapshotCache(dataRoot, zipPath, false); err != nil {
			return snapshotIndex{}, err
		} else if ok {
			cache.PlatformNames = existing.PlatformNames
		}
		cache.RequestedPlatforms = requestedPlatforms
		cache.ByPlatformTitle = index.byPlatformTitle
		cache.AlternateIDs = index.alternateIDs
		cache.ByID = index.byID
		progress.Update("writing cache", 0, 0, filepath.Base(launchBoxSnapshotCachePath(dataRoot)))
		if err := saveLaunchBoxSnapshotCache(dataRoot, cache); err != nil {
			return snapshotIndex{}, err
		}
		return addCompactPrimaryTitleKeys(index), nil
	}
	return snapshotIndex{}, fmt.Errorf("LaunchBox zip does not contain Metadata.xml")
}

func launchBoxSnapshotCachePath(dataRoot string) string {
	return filepath.Join(dataRoot, ".rommeta", "cache", "launchbox.sqlite")
}

func newLaunchBoxSnapshotCache(dataRoot, zipPath string) (launchBoxSnapshotCache, error) {
	info, err := catalogfs.Stat(zipPath)
	if err != nil {
		return launchBoxSnapshotCache{}, err
	}
	relativePath, err := relativeCachePath(dataRoot, zipPath)
	if err != nil {
		return launchBoxSnapshotCache{}, err
	}
	return launchBoxSnapshotCache{
		SchemaVersion:      6,
		ZipPath:            relativePath,
		ZipSize:            info.Size(),
		ZipModTimeUnixNano: info.ModTime().UnixNano(),
	}, nil
}

func relativeCachePath(dataRoot, path string) (string, error) {
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path)), nil
	}
	return filepath.ToSlash(relative), nil
}

func loadLaunchBoxSnapshotCache(dataRoot, zipPath string, includeIndex bool) (launchBoxSnapshotCache, bool, error) {
	expected, err := newLaunchBoxSnapshotCache(dataRoot, zipPath)
	if err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	db, err := openLaunchBoxCache(dataRoot)
	if err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	defer db.Close()

	cache, ok, err := loadLaunchBoxSnapshotMeta(db)
	if err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	if !ok {
		if legacy, legacyOK, err := loadLegacyLaunchBoxSnapshotCache(dataRoot, expected); err != nil {
			return launchBoxSnapshotCache{}, false, err
		} else if legacyOK {
			if err := saveLaunchBoxSnapshotCache(dataRoot, legacy); err != nil {
				return launchBoxSnapshotCache{}, false, err
			}
			cache = legacy
			ok = true
		}
	}
	if !ok || cache.SchemaVersion != expected.SchemaVersion || cache.ZipPath != expected.ZipPath || cache.ZipSize != expected.ZipSize || cache.ZipModTimeUnixNano != expected.ZipModTimeUnixNano {
		return expected, false, nil
	}
	if includeIndex {
		if err := loadLaunchBoxSnapshotIndex(db, &cache); err != nil {
			return launchBoxSnapshotCache{}, false, err
		}
	}
	return cache, true, nil
}

func loadLegacyLaunchBoxSnapshotCache(dataRoot string, expected launchBoxSnapshotCache) (launchBoxSnapshotCache, bool, error) {
	path := filepath.Join(dataRoot, ".rommeta", "cache", "launchbox-metadata.json")
	data, err := catalogfs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return launchBoxSnapshotCache{}, false, nil
		}
		return launchBoxSnapshotCache{}, false, err
	}
	var cache launchBoxSnapshotCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return launchBoxSnapshotCache{}, false, nil
	}
	legacyZipPath := cache.ZipPath
	if legacyZipPath != "" {
		if relative, err := relativeCachePath(dataRoot, legacyZipPath); err == nil {
			legacyZipPath = relative
		}
	}
	if cache.SchemaVersion != expected.SchemaVersion ||
		legacyZipPath != expected.ZipPath ||
		cache.ZipSize != expected.ZipSize ||
		cache.ZipModTimeUnixNano != expected.ZipModTimeUnixNano {
		return launchBoxSnapshotCache{}, false, nil
	}
	cache.ZipPath = expected.ZipPath
	return cache, true, nil
}

func saveLaunchBoxSnapshotCache(dataRoot string, cache launchBoxSnapshotCache) error {
	db, err := openLaunchBoxCache(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := saveLaunchBoxSnapshotCacheTx(tx, cache); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func openLaunchBoxCache(dataRoot string) (*sql.DB, error) {
	path := launchBoxSnapshotCachePath(dataRoot)
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS launchbox_snapshot_meta (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	schema_version INTEGER NOT NULL,
	zip_path TEXT NOT NULL,
	zip_size INTEGER NOT NULL,
	zip_mtime_ns INTEGER NOT NULL,
	platform_names_json TEXT NOT NULL,
	requested_platforms_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_launchbox_snapshot_signature ON launchbox_snapshot_meta(zip_path, zip_size, zip_mtime_ns);

CREATE TABLE IF NOT EXISTS launchbox_games (
	database_id TEXT PRIMARY KEY,
	platform TEXT NOT NULL,
	title TEXT NOT NULL,
 release_type TEXT NOT NULL DEFAULT '',
	release_date TEXT,
	developer TEXT,
	publisher TEXT,
	genres_json TEXT NOT NULL,
	max_players TEXT,
	community_rating TEXT,
	description TEXT,
	alternate_titles_json TEXT NOT NULL,
	title_candidates_json TEXT NOT NULL DEFAULT '[]',
	media_urls_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_launchbox_games_platform ON launchbox_games(platform);

CREATE TABLE IF NOT EXISTS launchbox_title_index (
	platform TEXT NOT NULL,
	key TEXT NOT NULL,
	database_id TEXT NOT NULL,
	PRIMARY KEY(platform, key, database_id)
);
CREATE INDEX IF NOT EXISTS idx_launchbox_title_index_database_id ON launchbox_title_index(database_id);

CREATE TABLE IF NOT EXISTS launchbox_alternate_ids (
	key TEXT PRIMARY KEY,
	database_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_launchbox_alternate_ids_database_id ON launchbox_alternate_ids(database_id);
`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureLaunchBoxSnapshotCacheColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureLaunchBoxTitleIndexSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureLaunchBoxSnapshotCacheColumns(db *sql.DB) error {
	for column, definition := range map[string]string{"title_candidates_json": "TEXT NOT NULL DEFAULT '[]'", "release_type": "TEXT NOT NULL DEFAULT ''"} {
		exists, err := launchBoxColumnExists(db, column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec("ALTER TABLE launchbox_games ADD COLUMN " + column + " " + definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func launchBoxColumnExists(db *sql.DB, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(launchbox_games)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ensureLaunchBoxTitleIndexSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(launchbox_title_index)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	primaryKey := map[string]int{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if pk > 0 {
			primaryKey[name] = pk
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if primaryKey["platform"] > 0 && primaryKey["key"] > 0 && primaryKey["database_id"] > 0 {
		return nil
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS launchbox_title_index`); err != nil {
		return err
	}
	_, err = db.Exec(`
CREATE TABLE launchbox_title_index (
	platform TEXT NOT NULL,
	key TEXT NOT NULL,
	database_id TEXT NOT NULL,
	PRIMARY KEY(platform, key, database_id)
);
CREATE INDEX IF NOT EXISTS idx_launchbox_title_index_database_id ON launchbox_title_index(database_id);
DELETE FROM launchbox_snapshot_meta;
`)
	return err
}

func loadLaunchBoxSnapshotMeta(db *sql.DB) (launchBoxSnapshotCache, bool, error) {
	row := db.QueryRow(`
SELECT schema_version, zip_path, zip_size, zip_mtime_ns, platform_names_json, requested_platforms_json
FROM launchbox_snapshot_meta
WHERE id = 1`)
	var cache launchBoxSnapshotCache
	var platformNamesJSON, requestedPlatformsJSON string
	err := row.Scan(&cache.SchemaVersion, &cache.ZipPath, &cache.ZipSize, &cache.ZipModTimeUnixNano, &platformNamesJSON, &requestedPlatformsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return launchBoxSnapshotCache{}, false, nil
		}
		return launchBoxSnapshotCache{}, false, err
	}
	if err := decodeJSONText(platformNamesJSON, &cache.PlatformNames); err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	if err := decodeJSONText(requestedPlatformsJSON, &cache.RequestedPlatforms); err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	return cache, true, nil
}

func loadLaunchBoxSnapshotIndex(db *sql.DB, cache *launchBoxSnapshotCache) error {
	cache.ByID = map[string]releaseMetadata{}
	rows, err := db.Query(`
SELECT database_id, platform, title, release_type, release_date, developer, publisher, genres_json,
       max_players, community_rating, description, alternate_titles_json, title_candidates_json, media_urls_json
FROM launchbox_games`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata releaseMetadata
		var genresJSON, alternateTitlesJSON, titleCandidatesJSON, mediaURLsJSON string
		if err := rows.Scan(
			&metadata.DatabaseID,
			&metadata.Platform,
			&metadata.Title,
			&metadata.ReleaseType,
			&metadata.ReleaseDate,
			&metadata.Developer,
			&metadata.Publisher,
			&genresJSON,
			&metadata.MaxPlayers,
			&metadata.CommunityRating,
			&metadata.Description,
			&alternateTitlesJSON,
			&titleCandidatesJSON,
			&mediaURLsJSON,
		); err != nil {
			return err
		}
		if err := decodeJSONText(genresJSON, &metadata.Genres); err != nil {
			return err
		}
		if err := decodeJSONText(alternateTitlesJSON, &metadata.AlternateTitles); err != nil {
			return err
		}
		if err := decodeJSONText(titleCandidatesJSON, &metadata.TitleCandidates); err != nil {
			return err
		}
		if err := decodeJSONText(mediaURLsJSON, &metadata.MediaURLs); err != nil {
			return err
		}
		cache.ByID[metadata.DatabaseID] = metadata
	}
	if err := rows.Err(); err != nil {
		return err
	}

	cache.ByPlatformTitle = map[string]map[string][]releaseMetadata{}
	rows, err = db.Query(`SELECT platform, key, database_id FROM launchbox_title_index`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var platform, key, id string
		if err := rows.Scan(&platform, &key, &id); err != nil {
			return err
		}
		metadata, ok := cache.ByID[id]
		if !ok {
			continue
		}
		if _, ok := cache.ByPlatformTitle[platform]; !ok {
			cache.ByPlatformTitle[platform] = map[string][]releaseMetadata{}
		}
		cache.ByPlatformTitle[platform][key] = appendLaunchBoxMetadata(cache.ByPlatformTitle[platform][key], metadata)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	cache.AlternateIDs = map[string]string{}
	rows, err = db.Query(`SELECT key, database_id FROM launchbox_alternate_ids`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, id string
		if err := rows.Scan(&key, &id); err != nil {
			return err
		}
		cache.AlternateIDs[key] = id
	}
	return rows.Err()
}

func saveLaunchBoxSnapshotCacheTx(tx *sql.Tx, cache launchBoxSnapshotCache) error {
	existing, exists, err := loadLaunchBoxSnapshotMetaTx(tx)
	if err != nil {
		return err
	}
	sameSignature := exists &&
		existing.SchemaVersion == cache.SchemaVersion &&
		existing.ZipPath == cache.ZipPath &&
		existing.ZipSize == cache.ZipSize &&
		existing.ZipModTimeUnixNano == cache.ZipModTimeUnixNano
	if !sameSignature {
		if err := clearLaunchBoxSnapshotIndexTx(tx); err != nil {
			return err
		}
		existing = launchBoxSnapshotCache{}
	}
	if len(cache.PlatformNames) == 0 && len(existing.PlatformNames) > 0 {
		cache.PlatformNames = existing.PlatformNames
	}
	if len(cache.RequestedPlatforms) == 0 && len(existing.RequestedPlatforms) > 0 {
		cache.RequestedPlatforms = existing.RequestedPlatforms
	}
	platformNamesJSON, err := encodeJSONText(cache.PlatformNames)
	if err != nil {
		return err
	}
	requestedPlatformsJSON, err := encodeJSONText(cache.RequestedPlatforms)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO launchbox_snapshot_meta (
	id, schema_version, zip_path, zip_size, zip_mtime_ns,
	platform_names_json, requested_platforms_json
) VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	schema_version = excluded.schema_version,
	zip_path = excluded.zip_path,
	zip_size = excluded.zip_size,
	zip_mtime_ns = excluded.zip_mtime_ns,
	platform_names_json = excluded.platform_names_json,
	requested_platforms_json = excluded.requested_platforms_json`,
		cache.SchemaVersion,
		cache.ZipPath,
		cache.ZipSize,
		cache.ZipModTimeUnixNano,
		platformNamesJSON,
		requestedPlatformsJSON,
	); err != nil {
		return err
	}
	if cache.ByID == nil {
		return nil
	}
	if err := clearLaunchBoxSnapshotIndexTx(tx); err != nil {
		return err
	}
	if err := insertLaunchBoxGamesTx(tx, cache.ByID); err != nil {
		return err
	}
	if err := insertLaunchBoxTitleIndexTx(tx, cache.ByPlatformTitle); err != nil {
		return err
	}
	return insertLaunchBoxAlternateIDsTx(tx, cache.AlternateIDs)
}

func loadLaunchBoxSnapshotMetaTx(tx *sql.Tx) (launchBoxSnapshotCache, bool, error) {
	row := tx.QueryRow(`
SELECT schema_version, zip_path, zip_size, zip_mtime_ns, platform_names_json, requested_platforms_json
FROM launchbox_snapshot_meta
WHERE id = 1`)
	var cache launchBoxSnapshotCache
	var platformNamesJSON, requestedPlatformsJSON string
	err := row.Scan(&cache.SchemaVersion, &cache.ZipPath, &cache.ZipSize, &cache.ZipModTimeUnixNano, &platformNamesJSON, &requestedPlatformsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return launchBoxSnapshotCache{}, false, nil
		}
		return launchBoxSnapshotCache{}, false, err
	}
	if err := decodeJSONText(platformNamesJSON, &cache.PlatformNames); err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	if err := decodeJSONText(requestedPlatformsJSON, &cache.RequestedPlatforms); err != nil {
		return launchBoxSnapshotCache{}, false, err
	}
	return cache, true, nil
}

func clearLaunchBoxSnapshotIndexTx(tx *sql.Tx) error {
	for _, query := range []string{
		`DELETE FROM launchbox_games`,
		`DELETE FROM launchbox_title_index`,
		`DELETE FROM launchbox_alternate_ids`,
	} {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func insertLaunchBoxGamesTx(tx *sql.Tx, games map[string]releaseMetadata) error {
	statement, err := tx.Prepare(`
INSERT INTO launchbox_games (
	database_id, platform, title, release_type, release_date, developer, publisher, genres_json,
	max_players, community_rating, description, alternate_titles_json, title_candidates_json, media_urls_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, metadata := range games {
		genresJSON, err := encodeJSONText(metadata.Genres)
		if err != nil {
			return err
		}
		alternateTitlesJSON, err := encodeJSONText(metadata.AlternateTitles)
		if err != nil {
			return err
		}
		titleCandidatesJSON, err := encodeJSONText(metadata.TitleCandidates)
		if err != nil {
			return err
		}
		mediaURLsJSON, err := encodeJSONText(metadata.MediaURLs)
		if err != nil {
			return err
		}
		if _, err := statement.Exec(
			metadata.DatabaseID,
			metadata.Platform,
			metadata.Title,
			metadata.ReleaseType,
			metadata.ReleaseDate,
			metadata.Developer,
			metadata.Publisher,
			genresJSON,
			metadata.MaxPlayers,
			metadata.CommunityRating,
			metadata.Description,
			alternateTitlesJSON,
			titleCandidatesJSON,
			mediaURLsJSON,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertLaunchBoxTitleIndexTx(tx *sql.Tx, byPlatformTitle map[string]map[string][]releaseMetadata) error {
	statement, err := tx.Prepare(`INSERT INTO launchbox_title_index(platform, key, database_id) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for platform, byTitle := range byPlatformTitle {
		for key, values := range byTitle {
			for _, metadata := range values {
				if metadata.DatabaseID == "" {
					continue
				}
				if _, err := statement.Exec(platform, key, metadata.DatabaseID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func insertLaunchBoxAlternateIDsTx(tx *sql.Tx, alternateIDs map[string]string) error {
	statement, err := tx.Prepare(`INSERT INTO launchbox_alternate_ids(key, database_id) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for key, id := range alternateIDs {
		if key == "" || id == "" {
			continue
		}
		if _, err := statement.Exec(key, id); err != nil {
			return err
		}
	}
	return nil
}

func encodeJSONText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeJSONText(value string, target any) error {
	if strings.TrimSpace(value) == "" {
		value = "null"
	}
	return json.Unmarshal([]byte(value), target)
}

func sortedPlatformNames(platforms map[string]bool) []string {
	names := make([]string, 0, len(platforms))
	for name := range platforms {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func parseMetadataXML(source io.Reader, platforms map[string]bool, index *snapshotIndex) error {
	decoder := xml.NewDecoder(source)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "Game":
			fields, err := readElementFields(decoder, start)
			if err != nil {
				return err
			}
			platform := strings.TrimSpace(fields["Platform"])
			if !platforms[platform] {
				continue
			}
			id := strings.TrimSpace(fields["DatabaseID"])
			title := strings.TrimSpace(firstNonEmpty(fields["Name"], fields["Title"]))
			if id == "" || title == "" {
				continue
			}
			metadata := releaseMetadata{
				DatabaseID:      id,
				ReleaseType:     strings.TrimSpace(fields["ReleaseType"]),
				Title:           title,
				Platform:        platform,
				Region:          strings.TrimSpace(fields["Region"]),
				ReleaseDate:     metadate.Normalize(fields["ReleaseDate"]),
				Developer:       strings.TrimSpace(fields["Developer"]),
				Publisher:       strings.TrimSpace(fields["Publisher"]),
				Genres:          splitGenres(fields["Genres"]),
				MaxPlayers:      strings.TrimSpace(fields["MaxPlayers"]),
				CommunityRating: strings.TrimSpace(fields["CommunityRating"]),
				Description:     strings.TrimSpace(firstNonEmpty(fields["Overview"], fields["Notes"])),
				TitleCandidates: titleCandidatesFor(title, fields["Region"], fields["Language"]),
				MediaURLs:       map[string][]sourceyaml.MediaCandidate{},
			}
			index.byID[id] = metadata
			if _, ok := index.byPlatformTitle[platform]; !ok {
				index.byPlatformTitle[platform] = map[string][]releaseMetadata{}
			}
			for _, key := range titleMatchKeys(title) {
				index.byPlatformTitle[platform][key] = appendLaunchBoxMetadata(index.byPlatformTitle[platform][key], metadata)
			}
		case "GameAlternateName":
			fields, err := readElementFields(decoder, start)
			if err != nil {
				return err
			}
			id := strings.TrimSpace(fields["DatabaseID"])
			title := strings.TrimSpace(fields["AlternateName"])
			if id == "" || title == "" {
				continue
			}
			if metadata, ok := index.byID[id]; ok {
				metadata.AlternateTitles = appendUnique(metadata.AlternateTitles, title)
				metadata.TitleCandidates = appendTitleCandidate(metadata.TitleCandidates, title, fields["Region"], fields["Language"])
				index.byID[id] = metadata
			}
			for _, key := range titleMatchKeys(title) {
				index.alternateIDs[key] = id
			}
		case "GameImage":
			fields, err := readElementFields(decoder, start)
			if err != nil {
				return err
			}
			id := strings.TrimSpace(fields["DatabaseID"])
			fileName := strings.TrimSpace(fields["FileName"])
			if id == "" || fileName == "" {
				continue
			}
			index.imagesByID[id] = append(index.imagesByID[id], launchboxImage{
				Type:     strings.TrimSpace(fields["Type"]),
				Region:   strings.TrimSpace(fields["Region"]),
				FileName: fileName,
				CRC32:    strings.TrimSpace(fields["CRC32"]),
			})
		}
	}
}

func titleCandidatesFor(title, region, language string) []sourceyaml.TitleCandidate {
	return appendTitleCandidate(nil, title, region, language)
}

func appendTitleCandidate(candidates []sourceyaml.TitleCandidate, title, region, language string) []sourceyaml.TitleCandidate {
	title = strings.TrimSpace(title)
	region = strings.TrimSpace(region)
	language = strings.TrimSpace(language)
	if title == "" || (region == "" && language == "") {
		return candidates
	}
	next := sourceyaml.TitleCandidate{Title: title, Region: region, Language: language}
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Title, next.Title) && strings.EqualFold(candidate.Region, next.Region) && strings.EqualFold(candidate.Language, next.Language) {
			return candidates
		}
	}
	return append(candidates, next)
}

func readElementFields(decoder *xml.Decoder, start xml.StartElement) (map[string]string, error) {
	fields := map[string]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			var value string
			if err := decoder.DecodeElement(&value, &t); err != nil {
				return nil, err
			}
			fields[t.Name.Local] = strings.TrimSpace(value)
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return fields, nil
			}
		}
	}
}

func mediaCandidates(images []launchboxImage) map[string][]sourceyaml.MediaCandidate {
	byKind := map[string][]mediaURLCandidate{}
	seenByKind := map[string]map[string]bool{}
	for index, image := range images {
		kind, _ := mapImageType(image.Type)
		if kind == "" || image.FileName == "" {
			continue
		}
		url := "https://images.launchbox-app.com/" + image.FileName
		if _, ok := seenByKind[kind]; !ok {
			seenByKind[kind] = map[string]bool{}
		}
		if seenByKind[kind][url] {
			continue
		}
		seenByKind[kind][url] = true
		byKind[kind] = append(byKind[kind], mediaURLCandidate{
			URL:           url,
			Region:        image.Region,
			SourceFile:    image.FileName,
			SourceType:    image.Type,
			CRC32:         image.CRC32,
			OriginalOrder: index,
		})
	}

	media := map[string][]sourceyaml.MediaCandidate{}
	for kind, candidates := range byKind {
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].OriginalOrder < candidates[j].OriginalOrder
		})
		for _, candidate := range candidates {
			media[kind] = append(media[kind], sourceyaml.MediaCandidate{
				URL:        candidate.URL,
				Region:     candidate.Region,
				SourceFile: candidate.SourceFile,
				SourceType: candidate.SourceType,
				CRC32:      candidate.CRC32,
			})
		}
	}
	return media
}

func mapImageType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "box - front", "box - front - reconstructed", "fanart - box - front":
		return "box_front", false
	case "box - back", "box - back - reconstructed", "fanart - box - back":
		return "box_back", false
	case "box - spine":
		return "box_spine", false
	case "box - full":
		return "box_full", false
	case "box - 3d":
		return "box_3d", false
	case "clear logo":
		return "logo", false
	case "banner":
		return "banner", false
	case "fanart - background", "amazon background", "epic games background", "gog background", "origin background", "steam background":
		return "fanart", false
	case "screenshot - game title":
		return "title_screen", false
	case "screenshot - gameplay", "screenshot - game select", "screenshot - high scores", "screenshot - game over", "amazon screenshot", "epic games screenshot", "gog screenshot", "steam screenshot":
		return "screenshot", true
	case "advertisement flyer - front":
		return "flyer_front", false
	case "advertisement flyer - back":
		return "flyer_back", false
	case "poster", "amazon poster", "epic games poster", "gog poster", "steam poster":
		return "poster", false
	case "cart - front":
		return "cart_front", false
	case "cart - back":
		return "cart_back", false
	case "cart - top":
		return "cart_top", false
	case "cart - 3d":
		return "cart_3d", false
	case "arcade - marquee", "marquee":
		return "marquee", false
	default:
		return normalizeAssetKind(value), false
	}
}

func renderMetadata(matches []matchedRelease) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "    matches:\n")
	for _, group := range groupMatchedReleases(matches) {
		if len(group) == 1 {
			writeMatchReleases(&b, []string{group[0].Release})
			renderReleaseMetadata(&b, "        ", group[0].Metadata)
			continue
		}
		if allReleaseMetadataEqual(group) {
			writeMatchReleases(&b, releaseNames(group))
			renderReleaseMetadata(&b, "        ", group[0].Metadata)
			continue
		}
		renderCompactReleaseMetadataGroup(&b, group)
	}
	return b.String()
}

func renderReleaseMetadata(b *bytes.Buffer, indent string, m releaseMetadata) {
	if m.DatabaseID != "" {
		fmt.Fprintf(b, "%sdatabase_id: %s\n", indent, yamlString(m.DatabaseID))
	}
	if m.Title != "" {
		fmt.Fprintf(b, "%stitle: %s\n", indent, yamlString(m.Title))
	}
	sourceyaml.RenderTitles(b, indent, m.TitleCandidates)
	if m.Platform != "" {
		fmt.Fprintf(b, "%splatform: %s\n", indent, yamlString(m.Platform))
	}
	if m.Region != "" {
		fmt.Fprintf(b, "%sregion: %s\n", indent, yamlString(m.Region))
	}
	if m.ReleaseType != "" {
		fmt.Fprintf(b, "%srelease_type: %s\n", indent, yamlString(m.ReleaseType))
	}

	if m.ReleaseDate != "" {
		fmt.Fprintf(b, "%srelease_date: %s\n", indent, yamlString(m.ReleaseDate))
	}
	if m.Developer != "" {
		fmt.Fprintf(b, "%sdeveloper: %s\n", indent, yamlString(m.Developer))
	}
	if m.Publisher != "" {
		fmt.Fprintf(b, "%spublisher: %s\n", indent, yamlString(m.Publisher))
	}
	if len(m.Genres) > 0 {
		fmt.Fprintf(b, "%sgenres: %s\n", indent, yamlStringList(m.Genres))
	}
	if m.MaxPlayers != "" {
		fmt.Fprintf(b, "%smax_players: %s\n", indent, yamlString(m.MaxPlayers))
	}
	if m.CommunityRating != "" {
		fmt.Fprintf(b, "%scommunity_rating: %s\n", indent, yamlString(m.CommunityRating))
	}
	if len(m.AlternateTitles) > 0 {
		fmt.Fprintf(b, "%salternate_titles: %s\n", indent, yamlStringList(m.AlternateTitles))
	}
	sourceyaml.RenderEnglishDescription(b, indent, m.Description)
	sourceyaml.RenderMedia(b, indent, m.MediaURLs)
}

func groupMatchedReleases(matches []matchedRelease) [][]matchedRelease {
	var grouped [][]matchedRelease
	byKey := map[string]int{}
	for _, match := range matches {
		key := matchedReleaseGroupKey(match)
		if index, ok := byKey[key]; ok {
			grouped[index] = append(grouped[index], match)
			continue
		}
		byKey[key] = len(grouped)
		grouped = append(grouped, []matchedRelease{match})
	}
	return grouped
}

func matchedReleaseGroupKey(match matchedRelease) string {
	if match.Metadata.DatabaseID != "" {
		return "database_id:" + match.Metadata.DatabaseID
	}
	return "metadata:" + matchedReleaseMetadataKey(match.Metadata)
}

func matchedReleaseMetadataKey(metadata releaseMetadata) string {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Sprintf("%#v", metadata)
	}
	return string(encoded)
}

func allReleaseMetadataEqual(matches []matchedRelease) bool {
	if len(matches) < 2 {
		return true
	}
	first := matches[0].Metadata
	for _, match := range matches[1:] {
		if !reflect.DeepEqual(first, match.Metadata) {
			return false
		}
	}
	return true
}

func releaseNames(matches []matchedRelease) []string {
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = appendUnique(result, match.Release)
	}
	return result
}

func renderCompactReleaseMetadataGroup(b *bytes.Buffer, matches []matchedRelease) {
	common := commonReleaseMetadata(matches)
	header := false
	switch {
	case common.DatabaseID != "":
		fmt.Fprintf(b, "      - database_id: %s\n", yamlString(common.DatabaseID))
		common.DatabaseID = ""
		header = true
	case common.Title != "":
		fmt.Fprintf(b, "      - title: %s\n", yamlString(common.Title))
		common.Title = ""
		header = true
	default:
		fmt.Fprintf(b, "      - releases:\n")
		header = false
	}
	if header {
		renderReleaseMetadata(b, "        ", common)
		fmt.Fprintf(b, "        releases:\n")
	}
	for _, match := range matches {
		fmt.Fprintf(b, "          - release: %s\n", yamlString(match.Release))
		override := releaseMetadataOverride(match.Metadata, commonReleaseMetadata(matches))
		renderReleaseMetadata(b, "            ", override)
	}
}

func commonReleaseMetadata(matches []matchedRelease) releaseMetadata {
	if len(matches) == 0 {
		return releaseMetadata{}
	}
	common := matches[0].Metadata
	for _, match := range matches[1:] {
		m := match.Metadata
		if common.DatabaseID != m.DatabaseID {
			common.DatabaseID = ""
		}
		if common.Title != m.Title {
			common.Title = ""
		}
		if common.Platform != m.Platform {
			common.Platform = ""
		}
		if common.Region != m.Region {
			common.Region = ""
		}
		if common.ReleaseDate != m.ReleaseDate {
			common.ReleaseDate = ""
		}
		if common.Developer != m.Developer {
			common.Developer = ""
		}
		if common.Publisher != m.Publisher {
			common.Publisher = ""
		}
		if !reflect.DeepEqual(common.Genres, m.Genres) {
			common.Genres = nil
		}
		if common.MaxPlayers != m.MaxPlayers {
			common.MaxPlayers = ""
		}
		if common.CommunityRating != m.CommunityRating {
			common.CommunityRating = ""
		}
		if common.Description != m.Description {
			common.Description = ""
		}
		if !reflect.DeepEqual(common.AlternateTitles, m.AlternateTitles) {
			common.AlternateTitles = nil
		}
		if !reflect.DeepEqual(common.TitleCandidates, m.TitleCandidates) {
			common.TitleCandidates = nil
		}
		if !reflect.DeepEqual(common.MediaURLs, m.MediaURLs) {
			common.MediaURLs = nil
		}
	}
	return common
}

func releaseMetadataOverride(metadata, common releaseMetadata) releaseMetadata {
	override := metadata
	if reflect.DeepEqual(override.DatabaseID, common.DatabaseID) {
		override.DatabaseID = ""
	}
	if override.Title == common.Title {
		override.Title = ""
	}
	if override.Platform == common.Platform {
		override.Platform = ""
	}
	if override.Region == common.Region {
		override.Region = ""
	}
	if override.ReleaseDate == common.ReleaseDate {
		override.ReleaseDate = ""
	}
	if override.Developer == common.Developer {
		override.Developer = ""
	}
	if override.Publisher == common.Publisher {
		override.Publisher = ""
	}
	if reflect.DeepEqual(override.Genres, common.Genres) {
		override.Genres = nil
	}
	if override.MaxPlayers == common.MaxPlayers {
		override.MaxPlayers = ""
	}
	if override.CommunityRating == common.CommunityRating {
		override.CommunityRating = ""
	}
	if override.Description == common.Description {
		override.Description = ""
	}
	if reflect.DeepEqual(override.AlternateTitles, common.AlternateTitles) {
		override.AlternateTitles = nil
	}
	if reflect.DeepEqual(override.TitleCandidates, common.TitleCandidates) {
		override.TitleCandidates = nil
	}
	if reflect.DeepEqual(override.MediaURLs, common.MediaURLs) {
		override.MediaURLs = nil
	}
	return override
}

func writeMatchReleases(b *bytes.Buffer, releases []string) {
	if len(releases) == 1 {
		fmt.Fprintf(b, "      - release: %s\n", yamlString(releases[0]))
		return
	}
	fmt.Fprintf(b, "      - releases: %s\n", yamlStringList(releases))
}

type matchedRelease struct {
	Release  string
	Metadata releaseMetadata
}

func downloadFile(url, path, stage, label string, progress taskprogress.Func) error {
	client := http.Client{Timeout: 10 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s failed: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download %s returned %s", url, response.Status)
	}

	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := response.ContentLength
	progress.UpdateBytes(stage, 0, total, label)
	body := taskprogress.NewReader(response.Body, progress, stage, total, label)
	_, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = catalogfs.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = catalogfs.Remove(tmp)
		return closeErr
	}
	if total > 0 {
		progress.UpdateBytes(stage, total, total, label)
	}
	return catalogfs.Rename(tmp, path)
}

func firstProgress(callbacks []taskprogress.Func) taskprogress.Func {
	if len(callbacks) == 0 {
		return nil
	}
	return callbacks[0]
}

func streamCatalogGames(dataRoot, platform, source string, forceOverwrite bool, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	return catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.MetadataReadOptions(platformFilters(platform), source, forceOverwrite), progress, func(cached catalogcache.Game) error {
		return fn(catalogGameFromCached(cached))
	})
}

func catalogGameFromCached(cached catalogcache.Game) catalogGame {
	var releases []catalogRelease
	for _, release := range cached.Releases {
		releases = append(releases, catalogRelease{
			MAMESet: release.MAMESet,
			Name:    release.Name,
			Title:   release.Title,
			Regions: release.Regions,
			Tags:    release.Tags,
		})
	}
	return catalogGame{
		ArcadePlatform:   cached.ArcadePlatform,
		Platform:         cached.Platform,
		Folder:           cached.Folder,
		Path:             cached.Path,
		Title:            cached.Title,
		Aliases:          cached.Aliases,
		Releases:         releases,
		Sources:          cached.Sources,
		PreservedSources: cached.PreservedSources,
	}
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func catalogPlatformNames(dataRoot, platform string) ([]string, error) {
	platformRoot := filepath.Join(dataRoot, "platforms")
	platform = strings.TrimSpace(platform)
	if platform != "" {
		info, err := catalogfs.Stat(filepath.Join(platformRoot, platform))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if !info.IsDir() || strings.HasPrefix(platform, ".") {
			return nil, nil
		}
		return []string{platform}, nil
	}
	entries, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func launchboxPlatformName(name string, available []string, registry *platformnames.Registry) string {
	if registry != nil {
		return registry.ResolveForSource(name, platformnames.SourceLaunchBox, available)
	}
	return platformnames.ResolveForSource(name, platformnames.SourceLaunchBox, available)
}

// Compact keys are derived from primary names only. Provider alternate names
// remain descriptive evidence and cannot silently become a different game's title.
func addCompactPrimaryTitleKeys(index snapshotIndex) snapshotIndex {
	for _, metadata := range index.byID {
		primary := index.byPlatformTitle[metadata.Platform]
		if primary == nil {
			continue
		}
		for _, key := range titleMatchKeys(metadata.Title) {
			compact := compactPrimaryTitleKey(key)
			primary[compact] = appendLaunchBoxMetadata(primary[compact], metadata)
		}
	}
	return index
}

func compactPrimaryTitleKey(key string) string { return "compact:" + strings.ReplaceAll(key, " ", "") }

func titleMatchKeys(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var keys []string
	for _, variant := range []string{
		value,
		stripTags(value),
		launchboxTitleShape(stripTags(value)),
	} {
		key := looseTitleKey(variant)
		if key != "" {
			keys = appendUnique(keys, key)
		}
	}
	return keys
}

func stripTags(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	value = strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " "))
	return value
}

func launchboxTitleShape(value string) string {
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
		case r == '+':
			b.WriteString(" plus ")
		default:
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(repeatedSpace.ReplaceAllString(b.String(), " "))
}

func normalizeAssetKind(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	var previous rune
	for _, r := range value {
		if unicode.IsUpper(r) && previous != 0 && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			b.WriteByte('-')
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('-')
		}
		previous = r
	}
	normalized := strings.Trim(b.String(), "-")
	normalized = repeatedDash.ReplaceAllString(normalized, "-")
	return normalized
}

func splitGenres(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == ','
	})
	var result []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func writeFile(path, contents string) error {
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return catalogfs.WriteFile(path, []byte(contents), 0o644)
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
	}
	return value
}

var tagPattern = regexp.MustCompile(`\s*[\(\[].*?[\)\]]\s*`)
var repeatedSpace = regexp.MustCompile(`\s+`)
var repeatedDash = regexp.MustCompile(`-+`)
