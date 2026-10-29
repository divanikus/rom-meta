package sourceassets

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/fetchstate"
	"rommeta/internal/medianames"
	"rommeta/internal/platformnames"
	"rommeta/internal/regions"
	"rommeta/internal/screenscraper"
	"rommeta/internal/sourceurl"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"rommeta/internal/titlematch"
)

// Options controls source-media downloads from source media stanzas.
type Options struct {
	DataRoot         string
	Source           string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	Limit            int
	Concurrency      int
	PlanPath         string
	PlanFormat       string
	PolicyScope      string
	PolicyNote       string
	Progress         taskprogress.Func
}

// Result describes a dry-run or applied source-media download.
type Result struct {
	DataRoot             string
	Source               string
	PlatformCount        int
	GameBundleCount      int
	CandidateCount       int
	PlannedDownloadCount int
	ExistingFileCount    int
	DuplicateURLCount    int
	DownloadedFileCount  int
	SkippedByLimitCount  int
	FailedDownloadCount  int
	Failures             []Failure
	StopReason           string
	PlanPath             string
	PlanFormat           string
	PlanItemCount        int
	ManifestPath         string
	ManifestItemCount    int
	Apply                bool
	Limit                int
}

// Failure describes an attempted download, including a request that exhausted
// the provider's quota and stopped further requests.
type Failure struct {
	URL    string
	Target string
	Error  string
}

type catalogGame struct {
	Platform     string
	Folder       string
	Path         string
	ReleaseCount int
	MediaURLs    []catalogcache.MediaURL
}

type assetCandidate struct {
	Platform     string
	Game         string
	GamePath     string
	Release      string
	MediaType    string
	URL          string
	Region       string
	Language     string
	Quality      string
	SourceID     string
	SourceFile   string
	SourceType   string
	LegacyTarget string
	Target       string
	DownloadURL  string
}

type mediaURLRecord struct {
	Release    string
	MediaType  string
	URL        string
	Region     string
	Language   string
	Quality    string
	SourceID   string
	SourceFile string
	SourceType string
	Order      int
}

// Fetch downloads or plans source-specific media from a source's media stanzas.
func Fetch(options Options) (result Result, returnErr error) {
	source := strings.TrimSpace(options.Source)
	if source == "" {
		return Result{}, fmt.Errorf("source is required")
	}
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return Result{}, err
	}

	transfers := taskprogress.NewTransfers(options.Progress)
	progress := options.Progress
	if options.Apply {
		defer transfers.Done()
		progress = taskprogress.Func(transfers.Progress)
	}

	result = Result{
		DataRoot: dataRoot,
		Source:   source,
		Apply:    options.Apply,
		Limit:    options.Limit,
	}

	defer func() {
		if returnErr != nil {
			result.StopReason = returnErr.Error()
		}
	}()
	planWriter, err := openAssetPlanWriter(options.PlanPath, options.PlanFormat, dataRoot, source)
	if err != nil {
		return result, err
	}
	if planWriter != nil {
		defer planWriter.Abort()
	}
	manifestWriter, err := openAssetManifestWriter(dataRoot, source, options.Apply)
	if err != nil {
		return result, err
	}
	if manifestWriter != nil {
		defer manifestWriter.Abort()
	}

	urlSigner, err := sourceMediaURLSigner(dataRoot, source, options.Apply)
	if err != nil {
		return result, err
	}
	if manifestWriter != nil {
		manifestWriter.transfers = transfers
	}
	var downloader *candidateDownloader
	if options.Apply {
		downloader = startCandidateDownloader(source, options.Concurrency, progress, manifestWriter)
	}
	defer func() {
		if downloader != nil {
			result.DownloadedFileCount, result.FailedDownloadCount, result.Failures, err = downloader.CloseAndWait()
			returnErr = errors.Join(returnErr, err)
		}
		if manifestWriter != nil {
			result.ManifestPath, result.ManifestItemCount, err = manifestWriter.Close()
			returnErr = errors.Join(returnErr, err)
		}
		if returnErr == nil {
			returnErr = fetchstate.Incomplete(result.FailedDownloadCount)
		}
		if returnErr != nil {
			result.StopReason = returnErr.Error()
		}
	}()
	sourcePolicyNeeded := false
	scanStage := "planning downloads"
	if options.Apply {
		scanStage = "scanning"
	}
	scannedPlatforms := map[string]bool{}
	scannedGames := 0
	result.PlatformCount, result.GameBundleCount, err = streamCatalogGames(dataRoot, options.Platform, scanStage, progress, func(game catalogGame) error {
		scannedPlatforms[game.Platform] = true
		scannedGames++
		if err := downloader.Err(); err != nil {
			return err
		}
		gameCandidates := gameAssetCandidates(game, source)
		for i := range gameCandidates {
			gameCandidates[i].Platform = game.Platform
			gameCandidates[i].Game = game.Folder
		}
		sortAssetCandidates(gameCandidates)
		result.CandidateCount += len(gameCandidates)

		seenDownloads := map[string]string{}
		for _, candidate := range gameCandidates {
			candidate.URL = sourceurl.StripSecrets(candidate.URL)
			candidate.Target = existingCandidatePath(candidate)
			dedupeKey := assetDedupeKey(source, candidate)
			if _, err := catalogfs.Stat(candidate.Target); err == nil {
				result.ExistingFileCount++
				transfers.Record("existing")
				if dedupeKey != "" {
					seenDownloads[dedupeKey] = candidate.Target
				}
				if options.Apply {
					sourcePolicyNeeded = true
					if err := manifestWriter.Write(candidate, "existing", "", ""); err != nil {
						return err
					}
				}
				continue
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}

			if dedupeKey != "" {
				if duplicateOf, exists := seenDownloads[dedupeKey]; exists {
					result.DuplicateURLCount++
					if err := manifestWriter.Write(candidate, "duplicate_url", duplicateOf, ""); err != nil {
						return err
					}
					continue
				}
			}

			if options.Limit > 0 && result.PlannedDownloadCount >= options.Limit {
				result.SkippedByLimitCount++
				if err := manifestWriter.Write(candidate, "skipped_by_limit", "", ""); err != nil {
					return err
				}
				continue
			}
			result.PlannedDownloadCount++
			if dedupeKey != "" {
				seenDownloads[dedupeKey] = candidate.Target
			}
			if planWriter != nil {
				if err := planWriter.Write(candidate); err != nil {
					return err
				}
			}
			if downloader != nil {
				candidate.DownloadURL = urlSigner(candidate.URL)
				if err := downloader.Enqueue(candidate); err != nil {
					return err
				}
				transfers.Record("queued")
			}
		}
		return nil
	})
	if err != nil {
		result.PlatformCount = len(scannedPlatforms)
		result.GameBundleCount = scannedGames
	}
	if err != nil && !errors.Is(err, screenscraper.ErrDailyQuota) {
		return result, err
	}
	stopErr := err

	if downloader != nil {
		downloaded, failed, failures, err := downloader.CloseAndWait()
		if err != nil {
			return result, err
		}
		stopErr = downloader.Err()
		result.DownloadedFileCount = downloaded
		result.FailedDownloadCount = failed
		result.Failures = failures
		if downloaded > 0 {
			sourcePolicyNeeded = true
		}
	}

	if sourcePolicyNeeded {
		progress.Update("writing policy", 0, 0, source)
		scope := strings.TrimSpace(options.PolicyScope)
		if scope == "" {
			scope = "review"
		}
		note := strings.TrimSpace(options.PolicyNote)
		if note == "" {
			note = fmt.Sprintf("Imported from %s media URLs; verify rights before sharing.", source)
		}
		if _, err := store.EnsureSourcePolicy(dataRoot, source, scope, note); err != nil {
			return result, err
		}
	}
	if strings.TrimSpace(options.PlanPath) != "" {
		progress.Update("writing plan", 0, 0, filepath.Base(options.PlanPath))
		planPath, planFormat, planItemCount, err := planWriter.Close()
		if err != nil {
			return result, err
		}
		result.PlanPath = planPath
		result.PlanFormat = planFormat
		result.PlanItemCount = planItemCount
	}
	return result, stopErr
}

func assetDedupeKey(source string, candidate assetCandidate) string {
	source = strings.ToLower(strings.TrimSpace(source))
	mediaType := strings.ToLower(strings.TrimSpace(candidate.MediaType))
	rawURL := sourceurl.StripSecrets(candidate.URL)
	if source == "" || mediaType == "" || rawURL == "" {
		return ""
	}
	key := source + "\x00" + mediaType + "\x00" + rawURL
	if candidate.Release != "" {
		key += "\x00" + candidate.GamePath + "\x00" + candidate.Release
	}
	return key
}

func sortAssetCandidates(candidates []assetCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Target < candidates[j].Target
	})
}

type downloadResult struct {
	candidate assetCandidate
	err       error
}

type candidateDownloader struct {
	jobs        chan assetCandidate
	results     chan downloadResult
	workers     sync.WaitGroup
	collector   sync.WaitGroup
	manifest    *assetManifestWriter
	mu          sync.Mutex
	closed      bool
	downloaded  int
	failed      int
	failures    []Failure
	manifestErr error
	stopped     chan struct{}
	stopErr     error
}

func startCandidateDownloader(source string, concurrency int, progress taskprogress.Func, manifest *assetManifestWriter) *candidateDownloader {
	workerCount := normalizedConcurrency(concurrency, 0)
	downloader := &candidateDownloader{
		jobs:     make(chan assetCandidate),
		results:  make(chan downloadResult),
		manifest: manifest,
		stopped:  make(chan struct{}),
	}
	downloader.workers.Add(workerCount)
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		worker := workerIndex + 1
		go func() {
			defer downloader.workers.Done()
			progress.UpdateWorker("waiting", worker, workerCount, "waiting...")
			for candidate := range downloader.jobs {
				if downloader.Err() != nil {
					return
				}
				err := downloadAsset(firstNonEmpty(candidate.DownloadURL, candidate.URL), candidate.URL, candidate.Target, progress, downloadLabel(candidate), worker, workerCount)
				var response *downloadResponseError
				if strings.EqualFold(source, "screenscraper") && errors.As(err, &response) && response.statusCode == 430 {
					err = screenscraper.ErrDailyQuota
					downloader.mu.Lock()
					if downloader.stopErr == nil {
						downloader.stopErr = err
						close(downloader.stopped)
					}
					downloader.mu.Unlock()
				}
				downloader.results <- downloadResult{
					candidate: candidate,
					err:       err,
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

func (downloader *candidateDownloader) Err() error {
	if downloader == nil {
		return nil
	}
	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	return downloader.stopErr
}

func (downloader *candidateDownloader) Enqueue(candidate assetCandidate) error {
	if downloader == nil {
		return nil
	}
	if err := downloader.Err(); err != nil {
		return err
	}
	select {
	case <-downloader.stopped:
		return downloader.Err()
	case downloader.jobs <- candidate:
		return nil
	}
}

func (downloader *candidateDownloader) CloseAndWait() (downloaded, failed int, failures []Failure, err error) {
	if downloader == nil {
		return 0, 0, nil, nil
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
	return downloader.downloaded, downloader.failed, downloader.failures, downloader.manifestErr
}

func (downloader *candidateDownloader) record(result downloadResult) {
	if downloader.manifest != nil && downloader.manifest.transfers != nil {
		status := "downloaded"
		if result.err != nil {
			status = "failed"
		}
		downloader.manifest.transfers.Record(status)
	}

	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	if result.err != nil {
		downloader.failed++
		if len(downloader.failures) < 10 {
			downloader.failures = append(downloader.failures, Failure{
				URL:    result.candidate.URL,
				Target: result.candidate.Target,
				Error:  result.err.Error(),
			})
		}
		if downloader.manifestErr == nil {
			downloader.manifestErr = downloader.manifest.Write(result.candidate, "failed", "", result.err.Error())
		}
		return
	}
	downloader.downloaded++
	if downloader.manifestErr == nil {
		downloader.manifestErr = downloader.manifest.Write(result.candidate, "downloaded", "", "")
	}
}

func downloadCandidates(candidates []assetCandidate, concurrency int, progress taskprogress.Func) (downloaded, failed int, failures []Failure) {
	downloader := startCandidateDownloader("", concurrency, progress, nil)
	for _, candidate := range candidates {
		downloader.Enqueue(candidate)
	}
	downloaded, failed, failures, _ = downloader.CloseAndWait()
	return downloaded, failed, failures
}

type assetPlanWriter struct {
	path     string
	tmpPath  string
	format   string
	dataRoot string
	source   string
	file     *os.File
	writer   *bufio.Writer
	encoder  *json.Encoder
	count    int
	closed   bool
}

func openAssetPlanWriter(path, format, dataRoot, source string) (*assetPlanWriter, error) {
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
	plan := &assetPlanWriter{
		path:     absPath,
		tmpPath:  tmpPath,
		format:   planFormat,
		dataRoot: dataRoot,
		source:   source,
		file:     file,
		writer:   writer,
	}
	if planFormat == "jsonl" {
		plan.encoder = json.NewEncoder(writer)
	}
	return plan, nil
}

func (plan *assetPlanWriter) Write(item assetCandidate) error {
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
		row := assetCandidateRow(plan.dataRoot, plan.source, item)
		if err := plan.encoder.Encode(row); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported plan format: %s", plan.format)
	}
	plan.count++
	return nil
}

func addPlanField(row map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		row[key] = value
	}
}

func (plan *assetPlanWriter) Close() (path, format string, count int, err error) {
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

func (plan *assetPlanWriter) Abort() {
	if plan == nil || plan.closed {
		return
	}
	plan.closed = true
	_ = plan.file.Close()
	_ = catalogfs.Remove(plan.tmpPath)
}

type assetManifestWriter struct {
	transfers *taskprogress.Transfers
	path      string
	tmpPath   string
	dataRoot  string
	source    string
	file      *os.File
	writer    *bufio.Writer
	encoder   *json.Encoder
	mu        sync.Mutex
	count     int
	closed    bool
}

func openAssetManifestWriter(dataRoot, source string, apply bool) (*assetManifestWriter, error) {
	if !apply {
		return nil, nil
	}
	fileName := sourceSuffix(source)
	if fileName == "" {
		fileName = "source"
	}
	dir := filepath.Join(dataRoot, ".rommeta", "runs")
	if err := catalogfs.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, fileName+"-artwork-*.jsonl.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := file.Name()
	absPath := strings.TrimSuffix(tmpPath, ".tmp")
	writer := bufio.NewWriter(file)
	return &assetManifestWriter{
		path:     absPath,
		tmpPath:  tmpPath,
		dataRoot: dataRoot,
		source:   source,
		file:     file,
		writer:   writer,
		encoder:  json.NewEncoder(writer),
	}, nil
}

func (manifest *assetManifestWriter) Write(item assetCandidate, status, duplicateOf, errorText string) error {
	if manifest == nil {
		return nil
	}
	manifest.mu.Lock()
	defer manifest.mu.Unlock()
	if manifest.closed {
		return fmt.Errorf("asset manifest already closed: %s", manifest.path)
	}
	row := assetCandidateRow(manifest.dataRoot, manifest.source, item)
	addPlanField(row, "status", status)
	if duplicateOf != "" {
		addPlanField(row, "duplicate_of", relativePlanPath(manifest.dataRoot, duplicateOf))
	}
	addPlanField(row, "error", errorText)
	if err := manifest.encoder.Encode(row); err != nil {
		return err
	}
	manifest.count++
	return manifest.writer.Flush()
}

func (manifest *assetManifestWriter) Close() (path string, count int, err error) {
	if manifest == nil {
		return "", 0, nil
	}
	manifest.mu.Lock()
	defer manifest.mu.Unlock()
	if manifest.closed {
		return manifest.path, manifest.count, nil
	}
	manifest.closed = true
	if err := manifest.writer.Flush(); err != nil {
		_ = manifest.file.Close()
		return "", 0, err
	}
	if err := manifest.file.Close(); err != nil {
		return "", 0, err
	}
	if err := catalogfs.Rename(manifest.tmpPath, manifest.path); err != nil {
		return "", 0, err
	}
	return manifest.path, manifest.count, nil
}

func (manifest *assetManifestWriter) Abort() {
	if manifest == nil {
		return
	}
	manifest.mu.Lock()
	defer manifest.mu.Unlock()
	if manifest.closed {
		return
	}
	manifest.closed = true
	_ = manifest.file.Close()
	_ = catalogfs.Remove(manifest.tmpPath)
}

func assetCandidateRow(dataRoot, source string, item assetCandidate) map[string]string {
	row := map[string]string{
		"source":     source,
		"platform":   item.Platform,
		"game":       item.Game,
		"release":    item.Release,
		"media_type": item.MediaType,
		"url":        sourceurl.StripSecrets(item.URL),
		"target":     relativePlanPath(dataRoot, item.Target),
	}
	addPlanField(row, "region", item.Region)
	addPlanField(row, "language", item.Language)
	addPlanField(row, "quality", item.Quality)
	addPlanField(row, "source_id", item.SourceID)
	addPlanField(row, "source_file", item.SourceFile)
	addPlanField(row, "source_type", item.SourceType)
	return row
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

func streamCatalogGames(dataRoot, platform, readingStage string, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	// The cache reader counts directory entries for progress, then reads each
	// card once. Do not add a preliminary full read or collect cards in a slice.
	return catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.ReadOptions{
		Platforms:    platformFilters(platform),
		ReadingStage: readingStage,
	}, progress, func(cached catalogcache.Game) error {
		return fn(catalogGame{
			Platform:     cached.Platform,
			Folder:       cached.Folder,
			Path:         cached.Path,
			ReleaseCount: len(cached.Releases),
			MediaURLs:    cached.MediaURLs,
		})
	})
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func sourceMediaURLSigner(dataRoot, source string, apply bool) (func(string) string, error) {
	stripOnly := func(raw string) string { return sourceurl.StripSecrets(raw) }
	if !apply || !strings.EqualFold(strings.TrimSpace(source), "screenscraper") {
		return stripOnly, nil
	}
	config, err := store.LoadSources(dataRoot)
	if err != nil {
		return nil, err
	}
	var configured store.Source
	found := false
	for _, candidate := range config.Sources {
		if strings.EqualFold(candidate.Name, "screenscraper") {
			configured = candidate
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("source screenscraper not found in %s", config.Path)
	}
	values := map[string]string{}
	credentialMap := map[string]string{
		"ssid":        "username",
		"sspassword":  "password",
		"devid":       "dev_id",
		"devpassword": "dev_password",
	}
	var missing []string
	for queryKey, credentialName := range credentialMap {
		value, ok := store.CredentialValue(config, configured, credentialName)
		if !ok && config.Secrets.Exists && config.Secrets.Err == nil {
			value = config.Secrets.Values["screenscraper"][credentialName]
			ok = strings.TrimSpace(value) != ""
		}
		if !ok || strings.TrimSpace(value) == "" {
			missing = append(missing, "sources.screenscraper."+credentialName)
			continue
		}
		values[queryKey] = value
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("ScreenScraper media credentials are missing: fill %s in config/secrets.yaml", strings.Join(missing, ", "))
	}
	return func(raw string) string {
		return sourceurl.AddQuery(sourceurl.StripSecrets(raw), values)
	}, nil
}

func gameAssetCandidates(game catalogGame, source string) []assetCandidate {
	var records []mediaURLRecord
	for _, media := range game.MediaURLs {
		if media.Source != source {
			continue
		}
		record := mediaURLRecord{
			Release:    media.Release,
			MediaType:  media.MediaType,
			URL:        sourceurl.StripSecrets(media.URL),
			Region:     regions.NormalizeDisplay(media.Region),
			Language:   media.Language,
			Quality:    media.Quality,
			SourceID:   media.SourceID,
			SourceFile: media.SourceFile,
			SourceType: media.SourceType,
			Order:      media.Order,
		}
		if _, ok := assetFileName(source, record, 1, false); !ok {
			continue
		}
		records = append(records, record)
	}
	return assetCandidatesFromRecords(game.Path, source, records, game.ReleaseCount)
}

func readGameAssetCandidates(gamePath, source string) ([]assetCandidate, error) {
	gameYAML := filepath.Join(gamePath, "game.yaml")
	contents, err := catalogfs.ReadFile(gameYAML)
	if err != nil {
		return nil, err
	}

	var records []mediaURLRecord
	var currentReleases []string
	matchMediaReleaseSpecific := false
	currentNestedRelease := ""
	inSources := false
	inRequestedSource := false
	inMatches := false
	inReleaseList := false
	inMedia := false
	inReleaseMedia := false
	currentMediaType := ""
	currentMediaIndex := -1
	currentReleaseMediaType := ""
	currentReleaseMediaIndex := -1
	mediaOrder := 0
	resetMatchState := func() {
		inMedia = false
		inReleaseList = false
		inReleaseMedia = false
		currentReleases = nil
		matchMediaReleaseSpecific = false
		currentNestedRelease = ""
		currentMediaType = ""
		currentMediaIndex = -1
		currentReleaseMediaType = ""
		currentReleaseMediaIndex = -1
	}

	for _, raw := range strings.Split(string(contents), "\n") {
		line := trimComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := countIndent(line)
		value := strings.TrimSpace(line)

		if indent == 0 {
			inSources = value == "sources:"
			inRequestedSource = false
			inMatches = false
			resetMatchState()
			continue
		}
		if !inSources {
			continue
		}
		if indent == 2 {
			inRequestedSource = value == source+":"
			inMatches = false
			resetMatchState()
			continue
		}
		if !inRequestedSource {
			continue
		}
		if indent == 4 {
			inMatches = value == "matches:"
			resetMatchState()
			continue
		}
		if !inMatches {
			continue
		}
		if indent == 6 && strings.HasPrefix(value, "- release:") {
			currentReleases = []string{parseScalar(strings.TrimSpace(strings.TrimPrefix(value, "- release:")))}
			matchMediaReleaseSpecific = true
			inMedia = false
			inReleaseList = false
			inReleaseMedia = false
			currentNestedRelease = ""
			currentMediaType = ""
			currentMediaIndex = -1
			currentReleaseMediaType = ""
			currentReleaseMediaIndex = -1
			continue
		}
		if indent == 6 && strings.HasPrefix(value, "- releases:") {
			currentReleases = parseInlineList(strings.TrimSpace(strings.TrimPrefix(value, "- releases:")))
			matchMediaReleaseSpecific = false
			inMedia = false
			inReleaseList = false
			inReleaseMedia = false
			currentNestedRelease = ""
			currentMediaType = ""
			currentMediaIndex = -1
			currentReleaseMediaType = ""
			currentReleaseMediaIndex = -1
			continue
		}
		if indent == 8 {
			inReleaseList = value == "releases:"
			if inReleaseList {
				matchMediaReleaseSpecific = false
			}
			inMedia = value == "media:"
			inReleaseMedia = false
			currentNestedRelease = ""
			currentMediaType = ""
			currentMediaIndex = -1
			currentReleaseMediaType = ""
			currentReleaseMediaIndex = -1
			continue
		}
		if inReleaseList && indent == 10 && strings.HasPrefix(value, "- release:") {
			currentNestedRelease = parseScalar(strings.TrimSpace(strings.TrimPrefix(value, "- release:")))
			currentReleases = appendUniqueInlineString(currentReleases, currentNestedRelease)
			inReleaseMedia = false
			currentReleaseMediaType = ""
			currentReleaseMediaIndex = -1
			continue
		}
		if inReleaseList && currentNestedRelease != "" && indent == 12 {
			inReleaseMedia = value == "media:"
			currentReleaseMediaType = ""
			currentReleaseMediaIndex = -1
			continue
		}
		if inReleaseList && inReleaseMedia && currentNestedRelease != "" && indent == 14 {
			currentReleaseMediaType = ""
			currentReleaseMediaIndex = -1
			if strings.HasSuffix(value, ":") {
				currentReleaseMediaType = parseScalar(strings.TrimSpace(strings.TrimSuffix(value, ":")))
			}
			continue
		}
		if inReleaseList && inReleaseMedia && currentNestedRelease != "" && indent == 16 && currentReleaseMediaType != "" && strings.HasPrefix(value, "- url:") {
			mediaURL := sourceurl.StripSecrets(parseScalar(strings.TrimSpace(strings.TrimPrefix(value, "- url:"))))
			if !strings.HasPrefix(mediaURL, "http://") && !strings.HasPrefix(mediaURL, "https://") {
				continue
			}
			mediaOrder++
			record := mediaURLRecord{
				Release:   currentNestedRelease,
				MediaType: currentReleaseMediaType,
				URL:       mediaURL,
				Order:     mediaOrder,
			}
			if _, ok := assetFileName(source, record, 1, false); !ok {
				continue
			}
			currentReleaseMediaIndex = len(records)
			records = append(records, record)
			continue
		}
		if inReleaseList && inReleaseMedia && indent == 18 && currentReleaseMediaIndex >= 0 {
			key, rawValue, ok := splitAssetKV(value)
			if !ok {
				continue
			}
			for index := currentReleaseMediaIndex; index < len(records); index++ {
				setMediaRecordField(&records[index], key, parseScalar(rawValue))
			}
			continue
		}
		if !inMedia {
			continue
		}
		if indent == 10 {
			currentMediaType = ""
			currentMediaIndex = -1
			if strings.HasSuffix(value, ":") {
				currentMediaType = parseScalar(strings.TrimSpace(strings.TrimSuffix(value, ":")))
			}
			continue
		}
		if indent == 12 && currentMediaType != "" && strings.HasPrefix(value, "- url:") {
			mediaURL := sourceurl.StripSecrets(parseScalar(strings.TrimSpace(strings.TrimPrefix(value, "- url:"))))
			if !strings.HasPrefix(mediaURL, "http://") && !strings.HasPrefix(mediaURL, "https://") {
				continue
			}
			mediaOrder++
			record := mediaURLRecord{
				MediaType: currentMediaType,
				URL:       mediaURL,
				Order:     mediaOrder,
			}
			if _, ok := assetFileName(source, record, 1, false); !ok {
				continue
			}
			currentMediaIndex = len(records)
			releases := []string{""}
			editionSpecific := false
			for _, release := range currentReleases {
				if !titlematch.CompatibleSymbols(filepath.Base(gamePath), release) {
					editionSpecific = true
				}
			}
			if (matchMediaReleaseSpecific || editionSpecific) && len(currentReleases) > 0 {
				releases = currentReleases
			}
			if len(releases) == 0 {
				releases = []string{""}
			}
			for _, release := range releases {
				record.Release = release
				records = append(records, record)
			}
			continue
		}
		if indent != 14 || currentMediaIndex < 0 {
			continue
		}
		key, rawValue, ok := splitAssetKV(value)
		if !ok {
			continue
		}
		for index := currentMediaIndex; index < len(records); index++ {
			setMediaRecordField(&records[index], key, parseScalar(rawValue))
		}
	}
	candidates := assetCandidatesFromRecords(gamePath, source, records, countTopLevelReleases(contents))
	return candidates, nil
}

func assetCandidatesFromRecords(gamePath, source string, records []mediaURLRecord, releaseCount int) []assetCandidate {
	recordsByType := map[string][]mediaURLRecord{}
	for _, record := range records {
		record = mediaRecordWithReleaseDefaults(record, releaseCount)
		recordsByType[record.MediaType] = append(recordsByType[record.MediaType], record)
	}

	var candidates []assetCandidate
	for mediaType, records := range recordsByType {
		sort.SliceStable(records, func(i, j int) bool {
			if records[i].Order != records[j].Order {
				return records[i].Order < records[j].Order
			}
			if records[i].Release != records[j].Release {
				return records[i].Release < records[j].Release
			}
			return records[i].URL < records[j].URL
		})
		records = collapseDuplicateURLRecords(gamePath, records)
		releaseSpecific := mediaRecordsNeedReleaseFolders(gamePath, records, releaseCount)

		type targetRecord struct {
			record    mediaURLRecord
			targetDir string
			release   string
		}
		targetRecords := make([]targetRecord, 0, len(records))
		countsByDir := map[string]int{}
		for _, record := range records {
			targetDir := filepath.Join(gamePath, "media")
			release := ""
			if releaseSpecific && releaseNeedsMediaFolder(gamePath, record.Release) {
				targetDir = filepath.Join(targetDir, record.Release)
				release = record.Release
			}
			targetRecords = append(targetRecords, targetRecord{record: record, targetDir: targetDir, release: release})
			countsByDir[targetDir]++
		}

		ordinalsByDir := map[string]int{}
		for _, targetRecord := range targetRecords {
			ordinalsByDir[targetRecord.targetDir]++
			record := targetRecord.record
			fileName, ok := assetFileName(source, record, ordinalsByDir[targetRecord.targetDir], countsByDir[targetRecord.targetDir] > 1)
			if !ok {
				continue
			}
			legacyRecord := record
			legacyRecord.SourceType = ""
			legacyName, _ := assetFileName(source, legacyRecord, ordinalsByDir[targetRecord.targetDir], countsByDir[targetRecord.targetDir] > 1)
			candidates = append(candidates, assetCandidate{
				GamePath:     gamePath,
				Release:      targetRecord.release,
				MediaType:    mediaType,
				URL:          record.URL,
				Region:       record.Region,
				Language:     record.Language,
				Quality:      record.Quality,
				SourceID:     record.SourceID,
				SourceFile:   record.SourceFile,
				SourceType:   record.SourceType,
				LegacyTarget: filepath.Join(targetRecord.targetDir, legacyName),
				Target:       filepath.Join(targetRecord.targetDir, fileName),
			})
		}
	}
	return candidates
}

func mediaRecordWithReleaseDefaults(record mediaURLRecord, releaseCount int) mediaURLRecord {
	if releaseCount > 1 && record.Region == "" {
		record.Region = releaseRegionLabel(record.Release)
	}
	return record
}

func mediaRecordsNeedReleaseFolders(gamePath string, records []mediaURLRecord, releaseCount int) bool {
	if releaseCount <= 1 {
		return false
	}
	for _, record := range records {
		if releaseNeedsMediaFolder(gamePath, record.Release) {
			return true
		}
	}
	return false
}

func countTopLevelReleases(contents []byte) int {
	inReleases := false
	count := 0
	for _, raw := range strings.Split(string(contents), "\n") {
		line := trimComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := countIndent(line)
		value := strings.TrimSpace(line)
		if indent == 0 {
			inReleases = value == "releases:"
			continue
		}
		if !inReleases {
			continue
		}
		if indent == 2 && strings.HasSuffix(value, ":") {
			count++
		}
	}
	return count
}

func collapseDuplicateURLRecords(gamePath string, records []mediaURLRecord) []mediaURLRecord {
	indexByURL := map[string]int{}
	collapsed := make([]mediaURLRecord, 0, len(records))
	for _, record := range records {
		key := sourceurl.StripSecrets(record.URL)
		if key == "" {
			continue
		}
		record.URL = key
		if releaseNeedsMediaFolder(gamePath, record.Release) {
			key += "\x00" + record.Release
		}
		if index, ok := indexByURL[key]; ok {
			mergeMediaRecord(&collapsed[index], record)
			continue
		}
		indexByURL[key] = len(collapsed)
		collapsed = append(collapsed, record)
	}
	return collapsed
}

func mergeMediaRecord(record *mediaURLRecord, next mediaURLRecord) {
	if record.Release != next.Release {
		record.Release = ""
	}
	if record.Region != next.Region {
		record.Region = ""
	}
	if record.Language != next.Language {
		record.Language = ""
	}
	if record.Quality != next.Quality {
		record.Quality = ""
	}
	if record.SourceID != next.SourceID {
		record.SourceID = ""
	}
	if record.SourceFile != next.SourceFile {
		record.SourceFile = ""
	}
	if record.SourceType != next.SourceType {
		record.SourceType = ""
	}
}

func setMediaRecordField(record *mediaURLRecord, key, value string) {
	switch strings.TrimSpace(key) {
	case "region":
		record.Region = regions.NormalizeDisplay(value)
	case "language":
		record.Language = value
	case "quality":
		record.Quality = value
	case "source_id":
		record.SourceID = value
	case "source_file":
		record.SourceFile = value
	case "source_type":
		record.SourceType = value
	}
}

func splitAssetKV(value string) (string, string, bool) {
	key, raw, ok := strings.Cut(value, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(raw), true
}

func assetFileName(source string, record mediaURLRecord, ordinal int, numbered bool) (string, bool) {
	stem := mediaKeyToFileStem(record.MediaType)
	suffix := sourceSuffix(source)
	if stem == "" || suffix == "" {
		return "", false
	}
	if numbered {
		stem = fmt.Sprintf("%s-%02d", stem, ordinal)
	}
	parts := []string{stem}
	for _, qualifier := range []string{record.Region, record.Language, record.Quality} {
		if token := filenameToken(qualifier); token != "" {
			parts = append(parts, token)
		}
	}
	if variant := medianames.SourceVariant(record.SourceType); variant != "" {
		parts = append(parts, "variant-"+variant)
	}
	parts = append(parts, suffix)
	extension := ".jpg"
	if parsed, err := url.Parse(record.URL); err == nil {
		if ext := strings.ToLower(filepath.Ext(parsed.Path)); isMediaExtension(ext) {
			extension = ext
		}
	}
	return strings.Join(parts, ".") + extension, true
}

func mediaKeyToFileStem(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if match := numberedMediaKey.FindStringSubmatch(value); len(match) == 3 {
		return strings.ReplaceAll(match[1], "_", "-") + "-" + match[2]
	}
	return strings.ReplaceAll(value, "_", "-")
}

func sourceSuffix(value string) string {
	return filenameToken(value)
}

func filenameToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(repeatedDash.ReplaceAllString(b.String(), "-"), "-")
}

func releaseNeedsMediaFolder(gamePath, release string) bool {
	release = strings.TrimSpace(release)
	if release == "" {
		return false
	}
	gameName := filepath.Base(filepath.Clean(gamePath))
	if releaseHasMediaFolderFlag(release) && !releaseHasMediaFolderFlag(gameName) {
		return true
	}
	releaseBase := stripReleaseFields(release)
	if releaseBase == "" {
		return false
	}
	for _, candidate := range []string{gameName, stripReleaseFields(gameName)} {
		if sameTitle(candidate, releaseBase) {
			return false
		}
	}
	return true
}

func releaseHasMediaFolderFlag(value string) bool {
	for _, match := range releaseTagValuePattern.FindAllStringSubmatch(value, -1) {
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

func releaseRegionLabel(release string) string {
	var values []string
	for _, match := range releaseTagValuePattern.FindAllStringSubmatch(release, -1) {
		if len(match) < 2 {
			continue
		}
		for _, part := range splitQualifier(match[1]) {
			if normalized, ok := regions.NormalizeDATDisplay(part); ok {
				values = appendUniqueInlineString(values, normalized)
			}
		}
	}
	return strings.Join(values, ", ")
}

func stripReleaseFields(value string) string {
	value = strings.TrimSpace(value)
	for {
		trimmed := stripTrailingField(value, "(", ")")
		if trimmed != value {
			value = trimmed
			continue
		}
		trimmed = stripTrailingField(value, "[", "]")
		if trimmed == value {
			return value
		}
		value = trimmed
	}
}

func stripTrailingField(value, open, close string) string {
	value = strings.TrimSpace(value)
	start := strings.LastIndex(value, open)
	end := strings.LastIndex(value, close)
	if start < 0 || end != len(value)-len(close) || start > end {
		return value
	}
	return strings.TrimSpace(value[:start])
}

func sameTitle(left, right string) bool {
	return titlematch.CompatibleSymbols(left, right) && compactTitleKey(left) != "" && compactTitleKey(left) == compactTitleKey(right)
}

func compactTitleKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '&':
			b.WriteString("and")
		}
	}
	return b.String()
}

func splitQualifier(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '+'
	})
}

func isMediaExtension(value string) bool {
	switch value {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".pdf", ".mp4":
		return true
	default:
		return false
	}
}

func downloadLabel(candidate assetCandidate) string {
	parts := make([]string, 0, 3)
	if candidate.Game != "" {
		parts = append(parts, candidate.Game)
	}
	if candidate.Release != "" {
		parts = append(parts, candidate.Release)
	}
	parts = append(parts, filepath.Base(candidate.Target))
	return strings.Join(parts, " / ")
}

func downloadAsset(requestURL, publicURL, targetPath string, progress taskprogress.Func, label string, worker, workers int) error {
	client := http.Client{Timeout: 60 * time.Second}
	publicURL = sourceurl.StripSecrets(firstNonEmpty(publicURL, requestURL))
	progress.UpdateWorker("preparing", worker, workers, label)
	response, err := client.Get(requestURL)
	if err != nil {
		// net/http includes the signed request URL in transport errors.
		var requestError *url.Error
		if errors.As(err, &requestError) {
			sanitized := *requestError
			sanitized.URL = sourceurl.StripSecrets(requestError.URL)
			err = &sanitized
		}
		return fmt.Errorf("download %s failed: %w", publicURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &downloadResponseError{publicURL: publicURL, status: response.Status, statusCode: response.StatusCode}
	}

	if err := catalogfs.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	tmp := targetPath + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := response.ContentLength
	progress.UpdateWorkerBytes("downloading", worker, workers, 0, total, label)
	if total <= 0 {
		defer progress.FinishWorkerBytes("downloading", worker, workers, label)
	}
	body := taskprogress.NewWorkerReader(response.Body, progress, "downloading", worker, workers, total, label)
	written, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = catalogfs.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = catalogfs.Remove(tmp)
		return closeErr
	}
	if err := validateDownloadedAsset(tmp, written); err != nil {
		_ = catalogfs.Remove(tmp)
		return fmt.Errorf("download %s failed: %w", publicURL, err)
	}
	if total > 0 {
		progress.UpdateWorkerBytes("downloading", worker, workers, total, total, label)
	}
	return catalogfs.Rename(tmp, targetPath)
}

// An HTTP 200 response can still represent an unavailable provider asset.
func validateDownloadedAsset(path string, size int64) error {
	if size == 0 {
		return fmt.Errorf("empty media response")
	}
	if size > 64 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(string(data)), "NOMEDIA") {
		return fmt.Errorf("provider returned NOMEDIA instead of media")
	}
	return nil
}

type downloadResponseError struct {
	publicURL  string
	status     string
	statusCode int
}

func (e *downloadResponseError) Error() string {
	return fmt.Sprintf("download %s returned %s", e.publicURL, e.status)
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
			result = appendUniqueInlineString(result, parsed)
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

func appendUniqueInlineString(values []string, next string) []string {
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

var numberedMediaKey = regexp.MustCompile(`^([a-z0-9_]+)_([0-9]{2})$`)
var releaseTagValuePattern = regexp.MustCompile(`[\(\[]([^\)\]]+)[\)\]]`)
var repeatedDash = regexp.MustCompile(`-+`)
