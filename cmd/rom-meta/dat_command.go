package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"rommeta/internal/dat"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
)

func runDAT(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printDATUsage()
		return nil
	}
	switch args[0] {
	case "arcade":
		return runDATArcade(args[1:])
	case "import":
		return runDATImport(args[1:])
	case "plan":
		return runDATPlan(args[1:])
	default:
		return fmt.Errorf("unknown dat command: %s", args[0])
	}
}

func runDATImport(args []string) error {
	return runDATImportCommand(args, false)
}

func runDATImportCommand(args []string, requireArcade bool) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printDATImportUsage()
		return nil
	}
	options := dat.ArcadeOptions{ImportOptions: dat.ImportOptions{DataRoot: "."}, Fold: true}
	var path, arcadeOption string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isHelpArg(arg) {
			printDATImportUsage()
			return nil
		}
		switch arg {
		case "--apply":
			options.Apply = true
			continue
		case "--offline":
			options.Offline = true
			continue
		case "--fold", "--no-fold":
			options.Fold = arg == "--fold"
			arcadeOption = arg
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			if path != "" {
				return fmt.Errorf("unexpected argument: %s", arg)
			}
			path = arg
			continue
		}
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--data-root", "--platform", "--member", "--classifications", "--platform-map", "--sets", "--report":
		default:
			return fmt.Errorf("unknown option: %s", key)
		}
		if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return fmt.Errorf("%s requires a value", key)
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s requires a value", key)
		}
		switch key {
		case "--data-root":
			options.DataRoot = value
		case "--platform":
			options.PlatformOverride = platformnames.ResolveCLI(value)
		case "--member":
			options.MemberPath = value
		case "--classifications":
			options.ClassificationPath = value
			arcadeOption = key
		case "--platform-map":
			options.RoutingPath = value
			arcadeOption = key
		case "--sets":
			options.Sets = strings.Split(value, ",")
			arcadeOption = key
		case "--report":
			options.ReportPath = value
			arcadeOption = key
		}
	}
	if path == "" {
		printDATImportUsage()
		return fmt.Errorf("DAT file path is required")
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
	if len(catalogConfig.Fold.Platforms) > 0 {
		options.PlatformRegionPriority = map[string][]string{}
		for platformName, platformConfig := range catalogConfig.Fold.Platforms {
			resolved := registry.ResolveCLI(platformName)
			if strings.TrimSpace(resolved) != "" {
				options.PlatformRegionPriority[resolved] = platformConfig.RegionPriority
			}
		}
	}
	format, err := dat.DetectFormat(path, options.MemberPath)
	if err != nil {
		return err
	}
	if requireArcade || format == "mame-listxml" {
		if options.PlatformOverride != "" && !strings.EqualFold(options.PlatformOverride, "Arcade") {
			return fmt.Errorf("MAME games use the shared Arcade catalog; --platform cannot override arcade hardware routing")
		}
		return importDATArcade(path, options)
	}
	if arcadeOption != "" {
		return fmt.Errorf("%s is supported only for official MAME listxml imports (detected %s)", arcadeOption, format)
	}

	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(snapshot dat.ImportProgress) {
		progress.UpdateTask("dat import", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	result, err := dat.Import(path, options.ImportOptions)
	progress.Done()
	if err != nil {
		if len(result.Changes.Conflicts) > 0 {
			printDATImportResult(result)
		}
		return err
	}
	printDATImportResult(result)
	return nil
}

func runDATPlan(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printDATPlanUsage()
		return nil
	}
	options := dat.PlanOptions{SampleLimit: 8}
	var path string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isHelpArg(arg) {
			printDATPlanUsage()
			return nil
		}
		switch {
		case arg == "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires a value")
			}
			i++
			options.PlatformOverride = platformnames.ResolveCLI(args[i])
		case strings.HasPrefix(arg, "--platform="):
			options.PlatformOverride = platformnames.ResolveCLI(strings.TrimPrefix(arg, "--platform="))
		case arg == "--member":
			if i+1 >= len(args) {
				return fmt.Errorf("--member requires a value")
			}
			i++
			options.MemberPath = args[i]
		case strings.HasPrefix(arg, "--member="):
			options.MemberPath = strings.TrimPrefix(arg, "--member=")
		case arg == "--sample-limit":
			if i+1 >= len(args) {
				return fmt.Errorf("--sample-limit requires a value")
			}
			i++
			limit, err := strconv.Atoi(args[i])
			if err != nil || limit < 0 {
				return fmt.Errorf("--sample-limit must be a non-negative integer")
			}
			options.SampleLimit = limit
		case strings.HasPrefix(arg, "--sample-limit="):
			value := strings.TrimPrefix(arg, "--sample-limit=")
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 0 {
				return fmt.Errorf("--sample-limit must be a non-negative integer")
			}
			options.SampleLimit = limit
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown option: %s", arg)
		default:
			if path != "" {
				return fmt.Errorf("unexpected argument: %s", arg)
			}
			path = arg
		}
	}
	if path == "" {
		printDATPlanUsage()
		return fmt.Errorf("DAT file path is required")
	}

	plan, err := dat.BuildPlan(path, options)
	if err != nil {
		return err
	}
	printDATPlan(plan)
	return nil
}

func printDATImportResult(result dat.ImportResult) {
	fmt.Print(renderDATImportResult(result, outputColorEnabled(os.Stdout)))
}

func printDATPlan(plan dat.Plan) {
	fmt.Print(renderDATPlan(plan, outputColorEnabled(os.Stdout)))
}

func renderDATImportResult(result dat.ImportResult, color bool) string {
	var b strings.Builder
	renderDATTitle(&b, "DAT import summary", color)
	renderDATSourceDetails(&b, result.Plan, color)
	fmt.Fprintln(&b)
	renderDATCatalogDetails(&b, result, color)
	fmt.Fprintln(&b)
	renderDATContentDetails(&b, result.Plan, color)
	renderDATImportSamples(&b, result, color)
	renderDATArcadeNote(&b, result.Plan, color)
	fmt.Fprintln(&b)
	if len(result.Changes.Conflicts) > 0 {
		fmt.Fprintf(&b, "%s: %s; no catalog files were changed\n",
			datLabel("Result", color),
			colorizeIf("needs review", ansiYellow, color),
		)
		renderDATConflicts(&b, result.Changes.Conflicts, color)
		return b.String()
	}
	if result.Apply {
		fmt.Fprintf(&b, "%s: %s; %s: %s\n",
			datLabel("Result", color),
			colorizeIf("applied", ansiGreen, color),
			datLabel("written files", color),
			formatCount(result.WrittenFileCount),
		)
		return b.String()
	}
	fmt.Fprintf(&b, "%s: %s\n", datLabel("Result", color), colorizeIf("dry run", ansiYellow, color))
	fmt.Fprintln(&b, "Next: rerun with --apply to write the catalog")
	return b.String()
}

func renderDATPlan(plan dat.Plan, color bool) string {
	var b strings.Builder
	renderDATTitle(&b, "DAT import plan", color)
	renderDATSourceDetails(&b, plan, color)
	fmt.Fprintln(&b)
	renderDATPlanCatalog(&b, plan, color)
	fmt.Fprintln(&b)
	renderDATContentDetails(&b, plan, color)
	renderDATSamples(&b, plan, color)
	renderDATArcadeNote(&b, plan, color)
	return b.String()
}

func renderDATTitle(b *strings.Builder, title string, color bool) {
	fmt.Fprintf(b, "%s\n", colorizeIf(title, ansiCyan, color))
}

func renderDATSourceDetails(b *strings.Builder, plan dat.Plan, color bool) {
	fmt.Fprintf(b, "%s\n", colorizeIf("Source", ansiCyan, color))
	datField(b, "input", plan.InputPath, color)
	datField(b, "container", plan.Container, color)
	if plan.MemberPath != "" {
		datField(b, "member", plan.MemberPath, color)
	}
	var formatParts []string
	if plan.Format != "" {
		formatParts = append(formatParts, datInlineValue("format", plan.Format, color))
	}
	if plan.Authority != "" {
		formatParts = append(formatParts, datInlineValue("authority", plan.Authority, color))
	}
	if len(formatParts) > 0 {
		fmt.Fprintf(b, "  %s\n", strings.Join(formatParts, "; "))
	}
	datField(b, "dataset", plan.DatasetName, color)
	var metadataParts []string
	if plan.Version != "" {
		metadataParts = append(metadataParts, datInlineValue("version", plan.Version, color))
	}
	if plan.Author != "" {
		metadataParts = append(metadataParts, datInlineValue("author", plan.Author, color))
	}
	if len(metadataParts) > 0 {
		fmt.Fprintf(b, "  %s\n", strings.Join(metadataParts, "; "))
	}
}

func renderDATCatalogDetails(b *strings.Builder, result dat.ImportResult, color bool) {
	fmt.Fprintf(b, "%s\n", colorizeIf("Catalog", ansiCyan, color))
	datField(b, "library", result.DataRoot, color)
	datField(b, "platform", datPlatformLabel(result.Plan), color)
	datField(b, "path", result.PlatformPath, color)
	operation := "create new platform"
	if result.Changes.Action == "merge" {
		operation = "merge into existing platform"
	}
	datField(b, "operation", operation, color)
	countLabel := "generated"
	if result.Changes.Action == "merge" {
		countLabel = "incoming"
	}
	fmt.Fprintf(b, "  %s: %s, %s, %s\n",
		datLabel(countLabel, color),
		formatDATNoun(result.GameBundleCount, "game bundle", "game bundles"),
		formatDATNoun(result.ReleaseCount, "release", "releases"),
		formatDATNoun(result.FingerprintCount, "fingerprint", "fingerprints"),
	)
	if result.Changes.Action == "merge" {
		fmt.Fprintf(b, "  %s: %s new, %s reconciled, %s\n",
			datLabel("changes", color),
			formatCount(result.Changes.Added),
			formatCount(result.Changes.Reconciled),
			formatDATNoun(len(result.Changes.Conflicts), "unresolved match", "unresolved matches"),
		)
	}
}

func renderDATConflicts(b *strings.Builder, conflicts []string, color bool) {
	if len(conflicts) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n", colorizeIf("Unresolved matches", ansiCyan, color))
	const shown = 8
	for index, conflict := range conflicts {
		if index >= shown {
			break
		}
		fmt.Fprintf(b, "  - %s\n", conflict)
	}
	if len(conflicts) > shown {
		fmt.Fprintf(b, "  ... %s more\n", formatCount(len(conflicts)-shown))
	}
}

func renderDATPlanCatalog(b *strings.Builder, plan dat.Plan, color bool) {
	fmt.Fprintf(b, "%s\n", colorizeIf("Catalog", ansiCyan, color))
	datField(b, "platform", datPlatformLabel(plan), color)
	if plan.PlatformOverride {
		datField(b, "platform source", "command line override", color)
	} else if plan.PlatformName != "" {
		datField(b, "platform source", "DAT header", color)
	}
	if plan.LayoutHint != "" {
		datField(b, "layout", plan.LayoutHint, color)
	}
}

func renderDATContentDetails(b *strings.Builder, plan dat.Plan, color bool) {
	fmt.Fprintf(b, "%s\n", colorizeIf("Contents", ansiCyan, color))
	if plan.MachineCount > 0 {
		var machineParts []string
		machineParts = append(machineParts, formatDATNoun(plan.MachineCount, "machine", "machines"))
		if plan.PlayableCount > 0 {
			machineParts = append(machineParts, formatDATNoun(plan.PlayableCount, "runnable candidate", "runnable candidates"))
		}
		if plan.BIOSCount > 0 {
			machineParts = append(machineParts, formatDATNoun(plan.BIOSCount, "BIOS", "BIOS"))
		}
		if plan.DeviceCount > 0 {
			machineParts = append(machineParts, formatDATNoun(plan.DeviceCount, "device", "devices"))
		}
		if plan.MechanicalCount > 0 {
			machineParts = append(machineParts, formatDATNoun(plan.MechanicalCount, "mechanical", "mechanical"))
		}
		fmt.Fprintf(b, "  %s: %s\n", datLabel("entries", color), strings.Join(machineParts, ", "))
	} else {
		fmt.Fprintf(b, "  %s: %s\n", datLabel("entries", color), formatDATNoun(plan.GameCount, "release", "releases"))
	}

	var payloadParts []string
	payloadParts = append(payloadParts, formatDATNoun(plan.ROMCount, "ROM", "ROMs"))
	if plan.DiskCount > 0 {
		payloadParts = append(payloadParts, formatDATNoun(plan.DiskCount, "disk/CHD", "disks/CHDs"))
	}
	if plan.SampleCount > 0 {
		payloadParts = append(payloadParts, formatDATNoun(plan.SampleCount, "sample", "samples"))
	}
	fmt.Fprintf(b, "  %s: %s\n", datLabel("payloads", color), strings.Join(payloadParts, ", "))
	fmt.Fprintf(b, "  %s: %s\n", datLabel("clone links", color), formatCount(plan.CloneLinkCount))
	if plan.MachineCount > 0 {
		fmt.Fprintf(b, "  %s: %s\n", datLabel("ROM dependencies", color), formatCount(plan.ROMDependencyCount))
	}
	fmt.Fprintf(b, "  %s: crc32 %s, md5 %s, sha1 %s, sha256 %s\n",
		datLabel("hashes", color),
		formatCount(plan.HashCounts.CRC32),
		formatCount(plan.HashCounts.MD5),
		formatCount(plan.HashCounts.SHA1),
		formatCount(plan.HashCounts.SHA256),
	)
}

func renderDATImportSamples(b *strings.Builder, result dat.ImportResult, color bool) {
	samples := result.SampleGames
	if len(samples) == 0 {
		samples = result.Plan.SampleGames
	}
	renderDATSamplesList(b, samples, result.Plan.MachineCount > 0, color)
}

func renderDATSamples(b *strings.Builder, plan dat.Plan, color bool) {
	renderDATSamplesList(b, plan.SampleGames, plan.MachineCount > 0, color)
}

func renderDATSamplesList(b *strings.Builder, samples []dat.GameSample, machines bool, color bool) {
	if len(samples) == 0 {
		return
	}
	fmt.Fprintln(b)
	if machines {
		fmt.Fprintf(b, "%s\n", colorizeIf("Sample machines", ansiCyan, color))
	} else {
		fmt.Fprintf(b, "%s\n", colorizeIf("Sample releases", ansiCyan, color))
	}
	for _, game := range samples {
		name := game.Name
		if machines && game.Description != "" {
			name = fmt.Sprintf("%s - %s", game.Name, game.Description)
		}
		var flags []string
		if game.BIOS {
			flags = append(flags, "BIOS")
		}
		if game.Device {
			flags = append(flags, "device")
		}
		if game.Mechanical {
			flags = append(flags, "mechanical")
		}
		if strings.EqualFold(game.Runnable, "no") {
			flags = append(flags, "not runnable")
		}
		if game.CloneOf != "" {
			flags = append(flags, "clone of "+game.CloneOf)
		}
		if len(flags) > 0 {
			fmt.Fprintf(b, "  - %s (%s)\n", name, strings.Join(flags, ", "))
			continue
		}
		fmt.Fprintf(b, "  - %s\n", name)
	}
}

func renderDATArcadeNote(b *strings.Builder, plan dat.Plan, color bool) {
	if plan.Format == "mame-listxml" {
		fmt.Fprintln(b)
		fmt.Fprintf(b, "%s: dat import automatically classifies MAME systems, excludes home systems and previews the Arcade catalog; add --apply to write it.\n", datLabel("Note", color))
		return
	}
	if plan.Authority == "mame" || plan.Authority == "fbneo" || plan.MachineCount > 0 {
		fmt.Fprintln(b)
		fmt.Fprintf(b, "%s: arcade or machine-style DAT/listxml import is plan-only for now; review parent/clone, BIOS/device, CHD, and sample data before generating a catalog.\n", datLabel("Note", color))
	}
}

func datPlatformLabel(plan dat.Plan) string {
	if plan.PlatformOverride {
		return plan.PlatformName + " (override)"
	}
	return plan.PlatformName + " (auto)"
}

func datField(b *strings.Builder, label, value string, color bool) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(b, "  %s: %s\n", datLabel(label, color), value)
}

func datInlineValue(label, value string, color bool) string {
	return fmt.Sprintf("%s: %s", datLabel(label, color), value)
}

func datLabel(label string, color bool) string {
	return colorizeIf(label, ansiYellow, color)
}

func formatDATNoun(count int, singular, plural string) string {
	word := plural
	if count == 1 {
		word = singular
	}
	return formatCount(count) + " " + word
}

func printDATUsage() {
	fmt.Println(`rom-meta dat inspects DAT/XML files and can generate platform catalogs.

Usage:
  rom-meta dat import <dat-file-or-zip> [--data-root <root>] [--platform <name>] [--member <zip-member>] [--apply]
  rom-meta dat plan <dat-file-or-zip> [--platform <name>] [--member <zip-member>] [--sample-limit <n>]

Commands:
  arcade  Compatibility alias for MAME imports; prefer dat import.
  import  Detect the DAT format and write a catalog only with --apply.
  plan    Inspect a DAT read-only and print the import plan.`)
}

func printDATImportUsage() {
	fmt.Println(`rom-meta dat import detects the DAT format and builds a local catalog.

Usage:
  rom-meta dat import <dat-file-or-zip> [--data-root <root>] [--platform <name>] [--member <zip-member>] [--apply]

Options:
  --data-root <root>   Data root containing platforms/ (default: current directory).
  --platform <name>    Override the platform name detected from the DAT header.
  --member <path>      Select a DAT/XML member inside a ZIP by path or unique basename.
  --apply              Create or merge into the platform. Without this flag, the command is a dry run.
  --offline            Disable support-data downloads.

MAME options (official listxml is detected automatically):
  --classifications <zip>  Use an explicit version-matched classification ZIP.
  --platform-map <json>    Override hardware routing with naming evidence.
  --sets <a,b,...>         Select exact MAME short names.
  --fold / --no-fold       Fold confirmed clone revisions (default: fold).
  --report <new.json>      Save classification, grouping and binding decisions.

MAME games share platforms/Arcade/ and retain hardware families in their cards.
Matching support data is reused locally or downloaded from Progetto-SNAPS.
Preview may populate the support cache; catalog changes still require --apply.
Distribution arcade DATs, Software Lists and FBNeo apply remain unsupported.`)
}

func printDATPlanUsage() {
	fmt.Println(`rom-meta dat plan inspects a DAT/XML file without writing catalog files.

Usage:
  rom-meta dat plan <dat-file-or-zip> [--platform <name>] [--member <zip-member>] [--sample-limit <n>]

Options:
  --platform <name>    Override the platform name detected from the DAT header.
  --member <path>      Select a DAT/XML member inside a ZIP by path or unique basename.
  --sample-limit <n>   Number of sample releases or machines to show.`)
}
