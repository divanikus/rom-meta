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
	all, list := false, false
	flags.StringVar(&api, "api", "", "Catalog HTTP(S) origin; reads local ROMs without a local catalog")
	flags.StringVar(&target, "target", "es-de", "Output target (es-de or launchbox)")
	flags.StringVar(&options.DataRoot, "data-root", ".", "Local catalog root")
	flags.StringVar(&options.Platform, "platform", "", "One catalog platform or supported ES-DE system")
	flags.StringVar(&selected, "platforms", "", "Comma-separated ES-DE system directories under --rom-root")
	flags.BoolVar(&all, "all", false, "Discover supported system directories under the ES-DE ROM root")
	flags.BoolVar(&list, "list-systems", false, "List supported ES-DE launch readers")
	flags.StringVar(&options.ArcadeSnapshot, "arcade-snapshot", "", "Saved MAME catalog snapshot (auto-select only when exactly one exists)")
	flags.StringVar(&options.ROMRoot, "rom-root", "", "ROM system directory, or ES-DE root with --all/--platforms")
	flags.StringVar(&options.Out, "out", "", "New output directory outside catalog/ROM trees")
	flags.StringVar(&options.ProfilePath, "profile", "", "Optional YAML export selection profile")
	flags.StringVar(&options.Match, "match", "", "Select ROM relative paths containing this text")
	flags.IntVar(&options.Limit, "limit", 0, "Maximum inputs per system in sorted path order (0: all)")
	flags.BoolVar(&options.Apply, "apply", false, "Write a new staged package after planning")
	flags.Usage = func() {
		fmt.Fprint(os.Stdout, "rom-meta export builds an offline frontend package from existing ROMs.\n\nUse --platform for one system, --platforms for selected folders or --all for the ES-DE root.\nUse --api <origin> for a remote catalog, or --data-root for local files.\nDry-run by default; --apply writes only a new output directory.\nSource ROMs and catalog files stay unchanged. See --list-systems for readers.\n")
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
	if _, err := exporter.NormalizeTarget(target); err != nil {
		return err
	}
	options.Target = target
	if list {
		fmt.Println("Supported ES-DE systems:", strings.Join(exporter.SupportedSystems(), ", "))
		fmt.Println("Cartridges: raw/single-payload ZIP. Discs: standalone CHD/ISO. Arcade/MAME: saved set-name bindings for ZIP/7z; no payload/dependency validation.")
		return nil
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
	if api != "" {
		explicitRoot := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "data-root" {
				explicitRoot = true
			}
		})
		if explicitRoot {
			return fmt.Errorf("choose --api or --data-root, not both")
		}
		return runNetworkExport(options, api, selected, all)
	}
	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(stage, name string, current, total int) {
		progress.UpdateTask("export", stage, current, total, name)
	}
	defer progress.Done()
	if all || selected != "" {
		var systems []string
		if selected != "" {
			for _, name := range strings.Split(selected, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					return fmt.Errorf("empty system in --platforms")
				}
				systems = append(systems, name)
			}
		}
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
		if !options.Apply {
			fmt.Println("Preview only. Repeat with --apply to create this new package.")
			return nil
		}
		if err := exporter.ApplyBatch(batch, options.Progress); err != nil {
			return err
		}
		fmt.Printf("Created %s\nOpen preview.html for all systems.\n", batch.Out)
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
	return nil
}
func printExportSummary(plan *exporter.Plan) {
	c := exporter.Coverage(plan)
	fmt.Printf("%s display: descriptions %d/%d | covers %d/%d | game-scope prose %d | unknown artwork categories %d\n", plan.System, c.Descriptions, c.Matched, c.Covers, c.Matched, c.GameDescriptions, c.UnknownVariants)
	fmt.Printf("%s / %s: %d ROMs | matched: %d (%d hash / %d release or set name / %d game-name) | ambiguous: %d | unmatched: %d | unsupported: %d | media: %d files / %.1f MiB\n", plan.System, plan.Platform, len(plan.Records), plan.Matched, plan.HashMatches, plan.NameMatches, plan.GameOnly, plan.Ambiguous, plan.Unmatched, plan.Unsupported, plan.MediaFiles, float64(plan.MediaBytes)/(1024*1024))
}

func runNetworkExport(options exporter.Options, api, selected string, all bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	progress := newTerminalProgress(os.Stdout)
	defer progress.Done()
	options.Progress = func(stage, name string, current, total int) {
		progress.UpdateTask("export", stage, current, total, name)
	}
	batch := exporter.BatchOptions{Options: options, All: all}
	if selected != "" {
		for _, value := range strings.Split(selected, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				return fmt.Errorf("empty system in --platforms")
			}
			batch.Systems = append(batch.Systems, value)
		}
	}
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
	if !options.Apply {
		fmt.Println("Preview only. Repeat with --apply to download selected media and create this new package.")
		return nil
	}
	if err = job.Apply(ctx); err != nil {
		return err
	}
	fmt.Printf("Created %s\nOpen preview.html before installing in the frontend.\n", job.Batch.Out)
	return nil
}
