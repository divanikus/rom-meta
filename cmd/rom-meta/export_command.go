package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"rommeta/internal/exportclient"
	"rommeta/internal/exporter"
)

func runExport(args []string) error {
	options := exporter.Options{DataRoot: "."}
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	target, selected := "es-de", ""
	api := ""
	var destination, packageRoot, mediaRoot, report string
	all, list := false, false
	flags.StringVar(&api, "api", "", "Catalog HTTP(S) origin; reads local ROMs without a local catalog")
	flags.StringVar(&target, "target", "es-de", "Output target (es-de or launchbox)")
	flags.StringVar(&options.DataRoot, "data-root", ".", "Local catalog root")
	flags.StringVar(&options.Platform, "platform", "", "One platform code, full name or registered alias")
	flags.StringVar(&selected, "platforms", "", "Comma-separated platform codes, full names or registered aliases")
	flags.BoolVar(&all, "all", false, "Discover platform folders by registered names under --rom-root")
	flags.BoolVar(&list, "list-platforms", false, "List platform codes, names, aliases and export support")
	flags.BoolVar(&list, "list-systems", false, "Compatibility alias for --list-platforms")
	flags.StringVar(&options.ArcadeSnapshot, "arcade-snapshot", "", "Saved MAME catalog snapshot (auto-select only when exactly one exists)")
	flags.StringVar(&options.ROMRoot, "rom-root", "", "One platform ROM directory, or collection root with --all/--platforms")
	flags.StringVar(&options.Out, "out", "", "Optional new portable package directory instead of direct installation")
	flags.StringVar(&destination, "destination", "", "Frontend application-data directory to create/enrich entries in place")
	flags.StringVar(&packageRoot, "package", "", "Optional existing export package instead of catalog/ROM inputs")
	flags.StringVar(&mediaRoot, "media-root", "", "Optional ES-DE media directory override; otherwise read native settings")
	flags.StringVar(&report, "report", "", "Optional new JSON destination-plan report (contains private paths)")
	flags.StringVar(&options.ProfilePath, "profile", "", "Optional YAML export selection profile")
	flags.StringVar(&options.Match, "match", "", "Select ROM relative paths containing this text")
	flags.IntVar(&options.Limit, "limit", 0, "Maximum inputs per system in sorted path order (0: all)")
	flags.BoolVar(&options.HashROMs, "hash-roms", false, "Hash complete CHD containers instead of using headers/catalog metadata")
	flags.BoolVar(&options.Apply, "apply", false, "Apply the previewed destination update or create the requested package")
	flags.Usage = func() {
		fmt.Fprint(os.Stdout, "rom-meta export adds games and enriches an existing frontend using one shared workflow.\n\nUse --destination <frontend data directory> for direct export/enrichment.\nUse --out <new directory> only when you want a portable package.\nUse --platform, --platforms or --all with --rom-root and --data-root or --api.\nAlternatively use --package <existing export> with --destination.\nPreview by default; --apply writes changes with backups. ROMs/catalogs stay unchanged.\nProfiles share selection policy; --target selects layout, not the optional profile target.\nListing cannot be combined with export selection/write flags.\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional export arguments")
	}
	normalizedTarget, err := exporter.NormalizeTarget(target)
	if err != nil {
		return err
	}
	target, options.Target = normalizedTarget, normalizedTarget
	if api != "" {
		var explicit bool
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "data-root" {
				explicit = true
			}
		})
		if explicit {
			return fmt.Errorf("choose --api or --data-root, not both")
		}
	}
	if list {
		if err := validateExportModeFlags(flags, "--list-platforms", "target", "api", "data-root", "rom-root", "list-platforms", "list-systems"); err != nil {
			return err
		}
		args := []string{}
		if api != "" {
			args = append(args, "--api", api)
		} else {
			args = append(args, "--data-root", options.DataRoot)
		}
		if options.ROMRoot != "" {
			args = append(args, "--rom-root", options.ROMRoot)
		}
		return runPlatformsList(args)
	}
	if destination != "" && options.Out != "" {
		return fmt.Errorf("choose --destination for direct export or --out for a package")
	}
	if packageRoot != "" {
		if destination == "" {
			return fmt.Errorf("--package requires --destination")
		}
		if err := validateExportModeFlags(flags, "--package", "target", "package", "destination", "media-root", "report", "apply"); err != nil {
			return err
		}
		return runPackageDestination(target, packageRoot, destination, mediaRoot, report, options.Apply)
	}
	if mediaRoot != "" && target != "es-de" {
		return fmt.Errorf("--media-root is only supported for ES-DE")
	}
	if destination == "" && (mediaRoot != "" || report != "") {
		return fmt.Errorf("--media-root and --report require --destination")
	}
	if options.Out == "" && destination == "" {
		return fmt.Errorf("--destination or --out is required")
	}
	modes := 0
	if options.Platform != "" {
		modes++
	}
	if selected != "" {
		modes++
	}
	if all {
		modes++
	}
	if modes != 1 {
		return fmt.Errorf("exactly one of --platform, --platforms or --all is required")
	}
	if options.ROMRoot == "" {
		return fmt.Errorf("--rom-root is required")
	}
	if options.Limit < 0 {
		return fmt.Errorf("--limit must be non-negative")
	}
	systems, err := selectedSystems(selected)
	if err != nil {
		return err
	}
	if destination != "" {
		return runDirectDestination(options, api, systems, all, destination, mediaRoot, report)
	}
	if api != "" {
		return runNetworkExport(options, api, systems, all)
	}
	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(stage, name string, current, total int) {
		updateExportProgress(progress, stage, name, current, total)
	}
	defer progress.Done()
	if all || selected != "" {
		batch, err := exporter.BuildBatch(exporter.BatchOptions{Options: options, All: all, Systems: systems})
		progress.Done()
		if err != nil {
			return err
		}
		fmt.Printf("\n%s export: %d systems | %d skipped\nOutput: %s\n", target, len(batch.Plans), len(batch.Skipped), batch.Out)
		for _, plan := range batch.Plans {
			printExportSummary(plan)
		}
		for _, skip := range batch.Skipped {
			fmt.Printf("Skipped %s: %s\n", skip.System, skip.Reason)
		}
		if len(batch.Plans) == 0 {
			return fmt.Errorf("no systems with selected inputs; no package created")
		}
		if !options.Apply {
			fmt.Println("Preview only. Repeat with --apply to create this new package.")
			return nil
		}
		if err := exporter.ApplyBatch(batch, options.Progress); err != nil {
			return err
		}
		fmt.Printf("Created %s\nOpen preview.html for all systems.\n", batch.Out)
		printLaunchBoxMergeHint(target)
		return nil
	}
	plan, err := exporter.Build(options)
	progress.Done()
	if err != nil {
		return err
	}
	printExportSummary(plan)
	fmt.Printf("Output: %s\n", plan.Out)
	if !options.Apply {
		fmt.Println("Preview only. Repeat with --apply to create this new package.")
		return nil
	}
	if err := exporter.Apply(plan, options.Progress); err != nil {
		return err
	}
	fmt.Printf("Created %s\nOpen preview.html, report.txt and README.txt there.\n", plan.Out)
	printLaunchBoxMergeHint(target)
	return nil
}
func printExportSummary(plan *exporter.Plan) {
	if selection := plan.PlatformSelection; selection != nil {
		fmt.Printf("Platform %q => %s (%s); reader %s\nROM folder: %s; %s\n", selection.Requested, selection.Code, selection.Name, selection.System, selection.Folder, selection.MatchedBy)
	}
	if plan.LaunchBox != nil {
		fmt.Printf("%s: %d LaunchBox game cards | %d versions\n", plan.System, len(plan.LaunchBox.Groups), plan.LaunchBox.VersionCount)
	}
	c := exporter.Coverage(plan)
	if c.SourceSets > 0 {
		fmt.Printf("Source sets requiring extraction/installation and launch setup: %d\n", c.SourceSets)
	}
	fmt.Printf("%s coverage: descriptions %d/%d | covers %d/%d | shared descriptions %d | uncategorized artwork %d\n", plan.System, c.Descriptions, c.Matched, c.Covers, c.Matched, c.GameDescriptions, c.UnknownVariants)
	fmt.Printf("%s / %s: %d ROMs | matched: %d (%d hash / %d release or set name / %d game-name) | ambiguous: %d | unmatched: %d | unsupported: %d | media: %d files / %.1f MiB\n", plan.System, plan.Platform, len(plan.Records), plan.Matched, plan.HashMatches, plan.NameMatches, plan.GameOnly, plan.Ambiguous, plan.Unmatched, plan.Unsupported, plan.MediaFiles, float64(plan.MediaBytes)/(1024*1024))
}

func runNetworkExport(options exporter.Options, api string, systems []string, all bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	progress := newTerminalProgress(os.Stdout)
	defer progress.Done()
	options.Progress = func(stage, name string, current, total int) {
		updateExportProgress(progress, stage, name, current, total)
	}
	batch := exporter.BatchOptions{Options: options, All: all, Systems: systems}
	job, err := exportclient.Build(ctx, exportclient.Options{BatchOptions: batch, API: api, Token: os.Getenv("ROMMETA_API_TOKEN")})
	progress.Done()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s network export: %d systems | %d skipped\n", job.Batch.Target, len(job.Batch.Plans), len(job.Batch.Skipped))
	for _, plan := range job.Batch.Plans {
		printExportSummary(plan)
	}
	for _, skip := range job.Batch.Skipped {
		fmt.Printf("Skipped %s: %s\n", skip.System, skip.Reason)
	}
	fmt.Printf("Output: %s\n", job.Batch.Out)
	if len(job.Batch.Plans) == 0 {
		return fmt.Errorf("no systems with selected inputs; no package created")
	}
	if !options.Apply {
		fmt.Println("Preview only. Repeat with --apply to download selected media and create this new package.")
		return nil
	}
	if err = job.Apply(ctx); err != nil {
		return err
	}
	fmt.Printf("Created %s\nOpen preview.html before installing in the frontend.\n", job.Batch.Out)
	printLaunchBoxMergeHint(options.Target)
	return nil
}

func printLaunchBoxMergeHint(target string) {
	if target == "launchbox" {
		fmt.Println("Install: rom-meta export --target launchbox --package <this package> --destination <installation> (add --apply to write).")
	}
}

func selectedSystems(selected string) ([]string, error) {
	var systems []string
	if selected == "" {
		return systems, nil
	}
	for _, name := range strings.Split(selected, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("empty system in --platforms")
		}
		systems = append(systems, name)
	}
	return systems, nil
}

// Listing and package installation must not silently ignore selection/write flags.
func validateExportModeFlags(flags *flag.FlagSet, mode string, allowed ...string) error {
	accepted := map[string]bool{}
	for _, name := range allowed {
		accepted[name] = true
	}
	var invalid []string
	flags.Visit(func(f *flag.Flag) {
		if !accepted[f.Name] {
			invalid = append(invalid, "--"+f.Name)
		}
	})
	if len(invalid) > 0 {
		return fmt.Errorf("%s excludes these options: %s", mode, strings.Join(invalid, ", "))
	}
	return nil
}
