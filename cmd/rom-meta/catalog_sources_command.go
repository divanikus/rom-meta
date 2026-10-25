package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"rommeta/internal/catalogfold"
	"rommeta/internal/catalogstate"
	"rommeta/internal/platformnames"
	"rommeta/internal/sourceaudit"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

func runCatalog(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printCatalogUsage()
		return nil
	}
	switch args[0] {
	case "status":
		return runCatalogStatus(args[1:])
	case "prune-empty":
		return runCatalogPruneEmpty(args[1:])
	case "media-names":
		return runCatalogMediaNames(args[1:])
	case "split":
		return runCatalogSplit(args[1:])
	case "fold":
		return runCatalogFold(args[1:])
	default:
		return fmt.Errorf("unknown catalog command: %s", args[0])
	}
}

func runCatalogStatus(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printCatalogStatusUsage()
		return nil
	}
	options := catalogstate.StatusOptions{DataRoot: "."}
	details := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--details":
			details = true
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires a value")
			}
			i++
			options.Platform = platformnames.ResolveCLI(args[i])
		case strings.HasPrefix(arg, "--platform="):
			options.Platform = platformnames.ResolveCLI(strings.TrimPrefix(arg, "--platform="))
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}

	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(snapshot taskprogress.Snapshot) {
		progress.UpdateTask("catalog status", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	result, err := catalogstate.BuildStatus(options)
	progress.Done()
	if err != nil {
		return err
	}
	printCatalogStatus(result, details)
	return nil
}

func runCatalogPruneEmpty(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printCatalogPruneEmptyUsage()
		return nil
	}
	options := catalogstate.PruneOptions{DataRoot: "."}
	details := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--apply":
			options.Apply = true
		case arg == "--details":
			details = true
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires a value")
			}
			i++
			options.Platform = platformnames.ResolveCLI(args[i])
		case strings.HasPrefix(arg, "--platform="):
			options.Platform = platformnames.ResolveCLI(strings.TrimPrefix(arg, "--platform="))
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}

	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(snapshot taskprogress.Snapshot) {
		progress.UpdateTask("catalog prune-empty", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	result, err := catalogstate.PruneEmpty(options)
	progress.Done()
	if err != nil {
		return err
	}
	printCatalogPruneEmptyResult(result, details)
	return nil
}

func runCatalogFold(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printCatalogFoldUsage()
		return nil
	}
	options := catalogfold.Options{DataRoot: "."}
	details := false
	interactive := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isHelpArg(arg) {
			printCatalogFoldUsage()
			return nil
		}
		switch {
		case arg == "--review-plan":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return fmt.Errorf("--review-plan requires a value")
			}
			i++
			options.ReviewPlan = args[i]
		case strings.HasPrefix(arg, "--review-plan="):
			options.ReviewPlan = strings.TrimPrefix(arg, "--review-plan=")
			if strings.TrimSpace(options.ReviewPlan) == "" {
				return fmt.Errorf("--review-plan requires a value")
			}
		case arg == "--editions-only":
			options.EditionsOnly = true
		case arg == "--apply":
			options.Apply = true
		case arg == "--interactive":
			interactive = true
		case arg == "--details":
			details = true
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires a value")
			}
			i++
			options.Platform = args[i]
		case strings.HasPrefix(arg, "--platform="):
			options.Platform = strings.TrimPrefix(arg, "--platform=")
		case arg == "--match":
			if i+1 >= len(args) {
				return fmt.Errorf("--match requires a value")
			}
			i++
			options.Match = args[i]
		case strings.HasPrefix(arg, "--match="):
			options.Match = strings.TrimPrefix(arg, "--match=")
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}
	if options.ReviewPlan != "" && (interactive || options.Platform != "" || options.Match != "" || options.EditionsOnly) {
		return fmt.Errorf("--review-plan cannot be combined with --platform, --match, --editions-only or --interactive")
	}
	if interactive && options.Apply {
		return fmt.Errorf("--interactive and --apply cannot be used together")
	}

	catalogConfig, err := store.LoadCatalogConfig(options.DataRoot)
	if err != nil {
		return err
	}
	registry, _, err := platformnames.LoadRegistry(catalogConfig.Root)
	if err != nil {
		return err
	}
	options.DataRoot = catalogConfig.Root
	options.RegionPriority = catalogConfig.Fold.RegionPriority
	options.RegionGroups = catalogConfig.Fold.RegionGroups
	if options.Platform != "" {
		options.Platform, err = catalogPlatformDirectory(options.DataRoot, options.Platform)
		if err != nil {
			return err
		}
	}
	if len(catalogConfig.Fold.Platforms) > 0 {
		options.PlatformRegionPriority = map[string][]string{}
		for platformName, platformConfig := range catalogConfig.Fold.Platforms {
			resolved := registry.ResolveCLI(platformName)
			if strings.TrimSpace(resolved) == "" {
				continue
			}
			options.PlatformRegionPriority[resolved] = platformConfig.RegionPriority
		}
	}
	if !details && !interactive {
		fmt.Print(renderCatalogFoldStartHint(details, outputColorEnabled(os.Stdout)))
	}

	setupProgress := newTerminalProgress(os.Stdout)
	var platformProgress *terminalProgress
	currentPlatform := ""
	reviewReader := bufio.NewReader(os.Stdin)
	if interactive {
		options.Review = func(prompt catalogfold.ReviewPrompt) (catalogfold.ReviewDecision, error) {
			if platformProgress != nil {
				platformProgress.Done()
			}
			return promptCatalogFoldReview(prompt, reviewReader, os.Stdout, outputColorEnabled(os.Stdout))
		}
	}
	options.Progress = func(snapshot taskprogress.Snapshot) {
		if currentPlatform != "" && platformProgress != nil {
			platformProgress.UpdateTask(currentPlatform, snapshot.Stage, snapshot.Current, snapshot.Total, catalogFoldProgressItem(snapshot.Item, currentPlatform))
			return
		}
		setupProgress.UpdateTask("catalog fold", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	options.OnPlatformStart = func(index, total int, platform string) {
		setupProgress.Done()
		if platformProgress != nil {
			platformProgress.Done()
		}
		fmt.Printf("[%d/%d] %s\n", index, total, platform)
		platformProgress = newTerminalProgress(os.Stdout)
		currentPlatform = platform
	}
	options.OnPlatformResult = func(platformResult catalogfold.PlatformResult) {
		if platformProgress != nil {
			platformProgress.Done()
		}
		fmt.Print(renderCatalogFoldPlatformResult(platformResult, options.Apply, interactive, outputColorEnabled(os.Stdout)))
		platformProgress = nil
		currentPlatform = ""
	}
	result, err := catalogfold.Fold(options)
	if platformProgress != nil {
		platformProgress.Done()
	} else {
		setupProgress.Done()
	}
	if err != nil {
		return err
	}
	printCatalogFoldResult(result, details)
	return nil
}

func catalogFoldProgressItem(item, platform string) string {
	prefix := platform + " / "
	if strings.HasPrefix(item, prefix) {
		return strings.TrimPrefix(item, prefix)
	}
	return item
}

func promptCatalogFoldReview(prompt catalogfold.ReviewPrompt, reader *bufio.Reader, out io.Writer, color bool) (catalogfold.ReviewDecision, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[%d/%d] fold candidate\n", prompt.Index, prompt.Total)
	renderCatalogFoldGroupDetails(&b, prompt.Group, color)
	fmt.Fprint(out, b.String())
	fmt.Fprintln(out, "yes merges this group; no saves rejection of this evidence; skip leaves it undecided; quit stops review.")
	if prompt.Group.ReviewRequired != "" {
		fmt.Fprintln(out, "yes confirms this grouping; no rejects its serial link and retains existing provider evidence.")
	}
	for {
		fmt.Fprint(out, "Apply fold? yes/no/skip/quit: ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return catalogfold.ReviewSkip, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "y", "yes":
			return catalogfold.ReviewApply, nil
		case "n", "no":
			return catalogfold.ReviewIgnoreEvidence, nil
		case "", "s", "skip":
			return catalogfold.ReviewSkip, nil
		case "q", "quit", "stop":
			return catalogfold.ReviewQuit, nil
		default:
			fmt.Fprintln(out, "Please answer yes/no/skip/quit.")
		}
		if err == io.EOF {
			return catalogfold.ReviewSkip, nil
		}
	}
}

func runSources(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printSourcesUsage()
		return nil
	}
	switch args[0] {
	case "list":
		return runSourcesList(args[1:])
	case "coverage":
		return runSourcesCoverage(args[1:])
	default:
		return fmt.Errorf("unknown sources command: %s", args[0])
	}
}

func runSourcesList(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printSourcesListUsage()
		return nil
	}
	dataRoot := "."
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			dataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			dataRoot = strings.TrimPrefix(arg, "--data-root=")
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}

	config, err := store.LoadSources(dataRoot)
	if err != nil {
		return err
	}
	printSourcesList(config)
	return nil
}

func runSourcesCoverage(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printSourcesCoverageUsage()
		return nil
	}
	options := sourceaudit.CoverageOptions{DataRoot: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--details":
			options.Details = true
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires a value")
			}
			i++
			options.Platform = platformnames.ResolveCLI(args[i])
		case strings.HasPrefix(arg, "--platform="):
			options.Platform = platformnames.ResolveCLI(strings.TrimPrefix(arg, "--platform="))
		case arg == "--source":
			if i+1 >= len(args) {
				return fmt.Errorf("--source requires a value")
			}
			i++
			options.Source = args[i]
		case strings.HasPrefix(arg, "--source="):
			options.Source = strings.TrimPrefix(arg, "--source=")
		case arg == "--low-score":
			if i+1 >= len(args) {
				return fmt.Errorf("--low-score requires a value")
			}
			i++
			score, err := strconv.Atoi(args[i])
			if err != nil || score < 0 {
				return fmt.Errorf("--low-score must be a non-negative integer")
			}
			options.LowScore = score
		case strings.HasPrefix(arg, "--low-score="):
			score, err := strconv.Atoi(strings.TrimPrefix(arg, "--low-score="))
			if err != nil || score < 0 {
				return fmt.Errorf("--low-score must be a non-negative integer")
			}
			options.LowScore = score
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}

	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(snapshot taskprogress.Snapshot) {
		progress.UpdateTask("sources coverage", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	report, err := sourceaudit.Coverage(options)
	progress.Done()
	if err != nil {
		return err
	}
	printSourceCoverageReport(report)
	return nil
}

func printCatalogStatus(result catalogstate.Status, details bool) {
	fmt.Print(renderCatalogStatus(result, details, outputColorEnabled(os.Stdout)))
}

func renderCatalogStatus(result catalogstate.Status, details, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf("Catalog status", ansiCyan, color))
	catalogField(&b, "Library", result.DataRoot, color)
	if result.PlatformFilter != "" {
		catalogField(&b, "Platform", result.PlatformFilter, color)
	}
	fmt.Fprintf(&b, "%s: %s platforms, %s games, %s releases\n",
		catalogLabel("Catalog", color),
		formatCount(result.PlatformCount),
		formatCount(result.GameCount),
		formatCount(result.ReleaseCount),
	)
	fmt.Fprintf(&b, "%s: %s matched, %s not matched, %s not checked\n",
		catalogLabel("Metadata", color),
		colorizeIf(formatCountWithPercent(result.GamesWithSourceMatches, result.GameCount), ansiGreen, color),
		colorizeIf(formatCountWithPercent(result.GamesCheckedNoMatches, result.GameCount), countAlertColor(result.GamesCheckedNoMatches), color),
		colorizeIf(formatCountWithPercent(result.GamesWithoutSourceData, result.GameCount), countAlertColor(result.GamesWithoutSourceData), color),
	)
	fmt.Fprintf(&b, "%s: %s\n",
		catalogLabel("Empty games (no metadata found)", color),
		colorizeIf(formatCountWithPercent(result.EmptyPlaceholderCount, result.GameCount), countAlertColor(result.EmptyPlaceholderCount), color),
	)
	fmt.Fprintf(&b, "%s: %s games with files (%s files), %s games with source URLs (%s URLs)\n",
		catalogLabel("Media", color),
		formatCount(result.GamesWithLocalMedia),
		formatCount(result.LocalMediaFileCount),
		formatCount(result.GamesWithMediaURLs),
		formatCount(result.SourceMediaURLCount),
	)
	renderCatalogSourceTable(&b, result.SourceCounts, result.GameCount, color)
	if details {
		renderCatalogPlatformDetails(&b, result.Platforms, color)
		renderCatalogEmptyDetails(&b, result.EmptyPlaceholders, 50, color)
	}
	return b.String()
}

func catalogField(b *strings.Builder, label, value string, color bool) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", catalogLabel(label, color), value)
}

func catalogLabel(label string, color bool) string {
	return colorizeIf(label, ansiYellow, color)
}

func countAlertColor(value int) string {
	if value == 0 {
		return ansiGreen
	}
	return ansiRed
}

func formatCountWithPercent(value, total int) string {
	return fmt.Sprintf("%s (%s)", formatCount(value), formatPercent(value, total))
}

func formatPercent(value, total int) string {
	if total <= 0 {
		return "0%"
	}
	percent := (int64(value)*100 + int64(total)/2) / int64(total)
	return fmt.Sprintf("%d%%", percent)
}

func printCatalogPruneEmptyResult(result catalogstate.PruneResult, details bool) {
	fmt.Println("Catalog prune-empty summary")
	fmt.Printf("Library: %s\n", result.DataRoot)
	if result.PlatformFilter != "" {
		fmt.Printf("Platform: %s\n", result.PlatformFilter)
	}
	fmt.Printf("Scanned: %d platforms, %d games\n", result.PlatformCount, result.GameCount)
	fmt.Printf("Empty games (no metadata found): %d\n", result.EmptyPlaceholderCount)
	if details {
		printCatalogEmptyDetails(result.EmptyPlaceholders, 50)
	}
	if result.Apply {
		fmt.Printf("Result: applied; deleted game folders: %d\n", result.DeletedGameCount)
		return
	}
	fmt.Println("Result: dry run only; add --apply to delete empty game folders")
}

func printCatalogFoldResult(result catalogfold.Result, details bool) {
	fmt.Print(renderCatalogFoldResult(result, details, outputColorEnabled(os.Stdout)))
}

func renderCatalogFoldStartHint(details, color bool) string {
	if details {
		return ""
	}
	return fmt.Sprintf("%s: add --details to review fold candidates and source-ID evidence\n\n", catalogLabel("Tip", color))
}

func renderCatalogFoldPlatformResult(result catalogfold.PlatformResult, apply, interactive, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf(result.Platform+" fold summary", ansiCyan, color))
	fmt.Fprintf(&b, "%s: %s games\n", catalogLabel("Scanned", color), formatCount(result.GameCount))
	if result.HeldGroupCount > 0 {
		fmt.Fprintf(&b, "Held: %s groups have unresolved conflicts or evidence and will be skipped by --apply (see --details)\n", formatCount(result.HeldGroupCount))
	}
	if result.ReviewRequiredCount > 0 {
		fmt.Fprintf(&b, "Review required: %s groups are skipped by --apply; use --interactive to confirm or reject them\n", formatCount(result.ReviewRequiredCount))
	}
	if result.RenamedGameCount > 0 {
		fmt.Fprintf(&b, "Card renames: %s\n", formatCount(result.RenamedGameCount))
	}
	for _, backup := range result.Backups {
		fmt.Fprintf(&b, "Original catalog backup: %s\n", backup)
	}
	fmt.Fprintf(&b, "%s: %s groups, %s game bundles to merge, %s evidence links\n",
		catalogLabel("Candidates", color),
		formatCount(result.CandidateGroupCount),
		formatCount(result.MergedGameCount),
		formatCount(result.IdentityCount),
	)
	fmt.Fprintf(&b, "%s: %s releases, %s fingerprints\n",
		catalogLabel("Data to merge", color),
		formatCount(result.ReleaseCount),
		formatCount(result.FingerprintCount),
	)
	if apply {
		fmt.Fprintf(&b, "%s: %s; groups: %s applied, %s skipped; moved files: %s\n\n",
			catalogLabel("Result", color),
			colorizeIf("applied", ansiGreen, color),
			formatCount(result.AppliedGroupCount),
			formatCount(result.SkippedGroupCount),
			formatCount(result.MovedFileCount),
		)
		return b.String()
	}
	if interactive {
		fmt.Fprintf(&b, "%s: %s; accepted: %s, rejected: %s, skipped: %s",
			catalogLabel("Result", color),
			colorizeIf("reviewed", ansiGreen, color),
			formatCount(result.AppliedGroupCount),
			formatCount(result.IgnoredGroupCount),
			formatCount(result.ReviewSkippedCount),
		)
		if result.SkippedGroupCount > 0 {
			fmt.Fprintf(&b, ", merge conflicts: %s", formatCount(result.SkippedGroupCount))
		}
		if result.MovedFileCount > 0 {
			fmt.Fprintf(&b, ", moved files: %s", formatCount(result.MovedFileCount))
		}
		if result.IgnoredFileCount > 0 {
			fmt.Fprintf(&b, ", rejection markers written: %s", formatCount(result.IgnoredFileCount))
		}
		if result.ReviewStopped {
			fmt.Fprintf(&b, "; stopped")
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b)
		return b.String()
	}
	if result.CandidateGroupCount == 0 {
		fmt.Fprintf(&b, "%s: %s; nothing to merge\n\n",
			catalogLabel("Result", color),
			colorizeIf("dry run", ansiYellow, color),
		)
		return b.String()
	}
	if result.CandidateGroupCount > 0 && result.HeldGroupCount+result.ReviewRequiredCount == result.CandidateGroupCount {
		fmt.Fprintln(&b, "Result: dry run; no automatically eligible groups")
		return b.String()
	}
	fmt.Fprintf(&b, "%s: %s; rerun with --apply to merge candidate groups\n\n",
		catalogLabel("Result", color),
		colorizeIf("dry run", ansiYellow, color),
	)
	return b.String()
}

func renderCatalogFoldResult(result catalogfold.Result, details, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf("Catalog fold summary", ansiCyan, color))
	catalogField(&b, "Library", result.DataRoot, color)
	if result.HeldGroupCount > 0 {
		fmt.Fprintf(&b, "Held: %s groups have unresolved conflicts or evidence and will be skipped by --apply (see --details)\n", formatCount(result.HeldGroupCount))
	}
	if result.PlatformFilter != "" {
		catalogField(&b, "Platform", result.PlatformFilter, color)
	}
	if result.Match != "" {
		catalogField(&b, "Match", result.Match, color)
	}
	if result.ReviewPlan != "" {
		catalogField(&b, "Review plan", result.ReviewPlan, color)
	}
	if result.ReviewRequiredCount > 0 {
		fmt.Fprintf(&b, "Review required: %s groups are skipped by --apply; use --interactive to confirm or reject them\n", formatCount(result.ReviewRequiredCount))
	}
	if result.RenamedGameCount > 0 {
		fmt.Fprintf(&b, "Card renames: %s\n", formatCount(result.RenamedGameCount))
	}
	for _, backup := range result.Backups {
		fmt.Fprintf(&b, "Original catalog backup: %s\n", backup)
	}
	fmt.Fprintf(&b, "%s: %s platforms, %s games\n",
		catalogLabel("Scanned", color),
		formatCount(result.PlatformCount),
		formatCount(result.GameCount),
	)
	fmt.Fprintf(&b, "%s: %s groups, %s game bundles to merge, %s evidence links\n",
		catalogLabel("Candidates", color),
		formatCount(result.CandidateGroupCount),
		formatCount(result.MergedGameCount),
		formatCount(result.IdentityCount),
	)
	fmt.Fprintf(&b, "%s: %s releases, %s fingerprints\n",
		catalogLabel("Data to merge", color),
		formatCount(result.ReleaseCount),
		formatCount(result.FingerprintCount),
	)
	if result.Interactive {
		fmt.Fprintf(&b, "%s: %s; accepted: %s, rejected: %s, skipped: %s",
			catalogLabel("Result", color),
			colorizeIf("reviewed", ansiGreen, color),
			formatCount(result.AppliedGroupCount),
			formatCount(result.IgnoredGroupCount),
			formatCount(result.ReviewSkippedCount),
		)
		if result.SkippedGroupCount > 0 {
			fmt.Fprintf(&b, ", merge conflicts: %s", formatCount(result.SkippedGroupCount))
		}
		if result.MovedFileCount > 0 {
			fmt.Fprintf(&b, ", moved files: %s", formatCount(result.MovedFileCount))
		}
		if result.IgnoredFileCount > 0 {
			fmt.Fprintf(&b, ", rejection markers written: %s", formatCount(result.IgnoredFileCount))
		}
		if result.ReviewStopped {
			fmt.Fprintf(&b, "; stopped")
		}
		fmt.Fprintln(&b)
	} else if result.Apply {
		fmt.Fprintf(&b, "%s: %s; groups: %s applied, %s skipped; moved files: %s\n",
			catalogLabel("Result", color),
			colorizeIf("applied", ansiGreen, color),
			formatCount(result.AppliedGroupCount),
			formatCount(result.SkippedGroupCount),
			formatCount(result.MovedFileCount),
		)
	} else if result.CandidateGroupCount == 0 {
		fmt.Fprintf(&b, "%s: %s; nothing to merge\n",
			catalogLabel("Result", color),
			colorizeIf("dry run", ansiYellow, color),
		)
	} else if result.HeldGroupCount+result.ReviewRequiredCount == result.CandidateGroupCount {
		fmt.Fprintln(&b, "Result: dry run; no automatically eligible groups")
	} else {
		fmt.Fprintf(&b, "%s: %s; rerun with --apply to merge candidate groups\n",
			catalogLabel("Result", color),
			colorizeIf("dry run", ansiYellow, color),
		)
	}
	renderCatalogFoldPlatformTable(&b, result.PlatformResults, color)
	if details {
		renderCatalogFoldDetails(&b, result.Groups, color)
	}
	return b.String()
}

func renderCatalogFoldPlatformTable(b *strings.Builder, platforms []catalogfold.PlatformResult, color bool) {
	if len(platforms) == 0 {
		return
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s\n", colorizeIf("By platform", ansiCyan, color))
	platformWidth := len("platform")
	gamesWidth := len("games")
	groupsWidth := len("groups")
	mergeWidth := len("to merge")
	identityWidth := len("links")
	releaseWidth := len("releases")
	fingerprintWidth := len("fingerprints")
	for _, platform := range platforms {
		platformWidth = maxInt(platformWidth, runeWidth(platform.Platform))
		gamesWidth = maxInt(gamesWidth, runeWidth(formatCount(platform.GameCount)))
		groupsWidth = maxInt(groupsWidth, runeWidth(formatCount(platform.CandidateGroupCount)))
		mergeWidth = maxInt(mergeWidth, runeWidth(formatCount(platform.MergedGameCount)))
		identityWidth = maxInt(identityWidth, runeWidth(formatCount(platform.IdentityCount)))
		releaseWidth = maxInt(releaseWidth, runeWidth(formatCount(platform.ReleaseCount)))
		fingerprintWidth = maxInt(fingerprintWidth, runeWidth(formatCount(platform.FingerprintCount)))
	}
	fmt.Fprintf(b, "  %s  %s  %s  %s  %s  %s  %s\n",
		catalogTableCell("platform", platformWidth, alignLeft, ansiYellow, color),
		catalogTableCell("games", gamesWidth, alignRight, ansiYellow, color),
		catalogTableCell("groups", groupsWidth, alignRight, ansiYellow, color),
		catalogTableCell("to merge", mergeWidth, alignRight, ansiYellow, color),
		catalogTableCell("links", identityWidth, alignRight, ansiYellow, color),
		catalogTableCell("releases", releaseWidth, alignRight, ansiYellow, color),
		catalogTableCell("fingerprints", fingerprintWidth, alignRight, ansiYellow, color),
	)
	for _, platform := range platforms {
		opportunityColor := foldOpportunityColor(platform.CandidateGroupCount)
		fmt.Fprintf(b, "  %s  %s  %s  %s  %s  %s  %s\n",
			catalogTableCell(platform.Platform, platformWidth, alignLeft, ansiCyan, color),
			catalogTableCell(formatCount(platform.GameCount), gamesWidth, alignRight, "", color),
			catalogTableCell(formatCount(platform.CandidateGroupCount), groupsWidth, alignRight, opportunityColor, color),
			catalogTableCell(formatCount(platform.MergedGameCount), mergeWidth, alignRight, opportunityColor, color),
			catalogTableCell(formatCount(platform.IdentityCount), identityWidth, alignRight, opportunityColor, color),
			catalogTableCell(formatCount(platform.ReleaseCount), releaseWidth, alignRight, "", color),
			catalogTableCell(formatCount(platform.FingerprintCount), fingerprintWidth, alignRight, "", color),
		)
	}
}

func foldOpportunityColor(value int) string {
	if value == 0 {
		return ansiGreen
	}
	return ansiYellow
}

func renderCatalogFoldDetails(b *strings.Builder, groups []catalogfold.Group, color bool) {
	if len(groups) == 0 {
		return
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s\n", colorizeIf("Fold details", ansiCyan, color))
	for _, group := range groups {
		renderCatalogFoldGroupDetails(b, group, color)
	}
}

func renderCatalogFoldGroupDetails(b *strings.Builder, group catalogfold.Group, color bool) {
	status := colorizeIf("candidate", ansiYellow, color)
	if group.ReviewRequired != "" {
		status = colorizeIf("review required", ansiYellow, color)
	}
	if group.HoldReason != "" {
		status = colorizeIf("held", ansiRed, color)
	}
	if group.Applied {
		status = colorizeIf("applied", ansiGreen, color)
	}
	if group.Ignored {
		status = colorizeIf("ignored", ansiGreen, color)
	}
	if group.Skipped {
		status = colorizeIf("skipped", ansiRed, color)
	}
	fmt.Fprintf(b, "- %s / %s <- %s (%s)\n",
		group.Platform,
		group.Keep.Folder,
		formatCatalogFoldRefs(group.Merge),
		status,
	)
	if group.TargetFolder != "" && group.TargetFolder != group.Keep.Folder {
		fmt.Fprintf(b, "  resulting folder: %s\n", group.TargetFolder)
	}
	if group.ReviewRequired != "" {
		label := "review required"
		if group.ReviewAccepted && group.Applied {
			label = "review accepted"
		}
		fmt.Fprintf(b, "  %s: %s\n", label, group.ReviewRequired)
		for _, ref := range append([]catalogfold.GameRef{group.Keep}, group.Merge...) {
			fmt.Fprintf(b, "  regions: %s: %s\n", ref.Folder, strings.Join(ref.Regions, ", "))
		}
	}
	if group.ReviewReason != "" {
		fmt.Fprintf(b, "  reviewed: %s\n", group.ReviewReason)
	}
	if group.SkipReason != "" {
		fmt.Fprintf(b, "  reason: %s\n", group.SkipReason)
	} else if group.HoldReason != "" {
		fmt.Fprintf(b, "  held: %s\n", group.HoldReason)
	}
	for _, change := range group.FileChanges {
		if change.Action == "relocate" || change.Action == "deduplicate" {
			fmt.Fprintf(b, "  %s: %s/%s -> %s\n", change.Action, change.FromFolder, change.Path, change.Target)
		}
	}
	if len(group.Evidence) > 0 {
		fmt.Fprintf(b, "  evidence:\n")
		for _, item := range formatCatalogFoldEvidenceItems(group.Evidence) {
			fmt.Fprintf(b, "    - %s\n", item)
		}
	}
	fmt.Fprintf(b, "  data to merge: %s releases, %s fingerprints",
		formatCount(group.ReleaseCount),
		formatCount(group.FingerprintCount),
	)
	if group.Applied || group.MovedFileCount > 0 {
		fmt.Fprintf(b, ", %s files moved", formatCount(group.MovedFileCount))
	}
	if group.Ignored || group.IgnoreFileCount > 0 {
		fmt.Fprintf(b, ", %s rejection markers written", formatCount(group.IgnoreFileCount))
	}
	fmt.Fprintln(b)
}

func formatCatalogFoldRefs(refs []catalogfold.GameRef) string {
	if len(refs) == 0 {
		return "none"
	}
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		values = append(values, ref.Folder)
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

func formatCatalogFoldEvidence(evidence []catalogfold.Evidence) string {
	return strings.Join(formatCatalogFoldEvidenceItems(evidence), ", ")
}

func formatCatalogFoldEvidenceItems(evidence []catalogfold.Evidence) []string {
	if len(evidence) == 0 {
		return []string{"none"}
	}
	values := make([]string, 0, len(evidence))
	for _, item := range evidence {
		value := item.Source + "." + item.Field + "=" + item.Value
		if strings.TrimSpace(item.URL) != "" {
			value += " -> " + strings.TrimSpace(item.URL)
		}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func renderCatalogSourceTable(b *strings.Builder, counts []catalogstate.SourceCount, totalGames int, color bool) {
	fmt.Fprintf(b, "%s\n", colorizeIf("Sources", ansiCyan, color))
	if len(counts) == 0 {
		fmt.Fprintln(b, "  none")
		return
	}
	sourceWidth := len("source")
	matchedWidth := len("matched")
	notMatchedWidth := len("not matched")
	checkedWidth := len("checked")
	for _, count := range counts {
		sourceWidth = maxInt(sourceWidth, runeWidth(count.Source))
		matchedWidth = maxInt(matchedWidth, runeWidth(formatCountWithPercent(count.MatchedGameCount, count.GameCount)))
		notMatchedWidth = maxInt(notMatchedWidth, runeWidth(formatCountWithPercent(sourceNotMatchedCount(count), count.GameCount)))
		checkedWidth = maxInt(checkedWidth, runeWidth(formatCountWithPercent(count.GameCount, totalGames)))
	}
	fmt.Fprintf(b, "  %s  %s  %s  %s\n",
		catalogTableCell("source", sourceWidth, alignLeft, ansiYellow, color),
		catalogTableCell("matched", matchedWidth, alignRight, ansiYellow, color),
		catalogTableCell("not matched", notMatchedWidth, alignRight, ansiYellow, color),
		catalogTableCell("checked", checkedWidth, alignRight, ansiYellow, color),
	)
	for _, count := range counts {
		notMatched := sourceNotMatchedCount(count)
		fmt.Fprintf(b, "  %s  %s  %s  %s\n",
			catalogTableCell(count.Source, sourceWidth, alignLeft, ansiCyan, color),
			catalogTableCell(formatCountWithPercent(count.MatchedGameCount, count.GameCount), matchedWidth, alignRight, ansiGreen, color),
			catalogTableCell(formatCountWithPercent(notMatched, count.GameCount), notMatchedWidth, alignRight, countAlertColor(notMatched), color),
			catalogTableCell(formatCountWithPercent(count.GameCount, totalGames), checkedWidth, alignRight, "", color),
		)
	}
}

func sourceNotMatchedCount(count catalogstate.SourceCount) int {
	if count.NoMatchGameCount > 0 {
		return count.NoMatchGameCount
	}
	if count.GameCount > count.MatchedGameCount {
		return count.GameCount - count.MatchedGameCount
	}
	return 0
}

func catalogTableCell(value string, width, align int, colorCode string, color bool) string {
	padding := width - runeWidth(value)
	if padding < 0 {
		padding = 0
	}
	if color && colorCode != "" {
		value = colorize(value, colorCode)
	}
	if align == alignLeft {
		return value + strings.Repeat(" ", padding)
	}
	return strings.Repeat(" ", padding) + value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func renderCatalogPlatformDetails(b *strings.Builder, platforms []catalogstate.PlatformStatus, color bool) {
	if len(platforms) == 0 {
		return
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s\n", colorizeIf("By platform", ansiCyan, color))
	for _, platform := range platforms {
		fmt.Fprintf(b, "- %s: %s games, %s releases, %s matched, %s not matched, %s empty\n",
			platform.Platform,
			formatCount(platform.GameCount),
			formatCount(platform.ReleaseCount),
			formatCount(platform.GamesWithSourceMatches),
			formatCount(platform.GamesCheckedNoMatches),
			colorizeIf(formatCount(platform.EmptyPlaceholderCount), countAlertColor(platform.EmptyPlaceholderCount), color),
		)
	}
}

func renderCatalogEmptyDetails(b *strings.Builder, games []catalogstate.GameRef, limit int, color bool) {
	if len(games) == 0 {
		return
	}
	if limit <= 0 || limit > len(games) {
		limit = len(games)
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s\n", colorizeIf("Empty games (no metadata found)", ansiCyan, color))
	for _, game := range games[:limit] {
		fmt.Fprintf(b, "- %s / %s\n", game.Platform, game.Game)
	}
	if omitted := len(games) - limit; omitted > 0 {
		fmt.Fprintf(b, "... %s more\n", formatCount(omitted))
	}
}

func printCatalogPlatformDetails(platforms []catalogstate.PlatformStatus) {
	if len(platforms) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("By platform:")
	for _, platform := range platforms {
		fmt.Printf("- %s: %d games, %d releases, %d with source data, %d empty\n",
			platform.Platform,
			platform.GameCount,
			platform.ReleaseCount,
			platform.GamesWithSourceData,
			platform.EmptyPlaceholderCount,
		)
	}
}

func printCatalogEmptyDetails(games []catalogstate.GameRef, limit int) {
	if len(games) == 0 {
		return
	}
	if limit <= 0 || limit > len(games) {
		limit = len(games)
	}
	fmt.Println()
	fmt.Println("Empty games (no metadata found):")
	for _, game := range games[:limit] {
		fmt.Printf("- %s / %s\n", game.Platform, game.Game)
	}
	if omitted := len(games) - limit; omitted > 0 {
		fmt.Printf("... %d more\n", omitted)
	}
}
