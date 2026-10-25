package emumovies

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"rommeta/internal/catalogfs"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

// ArtworkOptions controls EmuMovies artwork package planning and fetching.
type ArtworkOptions struct {
	DataRoot         string
	MediaTypes       []string
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

// ArtworkResult describes a dry-run or applied EmuMovies artwork package fetch.
type ArtworkResult struct {
	SkippedPlatforms           map[string]string
	DataRoot                   string
	FTPHost                    string
	FTPDirectory               string
	ArchiveExtractor           string
	MediaTypes                 []string
	PlatformCount              int
	GameBundleCount            int
	DirectoryEntryCount        int
	PackageCandidateCount      int
	NeedsExtractorPackageCount int
	NeedsExtractorRARCount     int
	NeedsExtractor7zCount      int
	SelectedPackageCount       int
	ExistingPackageCount       int
	PlannedPackageCount        int
	DownloadedPackageCount     int
	SkippedByLimitCount        int
	FailedDownloadCount        int
	Failures                   []Failure
	ExtractedPackageCount      int
	ExtractedFileCount         int
	IndexedArtworkFileCount    int
	MatchedArtworkFileCount    int
	ExistingArtworkFileCount   int
	WrittenArtworkFileCount    int
	InvalidArtworkFileCount    int
	InvalidArtworkFiles        []Failure
	UnmatchedArtworkFileCount  int
	UnmatchedManifestPath      string
	PlanPath                   string
	PlanFormat                 string
	PlanItemCount              int
	Apply                      bool
	Limit                      int
}

type artworkPackage struct {
	Platform    string
	Directory   string
	SourcePath  string
	Name        string
	URL         string
	Target      string
	MediaType   string
	Priority    int
	Size        int64
	ArchiveType string
	Extractable bool
	Parts       []artworkPackagePart
}

type artworkPackagePart struct {
	Name       string
	SourcePath string
	URL        string
	Target     string
	Size       int64
}

type artworkFileCandidate struct {
	Platform      string
	Game          string
	GamePath      string
	Release       string
	MediaType     string
	SourcePackage string
	SourcePath    string
	FileName      string
	Target        string
	Size          int64
	MatchScore    int
	MatchedKeys   []string
	ReleaseCount  int
}

const artworkParentDirectory = "Official/Artwork"

// FetchArtwork plans or downloads EmuMovies artwork package archives and
// materializes matched artwork files into the catalog.
func FetchArtwork(options ArtworkOptions) (ArtworkResult, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return ArtworkResult{}, err
	}

	config, err := store.LoadSources(dataRoot)
	if err != nil {
		return ArtworkResult{}, err
	}
	settings, err := emuMoviesSettings(config)
	if err != nil {
		return ArtworkResult{}, err
	}
	if err := ensureCredentials(settings); err != nil {
		return ArtworkResult{}, err
	}
	extractor, err := resolveArchiveExtractor(settings.ArchiveExtractor)
	if err != nil {
		return ArtworkResult{}, err
	}

	progress := options.Progress
	gameSet, err := readCatalogGameSet(dataRoot, options.Platform, progress)
	if err != nil {
		return ArtworkResult{}, err
	}
	result := ArtworkResult{
		DataRoot:         dataRoot,
		FTPHost:          settings.FTPHost,
		ArchiveExtractor: extractor.Display(),
		PlatformCount:    gameSet.PlatformCount,
		GameBundleCount:  gameSet.GameCount,
		Apply:            options.Apply,
		Limit:            options.Limit,
	}

	client := options.FTPClient
	if client == nil {
		client = NewFTPClient(settings.FTPHost, settings.Username, settings.Password, settings.ExplicitTLS)
	}

	result.SkippedPlatforms = map[string]string{}
	packages, firstDirectory, entryCount, candidateCount, needsExtractorCount, needsExtractorByType, err := artworkPackagesWithRegistry(client, settings, extractor, dataRoot, gameSet.ByPlatform, options.PlatformRegistry, result.SkippedPlatforms, progress)
	if err != nil {
		return ArtworkResult{}, err
	}
	result.FTPDirectory = firstDirectory
	result.DirectoryEntryCount = entryCount
	result.PackageCandidateCount = candidateCount
	result.NeedsExtractorPackageCount = needsExtractorCount
	result.NeedsExtractorRARCount = needsExtractorByType["rar"]
	result.NeedsExtractor7zCount = needsExtractorByType["7z"]
	mediaTypeFilter := normalizeArtworkMediaTypeFilters(options.MediaTypes)
	packages = filterArtworkPackages(packages, mediaTypeFilter)
	result.MediaTypes = mediaTypeFilterList(mediaTypeFilter)
	result.SelectedPackageCount = len(packages)

	var planItems []artworkPackage
	sourcePolicyNeeded := false
	stage := "planning packages"
	if options.Apply {
		stage = "checking packages"
	}
	for index, pkg := range packages {
		progress.Update(stage, index+1, len(packages), pkg.Name)
		missingParts, err := missingPackageParts(pkg)
		if err != nil {
			return ArtworkResult{}, err
		}
		if len(missingParts) == 0 {
			result.ExistingPackageCount++
			if options.Apply {
				sourcePolicyNeeded = true
			}
			continue
		}

		if options.Limit > 0 && result.PlannedPackageCount >= options.Limit {
			result.SkippedByLimitCount++
			continue
		}
		result.PlannedPackageCount++
		planItems = append(planItems, packageWithParts(pkg, missingParts))
	}

	if options.Apply && len(planItems) > 0 {
		downloaded, failed, failures := downloadArtworkPackages(client, planItems, options.Concurrency, progress)
		result.DownloadedPackageCount = downloaded
		result.FailedDownloadCount = failed
		result.Failures = failures
		if downloaded > 0 {
			sourcePolicyNeeded = true
		}
	}

	if strings.TrimSpace(options.PlanPath) != "" {
		progress.Update("writing plan", 0, 0, filepath.Base(options.PlanPath))
		planPath, err := writeArtworkPlan(options.PlanPath, options.PlanFormat, dataRoot, planItems)
		if err != nil {
			return ArtworkResult{}, err
		}
		result.PlanPath = planPath
		result.PlanFormat = normalizedPlanFormat(options.PlanPath, options.PlanFormat)
		result.PlanItemCount = artworkPlanItemCount(planItems)
	}

	if options.Apply {
		extractedPackages, extractedFiles, err := extractArtworkPackages(dataRoot, packages, extractor, progress)
		if err != nil {
			return ArtworkResult{}, err
		}
		result.ExtractedPackageCount = extractedPackages
		result.ExtractedFileCount = extractedFiles

		fileCandidates, indexedFiles, unmatchedFiles, unmatchedManifestPath, err := indexArtworkFiles(dataRoot, packages, gameSet.ByPlatform, progress)
		if err != nil {
			return ArtworkResult{}, err
		}
		result.IndexedArtworkFileCount = indexedFiles
		result.UnmatchedManifestPath = unmatchedManifestPath
		result.UnmatchedArtworkFileCount = unmatchedFiles
		result.MatchedArtworkFileCount = len(fileCandidates)
		assignArtworkTargets(fileCandidates)

		materialized, err := materializeArtworkFiles(fileCandidates, progress)
		if err != nil {
			return ArtworkResult{}, err
		}
		result.ExistingArtworkFileCount = materialized.existing
		result.WrittenArtworkFileCount = materialized.written
		result.InvalidArtworkFileCount = materialized.invalid
		result.InvalidArtworkFiles = materialized.failures
		if materialized.written > 0 || materialized.existing > 0 {
			sourcePolicyNeeded = true
		}
	}

	if sourcePolicyNeeded {
		progress.Update("writing policy", 0, 0, sourceName)
		if _, err := store.EnsureSourcePolicy(dataRoot, sourceName, defaultPolicyScope, defaultPolicyNote); err != nil {
			return ArtworkResult{}, err
		}
	}
	return result, nil
}

type artworkDownloadResult struct {
	pkg      artworkPackage
	failures []Failure
}

func downloadArtworkPackages(client FTPClient, packages []artworkPackage, concurrency int, progress taskprogress.Func) (downloaded, failed int, failures []Failure) {
	workerCount := normalizedConcurrency(concurrency, len(packages))
	jobs := make(chan artworkPackage)
	results := make(chan artworkDownloadResult)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		worker := workerIndex + 1
		go func() {
			defer workers.Done()
			progress.UpdateWorker("waiting", worker, workerCount, "waiting...")
			for pkg := range jobs {
				var packageFailures []Failure
				for _, part := range pkg.Parts {
					if err := downloadWithProgress(client, part.SourcePath, part.Target, part.Size, progress, part.Name, worker, workerCount); err != nil {
						packageFailures = append(packageFailures, Failure{
							SourcePath: part.SourcePath,
							Target:     part.Target,
							Error:      err.Error(),
						})
					}
				}
				results <- artworkDownloadResult{pkg: pkg, failures: packageFailures}
				progress.UpdateWorker("waiting", worker, workerCount, "waiting...")
			}
		}()
	}
	go func() {
		for _, pkg := range packages {
			jobs <- pkg
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()
	for result := range results {
		if len(result.failures) > 0 {
			failed += len(result.failures)
			for _, failure := range result.failures {
				if len(failures) < 10 {
					failures = append(failures, failure)
				}
			}
			continue
		}
		downloaded++
	}
	return downloaded, failed, failures
}

func artworkPackages(client FTPClient, settings sourceSettings, extractor archiveExtractor, dataRoot string, games []catalogGame, progressCallbacks ...taskprogress.Func) ([]artworkPackage, string, int, int, int, map[string]int, error) {
	return artworkPackagesWithRegistry(client, settings, extractor, dataRoot, catalogGamesByPlatform(games), nil, nil, progressCallbacks...)
}

func artworkPackagesWithRegistry(client FTPClient, settings sourceSettings, extractor archiveExtractor, dataRoot string, platformGames map[string][]catalogGame, registry *platformnames.Registry, skipped map[string]string, progressCallbacks ...taskprogress.Func) ([]artworkPackage, string, int, int, int, map[string]int, error) {
	progress := firstProgress(progressCallbacks)

	var all []artworkPackage
	totalEntries := 0
	totalCandidates := 0
	totalNeedsExtractor := 0
	needsExtractorByType := map[string]int{}
	firstDirectory := ""
	platforms := sortedCatalogPlatforms(platformGames)

	for index, platform := range platforms {
		progress.Update("listing artwork", index+1, len(platforms), platform)
		directory, entries, err := findArtworkDirectory(client, platform, registry)
		if err != nil {
			var missing missingPlatformDirectoryError
			if errors.As(err, &missing) {
				if skipped != nil {
					skipped[platform] = err.Error()
				}
				continue
			}
			return nil, "", 0, 0, 0, nil, err
		}
		if firstDirectory == "" {
			firstDirectory = directory
		}
		totalEntries += len(entries)

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})
		platformPackagesByKey := map[string]*artworkPackage{}
		for _, entry := range entries {
			if entry.IsDir || !isArchiveFile(entry.Name) {
				continue
			}
			mediaType, priority, ok := classifyArtworkPackage(entry.Name)
			if !ok {
				continue
			}
			totalCandidates++
			archiveType := archiveType(entry.Name)
			extractable := archiveType == "zip" || extractor.CanExtract(archiveType)
			if !extractable {
				totalNeedsExtractor++
				needsExtractorByType[archiveType]++
				continue
			}
			sourcePath := pathpkg.Join(directory, entry.Name)
			part := artworkPackagePart{
				Name:       entry.Name,
				SourcePath: sourcePath,
				URL:        ftpURL(settings.URLScheme, settings.FTPHost, sourcePath),
				Target:     filepath.Join(dataRoot, ".rommeta", "sources", sourceName, "packages", platform, entry.Name),
				Size:       entry.Size,
			}
			key := mediaType + "\x00" + archiveFamilyName(entry.Name)
			pkg, ok := platformPackagesByKey[key]
			if !ok {
				pkg = &artworkPackage{
					Platform:    platform,
					Directory:   directory,
					MediaType:   mediaType,
					Priority:    priority,
					ArchiveType: archiveType,
					Extractable: extractable,
				}
				platformPackagesByKey[key] = pkg
			}
			pkg.Parts = append(pkg.Parts, part)
			pkg.Size += entry.Size
		}
		var platformPackages []artworkPackage
		for _, pkg := range platformPackagesByKey {
			finalizeArtworkPackage(pkg)
			platformPackages = append(platformPackages, *pkg)
		}
		all = append(all, selectBestArtworkPackages(platformPackages)...)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Priority == all[j].Priority {
			if all[i].Platform == all[j].Platform {
				return all[i].Name < all[j].Name
			}
			return all[i].Platform < all[j].Platform
		}
		return all[i].Priority < all[j].Priority
	})
	return all, firstDirectory, totalEntries, totalCandidates, totalNeedsExtractor, needsExtractorByType, nil
}

func normalizeArtworkMediaTypeFilters(values []string) map[string]bool {
	filters := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized := normalizeArtworkMediaType(part)
			if normalized != "" {
				filters[normalized] = true
			}
		}
	}
	return filters
}

func normalizeArtworkMediaType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

func filterArtworkPackages(packages []artworkPackage, filters map[string]bool) []artworkPackage {
	if len(filters) == 0 {
		return packages
	}
	filtered := make([]artworkPackage, 0, len(packages))
	for _, pkg := range packages {
		if filters[pkg.MediaType] {
			filtered = append(filtered, pkg)
		}
	}
	return filtered
}

func mediaTypeFilterList(filters map[string]bool) []string {
	if len(filters) == 0 {
		return nil
	}
	values := make([]string, 0, len(filters))
	for value := range filters {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func finalizeArtworkPackage(pkg *artworkPackage) {
	sort.Slice(pkg.Parts, func(i, j int) bool {
		left := archivePartIndex(pkg.Parts[i].Name)
		right := archivePartIndex(pkg.Parts[j].Name)
		if left == right {
			return pkg.Parts[i].Name < pkg.Parts[j].Name
		}
		return left < right
	})
	if len(pkg.Parts) == 0 {
		return
	}
	first := pkg.Parts[0]
	pkg.SourcePath = first.SourcePath
	pkg.Name = first.Name
	pkg.URL = first.URL
	pkg.Target = first.Target
}

func archiveType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip":
		return "zip"
	case ".rar":
		return "rar"
	case ".7z":
		return "7z"
	default:
		return ""
	}
}

func archiveFamilyName(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.EqualFold(filepath.Ext(name), ".rar") {
		lower := strings.ToLower(stem)
		if index := strings.LastIndex(lower, ".part"); index >= 0 && allDigits(lower[index+len(".part"):]) {
			stem = stem[:index]
		}
	}
	return stem
}

func archivePartIndex(name string) int {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.EqualFold(filepath.Ext(name), ".rar") {
		lower := strings.ToLower(stem)
		if index := strings.LastIndex(lower, ".part"); index >= 0 {
			part := lower[index+len(".part"):]
			if allDigits(part) {
				value := 0
				for _, r := range part {
					value = value*10 + int(r-'0')
				}
				if value > 0 {
					return value
				}
			}
		}
	}
	return 0
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func findArtworkDirectory(client FTPClient, platform string, registry *platformnames.Registry) (string, []FTPEntry, error) {
	candidates := emuMoviesPlatformCandidates(platform, registry)
	var directErrors []string
	for _, candidate := range candidates {
		if strings.Contains(candidate, "/") {
			continue
		}
		direct := pathpkg.Join(artworkParentDirectory, candidate)
		entries, err := client.List(direct)
		if err == nil {
			return direct, entries, nil
		}
		if len(directErrors) < 3 {
			directErrors = append(directErrors, fmt.Sprintf("%s: %v", candidate, err))
		}
	}

	parentEntries, parentErr := client.List(artworkParentDirectory)
	if parentErr != nil {
		details := ""
		if len(directErrors) > 0 {
			details = " (direct attempts: " + strings.Join(directErrors, "; ") + ")"
		}
		return "", nil, fmt.Errorf("EmuMovies artwork directory not found for platform %q%s; parent list failed: %w", platform, details, parentErr)
	}
	var matches []string
	for _, candidate := range candidates {
		for _, entry := range parentEntries {
			if !entry.IsDir {
				continue
			}
			if platformDirectoryNameEqual(entry.Name, candidate) {
				matches = appendUnique(matches, pathpkg.Join(artworkParentDirectory, entry.Name))
			}
		}
		if len(matches) > 0 {
			break
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", nil, missingPlatformDirectoryError(fmt.Sprintf("EmuMovies artwork directory not found for platform %q under %s (tried: %s)", platform, artworkParentDirectory, strings.Join(candidates, ", ")))
	}
	entries, err := client.List(matches[0])
	if err != nil {
		return "", nil, err
	}
	return matches[0], entries, nil
}

func classifyArtworkPackage(name string) (string, int, bool) {
	key := looseTitleKey(strings.TrimSuffix(name, filepath.Ext(name)))
	switch {
	case strings.Contains(key, "logos"):
		return "logo", 10, true
	case strings.Contains(key, "2d boxes back"):
		return "box-back", 20, true
	case strings.Contains(key, "2d boxes") && !strings.Contains(key, "spine") && !strings.Contains(key, "full"):
		return "box-front", 30, true
	case strings.Contains(key, "3d boxes"):
		return "box-3d", 40, true
	case strings.Contains(key, "2d carts") && !strings.Contains(key, "labels"):
		return "cart-front", 50, true
	case strings.Contains(key, "3d carts") && !strings.Contains(key, "vector"):
		return "cart-3d", 60, true
	case strings.Contains(key, "backgrounds"):
		return "fanart", 70, true
	case strings.Contains(key, "banners") || strings.Contains(key, "marquees"):
		return "banner", 80, true
	case strings.Contains(key, "gameplay snaps"):
		return "screenshot", 90, true
	case strings.Contains(key, "title snaps"):
		return "title-screen", 100, true
	default:
		return "", 0, false
	}
}

func selectBestArtworkPackages(packages []artworkPackage) []artworkPackage {
	bestByMedia := map[string]artworkPackage{}
	for _, pkg := range packages {
		current, ok := bestByMedia[pkg.MediaType]
		if !ok || betterArtworkPackage(pkg, current) {
			bestByMedia[pkg.MediaType] = pkg
		}
	}

	selected := make([]artworkPackage, 0, len(bestByMedia))
	for _, pkg := range bestByMedia {
		selected = append(selected, pkg)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Priority == selected[j].Priority {
			return selected[i].Name < selected[j].Name
		}
		return selected[i].Priority < selected[j].Priority
	})
	return selected
}

func betterArtworkPackage(candidate, current artworkPackage) bool {
	candidateRank := artworkPackageRank(candidate)
	currentRank := artworkPackageRank(current)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	return candidate.Name < current.Name
}

func artworkPackageRank(pkg artworkPackage) int {
	key := looseTitleKey(pkg.Name)
	score := 0
	if strings.Contains(key, "1g1r") {
		score -= 500
	}
	if strings.Contains(key, "no intro") || strings.Contains(key, "redump") || strings.Contains(key, "mame") {
		score += 80
	}
	if strings.Contains(key, "em 4") || strings.Contains(key, "em 5") {
		score += 20
	}
	if pkg.ArchiveType == "zip" {
		score += 10
	}
	if strings.Contains(key, "vector") || strings.Contains(key, "spine") || strings.Contains(key, "full") {
		score -= 20
	}
	return score
}

func isArchiveFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".rar", ".7z":
		return true
	default:
		return false
	}
}

func writeArtworkPlan(path, format, dataRoot string, items []artworkPackage) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := catalogfs.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", err
	}

	switch normalizedPlanFormat(path, format) {
	case "aria2":
		return absPath, catalogfs.WriteFile(absPath, []byte(renderArtworkAria2Plan(dataRoot, items)), 0o644)
	case "jsonl":
		return absPath, catalogfs.WriteFile(absPath, []byte(renderArtworkJSONLPlan(dataRoot, items)), 0o644)
	default:
		return "", fmt.Errorf("unsupported plan format: %s", format)
	}
}

func renderArtworkJSONLPlan(dataRoot string, items []artworkPackage) string {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	for _, item := range items {
		for _, part := range item.Parts {
			_ = encoder.Encode(map[string]any{
				"source":       sourceName,
				"platform":     item.Platform,
				"media_type":   item.MediaType,
				"archive_type": item.ArchiveType,
				"url":          part.URL,
				"source_path":  part.SourcePath,
				"target":       relativePlanPath(dataRoot, part.Target),
				"size":         part.Size,
				"package":      item.Name,
				"part":         part.Name,
			})
		}
	}
	return b.String()
}

func renderArtworkAria2Plan(dataRoot string, items []artworkPackage) string {
	var b bytes.Buffer
	for _, item := range items {
		for _, part := range item.Parts {
			relativeTarget := relativePlanPath(dataRoot, part.Target)
			fmt.Fprintf(&b, "%s\n", part.URL)
			fmt.Fprintf(&b, "  dir=%s\n", pathpkg.Dir(relativeTarget))
			fmt.Fprintf(&b, "  out=%s\n", pathpkg.Base(relativeTarget))
		}
	}
	return b.String()
}

func artworkPlanItemCount(items []artworkPackage) int {
	count := 0
	for _, item := range items {
		count += len(item.Parts)
	}
	return count
}

func missingPackageParts(pkg artworkPackage) ([]artworkPackagePart, error) {
	var missing []artworkPackagePart
	for _, part := range pkg.Parts {
		if _, err := catalogfs.Stat(part.Target); err == nil {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		missing = append(missing, part)
	}
	return missing, nil
}

func packageWithParts(pkg artworkPackage, parts []artworkPackagePart) artworkPackage {
	pkg.Parts = append([]artworkPackagePart(nil), parts...)
	if len(parts) > 0 {
		pkg.SourcePath = parts[0].SourcePath
		pkg.Name = parts[0].Name
		pkg.URL = parts[0].URL
		pkg.Target = parts[0].Target
	}
	return pkg
}

type archiveExtractor struct {
	Path     string
	Kind     string
	Disabled bool
}

func resolveArchiveExtractor(configured string) (archiveExtractor, error) {
	configured = strings.TrimSpace(configured)
	switch strings.ToLower(configured) {
	case "none", "off", "false", "disabled":
		return archiveExtractor{Disabled: true}, nil
	}
	if configured == "" || strings.EqualFold(configured, "auto") {
		for _, candidate := range []string{"7z", "7zz", "7za", "unrar", "rar"} {
			path, err := exec.LookPath(candidate)
			if err != nil {
				continue
			}
			kind, ok := archiveExtractorKind(path)
			if ok {
				return archiveExtractor{Path: path, Kind: kind}, nil
			}
		}
		return archiveExtractor{}, nil
	}

	path := configured
	if !strings.ContainsAny(configured, `/\`) {
		resolved, err := exec.LookPath(configured)
		if err != nil {
			return archiveExtractor{}, fmt.Errorf("archive extractor %q was not found in PATH", configured)
		}
		path = resolved
	} else if _, err := catalogfs.Stat(path); err != nil {
		return archiveExtractor{}, fmt.Errorf("archive extractor %q is not available: %w", configured, err)
	}
	kind, ok := archiveExtractorKind(path)
	if !ok {
		return archiveExtractor{}, fmt.Errorf("archive extractor %q is not supported; use 7z, 7zz, 7za, unrar, or rar", configured)
	}
	return archiveExtractor{Path: path, Kind: kind}, nil
}

func archiveExtractorKind(path string) (string, bool) {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	switch name {
	case "7z", "7zz", "7za":
		return "7z", true
	case "unrar", "rar":
		return "rar", true
	default:
		return "", false
	}
}

func (extractor archiveExtractor) Available() bool {
	return strings.TrimSpace(extractor.Path) != "" && strings.TrimSpace(extractor.Kind) != ""
}

func (extractor archiveExtractor) CanExtract(archiveType string) bool {
	if !extractor.Available() {
		return false
	}
	switch extractor.Kind {
	case "7z":
		return archiveType == "rar" || archiveType == "7z"
	case "rar":
		return archiveType == "rar"
	default:
		return false
	}
}

func (extractor archiveExtractor) Display() string {
	switch {
	case extractor.Disabled:
		return "none"
	case extractor.Available():
		return extractor.Path
	default:
		return "auto (not found)"
	}
}

func extractArtworkPackages(dataRoot string, packages []artworkPackage, extractor archiveExtractor, progressCallbacks ...taskprogress.Func) (int, int, error) {
	progress := firstProgress(progressCallbacks)
	extractedPackages := 0
	extractedFiles := 0
	for index, pkg := range packages {
		progress.Update("extracting", index+1, len(packages), pkg.Name)
		if !pkg.Extractable {
			continue
		}
		missing, err := missingPackageParts(pkg)
		if err != nil {
			return 0, 0, err
		}
		if len(missing) > 0 {
			continue
		}

		targetRoot := extractedPackageDir(dataRoot, pkg)
		var count int
		switch pkg.ArchiveType {
		case "zip":
			count, err = extractZipPackage(pkg.Target, targetRoot)
		case "rar", "7z":
			count, err = extractExternalPackage(extractor, pkg.Target, targetRoot)
		default:
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if count > 0 {
			extractedPackages++
			extractedFiles += count
		}
	}
	return extractedPackages, extractedFiles, nil
}

func extractExternalPackage(extractor archiveExtractor, archivePath, targetRoot string) (int, error) {
	if !extractor.Available() {
		return 0, fmt.Errorf("no archive extractor available for %s", archivePath)
	}
	before, err := countRegularFiles(targetRoot)
	if err != nil {
		return 0, err
	}
	if err := catalogfs.MkdirAll(targetRoot, 0o755); err != nil {
		return 0, err
	}

	var args []string
	switch extractor.Kind {
	case "7z":
		args = []string{"x", "-y", "-aos", "-o" + targetRoot, archivePath}
	case "rar":
		args = []string{"x", "-o-", "-y", archivePath, targetRoot + string(os.PathSeparator)}
	default:
		return 0, fmt.Errorf("unsupported archive extractor %q", extractor.Path)
	}
	cmd := exec.Command(extractor.Path, args...)
	cmd.Dir = filepath.Dir(archivePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%s failed for %s: %w: %s", filepath.Base(extractor.Path), archivePath, err, trimmedCommandOutput(output))
	}
	after, err := countRegularFiles(targetRoot)
	if err != nil {
		return 0, err
	}
	if after <= before {
		return 0, nil
	}
	return after - before, nil
}

func countRegularFiles(root string) (int, error) {
	if _, err := catalogfs.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	err := catalogfs.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

func trimmedCommandOutput(output []byte) string {
	text := strings.TrimSpace(string(output))
	if len(text) > 500 {
		return text[:500] + "..."
	}
	return text
}

func extractZipPackage(zipPath, targetRoot string) (int, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	targetRoot = filepath.Clean(targetRoot)
	extracted := 0
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		relative := filepath.Clean(filepath.FromSlash(file.Name))
		if relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
			return 0, fmt.Errorf("unsafe archive path %q in %s", file.Name, zipPath)
		}
		target := filepath.Join(targetRoot, relative)
		if !pathWithin(targetRoot, target) {
			return 0, fmt.Errorf("unsafe archive path %q in %s", file.Name, zipPath)
		}
		if _, err := catalogfs.Stat(target); err == nil {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		if err := catalogfs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		in, err := file.Open()
		if err != nil {
			return 0, err
		}
		out, err := catalogfs.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = in.Close()
			return 0, err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		_ = in.Close()
		if copyErr != nil {
			return 0, copyErr
		}
		if closeErr != nil {
			return 0, closeErr
		}
		extracted++
	}
	return extracted, nil
}

type unmatchedArtworkManifestWriter struct {
	path    string
	tmpPath string
	file    *os.File
	writer  *bufio.Writer
	encoder *json.Encoder
	count   int
	closed  bool
}

func openUnmatchedArtworkManifest(dataRoot string) (*unmatchedArtworkManifestWriter, error) {
	path := filepath.Join(dataRoot, ".rommeta", "runs", "emumovies-artwork-unmatched.jsonl")
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}
	writer := bufio.NewWriter(file)
	return &unmatchedArtworkManifestWriter{
		path:    path,
		tmpPath: tmpPath,
		file:    file,
		writer:  writer,
		encoder: json.NewEncoder(writer),
	}, nil
}

func (manifest *unmatchedArtworkManifestWriter) Write(dataRoot string, pkg artworkPackage, root, path string, size int64) error {
	if manifest == nil {
		return nil
	}
	sourcePath, err := filepath.Rel(root, path)
	if err != nil {
		sourcePath = filepath.Base(path)
	}
	entry := map[string]any{
		"source":         sourceName,
		"platform":       pkg.Platform,
		"media_type":     pkg.MediaType,
		"package":        pkg.Name,
		"archive_type":   pkg.ArchiveType,
		"package_target": relativePlanPath(dataRoot, pkg.Target),
		"source_path":    filepath.ToSlash(sourcePath),
		"file_name":      filepath.Base(path),
		"reason":         "no confident catalog match",
	}
	if size > 0 {
		entry["size"] = size
	}
	if err := manifest.encoder.Encode(entry); err != nil {
		return err
	}
	manifest.count++
	return nil
}

func (manifest *unmatchedArtworkManifestWriter) Close() (string, int, error) {
	if manifest == nil || manifest.closed {
		return "", 0, nil
	}
	manifest.closed = true
	if err := manifest.writer.Flush(); err != nil {
		_ = manifest.file.Close()
		return "", 0, err
	}
	if err := manifest.file.Close(); err != nil {
		return "", 0, err
	}
	if manifest.count == 0 {
		_ = catalogfs.Remove(manifest.tmpPath)
		_ = catalogfs.Remove(manifest.path)
		return "", 0, nil
	}
	if err := catalogfs.Rename(manifest.tmpPath, manifest.path); err != nil {
		return "", 0, err
	}
	return manifest.path, manifest.count, nil
}

func (manifest *unmatchedArtworkManifestWriter) Abort() {
	if manifest == nil || manifest.closed {
		return
	}
	manifest.closed = true
	_ = manifest.file.Close()
	_ = catalogfs.Remove(manifest.tmpPath)
}

func indexArtworkFiles(dataRoot string, packages []artworkPackage, platformGames map[string][]catalogGame, progressCallbacks ...taskprogress.Func) ([]artworkFileCandidate, int, int, string, error) {
	progress := firstProgress(progressCallbacks)
	gameIndexes := map[string]mediaGameIndex{}
	for platform, games := range platformGames {
		gameIndexes[platform] = newMediaGameIndex(games)
	}
	manifest, err := openUnmatchedArtworkManifest(dataRoot)
	if err != nil {
		return nil, 0, 0, "", err
	}
	defer manifest.Abort()

	var candidates []artworkFileCandidate
	indexedFiles := 0
	unmatchedFiles := 0
	for index, pkg := range packages {
		progress.Update("indexing files", index+1, len(packages), pkg.Name)
		root := extractedPackageDir(dataRoot, pkg)
		if _, err := catalogfs.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, 0, 0, "", err
		}
		err := catalogfs.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !isArtworkFile(entry.Name()) {
				return nil
			}
			indexedFiles++
			info, err := entry.Info()
			if err != nil {
				return err
			}
			candidate, ok := matchArtworkFile(pkg, root, path, info.Size(), gameIndexes[pkg.Platform].candidates(path))
			if !ok {
				unmatchedFiles++
				if err := manifest.Write(dataRoot, pkg, root, path, info.Size()); err != nil {
					return err
				}
				return nil
			}
			candidates = append(candidates, candidate)
			return nil
		})
		if err != nil {
			return nil, 0, 0, "", err
		}
	}
	unmatchedManifestPath, _, err := manifest.Close()
	if err != nil {
		return nil, 0, 0, "", err
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].GamePath == candidates[j].GamePath {
			if candidates[i].MediaType == candidates[j].MediaType {
				return candidates[i].SourcePath < candidates[j].SourcePath
			}
			return candidates[i].MediaType < candidates[j].MediaType
		}
		return candidates[i].GamePath < candidates[j].GamePath
	})
	return candidates, indexedFiles, unmatchedFiles, unmatchedManifestPath, nil
}

func matchArtworkFile(pkg artworkPackage, root, path string, size int64, games []catalogGame) (artworkFileCandidate, bool) {
	name := filepath.Base(path)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	var best artworkFileCandidate
	ambiguous := false
	for _, game := range games {
		release, score, keys := scoreVideoAgainstGame(stem, game)
		if score < minimumMatchScore {
			continue
		}
		sourcePath, err := filepath.Rel(root, path)
		if err != nil {
			sourcePath = name
		}
		candidate := artworkFileCandidate{
			Platform:      pkg.Platform,
			Game:          game.Folder,
			GamePath:      game.Path,
			Release:       release,
			ReleaseCount:  len(game.Releases),
			MediaType:     pkg.MediaType,
			SourcePackage: pkg.Name,
			SourcePath:    filepath.ToSlash(sourcePath),
			FileName:      name,
			Size:          size,
			MatchScore:    score,
			MatchedKeys:   keys,
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
		return artworkFileCandidate{}, false
	}
	return best, true
}

func assignArtworkTargets(candidates []artworkFileCandidate) {
	byGameMedia := map[string][]int{}
	for i, candidate := range candidates {
		key := candidate.GamePath + "\x00" + candidate.MediaType
		byGameMedia[key] = append(byGameMedia[key], i)
	}
	for _, indexes := range byGameMedia {
		baseCounts := map[string]int{}
		releaseSpecific := artworkTargetsNeedReleaseFolders(candidates, indexes)
		for _, index := range indexes {
			candidate := &candidates[index]
			extension := strings.ToLower(filepath.Ext(candidate.FileName))
			if extension == "" {
				extension = ".png"
			}
			targetDir := filepath.Join(candidate.GamePath, "media")
			if releaseSpecific && releaseNeedsMediaFolder(candidate.Game, candidate.Release) {
				targetDir = filepath.Join(targetDir, candidate.Release)
			}
			region := firstRegionTag(candidate.Release)
			baseName := sourceArtworkFileName(candidate.MediaType, region, extension, 1)
			baseTarget := filepath.Join(targetDir, baseName)
			baseCounts[baseTarget]++
			name := sourceArtworkFileName(candidate.MediaType, region, extension, baseCounts[baseTarget])
			candidate.Target = filepath.Clean(filepath.Join(targetDir, name))
		}
	}
}

func artworkTargetsNeedReleaseFolders(candidates []artworkFileCandidate, indexes []int) bool {
	for _, index := range indexes {
		candidate := candidates[index]
		if candidate.ReleaseCount > 1 && releaseNeedsMediaFolder(candidate.Game, candidate.Release) {
			return true
		}
	}
	return false
}

func sourceArtworkFileName(mediaType, region, extension string, ordinal int) string {
	stem := mediaType
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

func firstRegionTag(value string) string {
	regions := regionTags(value)
	if len(regions) == 0 {
		return ""
	}
	return regions[0]
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
	return strings.Trim(strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == '-' }), "-"), "-")
}

type artworkMaterialization struct {
	existing, written, invalid int
	failures                   []Failure
}

func materializeArtworkFiles(candidates []artworkFileCandidate, progressCallbacks ...taskprogress.Func) (artworkMaterialization, error) {
	progress := firstProgress(progressCallbacks)
	var result artworkMaterialization
	for index, candidate := range candidates {
		progress.Update("materializing", index+1, len(candidates), candidate.Game)
		if candidate.Target == "" {
			continue
		}
		if _, err := catalogfs.Stat(candidate.Target); err == nil {
			result.existing++
			continue
		} else if !os.IsNotExist(err) {
			return result, err
		}
		if err := copyFile(candidate.SourcePathAbs(), candidate.Target); err != nil {
			if !errors.Is(err, errInvalidCachedArtwork) {
				return result, err
			}
			result.invalid++
			if len(result.failures) < 10 {
				result.failures = append(result.failures, Failure{SourcePath: filepath.Join(candidate.SourcePackage, filepath.FromSlash(candidate.SourcePath)), Target: candidate.Target, Error: err.Error()})
			}
			continue
		}
		result.written++
	}
	return result, nil
}

var errInvalidCachedArtwork = errors.New("invalid cached artwork source")

func (candidate artworkFileCandidate) SourcePathAbs() string {
	return filepath.Join(extractedPackageDir(candidate.DataRoot(), artworkPackage{
		Platform:  candidate.Platform,
		Name:      candidate.SourcePackage,
		MediaType: candidate.MediaType,
	}), filepath.FromSlash(candidate.SourcePath))
}

func (candidate artworkFileCandidate) DataRoot() string {
	platformRoot := filepath.Dir(filepath.Dir(candidate.GamePath))
	return filepath.Dir(platformRoot)
}

func copyFile(source, target string) error {
	in, err := catalogfs.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	reader := bufio.NewReader(in)
	prefix, err := reader.Peek(64)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(prefix) == 0 || bytes.Equal(prefix, make([]byte, len(prefix))) {
		return fmt.Errorf("%w: empty or zero-filled image", errInvalidCachedArtwork)
	}
	if err := catalogfs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	out, err := catalogfs.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		_ = catalogfs.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = catalogfs.Remove(tmp)
		return closeErr
	}
	return catalogfs.Rename(tmp, target)
}

func extractedPackageDir(dataRoot string, pkg artworkPackage) string {
	return filepath.Join(dataRoot, ".rommeta", "sources", sourceName, "extracted", pkg.Platform, safePackageStem(pkg.Name))
}

func safePackageStem(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	var b strings.Builder
	for _, r := range stem {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	value := strings.Trim(strings.TrimSpace(b.String()), ".")
	if value == "" {
		return "package"
	}
	return value
}

func pathWithin(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if target == root {
		return true
	}
	prefix := root + string(os.PathSeparator)
	return strings.HasPrefix(target, prefix)
}

func isArtworkFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}
