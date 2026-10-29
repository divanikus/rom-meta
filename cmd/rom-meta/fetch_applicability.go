package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/hasheous"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

// Explicit sources retain the manual workflow. Automatic jobs are scoped to
// actual catalog directories so one unsupported platform cannot affect another.
func selectFetchJobs(jobs []fetchJob, options fetchOptions, config store.SourcesConfig) ([]fetchJob, error) {
	if len(options.Sources) > 0 {
		return jobs, nil
	}
	platforms, err := automaticFetchPlatforms(options)
	if err != nil {
		return nil, err
	}
	registry := platformnames.DefaultRegistry()
	if options.PlatformRegistry != nil {
		registry = *options.PlatformRegistry
	}
	var selected []fetchJob
	for _, job := range jobs {
		for _, platform := range platforms {
			scoped := job
			scoped.Platform = platform
			scoped.Automatic = true
			scoped.SkipReason = automaticFetchMappingReason(job, platform, registry, config)
			selected = append(selected, scoped)
		}
	}
	return selected, nil
}

func automaticFetchPlatforms(options fetchOptions) ([]string, error) {
	entries, err := catalogfs.ReadDir(filepath.Join(options.DataRoot, "platforms"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	registry := platformnames.DefaultRegistry()
	if options.PlatformRegistry != nil {
		registry = *options.PlatformRegistry
	}
	input := strings.TrimSpace(options.Platform)
	var platforms, matches []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		platforms = append(platforms, entry.Name())
		if input != "" && strings.EqualFold(entry.Name(), input) {
			return []string{entry.Name()}, nil
		}
		if input != "" && registry.ResolveCLI(entry.Name()) == registry.ResolveCLI(input) {
			matches = append(matches, entry.Name())
		}
	}
	if input == "" {
		return platforms, nil
	}
	switch len(matches) {
	case 1:
		return matches, nil
	case 0:
		return nil, fmt.Errorf("no catalog platform directory matches %q", input)
	default:
		return nil, fmt.Errorf("platform alias %q matches multiple directories: %s; use an exact directory name", input, strings.Join(matches, ", "))
	}
}

func automaticFetchMappingReason(job fetchJob, platform string, registry platformnames.Registry, config store.SourcesConfig) string {
	configured := false
	for i := range config.Sources {
		if config.Sources[i].Name == job.Source {
			configured = true
			break
		}
	}
	if !configured {
		return "source is not configured in config/sources.yaml"
	}
	// Hasheous identifies ROMs by evidence, independently of platform mappings.
	if job.Source == "hasheous" {
		return ""
	}
	entry, known := registry.Entry(platform)
	if known {
		ref := entry.Sources[job.Source]
		if strings.TrimSpace(ref.Name) != "" || len(ref.Names) > 0 || (job.Source == "screenscraper" && strings.TrimSpace(ref.ID) != "") {
			return ""
		}
	}
	return fmt.Sprintf("no %s platform mapping for %q; add one in config/platforms.yaml or select --source explicitly", job.Source, platform)
}

var errFetchEvidenceFound = errors.New("fetch lookup evidence found")

// Probe pending cards locally, using exactly the same lookup rules as Hasheous.
// Stop at the first usable card rather than scanning a large ROM catalog twice.
func automaticFetchEvidenceReason(out io.Writer, job fetchJob, options fetchOptions) (string, error) {
	if !job.Automatic || job.Source != "hasheous" {
		return "", nil
	}
	var update taskprogress.Func
	if file, ok := out.(*os.File); ok {
		progress := newTerminalProgress(file)
		defer progress.Done()
		update = func(snapshot taskprogress.Snapshot) {
			progress.UpdateTask("Hasheous applicability", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
		}
	}
	_, _, err := catalogcache.ForEachGameWithOptions(options.DataRoot,
		catalogcache.MetadataReadOptions([]string{options.Platform}, "hasheous", options.ForceOverwrite),
		update, func(game catalogcache.Game) error {
			if catalogcache.HasSource(game.PreservedSources, "hasheous") {
				return nil
			}
			if !options.ForceOverwrite && catalogcache.HasSource(game.Sources, "hasheous") {
				return nil
			}
			if hasheous.HasLookupEvidence(game) {
				return errFetchEvidenceFound
			}
			return nil
		})
	if errors.Is(err, errFetchEvidenceFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return "no pending games with usable ROM hashes (MAME requires set identity and component SHA1); no lookups or not_found records", nil
}
