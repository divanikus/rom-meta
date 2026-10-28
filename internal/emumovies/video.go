package emumovies

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/platformnames"
	"rommeta/internal/regions"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

// VideoOptions controls EmuMovies video download planning and fetching.
type VideoOptions struct {
	DataRoot         string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	Limit            int
	Concurrency      int
	PlanPath         string
	PlanFormat       string
	FTPClient        FTPClient
	Progress         taskprogress.Func
}

// VideoResult describes a dry-run or applied EmuMovies video fetch.
type VideoResult struct {
	SkippedPlatforms     map[string]string
	DataRoot             string
	FTPHost              string
	FTPDirectory         string
	VideoQuality         string
	PlatformCount        int
	GameBundleCount      int
	DirectoryEntryCount  int
	VideoFileCount       int
	MatchedVideoCount    int
	ExistingFileCount    int
	PlannedDownloadCount int
	DownloadedFileCount  int
	SkippedByLimitCount  int
	FailedDownloadCount  int
	Failures             []Failure
	PlanPath             string
	PlanFormat           string
	PlanItemCount        int
	Apply                bool
	Limit                int
}

// Failure is a failed EmuMovies download that did not stop the whole run.
type Failure struct {
	SourcePath string
	Target     string
	Error      string
}

// FTPClient is the small subset of FTP operations EmuMovies fetching needs.
type FTPClient interface {
	List(path string) ([]FTPEntry, error)
	Download(path, target string) error
}

type progressFTPClient interface {
	DownloadWithProgress(path, target string, size int64, progress taskprogress.Func, item string, worker, workers int) error
}

// FTPEntry describes one remote FTP directory entry.
type FTPEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

type sourceSettings struct {
	Username         string
	Password         string
	FTPHost          string
	URLScheme        string
	ExplicitTLS      bool
	ArchiveExtractor string
}

type catalogGame struct {
	Platform       string
	Folder         string
	Path           string
	Title          string
	DefaultRelease string
	Releases       []releaseInfo
	Aliases        []string
}

type releaseInfo struct {
	MAMESet string
	Name    string
	Title   string
	Regions []string
}

type catalogGameSet struct {
	PlatformCount int
	GameCount     int
	ByPlatform    map[string][]catalogGame
}

type videoCandidate struct {
	Platform    string
	Game        string
	GamePath    string
	Release     string
	Region      string
	Title       string
	SourcePath  string
	FileName    string
	Quality     string
	URL         string
	Target      string
	Size        int64
	MatchScore  int
	MatchedKeys []string
}

const (
	defaultFTPBaseURL    = "ftps://files.emumovies.com"
	minimumMatchScore    = 100
	sourceName           = "emumovies"
	defaultMediaKey      = "gameplay_video"
	defaultMediaFileStem = "gameplay-video"
	defaultPolicyScope   = "personal"
	defaultPolicyNote    = "Imported from EmuMovies account downloads; do not include in shareable packages."
	defaultConnTimeout   = 30 * time.Second
	defaultReadDeadline  = 60 * time.Second
)

type videoQuality struct {
	Name   string
	Parent string
	Marker string
}

var videoQualityPreference = []videoQuality{
	{Name: "HD", Parent: "Official/Video Snaps (HD)", Marker: " hd"},
	{Name: "HQ", Parent: "Official/Video Snaps (HQ)", Marker: " hq"},
	{Name: "SQ", Parent: "Official/Video Snaps (SQ)", Marker: " sq"},
}

// FetchVideo plans or downloads EmuMovies video snaps for catalog games.
func FetchVideo(options VideoOptions) (VideoResult, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return VideoResult{}, err
	}

	config, err := store.LoadSources(dataRoot)
	if err != nil {
		return VideoResult{}, err
	}
	settings, err := emuMoviesSettings(config)
	if err != nil {
		return VideoResult{}, err
	}
	if err := ensureCredentials(settings); err != nil {
		return VideoResult{}, err
	}

	progress := options.Progress
	gameSet, err := readCatalogGameSet(dataRoot, options.Platform, progress)
	if err != nil {
		return VideoResult{}, err
	}
	result := VideoResult{
		DataRoot:        dataRoot,
		FTPHost:         settings.FTPHost,
		PlatformCount:   gameSet.PlatformCount,
		GameBundleCount: gameSet.GameCount,
		Apply:           options.Apply,
		Limit:           options.Limit,
	}

	client := options.FTPClient
	if client == nil {
		client = NewFTPClient(settings.FTPHost, settings.Username, settings.Password, settings.ExplicitTLS)
	}

	result.SkippedPlatforms = map[string]string{}
	candidates, directory, quality, entryCount, videoCount, err := videoCandidates(client, settings, gameSet.ByPlatform, options.PlatformRegistry, result.SkippedPlatforms, progress)
	if err != nil {
		return VideoResult{}, err
	}
	result.FTPDirectory = directory
	result.VideoQuality = quality
	result.DirectoryEntryCount = entryCount
	result.VideoFileCount = videoCount
	result.MatchedVideoCount = len(candidates)

	assignTargets(dataRoot, candidates)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Target < candidates[j].Target
	})

	planWriter, err := openVideoPlanWriter(options.PlanPath, options.PlanFormat, dataRoot)
	if err != nil {
		return VideoResult{}, err
	}
	if planWriter != nil {
		defer planWriter.Abort()
	}

	var downloader *videoDownloader
	if options.Apply {
		downloader = startVideoDownloader(client, options.Concurrency, progress)
	}

	var metadataItems []videoCandidate
	sourcePolicyNeeded := false
	scanStage := "planning downloads"
	if options.Apply {
		scanStage = "scanning"
	}
	for index, candidate := range candidates {
		progress.Update(scanStage, index+1, len(candidates), candidate.Platform+" / "+candidate.Game)
		if _, err := catalogfs.Stat(candidate.Target); err == nil {
			result.ExistingFileCount++
			if options.Apply {
				sourcePolicyNeeded = true
			}
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return VideoResult{}, err
		}

		if options.Limit > 0 && result.PlannedDownloadCount >= options.Limit {
			result.SkippedByLimitCount++
			continue
		}
		result.PlannedDownloadCount++
		if planWriter != nil {
			if err := planWriter.Write(candidate); err != nil {
				if downloader != nil {
					downloader.CloseAndWait()
				}
				return VideoResult{}, err
			}
		}
		if downloader != nil {
			downloader.Enqueue(candidate)
		}
	}

	if downloader != nil {
		downloaded, failed, failures := downloader.CloseAndWait()
		result.DownloadedFileCount = downloaded
		result.FailedDownloadCount = failed
		result.Failures = failures
		metadataItems = downloader.successfulCandidates()
		if downloaded > 0 {
			sourcePolicyNeeded = true
		}
	}

	if options.Apply {
		progress.Update("writing metadata", 0, 0, sourceName)
		if err := writeSourceStanzas(groupCandidatesByGame(metadataItems)); err != nil {
			return VideoResult{}, err
		}
	}
	if sourcePolicyNeeded {
		progress.Update("writing policy", 0, 0, sourceName)
		if _, err := store.EnsureSourcePolicy(dataRoot, sourceName, defaultPolicyScope, defaultPolicyNote); err != nil {
			return VideoResult{}, err
		}
	}
	if strings.TrimSpace(options.PlanPath) != "" {
		progress.Update("writing plan", 0, 0, filepath.Base(options.PlanPath))
		planPath, planFormat, planItemCount, err := planWriter.Close()
		if err != nil {
			return VideoResult{}, err
		}
		result.PlanPath = planPath
		result.PlanFormat = planFormat
		result.PlanItemCount = planItemCount
	}
	return result, nil
}

type videoDownloadResult struct {
	candidate videoCandidate
	err       error
}

type videoDownloader struct {
	jobs       chan videoCandidate
	results    chan videoDownloadResult
	workers    sync.WaitGroup
	collector  sync.WaitGroup
	mu         sync.Mutex
	closed     bool
	downloaded int
	failed     int
	failures   []Failure
	successful []videoCandidate
}

func startVideoDownloader(client FTPClient, concurrency int, progress taskprogress.Func) *videoDownloader {
	workerCount := normalizedConcurrency(concurrency, 0)
	downloader := &videoDownloader{
		jobs:    make(chan videoCandidate),
		results: make(chan videoDownloadResult),
	}
	downloader.workers.Add(workerCount)
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		worker := workerIndex + 1
		go func() {
			defer downloader.workers.Done()
			progress.UpdateWorker("waiting", worker, workerCount, "waiting...")
			for candidate := range downloader.jobs {
				downloader.results <- videoDownloadResult{
					candidate: candidate,
					err:       downloadWithProgress(client, candidate.SourcePath, candidate.Target, candidate.Size, progress, candidate.Platform+" / "+candidate.FileName, worker, workerCount),
				}
				progress.UpdateWorker("waiting", worker, workerCount, "waiting...")
			}
		}()
	}
	downloader.collector.Add(1)
	go func() {
		defer downloader.collector.Done()
		for result := range downloader.results {
			downloader.record(result)
		}
	}()
	return downloader
}

func (downloader *videoDownloader) Enqueue(candidate videoCandidate) {
	if downloader == nil {
		return
	}
	downloader.jobs <- candidate
}

func (downloader *videoDownloader) CloseAndWait() (downloaded, failed int, failures []Failure) {
	if downloader == nil {
		return 0, 0, nil
	}
	downloader.mu.Lock()
	closed := downloader.closed
	if !closed {
		downloader.closed = true
		close(downloader.jobs)
	}
	downloader.mu.Unlock()
	if !closed {
		downloader.workers.Wait()
		close(downloader.results)
		downloader.collector.Wait()
	}
	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	return downloader.downloaded, downloader.failed, downloader.failures
}

func (downloader *videoDownloader) record(result videoDownloadResult) {
	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	if result.err != nil {
		downloader.failed++
		if len(downloader.failures) < 10 {
			downloader.failures = append(downloader.failures, Failure{
				SourcePath: result.candidate.SourcePath,
				Target:     result.candidate.Target,
				Error:      result.err.Error(),
			})
		}
		return
	}
	downloader.downloaded++
	downloader.successful = append(downloader.successful, result.candidate)
}

func (downloader *videoDownloader) successfulCandidates() []videoCandidate {
	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	return append([]videoCandidate(nil), downloader.successful...)
}

func downloadVideoCandidates(client FTPClient, candidates []videoCandidate, concurrency int, progress taskprogress.Func) (downloaded, failed int, failures []Failure) {
	downloader := startVideoDownloader(client, concurrency, progress)
	for _, candidate := range candidates {
		downloader.Enqueue(candidate)
	}
	return downloader.CloseAndWait()
}

func normalizedConcurrency(value, itemCount int) int {
	if value < 1 {
		value = 1
	}
	if itemCount > 0 && value > itemCount {
		return itemCount
	}
	return value
}

func downloadWithProgress(client FTPClient, sourcePath, target string, size int64, progress taskprogress.Func, item string, worker, workers int) error {
	progress.UpdateWorker("preparing", worker, workers, item)
	if progressClient, ok := client.(progressFTPClient); ok {
		return progressClient.DownloadWithProgress(sourcePath, target, size, progress, item, worker, workers)
	}
	progress.UpdateWorkerBytes("downloading", worker, workers, 0, size, item)
	if size <= 0 {
		defer progress.FinishWorkerBytes("downloading", worker, workers, item)
	}
	err := client.Download(sourcePath, target)
	if err == nil && size > 0 {
		progress.UpdateWorkerBytes("downloading", worker, workers, size, size, item)
	}
	return err
}

func emuMoviesSettings(config store.SourcesConfig) (sourceSettings, error) {
	var source store.Source
	found := false
	for _, candidate := range config.Sources {
		if candidate.Name == sourceName {
			source = candidate
			found = true
			break
		}
	}
	if !found {
		return sourceSettings{}, fmt.Errorf("source %s not found in %s", sourceName, config.Path)
	}

	baseURL := firstNonEmpty(source.APIBaseURL, defaultFTPBaseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return sourceSettings{}, fmt.Errorf("invalid EmuMovies FTP base URL %q: %w", baseURL, err)
	}
	host := parsed.Host
	if host == "" && parsed.Scheme == "" {
		host = baseURL
	}
	host = strings.TrimSpace(strings.TrimSuffix(host, "/"))
	if host == "" {
		return sourceSettings{}, fmt.Errorf("EmuMovies FTP host is empty")
	}

	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" || scheme == "ftp" {
		scheme = "ftps"
	}
	settings := sourceSettings{
		FTPHost:          host,
		URLScheme:        scheme,
		ExplicitTLS:      scheme == "ftps",
		ArchiveExtractor: source.ArchiveExtractor,
	}
	if value, ok := store.CredentialValue(config, source, "username"); ok {
		settings.Username = value
	}
	if value, ok := store.CredentialValue(config, source, "password"); ok {
		settings.Password = value
	}
	return settings, nil
}

func ensureCredentials(settings sourceSettings) error {
	var missing []string
	if strings.TrimSpace(settings.Username) == "" {
		missing = append(missing, "sources.emumovies.username")
	}
	if strings.TrimSpace(settings.Password) == "" {
		missing = append(missing, "sources.emumovies.password")
	}
	if len(missing) > 0 {
		return fmt.Errorf("EmuMovies credentials are missing: fill %s in config/secrets.yaml", strings.Join(missing, ", "))
	}
	return nil
}

func videoCandidates(client FTPClient, settings sourceSettings, platformGames map[string][]catalogGame, registry *platformnames.Registry, skipped map[string]string, progress taskprogress.Func) ([]videoCandidate, string, string, int, int, error) {
	var all []videoCandidate
	totalEntries := 0
	totalVideos := 0
	firstDirectory := ""
	firstQuality := ""
	platforms := sortedCatalogPlatforms(platformGames)
	for index, platform := range platforms {
		progress.Update("listing videos", index+1, len(platforms), platform)
		directory, quality, err := findVideoDirectory(client, platform, registry)
		if err != nil {
			var missing missingPlatformDirectoryError
			if errors.As(err, &missing) {
				if skipped != nil {
					skipped[platform] = err.Error()
				}
				continue
			}
			return nil, "", "", 0, 0, err
		}
		if firstDirectory == "" {
			firstDirectory = directory
		}
		if firstQuality == "" {
			firstQuality = quality
		} else if firstQuality != quality {
			firstQuality = "mixed"
		}
		entries, err := client.List(directory)
		if err != nil {
			return nil, "", "", 0, 0, err
		}
		totalEntries += len(entries)
		gameIndex := newMediaGameIndex(platformGames[platform], videoDirectorySystemName(pathpkg.Base(directory)))
		for _, entry := range entries {
			if entry.IsDir || !isVideoFile(entry.Name) {
				continue
			}
			totalVideos++
			match, ok := matchVideoEntry(platform, directory, quality, settings.URLScheme, settings.FTPHost, entry, gameIndex.candidates(entry.Name))
			if ok {
				all = append(all, match)
			}
		}
	}
	return all, firstDirectory, firstQuality, totalEntries, totalVideos, nil
}

func findVideoDirectory(client FTPClient, platform string, registry *platformnames.Registry) (string, string, error) {
	candidates := emuMoviesPlatformCandidates(platform, registry)
	var listErrors []string
	for _, quality := range videoQualityPreference {
		entries, err := client.List(quality.Parent)
		if err != nil {
			listErrors = append(listErrors, fmt.Sprintf("%s: %v", quality.Parent, err))
			continue
		}
		var matches []videoDirectoryMatch
		for _, entry := range entries {
			if !entry.IsDir {
				continue
			}
			score := videoDirectoryMatchScore(entry.Name, candidates, quality.Marker)
			if score > 0 {
				matches = append(matches, videoDirectoryMatch{
					Path:  pathpkg.Join(quality.Parent, entry.Name),
					Score: score,
				})
			}
		}
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Score == matches[j].Score {
				return matches[i].Path < matches[j].Path
			}
			return matches[i].Score > matches[j].Score
		})
		if len(matches) > 0 {
			return matches[0].Path, quality.Name, nil
		}
	}

	details := ""
	if len(listErrors) > 0 {
		details = " (list errors: " + strings.Join(listErrors, "; ") + ")"
	}
	message := fmt.Sprintf("EmuMovies video directory not found for platform %q in HD/HQ/SQ paths (tried: %s)%s", platform, strings.Join(candidates, ", "), details)
	if len(listErrors) > 0 {
		return "", "", errors.New(message)
	}
	return "", "", missingPlatformDirectoryError(message)
}

type videoDirectoryMatch struct {
	Path  string
	Score int
}

func videoDirectoryMatchScore(name string, candidates []string, qualityMarker string) int {
	nameKey := looseTitleKey(name)
	qualityMatches := strings.Contains(nameKey, qualityMarker)
	// Some source directories label the same tier by resolution only.
	switch strings.TrimSpace(qualityMarker) {
	case "hq":
		qualityMatches = qualityMatches || strings.Contains(nameKey, " 480p")
	case "sq":
		qualityMatches = qualityMatches || strings.Contains(nameKey, " 240p")
	}
	if !qualityMatches {
		return 0
	}
	if !strings.Contains(nameKey, "video snaps") && !strings.Contains(nameKey, "videos snaps") && !strings.Contains(nameKey, "video slides") {
		return 0
	}
	systemName := videoDirectorySystemName(name)
	best := 0
	for index, candidate := range candidates {
		score := platformDirectoryNameScore(systemName, candidate)
		if score == 0 {
			continue
		}
		score -= index
		if score > best {
			best = score
		}
	}
	return best
}

func videoDirectorySystemName(name string) string {
	lower := strings.ToLower(name)
	cut := len(name)
	for _, marker := range []string{" (video snaps", " (videos snaps", " (video slides", "(video snaps", "(videos snaps", "(video slides"} {
		if index := strings.Index(lower, marker); index >= 0 && index < cut {
			cut = index
		}
	}
	return strings.TrimSpace(name[:cut])
}

func platformDirectoryNameEqual(left, right string) bool {
	return platformDirectoryNameScore(left, right) >= 290
}

func platformDirectoryNameScore(left, right string) int {
	leftKey := looseTitleKey(left)
	rightKey := looseTitleKey(right)
	if leftKey == "" || rightKey == "" {
		return 0
	}
	if leftKey == rightKey {
		return 300
	}
	if compactTitleKey(left) == compactTitleKey(right) {
		return 290
	}
	// Extra words can name another system (Xbox 360, Game Boy Color, etc.).
	// Provider spelling variants must be explicit platform registry aliases.
	return 0
}

func matchVideoEntry(platform, directory, quality, scheme, host string, entry FTPEntry, games []catalogGame) (videoCandidate, bool) {
	stem := strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name))
	var best videoCandidate
	ambiguous := false
	for _, game := range games {
		release, score, keys := scoreVideoAgainstGame(stem, game, videoDirectorySystemName(pathpkg.Base(directory)))
		if score < minimumMatchScore {
			continue
		}
		candidate := videoCandidate{
			Platform:    platform,
			Game:        game.Folder,
			GamePath:    game.Path,
			Release:     release,
			Region:      videoRegionLabel(stem, release),
			Title:       stem,
			SourcePath:  pathpkg.Join(directory, entry.Name),
			FileName:    entry.Name,
			Quality:     quality,
			URL:         ftpURL(scheme, host, pathpkg.Join(directory, entry.Name)),
			Size:        entry.Size,
			MatchScore:  score,
			MatchedKeys: keys,
		}
		switch {
		case score > best.MatchScore:
			best = candidate
			ambiguous = false
		case score == best.MatchScore && score > 0 && candidate.GamePath != best.GamePath:
			ambiguous = true
		}
	}
	if best.MatchScore == 0 || ambiguous {
		return videoCandidate{}, false
	}
	return best, true
}

func scoreVideoAgainstGame(stem string, game catalogGame, providerSystem ...string) (string, int, []string) {
	mame := false
	for _, release := range game.Releases {
		if release.MAMESet == "" {
			continue
		}
		mame = true
		if strings.EqualFold(stem, release.MAMESet) {
			return release.Name, 200, []string{"mame_set"}
		}
	}
	if mame {
		return "", 0, nil
	}

	fileExactKeys := exactTitleKeys(stem)
	fileBaseKeys := titleMatchKeys(stem)
	fileRegions := regionTags(stem)
	releases := game.Releases
	if len(releases) == 0 {
		releases = []releaseInfo{{Name: firstNonEmpty(game.DefaultRelease, game.Title), Title: game.Title}}
	}
	bestRelease, bestScore := "", 0
	var bestKeys []string
	for _, release := range releases {
		matchName := catalogMediaName(game.Platform, stem, release.Name, providerSystem...)
		if !mediaIdentityCompatible(stem, matchName) {
			continue
		}
		releaseRegions := release.Regions
		if len(releaseRegions) == 0 {
			releaseRegions = regionTags(release.Name)
		}
		if len(fileRegions) > 0 && len(releaseRegions) > 0 && !regionOverlap(fileRegions, releaseRegions) {
			continue
		}
		score := 0
		var matched []string
		if intersects(fileExactKeys, exactTitleKeys(matchName)) {
			score = 160
			matched = append(matched, "release")
		}
		for _, candidate := range []struct {
			title, key string
			score      int
		}{{release.Title, "release_title", 118}, {release.Name, "release_name", 112}, {game.Title, "title", 110}} {
			if intersects(fileBaseKeys, titleMatchKeys(catalogMediaName(game.Platform, stem, candidate.title, providerSystem...))) && candidate.score > score {
				score = candidate.score
				matched = []string{candidate.key}
			}
		}
		for _, alias := range game.Aliases {
			if intersects(fileBaseKeys, titleMatchKeys(catalogMediaName(game.Platform, stem, alias, providerSystem...))) && 122 > score {
				score = 122
				matched = []string{"alias"}
			}
		}
		if score > 0 && regionOverlap(fileRegions, releaseRegions) {
			score += 12
			matched = appendUnique(matched, "region")
		}
		if score > 0 && matchName != release.Name {
			key := "psp_minis_label"
			if wiiWareLabel.MatchString(release.Name) && !wiiWareLabel.MatchString(matchName) {
				key = "wiiware_label"
			}
			matched = appendUnique(matched, key)
		}
		if score > bestScore || (score > 0 && score == bestScore && release.Name == game.DefaultRelease) {
			bestRelease, bestScore, bestKeys = release.Name, score, matched
		}
	}
	return bestRelease, bestScore, bestKeys
}

func videoRegionLabel(stem, release string) string {
	if label := regionLabel(regionTags(stem)); label != "" {
		return label
	}
	return releaseRegionLabel(release)
}

func regionLabel(values []string) string {
	var normalized []string
	for _, value := range values {
		if display := regions.NormalizeDisplay(value); display != "" {
			normalized = appendUnique(normalized, display)
		}
	}
	return strings.Join(normalized, ", ")
}

func releaseRegionLabel(release string) string {
	var values []string
	for _, match := range tagValuePattern.FindAllStringSubmatch(release, -1) {
		if len(match) < 2 {
			continue
		}
		for _, part := range splitQualifier(match[1]) {
			if normalized, ok := regions.NormalizeDATDisplay(part); ok {
				values = appendUnique(values, normalized)
			}
		}
	}
	return strings.Join(values, ", ")
}

func releaseNeedsMediaFolder(gameName, release string) bool {
	release = strings.TrimSpace(release)
	if release == "" {
		return false
	}
	if releaseHasMediaFolderFlag(release) && !releaseHasMediaFolderFlag(gameName) {
		return true
	}
	releaseBase := stripTags(release)
	if releaseBase == "" {
		return false
	}
	if sameTitle(gameName, releaseBase) || sameTitle(stripTags(gameName), releaseBase) {
		return false
	}
	return true
}

func releaseHasMediaFolderFlag(value string) bool {
	for _, match := range tagValuePattern.FindAllStringSubmatch(value, -1) {
		if len(match) < 2 {
			continue
		}
		for _, part := range splitQualifier(match[1]) {
			lower := strings.ToLower(strings.TrimSpace(part))
			switch {
			case lower == "unl", strings.Contains(lower, "unlicensed"), strings.Contains(lower, "pirate"),
				strings.Contains(lower, "aftermarket"), strings.Contains(lower, "homebrew"),
				strings.Contains(lower, "proto"), strings.Contains(lower, "demo"), strings.Contains(lower, "sample"):
				return true
			}
		}
	}
	return false
}

func sameTitle(left, right string) bool {
	return compactTitleKey(left) != "" && compactTitleKey(left) == compactTitleKey(right)
}

func splitQualifier(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '+'
	})
}

func sourceVideoFileName(region, extension string, ordinal int) string {
	stem := defaultMediaFileStem
	if ordinal > 1 {
		stem = fmt.Sprintf("%s-%02d", stem, ordinal)
	}
	parts := []string{stem}
	if token := filenameToken(region); token != "" {
		parts = append(parts, token)
	}
	parts = append(parts, sourceName)
	return strings.Join(parts, ".") + extension
}

func assignTargets(dataRoot string, candidates []videoCandidate) {
	byGame := map[string][]int{}
	for i, candidate := range candidates {
		byGame[candidate.GamePath] = append(byGame[candidate.GamePath], i)
	}
	for _, indexes := range byGame {
		used := map[string]int{}
		for _, index := range indexes {
			candidate := &candidates[index]
			extension := strings.ToLower(filepath.Ext(candidate.FileName))
			if extension == "" {
				extension = ".mp4"
			}
			targetDir := filepath.Join(candidate.GamePath, "media")
			if len(indexes) > 1 && releaseNeedsMediaFolder(candidate.Game, candidate.Release) {
				targetDir = filepath.Join(targetDir, candidate.Release)
			}
			baseName := sourceVideoFileName(candidate.Region, extension, 1)
			baseTarget := filepath.Join(targetDir, baseName)
			used[baseTarget]++
			target := filepath.Join(targetDir, sourceVideoFileName(candidate.Region, extension, used[baseTarget]))
			candidate.Target = filepath.Clean(target)
		}
	}
	_ = dataRoot
}

func writeSourceStanzas(byGame map[string][]videoCandidate) error {
	paths := make([]string, 0, len(byGame))
	for path := range byGame {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		candidates := byGame[path]
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Release == candidates[j].Release {
				return candidates[i].SourcePath < candidates[j].SourcePath
			}
			return candidates[i].Release < candidates[j].Release
		})
		out := filepath.Join(path, "game.yaml")
		if err := appendVideoSourceMatches(out, renderMetadata(candidates)); err != nil {
			return err
		}
	}
	return nil
}

func groupCandidatesByGame(candidates []videoCandidate) map[string][]videoCandidate {
	byGame := map[string][]videoCandidate{}
	for _, candidate := range candidates {
		byGame[candidate.GamePath] = append(byGame[candidate.GamePath], candidate)
	}
	return byGame
}

func renderMetadata(candidates []videoCandidate) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "    matches:\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&b, "      - release: %s\n", yamlString(candidate.Release))
		fmt.Fprintf(&b, "        title: %s\n", yamlString(candidate.Title))
		fmt.Fprintf(&b, "        matched_file: %s\n", yamlString(candidate.FileName))
		fmt.Fprintf(&b, "        matched_path: %s\n", yamlString(candidate.SourcePath))
		fmt.Fprintf(&b, "        match_score: %d\n", candidate.MatchScore)
		if len(candidate.MatchedKeys) > 0 {
			fmt.Fprintf(&b, "        match_keys: %s\n", yamlStringList(candidate.MatchedKeys))
		}
		if candidate.Size > 0 {
			fmt.Fprintf(&b, "        size: %d\n", candidate.Size)
		}
		sourceyaml.RenderMedia(&b, "        ", map[string][]sourceyaml.MediaCandidate{
			defaultMediaKey: {{
				URL:        candidate.URL,
				Region:     candidate.Region,
				Quality:    candidate.Quality,
				SourceFile: candidate.FileName,
				SourceType: candidate.SourcePath,
				Size:       candidate.Size,
			}},
		})
	}
	return b.String()
}

type videoPlanWriter struct {
	path     string
	tmpPath  string
	format   string
	dataRoot string
	file     *os.File
	writer   *bufio.Writer
	encoder  *json.Encoder
	count    int
	closed   bool
}

func openVideoPlanWriter(path, format, dataRoot string) (*videoPlanWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := catalogfs.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, err
	}
	planFormat := normalizedPlanFormat(path, format)
	switch planFormat {
	case "aria2", "jsonl":
	default:
		return nil, fmt.Errorf("unsupported plan format: %s", format)
	}
	tmpPath := absPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}
	writer := bufio.NewWriter(file)
	plan := &videoPlanWriter{
		path:     absPath,
		tmpPath:  tmpPath,
		format:   planFormat,
		dataRoot: dataRoot,
		file:     file,
		writer:   writer,
	}
	if planFormat == "jsonl" {
		plan.encoder = json.NewEncoder(writer)
	}
	return plan, nil
}

func (plan *videoPlanWriter) Write(item videoCandidate) error {
	if plan == nil {
		return nil
	}
	switch plan.format {
	case "aria2":
		relativeTarget := relativePlanPath(plan.dataRoot, item.Target)
		if _, err := fmt.Fprintf(plan.writer, "%s\n", item.URL); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(plan.writer, "  dir=%s\n", pathpkg.Dir(relativeTarget)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(plan.writer, "  out=%s\n", pathpkg.Base(relativeTarget)); err != nil {
			return err
		}
	case "jsonl":
		if err := plan.encoder.Encode(map[string]any{
			"source":      sourceName,
			"platform":    item.Platform,
			"game":        item.Game,
			"release":     item.Release,
			"media_type":  defaultMediaKey,
			"region":      item.Region,
			"url":         item.URL,
			"source_path": item.SourcePath,
			"target":      relativePlanPath(plan.dataRoot, item.Target),
			"size":        item.Size,
			"match_score": item.MatchScore,
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported plan format: %s", plan.format)
	}
	plan.count++
	return nil
}

func (plan *videoPlanWriter) Close() (path, format string, count int, err error) {
	if plan == nil {
		return "", "", 0, nil
	}
	if plan.closed {
		return plan.path, plan.format, plan.count, nil
	}
	plan.closed = true
	if err := plan.writer.Flush(); err != nil {
		_ = plan.file.Close()
		return "", "", 0, err
	}
	if err := plan.file.Close(); err != nil {
		return "", "", 0, err
	}
	if err := catalogfs.Rename(plan.tmpPath, plan.path); err != nil {
		return "", "", 0, err
	}
	return plan.path, plan.format, plan.count, nil
}

func (plan *videoPlanWriter) Abort() {
	if plan == nil || plan.closed {
		return
	}
	plan.closed = true
	_ = plan.file.Close()
	_ = catalogfs.Remove(plan.tmpPath)
}

func normalizedPlanFormat(path, format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "" {
		return format
	}
	if strings.EqualFold(filepath.Ext(path), ".aria2") {
		return "aria2"
	}
	return "jsonl"
}

func relativePlanPath(dataRoot, target string) string {
	relative, err := filepath.Rel(dataRoot, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(relative)
}

func readCatalogGameSet(dataRoot, platform string, progressCallbacks ...taskprogress.Func) (catalogGameSet, error) {
	progress := firstProgress(progressCallbacks)
	set := catalogGameSet{ByPlatform: map[string][]catalogGame{}}
	platformCount, gameCount, err := catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.ReadOptions{
		Platforms:    platformFilters(platform),
		ReadingStage: "indexing catalog",
	}, progress, func(cached catalogcache.Game) error {
		game := catalogGame{
			Platform:       cached.Platform,
			Folder:         cached.Folder,
			Path:           cached.Path,
			Title:          cached.Title,
			DefaultRelease: cached.DefaultRelease,
			Aliases:        cached.Aliases,
		}
		for _, release := range cached.Releases {
			game.Releases = append(game.Releases, releaseInfo{
				MAMESet: release.MAMESet,
				Name:    release.Name,
				Title:   release.Title,
				Regions: release.Regions,
			})
		}
		set.ByPlatform[game.Platform] = append(set.ByPlatform[game.Platform], game)
		return nil
	})
	if err != nil {
		return catalogGameSet{}, err
	}
	set.PlatformCount = platformCount
	set.GameCount = gameCount
	return set, nil
}

func readCatalogGames(dataRoot, platform string, progressCallbacks ...taskprogress.Func) ([]catalogGame, int, error) {
	set, err := readCatalogGameSet(dataRoot, platform, progressCallbacks...)
	if err != nil {
		return nil, 0, err
	}
	return set.allGames(), set.PlatformCount, nil
}

func (set catalogGameSet) allGames() []catalogGame {
	games := make([]catalogGame, 0, set.GameCount)
	for _, platform := range sortedCatalogPlatforms(set.ByPlatform) {
		games = append(games, set.ByPlatform[platform]...)
	}
	return games
}

func catalogGamesByPlatform(games []catalogGame) map[string][]catalogGame {
	platformGames := map[string][]catalogGame{}
	for _, game := range games {
		platformGames[game.Platform] = append(platformGames[game.Platform], game)
	}
	return platformGames
}

func sortedCatalogPlatforms(platformGames map[string][]catalogGame) []string {
	platforms := make([]string, 0, len(platformGames))
	for platform := range platformGames {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	return platforms
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func firstProgress(callbacks []taskprogress.Func) taskprogress.Func {
	if len(callbacks) == 0 {
		return nil
	}
	return callbacks[0]
}

func isVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".avi", ".mkv", ".mov", ".webm":
		return true
	default:
		return false
	}
}

func displayPlatformName(name string, registry *platformnames.Registry) string {
	if registry != nil {
		return registry.ForSource(name, platformnames.SourceEmuMovies)
	}
	return platformnames.ForSource(name, platformnames.SourceEmuMovies)
}

func emuMoviesPlatformCandidates(name string, registry *platformnames.Registry) []string {
	if registry != nil {
		return registry.CandidatesForSource(name, platformnames.SourceEmuMovies)
	}
	return platformnames.CandidatesForSource(name, platformnames.SourceEmuMovies)
}

func firstReleaseName(game catalogGame) string {
	if len(game.Releases) == 0 {
		return ""
	}
	return game.Releases[0].Name
}

func exactTitleKeys(value string) []string {
	return []string{looseTitleKey(value), compactTitleKey(value)}
}

func titleMatchKeys(value string) []string {
	value = mediaTitle(value)
	if value == "" {
		return nil
	}
	var keys []string
	for _, variant := range []string{
		value,
		titleShape(value),
		strings.ReplaceAll(titleShape(value), ": ", " - "),
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

func regionTags(value string) []string {
	var values []string
	for _, match := range tagValuePattern.FindAllStringSubmatch(value, -1) {
		for _, part := range strings.Split(match[1], ",") {
			if normalized, ok := regions.NormalizeDATDisplay(part); ok {
				values = appendUnique(values, normalized)
			}
		}
	}
	return values
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
			return strings.TrimSpace(strings.TrimPrefix(article, ", ") + " " + base)
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if contains(right, value) {
			return true
		}
	}
	return false
}

func regionOverlap(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	for _, a := range left {
		for _, b := range right {
			if strings.EqualFold(a, b) || strings.EqualFold(a, "World") || strings.EqualFold(b, "World") {
				return true
			}
		}
	}
	return false
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

func ftpURL(scheme, host, remotePath string) string {
	if strings.TrimSpace(scheme) == "" {
		scheme = "ftps"
	}
	parts := strings.Split(remotePath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return scheme + "://" + host + "/" + strings.Join(parts, "/")
}

func parseFTPList(contents string) []FTPEntry {
	var entries []FTPEntry
	for _, raw := range strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if entry, ok := parseFTPLine(line); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseFTPLine(line string) (FTPEntry, bool) {
	if match := windowsFTPLine.FindStringSubmatch(line); len(match) == 3 {
		sizeOrDir := strings.TrimSpace(match[1])
		name := strings.TrimSpace(match[2])
		if sizeOrDir == "<DIR>" {
			return FTPEntry{Name: name, IsDir: true}, true
		}
		size, _ := strconv.ParseInt(sizeOrDir, 10, 64)
		return FTPEntry{Name: name, Size: size}, true
	}
	if match := unixFTPLine.FindStringSubmatch(line); len(match) == 4 {
		size, _ := strconv.ParseInt(match[2], 10, 64)
		return FTPEntry{Name: strings.TrimSpace(match[3]), IsDir: strings.HasPrefix(match[1], "d"), Size: size}, true
	}
	return FTPEntry{}, false
}

// NewFTPClient creates a standard FTP client for EmuMovies file server access.
func NewFTPClient(host, username, password string, explicitTLS bool) FTPClient {
	return &ftpClient{
		host:        host,
		username:    username,
		password:    password,
		explicitTLS: explicitTLS,
		timeout:     defaultConnTimeout,
	}
}

type ftpClient struct {
	host        string
	username    string
	password    string
	explicitTLS bool
	timeout     time.Duration
}

func (client *ftpClient) List(remotePath string) ([]FTPEntry, error) {
	session, err := client.connect()
	if err != nil {
		return nil, err
	}
	defer session.close()

	data, err := session.readData("LIST " + remotePath)
	if err != nil {
		return nil, err
	}
	return parseFTPList(string(data)), nil
}

func (client *ftpClient) Download(remotePath, target string) error {
	session, err := client.connect()
	if err != nil {
		return err
	}
	defer session.close()
	return session.download(remotePath, target)
}

func (client *ftpClient) DownloadWithProgress(remotePath, target string, size int64, progress taskprogress.Func, item string, worker, workers int) error {
	session, err := client.connect()
	if err != nil {
		return err
	}
	defer session.close()
	return session.downloadWithProgress(remotePath, target, size, progress, item, worker, workers)
}

func (client *ftpClient) connect() (*ftpSession, error) {
	host := client.host
	if !strings.Contains(host, ":") {
		host += ":21"
	}
	conn, err := net.DialTimeout("tcp", host, client.timeout)
	if err != nil {
		return nil, err
	}
	session := &ftpSession{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		serverName: serverName(client.host),
	}
	if err := session.setDeadline(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if code, message, err := session.readReply(); err != nil || code != 220 {
		_ = conn.Close()
		return nil, fmt.Errorf("FTP welcome failed: %d %s %v", code, message, err)
	}
	if client.explicitTLS {
		if err := session.enableExplicitTLS(); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := session.commandExpect("PBSZ 0", 200); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := session.commandExpect("PROT P", 200); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	if err := session.commandExpect("USER "+client.username, 230, 331); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := session.commandExpect("PASS "+client.password, 230); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := session.commandExpect("TYPE I", 200); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return session, nil
}

type ftpSession struct {
	conn   net.Conn
	reader *bufio.Reader

	serverName string
	secureData bool
}

func (session *ftpSession) close() {
	_ = session.commandExpect("QUIT", 221)
	_ = session.conn.Close()
}

func (session *ftpSession) enableExplicitTLS() error {
	if err := session.send("AUTH TLS"); err != nil {
		return err
	}
	code, message, err := session.readReply()
	if err != nil {
		return err
	}
	if code != 234 {
		if err := session.send("AUTH SSL"); err != nil {
			return err
		}
		code, message, err = session.readReply()
		if err != nil {
			return err
		}
	}
	if code != 234 {
		return fmt.Errorf("FTP server did not accept explicit TLS: %d %s", code, message)
	}
	tlsConn := tls.Client(session.conn, &tls.Config{
		ServerName: session.serverName,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		return err
	}
	session.conn = tlsConn
	session.reader = bufio.NewReader(tlsConn)
	session.secureData = true
	return nil
}

func (session *ftpSession) download(remotePath, target string) error {
	return session.downloadWithProgress(remotePath, target, 0, nil, filepath.Base(target), 0, 0)
}

func (session *ftpSession) downloadWithProgress(remotePath, target string, size int64, progress taskprogress.Func, item string, worker, workers int) error {
	dataConn, err := session.openPassive()
	if err != nil {
		return err
	}
	defer dataConn.Close()

	if err := session.send("RETR " + remotePath); err != nil {
		return err
	}
	code, message, err := session.readReply()
	if err != nil {
		return err
	}
	if code < 100 || code >= 200 {
		return fmt.Errorf("FTP RETR failed: %d %s", code, message)
	}
	dataConn, err = session.secureDataConnection(dataConn)
	if err != nil {
		return err
	}

	if err := catalogfs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_ = dataConn.SetDeadline(time.Time{})
	updateDownloadBytes(progress, worker, workers, 0, size, item)
	if size <= 0 {
		defer finishDownloadBytes(progress, worker, workers, item)
	}
	_, copyErr := copyWithIdleDeadline(file, dataConn, defaultReadDeadline, progress, "downloading", size, item, worker, workers)
	closeErr := file.Close()
	_ = dataConn.Close()
	if copyErr != nil {
		_ = catalogfs.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = catalogfs.Remove(tmp)
		return closeErr
	}
	code, message, err = session.readReply()
	if err != nil {
		_ = catalogfs.Remove(tmp)
		return err
	}
	if code < 200 || code >= 300 {
		_ = catalogfs.Remove(tmp)
		return fmt.Errorf("FTP RETR final reply failed: %d %s", code, message)
	}
	if size > 0 {
		updateDownloadBytes(progress, worker, workers, size, size, item)
	}
	return catalogfs.Rename(tmp, target)
}

func copyWithIdleDeadline(dst io.Writer, src net.Conn, idleTimeout time.Duration, progress taskprogress.Func, stage string, total int64, item string, worker, workers int) (int64, error) {
	buffer := make([]byte, 128*1024)
	var written int64
	for {
		if idleTimeout > 0 {
			if err := src.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
				return written, err
			}
			// A peer may close between reads. Read once more to distinguish its EOF
			// from a local close/error instead of failing on deadline renewal.
		}
		readCount, readErr := src.Read(buffer)
		if readCount > 0 {
			writeCount, writeErr := dst.Write(buffer[:readCount])
			written += int64(writeCount)
			if worker > 0 {
				progress.UpdateWorkerBytes(stage, worker, workers, written, total, item)
			} else {
				progress.UpdateBytes(stage, written, total, item)
			}
			if writeErr != nil {
				return written, writeErr
			}
			if writeCount != readCount {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				if idleTimeout > 0 {
					_ = src.SetReadDeadline(time.Time{})
				}
				return written, nil
			}
			return written, readErr
		}
	}
}

func updateDownloadBytes(progress taskprogress.Func, worker, workers int, current, total int64, item string) {
	if worker > 0 {
		progress.UpdateWorkerBytes("downloading", worker, workers, current, total, item)
		return
	}
	progress.UpdateBytes("downloading", current, total, item)
}

func finishDownloadBytes(progress taskprogress.Func, worker, workers int, item string) {
	if worker > 0 {
		progress.FinishWorkerBytes("downloading", worker, workers, item)
		return
	}
	progress.FinishBytes("downloading", item)
}

func (session *ftpSession) readData(command string) ([]byte, error) {
	dataConn, err := session.openPassive()
	if err != nil {
		return nil, err
	}
	defer dataConn.Close()

	if err := session.send(command); err != nil {
		return nil, err
	}
	code, message, err := session.readReply()
	if err != nil {
		return nil, err
	}
	if code < 100 || code >= 200 {
		return nil, fmt.Errorf("FTP data command failed: %d %s", code, message)
	}
	dataConn, err = session.secureDataConnection(dataConn)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(dataConn)
	_ = dataConn.Close()
	if err != nil {
		return nil, err
	}
	code, message, err = session.readReply()
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("FTP data final reply failed: %d %s", code, message)
	}
	return data, nil
}

func (session *ftpSession) openPassive() (net.Conn, error) {
	if err := session.send("PASV"); err != nil {
		return nil, err
	}
	code, message, err := session.readReply()
	if err != nil {
		return nil, err
	}
	if code != 227 {
		return nil, fmt.Errorf("FTP PASV failed: %d %s", code, message)
	}
	host, port, err := parsePASV(message)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), defaultConnTimeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(defaultReadDeadline))
	return conn, nil
}

func (session *ftpSession) secureDataConnection(conn net.Conn) (net.Conn, error) {
	if !session.secureData {
		return conn, nil
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: session.serverName,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func parsePASV(message string) (string, int, error) {
	start := strings.Index(message, "(")
	end := strings.Index(message, ")")
	if start == -1 || end == -1 || end <= start {
		return "", 0, fmt.Errorf("invalid PASV reply: %s", message)
	}
	parts := strings.Split(message[start+1:end], ",")
	if len(parts) != 6 {
		return "", 0, fmt.Errorf("invalid PASV address: %s", message)
	}
	values := make([]int, 6)
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return "", 0, err
		}
		values[i] = value
	}
	host := fmt.Sprintf("%d.%d.%d.%d", values[0], values[1], values[2], values[3])
	port := values[4]*256 + values[5]
	return host, port, nil
}

func (session *ftpSession) commandExpect(command string, expected ...int) error {
	if err := session.send(command); err != nil {
		return err
	}
	code, message, err := session.readReply()
	if err != nil {
		return err
	}
	for _, value := range expected {
		if code == value {
			return nil
		}
	}
	return fmt.Errorf("FTP command %q returned %d %s", commandName(command), code, message)
}

func (session *ftpSession) send(command string) error {
	if err := session.setDeadline(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(session.conn, "%s\r\n", command)
	return err
}

func (session *ftpSession) readReply() (int, string, error) {
	if err := session.setDeadline(); err != nil {
		return 0, "", err
	}
	line, err := session.reader.ReadString('\n')
	if err != nil {
		return 0, strings.TrimSpace(line), err
	}
	message := strings.TrimRight(line, "\r\n")
	if len(message) < 3 {
		return 0, message, fmt.Errorf("short FTP reply: %s", message)
	}
	code, err := strconv.Atoi(message[:3])
	if err != nil {
		return 0, message, err
	}
	if len(message) > 3 && message[3] == '-' {
		prefix := message[:3] + " "
		for {
			next, err := session.reader.ReadString('\n')
			if err != nil {
				return code, message, err
			}
			next = strings.TrimRight(next, "\r\n")
			message += "\n" + next
			if strings.HasPrefix(next, prefix) {
				break
			}
		}
	}
	return code, message, nil
}

func (session *ftpSession) setDeadline() error {
	return session.conn.SetDeadline(time.Now().Add(defaultReadDeadline))
}

func commandName(command string) string {
	name, _, _ := strings.Cut(command, " ")
	return name
}

func serverName(host string) string {
	name := strings.TrimSpace(host)
	if withoutPort, _, err := net.SplitHostPort(name); err == nil {
		return withoutPort
	}
	return strings.TrimSuffix(name, ":21")
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

var tagPattern = regexp.MustCompile(`\s*[\(\[].*?[\)\]]\s*`)
var tagValuePattern = regexp.MustCompile(`[\(\[]([^\)\]]+)[\)\]]`)
var repeatedSpace = regexp.MustCompile(`\s+`)
var windowsFTPLine = regexp.MustCompile(`^\d{2}-\d{2}-\d{2}\s+\d{2}:\d{2}[AP]M\s+(<DIR>|\d+)\s+(.+)$`)
var unixFTPLine = regexp.MustCompile(`^([dl-][rwxstST-]{9})\s+\S+\s+\S+\s+\S+\s+(\d+)\s+\S+\s+\S+\s+\S+\s+(.+)$`)
