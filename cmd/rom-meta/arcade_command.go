package main

import (
	"fmt"
	"os"

	"rommeta/internal/dat"
)

// Retained for scripts written before automatic DAT dispatch.
func runDATArcade(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printDATArcadeUsage()
		return nil
	}
	return runDATImportCommand(args, true)
}

func importDATArcade(path string, options dat.ArcadeOptions) error {
	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(p dat.ImportProgress) { progress.UpdateTask("dat import", p.Stage, p.Current, p.Total, p.Item) }
	result, err := dat.ImportArcade(path, options)
	progress.Done()
	if result.Classification.Path != "" {
		fmt.Printf("Classifications: %s (%s); SHA256 %s\n", result.Classification.Version, result.Classification.Origin, result.Classification.SHA256)
		fmt.Printf("  %s\n", result.Classification.Path)
		if result.Classification.URL != "" {
			fmt.Printf("  Source: %s\n", result.Classification.URL)
		}
	}
	if result.Version != "" {
		fmt.Printf("Library: %s\n", options.DataRoot)
		fmt.Printf("MAME %s arcade import; snapshot %s; fold: %t\n", result.Version, result.Snapshot, result.Fold)
		fmt.Printf("Scope: %s eligible, %s excluded, %s support, %s require review\n",
			formatCount(result.Counts["eligible"]), formatCount(result.Counts["excluded"]), formatCount(result.Counts["support"]), formatCount(result.Counts["review"]))
		fmt.Printf("Selected: %s releases -> %s game cards; %s folds (%s fewer cards)\n",
			formatCount(result.Counts["selected"]), formatCount(result.Counts["games"]), formatCount(result.Counts["fold_groups"]), formatCount(result.Counts["folded_releases"]))
		fmt.Printf("Arcade hardware families/collections: %s; storage: platforms/Arcade/<game>\n", formatCount(len(result.Families)))
		for _, p := range result.Platforms {
			fmt.Printf("  %s [%s]: %s sets -> %s cards\n", p.Platform, p.Kind, formatCount(p.Releases), formatCount(p.Games))
		}
		if result.Counts["fold_review"] > 0 {
			fmt.Printf("Kept separately for fold review: %s releases\n", formatCount(result.Counts["fold_review"]))
		}
		for _, conflict := range result.Conflicts {
			fmt.Printf("Conflict: %s\n", conflict)
		}
		switch {
		case result.Applied:
			fmt.Println("Result: applied")
		case err != nil:
			fmt.Println("Result: stopped; consult the report for completed platforms")
		default:
			fmt.Println("Result: dry run")
		}
		if options.ReportPath != "" {
			fmt.Printf("Report: %s\n", options.ReportPath)
		}
	}
	return err
}

func printDATArcadeUsage() {
	fmt.Println("dat arcade is a compatibility alias for MAME imports. Prefer dat import.")
	printDATImportUsage()
}
