package openvgdb

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/metadate"
	"rommeta/internal/platformnames"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/taskprogress"

	_ "modernc.org/sqlite"
)

// ImportOptions controls OpenVGDB snapshot enrichment.
type ImportOptions struct {
	DataRoot         string
	DBPath           string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	ForceOverwrite   bool
	Progress         taskprogress.Func
}

// ImportResult describes a dry-run or applied OpenVGDB enrichment.
type ImportResult struct {
	DataRoot              string
	DBPath                string
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

// SnapshotResult describes the OpenVGDB snapshot used by a fetch.
type SnapshotResult struct {
	Checked    bool
	Downloaded bool
	URL        string
	DBPath     string
	ZipPath    string
}

const defaultSnapshotURL = "https://github.com/OpenVGDB/OpenVGDB/releases/download/v29.0/openvgdb.zip"

// DefaultSnapshotURL returns the built-in OpenVGDB release URL.
func DefaultSnapshotURL() string {
	return defaultSnapshotURL
}

// DefaultSnapshotPath returns the runtime-local OpenVGDB SQLite path.
func DefaultSnapshotPath(dataRoot string) string {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	return filepath.Join(dataRoot, ".rommeta", "sources", "openvgdb", "openvgdb.sqlite")
}

// EnsureSnapshot downloads the OpenVGDB release snapshot into .rommeta when missing.
func EnsureSnapshot(dataRoot string) (SnapshotResult, error) {
	return ensureSnapshot(dataRoot, defaultSnapshotURL)
}

// EnsureSnapshotFromURL downloads the OpenVGDB snapshot from a configured URL.
func EnsureSnapshotFromURL(dataRoot, url string, progressCallbacks ...taskprogress.Func) (SnapshotResult, error) {
	if strings.TrimSpace(url) == "" {
		url = defaultSnapshotURL
	}
	return ensureSnapshot(dataRoot, url, progressCallbacks...)
}

func ensureSnapshot(dataRoot, url string, progressCallbacks ...taskprogress.Func) (SnapshotResult, error) {
	progress := firstProgress(progressCallbacks)
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return SnapshotResult{}, err
	}
	dbPath := DefaultSnapshotPath(dataRoot)
	zipPath := filepath.Join(dataRoot, ".rommeta", "downloads", "openvgdb", "openvgdb.zip")
	result := SnapshotResult{Checked: true, URL: url, DBPath: dbPath, ZipPath: zipPath}
	if _, err := catalogfs.Stat(dbPath); err == nil {
		return result, nil
	} else if err != nil && !os.IsNotExist(err) {
		return SnapshotResult{}, err
	}

	if err := catalogfs.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return SnapshotResult{}, err
	}
	if err := catalogfs.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return SnapshotResult{}, err
	}
	if err := downloadFile(url, zipPath, "downloading snapshot", filepath.Base(zipPath), progress); err != nil {
		return SnapshotResult{}, err
	}
	progress.Update("extracting snapshot", 0, 0, filepath.Base(dbPath))
	if err := extractSQLite(zipPath, dbPath); err != nil {
		return SnapshotResult{}, err
	}
	result.Downloaded = true
	return result, nil
}

type catalogGame struct {
	PreservedSources []string
	MAMESets         map[string]string
	Platform         string
	Folder           string
	Path             string
	Title            string
	Releases         []string
	Sources          []string
	Fingerprints     []catalogcache.Fingerprint
}

type releaseMetadata struct {
	ROMFileName       string
	ROMCRC32          string
	ROMMD5            string
	ROMSHA1           string
	Title             string
	Region            string
	CoverFrontURL     string
	CoverBackURL      string
	CoverCartURL      string
	CoverDiscURL      string
	Description       string
	Developer         string
	Publisher         string
	Genre             string
	Date              string
	ReferenceURL      string
	ReferenceImageURL string
}

// Import matches platform release entries against an OpenVGDB SQLite snapshot
// and optionally writes source-specific candidate stanzas into game.yaml.
func Import(options ImportOptions) (ImportResult, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}
	dbPath, err := filepath.Abs(options.DBPath)
	if err != nil {
		return ImportResult{}, err
	}

	progress := options.Progress
	result := ImportResult{
		DataRoot: dataRoot,
		DBPath:   dbPath,
		Apply:    options.Apply,
	}

	var db *sql.DB
	byPlatform := map[string]platformReleaseIndex{}
	setupDone := false
	setupDB := func() error {
		if setupDone {
			return nil
		}
		progress.Update("opening database", 0, 0, filepath.Base(dbPath))
		var err error
		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			return err
		}
		setupDone = true
		return nil
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

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
	platformCount, gameCount, err := streamCatalogGames(dataRoot, options.Platform, "openvgdb", options.ForceOverwrite, streamProgress, func(game catalogGame) error {
		if catalogcache.HasSource(game.PreservedSources, "openvgdb") {
			result.SkippedPreservedCount++
			return nil
		}
		if !options.ForceOverwrite && catalogcache.HasSource(game.Sources, "openvgdb") {
			result.SkippedExistingCount++
			return nil
		}
		if err := setupDB(); err != nil {
			return err
		}
		current := catalogCurrent
		total := catalogTotal
		if current == 0 {
			current = result.GameFileCount + result.NotFoundGameCount + 1
		}
		progress.Update("matching", current, total, game.Platform+" / "+game.Folder)
		platformMatches, err := openvgdbReleases(db, game.Platform, byPlatform, options.PlatformRegistry)
		if err != nil {
			return err
		}
		var matches []matchedRelease
		for _, release := range game.Releases {
			result.ReleaseCount++
			var metadata releaseMetadata
			var ok bool
			if set := game.MAMESets[release]; set != "" {
				metadata, ok = platformMatches.matchMAMESet(set)
			} else {
				metadata, ok = platformMatches.match(release, game.Fingerprints)
			}
			if !ok {
				continue
			}
			result.MatchedReleaseCount++
			matches = append(matches, matchedRelease{
				Release:  release,
				Metadata: metadata,
			})
		}
		if len(matches) == 0 {
			result.NotFoundGameCount++
			if options.Apply {
				out := filepath.Join(game.Path, "game.yaml")
				if err := sourceyaml.WriteSourceMetadata(out, "openvgdb", renderNotFoundMetadata()); err != nil {
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
		if err := sourceyaml.WriteSourceMetadata(out, "openvgdb", renderMetadata(matches)); err != nil {
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

const snapshotDownloadTimeout = 10 * time.Minute

func downloadFile(url, path, stage, label string, progress taskprogress.Func) error {
	return downloadFileWithClient(&http.Client{Timeout: snapshotDownloadTimeout}, url, path, stage, label, progress)
}

func downloadFileWithClient(client *http.Client, url, path, stage, label string, progress taskprogress.Func) error {
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

func extractSQLite(zipPath, dbPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var member *zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Base(file.Name), "openvgdb.sqlite") {
			member = file
			break
		}
	}
	if member == nil {
		return fmt.Errorf("OpenVGDB zip does not contain openvgdb.sqlite")
	}

	source, err := member.Open()
	if err != nil {
		return err
	}
	defer source.Close()

	tmp := dbPath + ".tmp"
	target, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		_ = catalogfs.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = catalogfs.Remove(tmp)
		return closeErr
	}
	return catalogfs.Rename(tmp, dbPath)
}

type matchedRelease struct {
	Release  string
	Metadata releaseMetadata
}

type platformReleaseIndex struct {
	byName map[string]releaseMetadata
	byHash map[string][]releaseMetadata
}

func (index platformReleaseIndex) match(release string, fingerprints []catalogcache.Fingerprint) (releaseMetadata, bool) {
	for _, fingerprint := range fingerprints {
		if fingerprint.Release != "" && normalizeKey(fingerprint.Release) != normalizeKey(release) {
			continue
		}
		for _, key := range fingerprintHashKeys(fingerprint) {
			candidates := index.byHash[key]
			if len(candidates) > 0 {
				return chooseHashCandidate(release, candidates), true
			}
		}
	}
	metadata, ok := index.byName[normalizeKey(release)]
	return metadata, ok
}

func (index platformReleaseIndex) matchMAMESet(set string) (releaseMetadata, bool) {
	metadata, ok := index.byName[normalizeKey(set)]
	return metadata, ok && strings.EqualFold(metadata.ROMFileName, set+".zip")
}

func fingerprintHashKeys(fingerprint catalogcache.Fingerprint) []string {
	if fingerprint.PayloadKind != "" {
		return nil
	}
	var keys []string
	keys = appendHashKey(keys, "crc32", fingerprint.CRC32)
	keys = appendHashKey(keys, "md5", fingerprint.MD5)
	keys = appendHashKey(keys, "sha1", fingerprint.SHA1)
	return keys
}

func releaseHashKeys(metadata releaseMetadata) []string {
	var keys []string
	keys = appendHashKey(keys, "crc32", metadata.ROMCRC32)
	keys = appendHashKey(keys, "md5", metadata.ROMMD5)
	keys = appendHashKey(keys, "sha1", metadata.ROMSHA1)
	return keys
}

func appendHashKey(keys []string, kind, value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return keys
	}
	return append(keys, kind+":"+value)
}

func chooseHashCandidate(release string, candidates []releaseMetadata) releaseMetadata {
	if len(candidates) == 1 {
		return candidates[0]
	}
	releaseKey := normalizeKey(release)
	for _, candidate := range candidates {
		if normalizeKey(candidate.ROMFileName) == releaseKey || normalizeKey(strings.TrimSuffix(candidate.ROMFileName, filepath.Ext(candidate.ROMFileName))) == releaseKey {
			return candidate
		}
	}
	lowerRelease := strings.ToLower(release)
	for _, candidate := range candidates {
		region := strings.ToLower(strings.TrimSpace(candidate.Region))
		if region != "" && strings.Contains(lowerRelease, region) {
			return candidate
		}
	}
	return candidates[0]
}

func openvgdbReleases(db *sql.DB, platform string, cache map[string]platformReleaseIndex, registry *platformnames.Registry) (platformReleaseIndex, error) {
	key := displayPlatformName(platform, registry)
	if cached, ok := cache[key]; ok {
		return cached, nil
	}

	rows, err := db.Query(`
select
  rom.romExtensionlessFileName,
  rom.romFileName,
  rom.romHashCRC,
  rom.romHashMD5,
  rom.romHashSHA1,
  rel.releaseTitleName,
  rel.TEMPregionLocalizedName,
  rel.releaseCoverFront,
  rel.releaseCoverBack,
  rel.releaseCoverCart,
  rel.releaseCoverDisc,
  rel.releaseDescription,
  rel.releaseDeveloper,
  rel.releasePublisher,
  rel.releaseGenre,
  rel.releaseDate,
  rel.releaseReferenceURL,
  rel.releaseReferenceImageURL
from ROMs rom
join SYSTEMS sys on sys.systemID = rom.systemID
left join RELEASES rel on rel.romID = rom.romID
where sys.systemName = ?
`, key)
	if err != nil {
		return platformReleaseIndex{}, err
	}
	defer rows.Close()

	result := platformReleaseIndex{
		byName: map[string]releaseMetadata{},
		byHash: map[string][]releaseMetadata{},
	}
	for rows.Next() {
		var record nullableReleaseMetadata
		if err := rows.Scan(
			&record.ExtensionlessName,
			&record.ROMFileName,
			&record.ROMCRC32,
			&record.ROMMD5,
			&record.ROMSHA1,
			&record.Title,
			&record.Region,
			&record.CoverFrontURL,
			&record.CoverBackURL,
			&record.CoverCartURL,
			&record.CoverDiscURL,
			&record.Description,
			&record.Developer,
			&record.Publisher,
			&record.Genre,
			&record.Date,
			&record.ReferenceURL,
			&record.ReferenceImageURL,
		); err != nil {
			return platformReleaseIndex{}, err
		}
		name := record.ExtensionlessName.String
		if name == "" {
			name = strings.TrimSuffix(record.ROMFileName.String, filepath.Ext(record.ROMFileName.String))
		}
		if name == "" {
			continue
		}
		metadata := releaseMetadata{
			ROMFileName:       record.ROMFileName.String,
			ROMCRC32:          strings.ToLower(record.ROMCRC32.String),
			ROMMD5:            strings.ToLower(record.ROMMD5.String),
			ROMSHA1:           strings.ToLower(record.ROMSHA1.String),
			Title:             record.Title.String,
			Region:            record.Region.String,
			CoverFrontURL:     record.CoverFrontURL.String,
			CoverBackURL:      record.CoverBackURL.String,
			CoverCartURL:      record.CoverCartURL.String,
			CoverDiscURL:      record.CoverDiscURL.String,
			Description:       record.Description.String,
			Developer:         record.Developer.String,
			Publisher:         record.Publisher.String,
			Genre:             record.Genre.String,
			Date:              metadate.Normalize(record.Date.String),
			ReferenceURL:      record.ReferenceURL.String,
			ReferenceImageURL: record.ReferenceImageURL.String,
		}
		result.byName[normalizeKey(name)] = metadata
		for _, hashKey := range releaseHashKeys(metadata) {
			result.byHash[hashKey] = append(result.byHash[hashKey], metadata)
		}
	}
	if err := rows.Err(); err != nil {
		return platformReleaseIndex{}, err
	}
	cache[key] = result
	return result, nil
}

type nullableReleaseMetadata struct {
	ExtensionlessName sql.NullString
	ROMFileName       sql.NullString
	ROMCRC32          sql.NullString
	ROMMD5            sql.NullString
	ROMSHA1           sql.NullString
	Title             sql.NullString
	Region            sql.NullString
	CoverFrontURL     sql.NullString
	CoverBackURL      sql.NullString
	CoverCartURL      sql.NullString
	CoverDiscURL      sql.NullString
	Description       sql.NullString
	Developer         sql.NullString
	Publisher         sql.NullString
	Genre             sql.NullString
	Date              sql.NullString
	ReferenceURL      sql.NullString
	ReferenceImageURL sql.NullString
}

func streamCatalogGames(dataRoot, platform, source string, forceOverwrite bool, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	return catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.MetadataReadOptions(platformFilters(platform), source, forceOverwrite), progress, func(cached catalogcache.Game) error {
		return fn(catalogGameFromCached(cached))
	})
}

func catalogGameFromCached(cached catalogcache.Game) catalogGame {
	sets := map[string]string{}
	var releases []string
	for _, release := range cached.Releases {
		releases = append(releases, release.Name)
		if release.MAMESet != "" {
			sets[release.Name] = release.MAMESet
		}
	}
	return catalogGame{
		MAMESets:         sets,
		Platform:         cached.Platform,
		Folder:           cached.Folder,
		Path:             cached.Path,
		Title:            cached.Title,
		Releases:         releases,
		Sources:          cached.Sources,
		PreservedSources: cached.PreservedSources,
		Fingerprints:     cached.Fingerprints,
	}
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
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
	if m.Title != "" {
		fmt.Fprintf(b, "%stitle: %s\n", indent, yamlString(m.Title))
	}
	if m.Region != "" {
		fmt.Fprintf(b, "%sregion: %s\n", indent, yamlString(m.Region))
	}
	if m.Date != "" {
		fmt.Fprintf(b, "%srelease_date: %s\n", indent, yamlString(m.Date))
	}
	if m.Developer != "" {
		fmt.Fprintf(b, "%sdeveloper: %s\n", indent, yamlString(m.Developer))
	}
	if m.Publisher != "" {
		fmt.Fprintf(b, "%spublisher: %s\n", indent, yamlString(m.Publisher))
	}
	if genres := splitCSV(m.Genre); len(genres) > 0 {
		fmt.Fprintf(b, "%sgenres: %s\n", indent, yamlStringList(genres))
	}
	sourceyaml.RenderEnglishDescription(b, indent, m.Description)
	if m.ReferenceURL != "" || m.ReferenceImageURL != "" {
		fmt.Fprintf(b, "%sreferences:\n", indent)
		if m.ReferenceURL != "" {
			fmt.Fprintf(b, "%s  page: %s\n", indent, yamlString(m.ReferenceURL))
		}
		if m.ReferenceImageURL != "" {
			fmt.Fprintf(b, "%s  image_page: %s\n", indent, yamlString(m.ReferenceImageURL))
		}
	}
	sourceyaml.RenderMedia(b, indent, openVGDBMediaCandidates(m))
	if m.ROMFileName != "" || m.ROMCRC32 != "" || m.ROMMD5 != "" || m.ROMSHA1 != "" {
		fmt.Fprintf(b, "%sopenvgdb_rom:\n", indent)
		if m.ROMFileName != "" {
			fmt.Fprintf(b, "%s  file_name: %s\n", indent, yamlString(m.ROMFileName))
		}
		if m.ROMCRC32 != "" {
			fmt.Fprintf(b, "%s  crc32: %s\n", indent, m.ROMCRC32)
		}
		if m.ROMMD5 != "" {
			fmt.Fprintf(b, "%s  md5: %s\n", indent, m.ROMMD5)
		}
		if m.ROMSHA1 != "" {
			fmt.Fprintf(b, "%s  sha1: %s\n", indent, m.ROMSHA1)
		}
	}
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
	m := match.Metadata
	keyParts := []string{
		m.Title,
		m.Description,
		m.Developer,
		m.Publisher,
		m.Genre,
		m.ReferenceURL,
		m.ReferenceImageURL,
	}
	key := strings.Join(keyParts, "\x00")
	if strings.Trim(key, "\x00") == "" {
		return "metadata:" + matchedReleaseMetadataKey(m)
	}
	return "common:" + key
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
	if common.Title != "" {
		fmt.Fprintf(b, "      - title: %s\n", yamlString(common.Title))
		common.Title = ""
		header = true
	} else {
		fmt.Fprintf(b, "      - releases:\n")
	}
	if header {
		renderReleaseMetadata(b, "        ", common)
		fmt.Fprintf(b, "        releases:\n")
	}
	groupCommon := commonReleaseMetadata(matches)
	for _, match := range matches {
		fmt.Fprintf(b, "          - release: %s\n", yamlString(match.Release))
		override := releaseMetadataOverride(match.Metadata, groupCommon)
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
		if common.ROMFileName != m.ROMFileName {
			common.ROMFileName = ""
		}
		if common.ROMCRC32 != m.ROMCRC32 {
			common.ROMCRC32 = ""
		}
		if common.ROMMD5 != m.ROMMD5 {
			common.ROMMD5 = ""
		}
		if common.ROMSHA1 != m.ROMSHA1 {
			common.ROMSHA1 = ""
		}
		if common.Title != m.Title {
			common.Title = ""
		}
		if common.Region != m.Region {
			common.Region = ""
		}
		if common.CoverFrontURL != m.CoverFrontURL {
			common.CoverFrontURL = ""
		}
		if common.CoverBackURL != m.CoverBackURL {
			common.CoverBackURL = ""
		}
		if common.CoverCartURL != m.CoverCartURL {
			common.CoverCartURL = ""
		}
		if common.CoverDiscURL != m.CoverDiscURL {
			common.CoverDiscURL = ""
		}
		if common.Description != m.Description {
			common.Description = ""
		}
		if common.Developer != m.Developer {
			common.Developer = ""
		}
		if common.Publisher != m.Publisher {
			common.Publisher = ""
		}
		if common.Genre != m.Genre {
			common.Genre = ""
		}
		if common.Date != m.Date {
			common.Date = ""
		}
		if common.ReferenceURL != m.ReferenceURL {
			common.ReferenceURL = ""
		}
		if common.ReferenceImageURL != m.ReferenceImageURL {
			common.ReferenceImageURL = ""
		}
	}
	return common
}

func releaseMetadataOverride(metadata, common releaseMetadata) releaseMetadata {
	override := metadata
	if override.ROMFileName == common.ROMFileName {
		override.ROMFileName = ""
	}
	if override.ROMCRC32 == common.ROMCRC32 {
		override.ROMCRC32 = ""
	}
	if override.ROMMD5 == common.ROMMD5 {
		override.ROMMD5 = ""
	}
	if override.ROMSHA1 == common.ROMSHA1 {
		override.ROMSHA1 = ""
	}
	if override.Title == common.Title {
		override.Title = ""
	}
	if override.Region == common.Region {
		override.Region = ""
	}
	if override.CoverFrontURL == common.CoverFrontURL {
		override.CoverFrontURL = ""
	}
	if override.CoverBackURL == common.CoverBackURL {
		override.CoverBackURL = ""
	}
	if override.CoverCartURL == common.CoverCartURL {
		override.CoverCartURL = ""
	}
	if override.CoverDiscURL == common.CoverDiscURL {
		override.CoverDiscURL = ""
	}
	if override.Description == common.Description {
		override.Description = ""
	}
	if override.Developer == common.Developer {
		override.Developer = ""
	}
	if override.Publisher == common.Publisher {
		override.Publisher = ""
	}
	if override.Genre == common.Genre {
		override.Genre = ""
	}
	if override.Date == common.Date {
		override.Date = ""
	}
	if override.ReferenceURL == common.ReferenceURL {
		override.ReferenceURL = ""
	}
	if override.ReferenceImageURL == common.ReferenceImageURL {
		override.ReferenceImageURL = ""
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

func openVGDBMediaCandidates(m releaseMetadata) map[string][]sourceyaml.MediaCandidate {
	media := map[string][]sourceyaml.MediaCandidate{}
	addOpenVGDBMediaCandidate(media, "box_front", m.CoverFrontURL, m.Region)
	addOpenVGDBMediaCandidate(media, "box_back", m.CoverBackURL, m.Region)
	addOpenVGDBMediaCandidate(media, "physical_media", m.CoverCartURL, m.Region)
	addOpenVGDBMediaCandidate(media, "disc", m.CoverDiscURL, m.Region)
	return media
}

func addOpenVGDBMediaCandidate(media map[string][]sourceyaml.MediaCandidate, kind, mediaURL, region string) {
	if strings.TrimSpace(mediaURL) == "" {
		return
	}
	media[kind] = append(media[kind], sourceyaml.MediaCandidate{
		URL:    mediaURL,
		Region: region,
	})
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func displayPlatformName(name string, registry *platformnames.Registry) string {
	if registry != nil {
		return registry.ForSource(name, platformnames.SourceOpenVGDB)
	}
	return platformnames.ForSource(name, platformnames.SourceOpenVGDB)
}

func splitCSV(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
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
