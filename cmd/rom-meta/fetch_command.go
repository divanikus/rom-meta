package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
)

type fetchOptions struct {
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

type fetchJob struct {
	Type       string
	Source     string
	Platform   string
	Automatic  bool
	SkipReason string
}

func runFetch(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printFetchUsage()
		return nil
	}
	options := fetchOptions{DataRoot: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isHelpArg(arg) {
			printFetchUsage()
			return nil
		}
		switch {
		case arg == "--refresh-links":
			options.RefreshLinks = true
		case arg == "--link-review":
			if i+1 >= len(args) {
				return fmt.Errorf("--link-review requires a path")
			}
			i++
			options.LinkReview = args[i]
		case strings.HasPrefix(arg, "--link-review="):
			options.LinkReview = strings.TrimPrefix(arg, "--link-review=")
		case arg == "--refresh-relations":
			options.RefreshRelations = true
		case arg == "--apply":
			options.Apply = true
		case arg == "--force-overwrite":
			options.ForceOverwrite = true
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--type":
			if i+1 >= len(args) {
				return fmt.Errorf("--type requires a value")
			}
			i++
			options.Types = appendFetchValues(options.Types, args[i])
		case strings.HasPrefix(arg, "--type="):
			options.Types = appendFetchValues(options.Types, strings.TrimPrefix(arg, "--type="))
		case arg == "--source":
			if i+1 >= len(args) {
				return fmt.Errorf("--source requires a value")
			}
			i++
			options.Sources = appendFetchValues(options.Sources, args[i])
		case strings.HasPrefix(arg, "--source="):
			options.Sources = appendFetchValues(options.Sources, strings.TrimPrefix(arg, "--source="))
		case arg == "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires a value")
			}
			i++
			options.Platform = args[i]
		case strings.HasPrefix(arg, "--platform="):
			options.Platform = strings.TrimPrefix(arg, "--platform=")
		case arg == "--media-type":
			if i+1 >= len(args) {
				return fmt.Errorf("--media-type requires a value")
			}
			i++
			options.MediaTypes = appendFetchValues(options.MediaTypes, args[i])
		case strings.HasPrefix(arg, "--media-type="):
			options.MediaTypes = appendFetchValues(options.MediaTypes, strings.TrimPrefix(arg, "--media-type="))
		case arg == "--limit":
			if i+1 >= len(args) {
				return fmt.Errorf("--limit requires a value")
			}
			i++
			limit, err := strconv.Atoi(args[i])
			if err != nil || limit < 0 {
				return fmt.Errorf("--limit must be a non-negative integer")
			}
			options.Limit = limit
		case strings.HasPrefix(arg, "--limit="):
			limit, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil || limit < 0 {
				return fmt.Errorf("--limit must be a non-negative integer")
			}
			options.Limit = limit
		case arg == "--concurrency":
			if i+1 >= len(args) {
				return fmt.Errorf("--concurrency requires a value")
			}
			i++
			concurrency, err := strconv.Atoi(args[i])
			if err != nil || concurrency < 1 {
				return fmt.Errorf("--concurrency must be a positive integer")
			}
			options.Concurrency = concurrency
			options.ConcurrencySet = true
		case strings.HasPrefix(arg, "--concurrency="):
			concurrency, err := strconv.Atoi(strings.TrimPrefix(arg, "--concurrency="))
			if err != nil || concurrency < 1 {
				return fmt.Errorf("--concurrency must be a positive integer")
			}
			options.Concurrency = concurrency
			options.ConcurrencySet = true
		case arg == "--plan":
			if i+1 >= len(args) {
				return fmt.Errorf("--plan requires a value")
			}
			i++
			options.PlanPath = args[i]
		case strings.HasPrefix(arg, "--plan="):
			options.PlanPath = strings.TrimPrefix(arg, "--plan=")
		case arg == "--plan-format":
			if i+1 >= len(args) {
				return fmt.Errorf("--plan-format requires a value")
			}
			i++
			options.PlanFormat = args[i]
		case strings.HasPrefix(arg, "--plan-format="):
			options.PlanFormat = strings.TrimPrefix(arg, "--plan-format=")
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}

	config, err := store.LoadSources(options.DataRoot)
	if err != nil {
		return err
	}
	options.DataRoot = config.Root
	registry, _, err := platformnames.LoadRegistry(config.Root)
	if err != nil {
		return err
	}
	registry.MergeAliases(config.PlatformAliases)
	options.PlatformRegistry = &registry
	if len(options.Sources) > 0 {
		options.Platform = registry.ResolveCLI(options.Platform)
	}
	jobs := buildFetchJobs(options, config)
	if options.RefreshLinks && (options.LinkReview == "" || options.RefreshRelations || options.ForceOverwrite || options.Limit != 0 || len(jobs) != 1 || jobs[0].Source != "igdb" || jobs[0].Type != "metadata") {
		return fmt.Errorf("--refresh-links requires --link-review, --source igdb --type metadata, without force-overwrite/refresh-relations/limit")
	}
	if options.LinkReview != "" && !options.RefreshLinks {
		return fmt.Errorf("--link-review requires --refresh-links")
	}
	if options.RefreshRelations && (len(jobs) != 1 || jobs[0].Source != "igdb" || jobs[0].Type != "metadata") {
		return fmt.Errorf("--refresh-relations requires --source igdb --type metadata")
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no fetch jobs selected")
	}
	if err := validateFetchJobs(jobs); err != nil {
		return err
	}

	jobs, err = selectFetchJobs(jobs, options, config)
	if err != nil {
		return err
	}
	printFetchHeader(config.Root, options.Platform, jobs)
	if len(options.Sources) == 0 {
		fmt.Println("Selection: automatic; configured source order, platform mappings and lookup evidence")
		if len(jobs) == 0 {
			fmt.Println("Result: skipped; no catalog platforms to fetch")
		}
	}
	return runFetchJobs(os.Stdout, jobs, options, config, runFetchJob)
}

type fetchJobRunner func(fetchJob, fetchOptions, store.SourcesConfig) error

type fetchJobFailure struct {
	Job fetchJob
	Err error
}

type fetchJobFailures []fetchJobFailure

func (failures fetchJobFailures) Error() string {
	if len(failures) == 0 {
		return ""
	}
	if len(failures) == 1 {
		failure := failures[0]
		return fmt.Sprintf("fetch job %s failed: %v", fetchJobName(failure.Job), failure.Err)
	}
	labels := make([]string, 0, len(failures))
	for _, failure := range failures {
		labels = append(labels, fmt.Sprintf("%s: %v", fetchJobName(failure.Job), failure.Err))
	}
	return fmt.Sprintf("%d fetch jobs failed: %s", len(failures), strings.Join(labels, ", "))
}

func runFetchJobs(out io.Writer, jobs []fetchJob, options fetchOptions, config store.SourcesConfig, runner fetchJobRunner) error {
	var failures fetchJobFailures
	completed, skipped, incomplete, failed := 0, 0, 0, 0
	quotaExceeded := map[string]bool{}
	for index, job := range jobs {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "[%d/%d] %s\n", index+1, len(jobs), fetchJobName(job))
		if job.SkipReason != "" {
			fmt.Fprintf(out, "Result: skipped; %s\n", job.SkipReason)
			skipped++
			continue
		}
		if quotaExceeded[job.Source] {
			fmt.Fprintln(out, "Result: skipped; ScreenScraper daily quota was already exceeded in this run")
			skipped++
			continue
		}
		scoped := options
		if job.Automatic {
			scoped.Platform = job.Platform
		}
		reason, err := automaticFetchEvidenceReason(out, job, scoped)
		if err == nil && reason != "" {
			fmt.Fprintf(out, "Result: skipped; %s\n", reason)
			skipped++
			continue
		}
		if err == nil {
			err = runner(job, scoped, config)
		}
		if err != nil {
			if errors.Is(err, screenscraper.ErrDailyQuota) {
				quotaExceeded[job.Source] = true
			}
			failures = append(failures, fetchJobFailure{Job: job, Err: err})
			var partial *fetchstate.PartialError
			if errors.As(err, &partial) || errors.Is(err, screenscraper.ErrDailyQuota) {
				incomplete++
				fmt.Fprintf(out, "Result: incomplete; %v\n", err)
			} else {
				failed++
				fmt.Fprintf(out, "Result: failed; error: %v\n", err)
			}
			continue
		}
		completed++
	}
	fmt.Fprintf(out, "\nFetch summary: %d completed, %d incomplete, %d failed, %d skipped jobs\n", completed, incomplete, failed, skipped)
	if len(failures) > 0 {
		return failures
	}
	return nil
}

func validateFetchJobs(jobs []fetchJob) error {
	for _, job := range jobs {
		if !fetchJobSupported(job) {
			return fmt.Errorf("fetch source %q does not support type %q yet", job.Source, job.Type)
		}
	}
	return nil
}

func fetchJobSupported(job fetchJob) bool {
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

func printFetchHeader(root, platform string, jobs []fetchJob) {
	fmt.Println("Preparing fetch")
	fmt.Printf("Library: %s\n", root)
	if strings.TrimSpace(platform) != "" {
		fmt.Printf("Platform: %s\n", platform)
	}
	fmt.Printf("Work: %d %s", len(jobs), pluralize(len(jobs), "task", "tasks"))
	if len(jobs) > 0 && len(jobs) <= 12 {
		fmt.Printf(" (%s)", fetchJobList(jobs))
	}
	fmt.Println()
}

func fetchJobList(jobs []fetchJob) string {
	labels := make([]string, 0, len(jobs))
	for _, job := range jobs {
		labels = append(labels, fetchJobName(job))
	}
	return strings.Join(labels, ", ")
}

func fetchJobName(job fetchJob) string {
	name := job.Source + "/" + job.Type
	if job.Platform != "" {
		name += " [" + job.Platform + "]"
	}
	return name
}

func fetchJobLabel(job fetchJob) string {
	return sourceDisplayName(job.Source) + " " + typeDisplayName(job.Type)
}

func sourceDisplayName(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "emumovies":
		return "EmuMovies"
	case "hasheous":
		return "Hasheous"
	case "igdb":
		return "IGDB"
	case "launchbox":
		return "LaunchBox"
	case "openvgdb":
		return "OpenVGDB"
	case "screenscraper":
		return "ScreenScraper"
	default:
		return source
	}
}

func typeDisplayName(typ string) string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "metadata":
		return "metadata"
	case "artwork":
		return "artwork"
	case "video":
		return "videos"
	default:
		return typ
	}
}

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func buildFetchJobs(options fetchOptions, config store.SourcesConfig) []fetchJob {
	if len(options.Types) == 0 && len(options.Sources) == 0 {
		var jobs []fetchJob
		for _, typ := range []string{"metadata", "artwork", "video"} {
			for _, source := range config.Fetch[typ] {
				jobs = append(jobs, fetchJob{Type: typ, Source: source})
			}
		}
		return jobs
	}

	types := options.Types
	if len(types) == 0 {
		types = []string{"metadata", "artwork", "video"}
	}
	var jobs []fetchJob
	for _, typ := range types {
		sources := options.Sources
		if len(sources) == 0 {
			sources = config.Fetch[typ]
		}
		for _, source := range sources {
			jobs = append(jobs, fetchJob{Type: typ, Source: source})
		}
	}
	return jobs
}

func appendFetchValues(values []string, raw string) []string {
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func runFetchJob(job fetchJob, options fetchOptions, config store.SourcesConfig) error {
	if len(options.MediaTypes) > 0 && !(job.Source == "emumovies" && job.Type == "artwork") {
		return fmt.Errorf("--media-type is currently supported only for fetch --source emumovies --type artwork")
	}
	concurrency := fetchConcurrency(options, config, job)
	switch {
	case job.Source == "openvgdb" && job.Type == "metadata":
		args := []string{"--data-root", options.DataRoot}
		if options.Platform != "" {
			args = append(args, "--platform", options.Platform)
		}
		if options.Apply {
			args = append(args, "--download", "--apply")
		}
		if options.ForceOverwrite {
			args = append(args, "--force-overwrite")
		}
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := runFetchOpenVGDB(args, progressFunc, options.PlatformRegistry)
		progress.Done()
		if err != nil {
			return err
		}
		printOpenVGDBResult(result)
		return nil
	case job.Source == "openvgdb" && job.Type == "artwork":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := sourceassets.Fetch(sourceassets.Options{
			DataRoot:         options.DataRoot,
			Source:           "openvgdb",
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			Limit:            options.Limit,
			Concurrency:      concurrency,
			PlanPath:         options.PlanPath,
			PlanFormat:       options.PlanFormat,
			PolicyScope:      "review",
			PolicyNote:       "Imported from OpenVGDB media URLs; verify rights before sharing.",
			Progress:         progressFunc,
		})
		progress.Done()
		if result.DataRoot != "" {
			printSourceAssetResult(result)
		}
		return err
	case job.Source == "launchbox" && job.Type == "metadata":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := runFetchLaunchBox(fetchOptions{
			DataRoot:         options.DataRoot,
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			ForceOverwrite:   options.ForceOverwrite,
			Limit:            options.Limit,
			Concurrency:      concurrency,
		}, progressFunc)
		progress.Done()
		if err != nil {
			return err
		}
		printLaunchBoxResult(result)
		return nil
	case job.Source == "launchbox" && job.Type == "artwork":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := sourceassets.Fetch(sourceassets.Options{
			DataRoot:         options.DataRoot,
			Source:           "launchbox",
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			Limit:            options.Limit,
			Concurrency:      concurrency,
			PlanPath:         options.PlanPath,
			PlanFormat:       options.PlanFormat,
			PolicyScope:      "review",
			PolicyNote:       "Imported from LaunchBox Games Database media URLs; verify rights before sharing.",
			Progress:         progressFunc,
		})
		progress.Done()
		if result.DataRoot != "" {
			printSourceAssetResult(result)
		}
		return err
	case job.Source == "igdb" && job.Type == "metadata":
		progress := newTerminalProgress(os.Stdout)
		result, err := igdb.Import(igdb.ImportOptions{
			RefreshRelations: options.RefreshRelations,
			RefreshLinks:     options.RefreshLinks, LinkReview: options.LinkReview,
			DataRoot:         options.DataRoot,
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			ForceOverwrite:   options.ForceOverwrite,
			Limit:            options.Limit,
			Progress: func(snapshot igdb.ImportProgress) {
				updateFetchMetadataProgress(progress, job, snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Queries, snapshot.Matches, snapshot.Game)
			},
		})
		progress.Done()
		if err != nil {
			if result.Apply && result.DataRoot != "" {
				printIGDBResult(result, err)
			}
			return err
		}
		printIGDBResult(result)
		return fetchstate.Incomplete(result.FailedSearchCount)
	case job.Source == "hasheous" && job.Type == "metadata":
		progress := newTerminalProgress(os.Stdout)
		result, err := hasheous.Import(hasheous.ImportOptions{
			DataRoot:            options.DataRoot,
			Platform:            options.Platform,
			Apply:               options.Apply,
			ForceOverwrite:      options.ForceOverwrite,
			SkipWithoutEvidence: job.Automatic,
			Limit:               options.Limit,
			Concurrency:         concurrency,
			Progress: func(snapshot hasheous.ImportProgress) {
				updateHasheousProgress(progress, job, snapshot)
			},
		})
		progress.Done()
		if err != nil {
			if result.Apply {
				printHasheousResult(result, err)
			}
			return err
		}
		printHasheousResult(result)
		return fetchstate.Incomplete(result.FailedLookupCount)
	case job.Source == "igdb" && job.Type == "artwork":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := sourceassets.Fetch(sourceassets.Options{
			DataRoot:         options.DataRoot,
			Source:           "igdb",
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			Limit:            options.Limit,
			Concurrency:      concurrency,
			PlanPath:         options.PlanPath,
			PlanFormat:       options.PlanFormat,
			PolicyScope:      "review",
			PolicyNote:       "Imported from IGDB media URLs; verify rights before sharing.",
			Progress:         progressFunc,
		})
		progress.Done()
		if result.DataRoot != "" {
			printSourceAssetResult(result)
		}
		return err
	case job.Source == "screenscraper" && job.Type == "metadata":
		progress := newTerminalProgress(os.Stdout)
		result, err := screenscraper.Import(screenscraper.ImportOptions{
			DataRoot:         options.DataRoot,
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			ForceOverwrite:   options.ForceOverwrite,
			Limit:            options.Limit,
			Progress: func(snapshot screenscraper.ImportProgress) {
				updateFetchMetadataProgress(progress, job, snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Queries, snapshot.Matches, snapshot.Game)
			},
		})
		progress.Done()
		if err != nil {
			if result.Apply && result.DataRoot != "" {
				printScreenScraperResult(result, err)
			}
			return err
		}
		printScreenScraperResult(result)
		return fetchstate.Incomplete(result.FailedSearchCount)
	case job.Source == "screenscraper" && job.Type == "artwork":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := sourceassets.Fetch(sourceassets.Options{
			DataRoot:         options.DataRoot,
			Source:           "screenscraper",
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			Limit:            options.Limit,
			Concurrency:      concurrency,
			PlanPath:         options.PlanPath,
			PlanFormat:       options.PlanFormat,
			PolicyScope:      "review",
			PolicyNote:       "Imported from ScreenScraper media URLs; verify rights before sharing.",
			Progress:         progressFunc,
		})
		progress.Done()
		if result.DataRoot != "" {
			printSourceAssetResult(result)
		}
		return err
	case job.Source == "emumovies" && job.Type == "video":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := emumovies.FetchVideo(emumovies.VideoOptions{
			DataRoot:         options.DataRoot,
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			Limit:            options.Limit,
			Concurrency:      concurrency,
			PlanPath:         options.PlanPath,
			PlanFormat:       options.PlanFormat,
			Progress:         progressFunc,
		})
		progress.Done()
		if result.DataRoot != "" {
			printEmuMoviesVideoResult(result)
		}
		return err
	case job.Source == "emumovies" && job.Type == "artwork":
		progress, progressFunc := newFetchTaskProgress(job)
		result, err := emumovies.FetchArtwork(emumovies.ArtworkOptions{
			DataRoot:         options.DataRoot,
			MediaTypes:       options.MediaTypes,
			Platform:         options.Platform,
			PlatformRegistry: options.PlatformRegistry,
			Apply:            options.Apply,
			Limit:            options.Limit,
			Concurrency:      concurrency,
			PlanPath:         options.PlanPath,
			PlanFormat:       options.PlanFormat,
			Progress:         progressFunc,
		})
		progress.Done()
		if result.DataRoot != "" {
			printEmuMoviesArtworkResult(result)
		}
		return err
	default:
		return fmt.Errorf("fetch source %q does not support type %q yet", job.Source, job.Type)
	}
}

func fetchConcurrency(options fetchOptions, config store.SourcesConfig, job fetchJob) int {
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

func parallelFetchSupported(job fetchJob) bool {
	if !store.SourceSupportsParallelFetch(job.Source) {
		return false
	}
	if job.Type == "metadata" {
		return job.Source == "hasheous"
	}
	return job.Type == "artwork" || job.Type == "video"
}

func newFetchTaskProgress(job fetchJob) (*terminalProgress, taskprogress.Func) {
	progress := newTerminalProgress(os.Stdout)
	progress.compactTask = true
	return progress, func(snapshot taskprogress.Snapshot) {
		if snapshot.Transfers != nil {
			progress.UpdateTransfers(job.Source+"/"+job.Type, *snapshot.Transfers)
			if snapshot.Stage == "files" {
				return
			}
			if snapshot.Worker > 0 && !progress.interactive {
				return
			}
		}
		if snapshot.Worker > 0 {
			if snapshot.BytesCurrent != 0 || snapshot.BytesTotal != 0 {
				progress.UpdateWorkerBytes(job.Source+"/"+job.Type, snapshot.Stage, snapshot.Worker, snapshot.Workers, snapshot.BytesCurrent, snapshot.BytesTotal, snapshot.Item)
				return
			}
			progress.UpdateWorker(job.Source+"/"+job.Type, snapshot.Stage, snapshot.Worker, snapshot.Workers, snapshot.Item)
			return
		}
		if snapshot.BytesCurrent != 0 || snapshot.BytesTotal != 0 {
			progress.UpdateTaskBytes(job.Source+"/"+job.Type, snapshot.Stage, snapshot.BytesCurrent, snapshot.BytesTotal, snapshot.Item)
			return
		}
		progress.UpdateTask(job.Source+"/"+job.Type, snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
}

func updateFetchMetadataProgress(progress *terminalProgress, job fetchJob, stage string, current, total, queries, matches int, label string) {
	task := job.Source + "/" + job.Type
	if strings.TrimSpace(stage) != "" {
		progress.UpdateTask(task, stage, current, total, label)
		return
	}
	progress.UpdateTask(task, "matching", current, total, fetchMetadataProgressLabel(queries, matches, label))
}

func updateHasheousProgress(progress *terminalProgress, job fetchJob, snapshot hasheous.ImportProgress) {
	if snapshot.Worker > 0 {
		label := snapshot.Game
		if snapshot.Requests > 0 {
			label = fmt.Sprintf("request %d/%d | %s", snapshot.Request, snapshot.Requests, label)
		}
		progress.UpdateWorker(job.Source+"/"+job.Type, snapshot.Stage, snapshot.Worker, snapshot.Workers, label)
		return
	}
	stage := snapshot.Stage
	if stage == "matching" {
		stage = ""
	}
	updateFetchMetadataProgress(progress, job, stage, snapshot.Current, snapshot.Total, snapshot.Queries, snapshot.Matches, snapshot.Game)
}

func fetchMetadataProgressLabel(queries, matches int, label string) string {
	parts := []string{
		fmt.Sprintf("queries=%d", queries),
		fmt.Sprintf("matches=%d", matches),
	}
	if strings.TrimSpace(label) != "" {
		parts = append(parts, label)
	}
	return strings.Join(parts, " | ")
}

func runFetchLaunchBox(options fetchOptions, progress taskprogress.Func) (launchbox.ImportResult, error) {
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

func runFetchOpenVGDB(args []string, progress taskprogress.Func, registry *platformnames.Registry) (openvgdb.ImportResult, error) {
	options := openvgdb.ImportOptions{DataRoot: ".", PlatformRegistry: registry, Progress: progress}
	download := false
	downloadURL := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--apply":
			options.Apply = true
		case arg == "--force-overwrite":
			options.ForceOverwrite = true
		case arg == "--download":
			download = true
		case arg == "--data-root":
			if i+1 >= len(args) {
				return openvgdb.ImportResult{}, fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--platform":
			if i+1 >= len(args) {
				return openvgdb.ImportResult{}, fmt.Errorf("--platform requires a value")
			}
			i++
			options.Platform = args[i]
		case strings.HasPrefix(arg, "--platform="):
			options.Platform = strings.TrimPrefix(arg, "--platform=")
		case arg == "--db":
			if i+1 >= len(args) {
				return openvgdb.ImportResult{}, fmt.Errorf("--db requires a value")
			}
			i++
			options.DBPath = args[i]
		case strings.HasPrefix(arg, "--db="):
			options.DBPath = strings.TrimPrefix(arg, "--db=")
		case arg == "--url":
			if i+1 >= len(args) {
				return openvgdb.ImportResult{}, fmt.Errorf("--url requires a value")
			}
			i++
			downloadURL = args[i]
		case strings.HasPrefix(arg, "--url="):
			downloadURL = strings.TrimPrefix(arg, "--url=")
		default:
			return openvgdb.ImportResult{}, fmt.Errorf("unknown option: %s", arg)
		}
	}

	needsSourceConfig := options.DBPath == "" || (download && downloadURL == "")
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
				if download && downloadURL == "" {
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
	if download {
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

func printOpenVGDBResult(result openvgdb.ImportResult) {
	fmt.Println("OpenVGDB metadata summary")
	printPreservedSources(result.SkippedPreservedCount)
	if result.Snapshot.Checked {
		if result.Snapshot.Downloaded {
			fmt.Printf("Snapshot: downloaded from %s\n", result.Snapshot.URL)
		} else {
			fmt.Println("Snapshot: kept existing")
		}
	}
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d games (matched/not found/total)\n", result.GameFileCount, result.NotFoundGameCount, result.GameBundleCount)
	if result.Apply {
		fmt.Printf("Result: applied; written files: %d\n", result.WrittenFileCount)
		return
	}
	fmt.Println("Result: dry run; add --apply to write source metadata")
}

func printOpenVGDBAssetResult(result openvgdb.AssetResult) {
	fmt.Println("OpenVGDB artwork summary")
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d/%d files (to download/existing/duplicate URL/total)\n", result.PlannedDownloadCount, result.ExistingFileCount, result.DuplicateURLCount, result.CandidateCount)
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.PlanPath != "" {
		fmt.Printf("Plan: %s (%s, %d items)\n", result.PlanPath, result.PlanFormat, result.PlanItemCount)
	}
	if result.ManifestPath != "" {
		fmt.Printf("Manifest: %s (jsonl, %d items)\n", result.ManifestPath, result.ManifestItemCount)
	}
	if result.Apply {
		fmt.Printf("Downloads: %d completed, %d failed\n", result.DownloadedFileCount, result.FailedDownloadCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s -> %s: %s\n", failure.URL, failure.Target, failure.Error)
		}
		printFetchApplied(result.FailedDownloadCount)
		return
	}
	fmt.Println("Result: dry run; add --apply to download source media")
}

func printLaunchBoxResult(result launchbox.ImportResult) {
	fmt.Println("LaunchBox metadata summary")
	printPreservedSources(result.SkippedPreservedCount)
	if result.Snapshot.Checked {
		if result.Snapshot.Downloaded {
			fmt.Printf("Snapshot: downloaded from %s\n", result.Snapshot.URL)
		} else {
			fmt.Println("Snapshot: kept existing")
		}
	}
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d games (matched/not found/total)\n", result.GameFileCount, result.NotFoundGameCount, result.GameBundleCount)
	if result.Apply {
		fmt.Printf("Result: applied; written files: %d\n", result.WrittenFileCount)
		return
	}
	fmt.Println("Result: dry run; add --apply to download the snapshot and write source metadata")
}

func printLaunchBoxAssetResult(result launchbox.AssetResult) {
	fmt.Println("LaunchBox artwork summary")
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d/%d files (to download/existing/duplicate URL/total)\n", result.PlannedDownloadCount, result.ExistingFileCount, result.DuplicateURLCount, result.CandidateCount)
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.PlanPath != "" {
		fmt.Printf("Plan: %s (%s, %d items)\n", result.PlanPath, result.PlanFormat, result.PlanItemCount)
	}
	if result.ManifestPath != "" {
		fmt.Printf("Manifest: %s (jsonl, %d items)\n", result.ManifestPath, result.ManifestItemCount)
	}
	if result.Apply {
		fmt.Printf("Downloads: %d completed, %d failed\n", result.DownloadedFileCount, result.FailedDownloadCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s -> %s: %s\n", failure.URL, failure.Target, failure.Error)
		}
		printFetchApplied(result.FailedDownloadCount)
		return
	}
	fmt.Println("Result: dry run; add --apply to download source media")
}

func printHasheousResult(result hasheous.ImportResult, causes ...error) {
	fmt.Println("Hasheous metadata summary")
	printPreservedSources(result.SkippedPreservedCount)
	fmt.Printf("API: %s\n", result.APIBaseURL)
	fmt.Printf("Workers: %d; request limit: %d/s (shared)\n", result.Concurrency, result.RateLimitPerSecond)
	if result.GameBundleCount == 0 && result.SelectedGameCount > 0 {
		fmt.Printf("Scan interrupted; selected games: %d, skipped existing: %d\n", result.SelectedGameCount, result.SkippedExistingCount)
		fmt.Printf("Pass: %d matched, %d not found\n", result.GameFileCount, result.NotFoundGameCount)
	} else {
		fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
		fmt.Printf("Pass: %d/%d/%d games (matched/not found/total)\n", result.GameFileCount, result.NotFoundGameCount, result.GameBundleCount)
	}
	if result.SkippedWithoutEvidenceCount > 0 {
		fmt.Printf("Skipped: %d games without usable ROM lookup evidence; no not_found records\n", result.SkippedWithoutEvidenceCount)
	}
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.Apply {
		if len(causes) > 0 && causes[0] != nil {
			fmt.Printf("Result: incomplete; %v\n", causes[0])
			return
		}
		fmt.Printf("Lookups: %d planned, %d executed\n", result.PlannedLookupCount, result.QueriedLookupCount)
	} else {
		fmt.Printf("Lookups: %d planned\n", result.PlannedLookupCount)
	}
	if result.FailedLookupCount > 0 {
		fmt.Printf("failed lookups: %d\n", result.FailedLookupCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s via %s: %s\n", failure.Game, failure.Query, failure.Error)
		}
		printOmittedFailures(result.FailedLookupCount, len(result.Failures))
	}
	if result.Apply {
		if result.FailedLookupCount > 0 {
			printFetchApplied(result.FailedLookupCount)
		} else {
			fmt.Printf("Result: applied; written files: %d\n", result.WrittenFileCount)
		}
		return
	}
	fmt.Println("Result: dry run; add --apply to query Hasheous and write source metadata")
}

func printIGDBResult(result igdb.ImportResult, causes ...error) {
	for _, row := range result.LinkRows {
		fmt.Println(row)
	}
	if len(result.LinkRows) > 0 {
		fmt.Printf("Identity links: %d reviewed cards; %d pending, %d unchanged; %d written. Official queries: %d remote, %d cached. No metadata/media rescrape.\n", result.GameBundleCount, result.SelectedGameCount, result.SkippedExistingCount, result.WrittenFileCount, result.QueriedSearchCount, result.CachedSearchCount)
		return
	}
	fmt.Println("IGDB metadata summary")
	printPreservedSources(result.SkippedPreservedCount)
	fmt.Printf("API: %s\n", result.APIBaseURL)
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d games (matched/not found/total)\n", result.GameFileCount, result.NotFoundGameCount, result.GameBundleCount)
	if result.TokenChecked {
		if result.UsedCachedToken {
			fmt.Println("Token: kept cached")
		} else {
			fmt.Println("Token: refreshed")
		}
	}
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.Apply {
		if len(causes) > 0 && causes[0] != nil {
			fmt.Printf("Result: incomplete; %v\n", causes[0])
			return
		}
		fmt.Printf("Queries: %d planned, %d network, %d cached\n", result.PlannedSearchCount, result.QueriedSearchCount, result.CachedSearchCount)
		if result.UsedCachedPlatforms {
			fmt.Println("Platforms: kept cached IGDB list")
		}
	} else {
		fmt.Printf("Queries: %d planned\n", result.PlannedSearchCount)
	}
	if result.FailedSearchCount > 0 {
		fmt.Printf("failed searches: %d\n", result.FailedSearchCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s via %s: %s\n", failure.Game, failure.Query, failure.Error)
		}
		printOmittedFailures(result.FailedSearchCount, len(result.Failures))
	}
	if result.Apply {
		if result.FailedSearchCount > 0 {
			printFetchApplied(result.FailedSearchCount)
		} else {
			fmt.Printf("Result: applied; written files: %d\n", result.WrittenFileCount)
		}
		return
	}
	fmt.Println("Result: dry run; add --apply to search IGDB and write source metadata")
}

func printScreenScraperResult(result screenscraper.ImportResult, causes ...error) {
	fmt.Println("ScreenScraper metadata summary")
	printPreservedSources(result.SkippedPreservedCount)
	fmt.Printf("API: %s\n", result.APIBaseURL)
	if result.PlatformListCount > 0 {
		source := "fetched"
		if result.UsedCachedPlatforms {
			source = "cached"
		}
		fmt.Printf("Platforms: %d source systems (%s)\n", result.PlatformListCount, source)
	}
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d games (matched/not found/total)\n", result.GameFileCount, result.NotFoundGameCount, result.GameBundleCount)
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.Apply {
		if len(causes) > 0 && causes[0] != nil {
			fmt.Printf("Result: incomplete; %v\n", causes[0])
			return
		}
		fmt.Printf("Queries: %d planned, %d executed\n", result.PlannedSearchCount, result.QueriedSearchCount)
	} else {
		fmt.Printf("Queries: %d planned\n", result.PlannedSearchCount)
	}
	if result.SkippedUnmappedCount > 0 {
		fmt.Printf("Skipped: %d games without a ScreenScraper platform mapping\n", result.SkippedUnmappedCount)
		platforms := make([]string, 0, len(result.UnmappedPlatforms))
		for platform := range result.UnmappedPlatforms {
			platforms = append(platforms, platform)
		}
		sort.Strings(platforms)
		for _, platform := range platforms {
			fmt.Printf("  %s: %d games\n", platform, result.UnmappedPlatforms[platform])
		}
	}
	if result.FailedSearchCount > 0 {
		fmt.Printf("failed searches: %d\n", result.FailedSearchCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s via %s: %s\n", failure.Game, failure.Query, failure.Error)
		}
		printOmittedFailures(result.FailedSearchCount, len(result.Failures))
	}
	if result.Apply {
		if result.FailedSearchCount > 0 {
			printFetchApplied(result.FailedSearchCount)
		} else {
			fmt.Printf("Result: applied; written files: %d\n", result.WrittenFileCount)
		}
		return
	}
	fmt.Println("Result: dry run; add --apply to search ScreenScraper and write source metadata")
}

func printOmittedFailures(total, shown int) {
	if line := omittedFailuresLine(total, shown); line != "" {
		fmt.Println(line)
	}
}

func omittedFailuresLine(total, shown int) string {
	omitted := total - shown
	if omitted <= 0 {
		return ""
	}
	label := "failures"
	if omitted == 1 {
		label = "failure"
	}
	return fmt.Sprintf("  ... %d more %s omitted", omitted, label)
}

func printSourceAssetResult(result sourceassets.Result) {
	fmt.Printf("%s artwork summary\n", sourceDisplayName(result.Source))
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameBundleCount)
	fmt.Printf("Pass: %d/%d/%d/%d files (to download/existing/duplicate URL/total)\n", result.PlannedDownloadCount, result.ExistingFileCount, result.DuplicateURLCount, result.CandidateCount)
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.PlanPath != "" {
		fmt.Printf("Plan: %s (%s, %d items)\n", result.PlanPath, result.PlanFormat, result.PlanItemCount)
	}
	printDownloadReport(result.ManifestPath, result.ManifestItemCount, result.FailedDownloadCount)
	if result.Apply {
		fmt.Printf("Downloads: %d completed, %d failed\n", result.DownloadedFileCount, result.FailedDownloadCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s -> %s: %s\n", failure.URL, failure.Target, failure.Error)
		}
		if result.StopReason != "" {
			fmt.Printf("Result: incomplete; %s\n", result.StopReason)
		} else {
			fmt.Println("Result: applied")
		}
		return
	}
	fmt.Println("Result: dry run; add --apply to download source media")
}

func printEmuMoviesVideoResult(result emumovies.VideoResult) {
	fmt.Println("EmuMovies video summary")
	printEmuMoviesSkippedPlatforms(result.SkippedPlatforms)
	fmt.Printf("FTP: %s/%s\n", strings.TrimRight(result.FTPHost, "/"), strings.TrimLeft(result.FTPDirectory, "/"))
	fmt.Printf("video quality: %s\n", result.VideoQuality)
	fmt.Printf("Scanned: %d platforms, %d games; remote files: %d\n", result.PlatformCount, result.GameBundleCount, result.VideoFileCount)
	fmt.Printf("Pass: %d/%d/%d videos (to download/existing/matched)\n", result.PlannedDownloadCount, result.ExistingFileCount, result.MatchedVideoCount)
	if result.Limit > 0 {
		fmt.Printf("Limit: %d; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.PlanPath != "" {
		fmt.Printf("Plan: %s (%s, %d items)\n", result.PlanPath, result.PlanFormat, result.PlanItemCount)
	}
	printDownloadReport(result.ManifestPath, result.ManifestItemCount, result.FailedDownloadCount)
	if result.Apply {
		fmt.Printf("Downloads: %d completed, %d failed\n", result.DownloadedFileCount, result.FailedDownloadCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s -> %s: %s\n", failure.SourcePath, failure.Target, failure.Error)
		}
		if result.StopReason != "" {
			fmt.Printf("Result: incomplete; %s\n", result.StopReason)
		} else {
			printFetchApplied(result.FailedDownloadCount)
		}
		return
	}
	fmt.Println("Result: dry run; add --apply to download EmuMovies videos")
}

func printEmuMoviesArtworkResult(result emumovies.ArtworkResult) {
	fmt.Println("EmuMovies artwork summary")
	printEmuMoviesSkippedPlatforms(result.SkippedPlatforms)
	fmt.Printf("FTP: %s/%s\n", strings.TrimRight(result.FTPHost, "/"), strings.TrimLeft(result.FTPDirectory, "/"))
	fmt.Printf("archive extractor: %s\n", result.ArchiveExtractor)
	if len(result.MediaTypes) > 0 {
		fmt.Printf("media types: %s\n", strings.Join(result.MediaTypes, ", "))
	}
	fmt.Printf("Scanned: %d platforms, %d games; directory entries: %d\n", result.PlatformCount, result.GameBundleCount, result.DirectoryEntryCount)
	fmt.Printf("Packages: %d candidates, %d selected, %d existing, %d planned, %d needing extractor\n", result.PackageCandidateCount, result.SelectedPackageCount, result.ExistingPackageCount, result.PlannedPackageCount, result.NeedsExtractorPackageCount)
	if result.NeedsExtractorRARCount > 0 || result.NeedsExtractor7zCount > 0 {
		fmt.Printf("archives needing extractor: rar=%d 7z=%d\n", result.NeedsExtractorRARCount, result.NeedsExtractor7zCount)
	}
	if result.Limit > 0 {
		fmt.Printf("Limit: %d packages; skipped by limit: %d\n", result.Limit, result.SkippedByLimitCount)
	}
	if result.PlanPath != "" {
		fmt.Printf("Plan: %s (%s, %d items)\n", result.PlanPath, result.PlanFormat, result.PlanItemCount)
	}
	printDownloadReport(result.ManifestPath, result.ManifestItemCount, result.FailedDownloadCount)
	if result.Apply {
		fmt.Printf("Downloads: %d packages completed, %d failed\n", result.DownloadedPackageCount, result.FailedDownloadCount)
		for _, failure := range result.Failures {
			fmt.Printf("  failed: %s -> %s: %s\n", failure.SourcePath, failure.Target, failure.Error)
		}
		fmt.Printf("Extracted: %d packages, %d files\n", result.ExtractedPackageCount, result.ExtractedFileCount)
		fmt.Printf("Artwork: %d indexed, %d matched, %d unmatched, %d existing, %d written\n", result.IndexedArtworkFileCount, result.MatchedArtworkFileCount, result.UnmatchedArtworkFileCount, result.ExistingArtworkFileCount, result.WrittenArtworkFileCount)
		if result.InvalidArtworkFileCount > 0 {
			fmt.Printf("Invalid artwork sources: %d skipped\n", result.InvalidArtworkFileCount)
			for _, failure := range result.InvalidArtworkFiles {
				fmt.Printf("  skipped: %s -> %s: %s\n", failure.SourcePath, failure.Target, failure.Error)
			}
		}
		if result.UnmatchedManifestPath != "" {
			fmt.Printf("Unmatched report: %s\n", result.UnmatchedManifestPath)
		}
		if result.StopReason != "" {
			fmt.Printf("Result: incomplete; %s\n", result.StopReason)
		} else {
			printFetchApplied(result.FailedDownloadCount + result.InvalidArtworkFileCount)
		}
		return
	}
	fmt.Println("Result: dry run; add --apply to download and extract EmuMovies artwork packages")
}

func printFetchUsage() {
	fmt.Println(`rom-meta fetch downloads or plans source-specific metadata, artwork, and video.

Usage:
  rom-meta fetch [--data-root <root>] [--platform <name>] [--type metadata|artwork|video] [--source <source>] [--media-type <name>] [--limit <n>] [--concurrency <n>] [--plan <path>] [--plan-format jsonl|aria2] [--apply] [--force-overwrite]

Options:
  --data-root <root>       Data root containing config/ and platforms/.
  --platform <name>        Limit to one platform. Built-in aliases such as segacd, n64, or pcengine are accepted.
  --type <type>            Fetch metadata, artwork, or video. May be repeated or comma-separated.
  --source <source>        Fetch from a specific source. May be repeated or comma-separated.
  --media-type <name>      Limit EmuMovies artwork package selection to one media type.
  --limit <n>              Limit planned downloads or imported games for a trial run.
  --concurrency <n>        Override worker count for media and Hasheous metadata.
  --plan <path>            Write a portable external downloader plan.
  --plan-format <format>   Plan format: jsonl or aria2.
  --apply                  Download/import instead of dry-run planning.
  --force-overwrite        Refresh source metadata except fetch.preserve_sources.
  --refresh-relations      Update only relationships on existing IGDB metadata matches.
  --refresh-links          Query reviewed known IDs; fill minimal identity links only.
  --link-review <path>     Scoped SHA-bound review JSON (required by refresh-links).

Config:
  Without --source, fetch selects sources from fetch.<type> in order,
  per catalog platform. Missing platform mappings and unusable Hasheous evidence
  are skipped with a reason, without requests or not_found records.
  config/platforms.yaml can add mappings. Explicit --source uses manual selection.
  Automatic --limit applies per source/type/platform task.
  Applicable sources still require their normal credentials and service access.

  fetch.concurrency is the global default.
  sources.<source>.concurrency overrides it for one source.
  platforms.aliases can add local CLI platform aliases.
  Sources marked unsafe for parallel fetches are still forced to one worker.
  Hasheous metadata workers share sources.hasheous.api.rate_limit_per_second.

  Metadata fetch skips games that already have that source stanza in game.yaml.
  Use --force-overwrite to refresh existing source metadata.
  Per-card fetch.preserve_sources always keeps reviewed source stanzas.`)
}

func printEmuMoviesSkippedPlatforms(skipped map[string]string) {
	if len(skipped) == 0 {
		return
	}
	names := make([]string, 0, len(skipped))
	for name := range skipped {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("Skipped platforms without a media directory: %d\n", len(names))
	for _, name := range names {
		fmt.Printf("  %s\n", name)
	}
}

func printFetchApplied(failed int) {
	if failed > 0 {
		fmt.Printf("Result: incomplete; %d operations failed; successful results retained\n", failed)
	} else {
		fmt.Println("Result: applied")
	}
}
func printDownloadReport(path string, count, failed int) {
	if path == "" {
		return
	}
	fmt.Printf("Download report: %s (jsonl, %d items)\n", path, count)
	if failed > 0 {
		fmt.Println("Retry: rerun the same fetch command; existing files are skipped")
	}
}

func printPreservedSources(count int) {
	if count > 0 {
		fmt.Printf("Preserved: %d source stanzas (fetch.preserve_sources)\n", count)
	}
}
