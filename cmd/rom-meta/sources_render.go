package main

import (
	"fmt"
	"os"
	"strings"

	"rommeta/internal/sourceaudit"
	"rommeta/internal/store"
)

func printSourcesList(config store.SourcesConfig) {
	fmt.Print(renderSourcesList(config, outputColorEnabled(os.Stdout)))
}

func renderSourcesList(config store.SourcesConfig, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf("Source configuration", ansiCyan, color))
	fmt.Fprintf(&b, "Library: %s\n", config.Root)
	fmt.Fprintf(&b, "source config: %s\n", config.Path)
	if len(config.Fetch) > 0 {
		fmt.Fprintf(&b, "fetch defaults:\n")
		for _, typ := range []string{"metadata", "artwork", "video"} {
			if sources := config.Fetch[typ]; len(sources) > 0 {
				fmt.Fprintf(&b, "  %s: %s\n", colorizeIf(typ, ansiGreen, color), strings.Join(sources, ", "))
			}
		}
		if config.FetchConcurrency > 0 {
			fmt.Fprintf(&b, "  concurrency: %d\n", config.FetchConcurrency)
		}
		fmt.Fprintln(&b)
	}
	if len(config.Sources) == 0 {
		fmt.Fprintln(&b, "sources: none")
		return b.String()
	}
	fmt.Fprintf(&b, "%s\n", colorizeIf("sources", ansiCyan, color))
	for _, source := range config.Sources {
		fmt.Fprintf(&b, "%s\n", colorizeIf(source.Name, ansiCyan, color))
		if provides := providedSourceTypes(source); len(provides) > 0 {
			fmt.Fprintf(&b, "  provides: %s\n", colorizedList(provides, ansiGreen, color))
		}
		if source.SnapshotPath != "" {
			fmt.Fprintf(&b, "  snapshot: %s\n", source.SnapshotPath)
		}
		if source.SnapshotURL != "" {
			fmt.Fprintf(&b, "  snapshot url: %s\n", source.SnapshotURL)
		}
		if source.APIBaseURL != "" {
			fmt.Fprintf(&b, "  endpoint: %s\n", source.APIBaseURL)
		}
		if source.ArchiveExtractor != "" {
			fmt.Fprintf(&b, "  archive extractor: %s\n", source.ArchiveExtractor)
		}
		if source.FetchConcurrency > 0 {
			fmt.Fprintf(&b, "  concurrency: %d\n", source.FetchConcurrency)
		}
		_, missing := store.CredentialStatus(config, source)
		if len(missing) > 0 {
			fmt.Fprintf(&b, "  %s: %s\n", colorizeIf("missing credentials", ansiRed, color), credentialNames(missing))
		}
	}
	return b.String()
}

func providedSourceTypes(source store.Source) []string {
	if provides := store.SourceProvides(source.Name); len(provides) > 0 {
		return provides
	}
	return configuredSourceTypes(source)
}

func configuredSourceTypes(source store.Source) []string {
	var result []string
	for _, typ := range []string{"metadata", "artwork", "video"} {
		if source.Levels[typ] {
			result = append(result, typ)
		}
	}
	return result
}

func colorizedList(values []string, colorCode string, color bool) string {
	if !color {
		return strings.Join(values, ", ")
	}
	colored := make([]string, 0, len(values))
	for _, value := range values {
		colored = append(colored, colorize(value, colorCode))
	}
	return strings.Join(colored, ", ")
}

func credentialNames(values []store.CredentialStatusItem) string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Name)
	}
	return strings.Join(names, ", ")
}

func printSourceCoverageReport(report sourceaudit.CoverageReport) {
	fmt.Print(renderSourceCoverageReport(report, outputColorEnabled(os.Stdout)))
}

func renderSourceCoverageReport(report sourceaudit.CoverageReport, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf("Source coverage", ansiCyan, color))
	catalogField(&b, "Library", report.DataRoot, color)
	if report.PlatformFilter != "" {
		catalogField(&b, "Platform", report.PlatformFilter, color)
	}
	if report.SourceFilter != "" {
		catalogField(&b, "Source", report.SourceFilter, color)
	}
	fmt.Fprintf(&b, "%s: %s platforms, %s games, %s sources\n",
		catalogLabel("Catalog", color),
		formatCount(report.PlatformCount),
		formatCount(report.GameCount),
		formatCount(len(report.Sources)),
	)
	fmt.Fprintf(&b, "%s: %d\n", catalogLabel("Low score threshold", color), report.LowScore)
	renderSourceCoverageTable(&b, report.Sources, report.GameCount, color)
	for _, source := range report.Sources {
		if report.Details {
			renderCoverageDetails(&b, source, color)
		}
	}
	if !report.Details {
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "%s: rerun with --details to list missing, stale, and low-confidence items\n", catalogLabel("Next", color))
	}
	return b.String()
}

func renderSourceCoverageTable(b *strings.Builder, sources []sourceaudit.SourceCoverage, totalGames int, color bool) {
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s\n", colorizeIf("Sources", ansiCyan, color))
	if len(sources) == 0 {
		fmt.Fprintln(b, "  none")
		return
	}
	sourceWidth := len("source")
	checkedWidth := len("checked")
	matchedWidth := len("matched")
	notMatchedWidth := len("not matched")
	missingWidth := len("missing")
	entriesWidth := len("entries")
	staleWidth := len("stale")
	lowScoreWidth := len("low score")
	for _, source := range sources {
		sourceWidth = maxInt(sourceWidth, runeWidth(source.Source))
		checkedWidth = maxInt(checkedWidth, runeWidth(formatCountWithPercent(source.CoveredGameCount, totalGames)))
		matchedWidth = maxInt(matchedWidth, runeWidth(formatCountWithPercent(source.MatchedGameCount, totalGames)))
		notMatchedWidth = maxInt(notMatchedWidth, runeWidth(formatCountWithPercent(sourceNotMatchedGameCount(source), totalGames)))
		missingWidth = maxInt(missingWidth, runeWidth(formatCountWithPercent(source.MissingGameCount, totalGames)))
		entriesWidth = maxInt(entriesWidth, runeWidth(formatCount(source.MatchCount)))
		staleWidth = maxInt(staleWidth, runeWidth(formatCount(source.StaleMatchCount)))
		lowScoreWidth = maxInt(lowScoreWidth, runeWidth(formatCount(source.LowConfidenceCount)))
	}
	fmt.Fprintf(b, "  %s  %s  %s  %s  %s  %s  %s  %s\n",
		catalogTableCell("source", sourceWidth, alignLeft, ansiYellow, color),
		catalogTableCell("checked", checkedWidth, alignRight, ansiYellow, color),
		catalogTableCell("matched", matchedWidth, alignRight, ansiYellow, color),
		catalogTableCell("not matched", notMatchedWidth, alignRight, ansiYellow, color),
		catalogTableCell("missing", missingWidth, alignRight, ansiYellow, color),
		catalogTableCell("entries", entriesWidth, alignRight, ansiYellow, color),
		catalogTableCell("stale", staleWidth, alignRight, ansiYellow, color),
		catalogTableCell("low score", lowScoreWidth, alignRight, ansiYellow, color),
	)
	for _, source := range sources {
		notMatched := sourceNotMatchedGameCount(source)
		fmt.Fprintf(b, "  %s  %s  %s  %s  %s  %s  %s  %s\n",
			catalogTableCell(source.Source, sourceWidth, alignLeft, ansiCyan, color),
			catalogTableCell(formatCountWithPercent(source.CoveredGameCount, totalGames), checkedWidth, alignRight, "", color),
			catalogTableCell(formatCountWithPercent(source.MatchedGameCount, totalGames), matchedWidth, alignRight, sourceMatchedColor(source.MatchedGameCount), color),
			catalogTableCell(formatCountWithPercent(notMatched, totalGames), notMatchedWidth, alignRight, countAlertColor(notMatched), color),
			catalogTableCell(formatCountWithPercent(source.MissingGameCount, totalGames), missingWidth, alignRight, countAlertColor(source.MissingGameCount), color),
			catalogTableCell(formatCount(source.MatchCount), entriesWidth, alignRight, "", color),
			catalogTableCell(formatCount(source.StaleMatchCount), staleWidth, alignRight, countAlertColor(source.StaleMatchCount), color),
			catalogTableCell(formatCount(source.LowConfidenceCount), lowScoreWidth, alignRight, countAlertColor(source.LowConfidenceCount), color),
		)
	}
}

func sourceNotMatchedGameCount(source sourceaudit.SourceCoverage) int {
	value := source.CoveredGameCount - source.MatchedGameCount
	if value < 0 {
		return 0
	}
	return value
}

func sourceMatchedColor(value int) string {
	if value == 0 {
		return ansiRed
	}
	return ansiGreen
}

func renderCoverageDetails(b *strings.Builder, source sourceaudit.SourceCoverage, color bool) {
	if len(source.MissingGames) == 0 && len(source.StaleMatches) == 0 && len(source.LowConfidenceMatches) == 0 {
		return
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s\n", colorizeIf(source.Source+" details", ansiCyan, color))
	if len(source.MissingGames) > 0 {
		fmt.Fprintf(b, "  %s:\n", colorizeIf("missing", ansiYellow, color))
		for _, game := range source.MissingGames {
			fmt.Fprintf(b, "    - %s\n", coverageGameLabel(game))
		}
	}
	if len(source.StaleMatches) > 0 {
		fmt.Fprintf(b, "  %s:\n", colorizeIf("stale release refs", ansiYellow, color))
		for _, match := range source.StaleMatches {
			fmt.Fprintf(b, "    - %s -> %s\n", coverageGameLabel(match.GameRef), match.Release)
		}
	}
	if len(source.LowConfidenceMatches) > 0 {
		fmt.Fprintf(b, "  %s:\n", colorizeIf("low-confidence", ansiYellow, color))
		for _, match := range source.LowConfidenceMatches {
			release := match.Release
			if release == "" {
				release = "(no release)"
			}
			fmt.Fprintf(b, "    - %s -> %s score=%d\n", coverageGameLabel(match.GameRef), release, match.Score)
		}
	}
}

func coverageGameLabel(game sourceaudit.GameRef) string {
	if game.Platform == "" {
		return game.Game
	}
	return game.Platform + " / " + game.Game
}

func printSourcesUsage() {
	fmt.Println(`rom-meta sources inspects configured metadata and media sources.

Usage:
  rom-meta sources list [--data-root <root>]
  rom-meta sources coverage [--data-root <root>] [--platform <name>] [--source <source>] [--low-score <n>] [--details]

Commands:
  list      Show fetch defaults, source endpoints, and missing credentials.
  coverage  Report source metadata coverage, stale release refs, and low-confidence matches.`)
}

func printSourcesListUsage() {
	fmt.Println(`rom-meta sources list shows source configuration without printing secrets.

Usage:
  rom-meta sources list [--data-root <root>]

Options:
  --data-root <root>  Data root containing config/sources.yaml and optional config/secrets.yaml.`)
}

func printSourcesCoverageUsage() {
	fmt.Println(`rom-meta sources coverage audits source metadata already written to game.yaml.

Usage:
  rom-meta sources coverage [--data-root <root>] [--platform <name>] [--source <source>] [--low-score <n>] [--details]

Options:
  --data-root <root>  Data root containing platforms/.
  --platform <name>   Limit to one platform. Short aliases such as n64 or segacd are accepted.
  --source <source>   Limit to one source, such as launchbox, igdb, or screenscraper.
  --low-score <n>     Threshold for low-confidence scored matches.
  --details           Include missing, stale, and low-confidence item lists.`)
}
