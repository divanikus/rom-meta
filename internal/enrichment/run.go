// Package enrichment runs the same provider jobs for CLI and browser operations.
package enrichment

import (
	"fmt"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/emumovies"
	"rommeta/internal/fetchstate"
	"rommeta/internal/hasheous"
	"rommeta/internal/igdb"
	"rommeta/internal/launchbox"
	"rommeta/internal/openvgdb"
	"rommeta/internal/platformnames"
	"rommeta/internal/screenscraper"
	"rommeta/internal/sourceassets"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"strings"
)

type Options struct {
	DataRoot         string
	Types            []string
	Sources          []string
	MediaTypes       []string
	Platform         string
	PlatformRegistry *platformnames.Registry
	Apply            bool
	ForceOverwrite   bool
	RefreshRelations bool
	RefreshLinks     bool
	LinkReview       string
	Limit            int
	Concurrency      int
	ConcurrencySet   bool
	PlanPath         string
	PlanFormat       string
}

type Job struct {
	Type       string
	Source     string
	Platform   string
	Automatic  bool
	SkipReason string
}

func Supported(job Job) bool {
	switch {
	case job.Source == "openvgdb" && job.Type == "metadata":
		return true
	case job.Source == "openvgdb" && job.Type == "artwork":
		return true
	case job.Source == "launchbox" && job.Type == "metadata":
		return true
	case job.Source == "launchbox" && job.Type == "artwork":
		return true
	case job.Source == "igdb" && job.Type == "metadata":
		return true
	case job.Source == "hasheous" && job.Type == "metadata":
		return true
	case job.Source == "igdb" && job.Type == "artwork":
		return true
	case job.Source == "screenscraper" && job.Type == "metadata":
		return true
	case job.Source == "screenscraper" && job.Type == "artwork":
		return true
	case job.Source == "emumovies" && job.Type == "video":
		return true
	case job.Source == "emumovies" && job.Type == "artwork":
		return true
	default:
		return false
	}
}

func Concurrency(options Options, config store.SourcesConfig, job Job) int {
	concurrency := 1
	if configured := config.FetchConcurrency; configured > 0 {
		concurrency = configured
	}
	if configured := sourceConcurrency(config, job.Source); configured > 0 {
		concurrency = configured
	}
	if options.ConcurrencySet {
		concurrency = options.Concurrency
	}
	if !parallelFetchSupported(job) {
		return 1
	}
	if concurrency < 1 {
		return 1
	}
	return concurrency
}

func sourceConcurrency(config store.SourcesConfig, sourceName string) int {
	for _, source := range config.Sources {
		if strings.EqualFold(source.Name, sourceName) {
			return source.FetchConcurrency
		}
	}
	return 0
}

func parallelFetchSupported(job Job) bool {
	if !store.SourceSupportsParallelFetch(job.Source) {
		return false
	}
	if job.Type == "metadata" {
		return job.Source == "hasheous"
	}
	return job.Type == "artwork" || job.Type == "video"
}

func runLaunchBox(options Options, progress taskprogress.Func) (launchbox.ImportResult, error) {
	importOptions := launchbox.ImportOptions{DataRoot: options.DataRoot, Platform: options.Platform, PlatformRegistry: options.PlatformRegistry, Apply: options.Apply, ForceOverwrite: options.ForceOverwrite, Progress: progress}
	downloadURL := ""
	config, err := store.LoadSources(options.DataRoot)
	if err != nil {
		return launchbox.ImportResult{}, err
	}
	for _, source := range config.Sources {
		if source.Name == "launchbox" {
			importOptions.ZipPath = store.ResolveDataRootPath(options.DataRoot, source.SnapshotPath)
			downloadURL = source.SnapshotURL
			break
		}
	}
	if importOptions.ZipPath == "" {
		importOptions.ZipPath = launchbox.DefaultSnapshotPath(options.DataRoot)
	}
	if downloadURL == "" {
		downloadURL = launchbox.DefaultSnapshotURL()
	}

	var snapshot launchbox.SnapshotResult
	if options.Apply {
		snapshot, err = launchbox.EnsureSnapshotFromURL(options.DataRoot, downloadURL, progress)
		if err != nil {
			return launchbox.ImportResult{}, err
		}
		importOptions.ZipPath = snapshot.ZipPath
	}
	progress.Update("check snapshot", 0, 0, filepath.Base(importOptions.ZipPath))
	if _, err := catalogfs.Stat(importOptions.ZipPath); err != nil {
		if os.IsNotExist(err) {
			return launchbox.ImportResult{}, fmt.Errorf("LaunchBox snapshot not found: %s (rerun with --apply to download it, or set sources.launchbox.snapshot.path)", importOptions.ZipPath)
		}
		return launchbox.ImportResult{}, err
	}

	result, err := launchbox.Import(importOptions)
	if err != nil {
		return launchbox.ImportResult{}, err
	}
	result.Snapshot = snapshot
	return result, nil
}

func runOpenVGDB(input Options, progress taskprogress.Func) (openvgdb.ImportResult, error) {
	options := openvgdb.ImportOptions{DataRoot: input.DataRoot, Platform: input.Platform, PlatformRegistry: input.PlatformRegistry, Apply: input.Apply, ForceOverwrite: input.ForceOverwrite, Progress: progress}
	downloadURL := ""
	needsSourceConfig := options.DBPath == "" || (options.Apply && downloadURL == "")
	if needsSourceConfig {
		config, err := store.LoadSources(options.DataRoot)
		if err != nil {
			return openvgdb.ImportResult{}, err
		}
		for _, source := range config.Sources {
			if source.Name == "openvgdb" {
				if options.DBPath == "" {
					options.DBPath = store.ResolveDataRootPath(options.DataRoot, source.SnapshotPath)
				}
				if options.Apply && downloadURL == "" {
					downloadURL = source.SnapshotURL
				}
				break
			}
		}
	}
	if downloadURL == "" {
		downloadURL = openvgdb.DefaultSnapshotURL()
	}
	var snapshot openvgdb.SnapshotResult
	var err error
	if options.Apply {
		snapshot, err = openvgdb.EnsureSnapshotFromURL(options.DataRoot, downloadURL, progress)
		if err != nil {
			return openvgdb.ImportResult{}, err
		}
		options.DBPath = snapshot.DBPath
	}
	if options.DBPath == "" {
		options.DBPath = openvgdb.DefaultSnapshotPath(options.DataRoot)
	}
	progress.Update("check snapshot", 0, 0, filepath.Base(options.DBPath))
	if _, err := catalogfs.Stat(options.DBPath); err != nil {
		if os.IsNotExist(err) {
			return openvgdb.ImportResult{}, fmt.Errorf("OpenVGDB snapshot not found: %s (rerun with --download, pass --db, or set sources.openvgdb.snapshot.path)", options.DBPath)
		}
		return openvgdb.ImportResult{}, err
	}

	result, err := openvgdb.Import(options)
	if err != nil {
		return openvgdb.ImportResult{}, err
	}
	result.Snapshot = snapshot
	return result, nil
}

// Run executes one validated provider job. The caller owns catalog locking,
// index batching and progress lifetime; no terminal or HTTP request is required.
func Run(job Job, o Options, config store.SourcesConfig, progress taskprogress.Func) (any, error) {
	if !Supported(job) {
		return nil, fmt.Errorf("fetch source %q does not support type %q yet", job.Source, job.Type)
	}
	if len(o.MediaTypes) > 0 && !(job.Source == "emumovies" && job.Type == "artwork") {
		return nil, fmt.Errorf("--media-type is currently supported only for fetch --source emumovies --type artwork")
	}
	concurrency := Concurrency(o, config, job)
	metadataProgress := func(stage string, current, total, queries, matches int, item string) {
		if stage == "" {
			stage = "matching"
			item = fmt.Sprintf("queries=%d | matches=%d | %s", queries, matches, item)
		}
		progress.Update(stage, current, total, item)
	}
	switch {
	case job.Source == "launchbox" && job.Type == "metadata":
		return runLaunchBox(o, progress)
	case job.Source == "openvgdb" && job.Type == "metadata":
		return runOpenVGDB(o, progress)
	case job.Source == "igdb" && job.Type == "metadata":
		result, err := igdb.Import(igdb.ImportOptions{DataRoot: o.DataRoot, Platform: o.Platform, PlatformRegistry: o.PlatformRegistry, Apply: o.Apply, ForceOverwrite: o.ForceOverwrite, Limit: o.Limit, RefreshRelations: o.RefreshRelations, RefreshLinks: o.RefreshLinks, LinkReview: o.LinkReview,
			Progress: func(p igdb.ImportProgress) {
				metadataProgress(p.Stage, p.Current, p.Total, p.Queries, p.Matches, p.Game)
			}})
		if err == nil {
			err = fetchstate.Incomplete(result.FailedSearchCount)
		}
		return result, err
	case job.Source == "screenscraper" && job.Type == "metadata":
		result, err := screenscraper.Import(screenscraper.ImportOptions{DataRoot: o.DataRoot, Platform: o.Platform, PlatformRegistry: o.PlatformRegistry, Apply: o.Apply, ForceOverwrite: o.ForceOverwrite, Limit: o.Limit,
			Progress: func(p screenscraper.ImportProgress) {
				metadataProgress(p.Stage, p.Current, p.Total, p.Queries, p.Matches, p.Game)
			}})
		if err == nil {
			err = fetchstate.Incomplete(result.FailedSearchCount)
		}
		return result, err
	case job.Source == "hasheous" && job.Type == "metadata":
		result, err := hasheous.Import(hasheous.ImportOptions{DataRoot: o.DataRoot, Platform: o.Platform, Apply: o.Apply, ForceOverwrite: o.ForceOverwrite, SkipWithoutEvidence: job.Automatic, Limit: o.Limit, Concurrency: concurrency,
			Progress: func(p hasheous.ImportProgress) {
				if p.Worker > 0 {
					item := p.Game
					if p.Requests > 0 {
						item = fmt.Sprintf("request %d/%d | %s", p.Request, p.Requests, item)
					}
					progress.UpdateWorker(p.Stage, p.Worker, p.Workers, item)
					return
				}
				stage := p.Stage
				if stage == "matching" {
					stage = ""
				}
				metadataProgress(stage, p.Current, p.Total, p.Queries, p.Matches, p.Game)
			}})
		if err == nil {
			err = fetchstate.Incomplete(result.FailedLookupCount)
		}
		return result, err
	case job.Source == "emumovies" && job.Type == "video":
		return emumovies.FetchVideo(emumovies.VideoOptions{DataRoot: o.DataRoot, Platform: o.Platform, PlatformRegistry: o.PlatformRegistry, Apply: o.Apply, Limit: o.Limit, Concurrency: concurrency, PlanPath: o.PlanPath, PlanFormat: o.PlanFormat, Progress: progress})
	case job.Source == "emumovies" && job.Type == "artwork":
		return emumovies.FetchArtwork(emumovies.ArtworkOptions{DataRoot: o.DataRoot, Platform: o.Platform, PlatformRegistry: o.PlatformRegistry, Apply: o.Apply, MediaTypes: o.MediaTypes, Limit: o.Limit, Concurrency: concurrency, PlanPath: o.PlanPath, PlanFormat: o.PlanFormat, Progress: progress})
	default:
		notes := map[string]string{"openvgdb": "OpenVGDB", "launchbox": "LaunchBox Games Database", "igdb": "IGDB", "screenscraper": "ScreenScraper"}
		return sourceassets.Fetch(sourceassets.Options{DataRoot: o.DataRoot, Source: job.Source, Platform: o.Platform, PlatformRegistry: o.PlatformRegistry, Apply: o.Apply, Limit: o.Limit, Concurrency: concurrency, PlanPath: o.PlanPath, PlanFormat: o.PlanFormat, PolicyScope: "review", PolicyNote: "Imported from " + notes[job.Source] + " media URLs; verify rights before sharing.", Progress: progress})
	}
}
