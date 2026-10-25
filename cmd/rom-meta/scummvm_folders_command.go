package main

import (
	"flag"
	"fmt"
	"os"

	"rommeta/internal/scummvm"
	"rommeta/internal/taskprogress"
)

func runScummVMRenameFolders(args []string) error {
	flags := flag.NewFlagSet("scummvm rename-folders", flag.ContinueOnError)
	root := flags.String("data-root", ".", "catalog root")
	apply := flags.Bool("apply", false, "rename legacy folders with their contents")
	details := flags.Bool("details", false, "print the complete rename mapping")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected scummvm rename-folders argument: %s", flags.Arg(0))
	}
	progress := newTerminalProgress(os.Stdout)
	result, err := scummvm.RenameFolders(scummvm.RenameFoldersOptions{
		DataRoot: *root, Apply: *apply,
		Progress: func(snapshot taskprogress.Snapshot) {
			progress.UpdateTask("scummvm rename-folders", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
		},
	})
	progress.Done()
	if *details {
		for _, change := range result.Changes {
			fmt.Printf("%s -> %s\n", change.From, change.To)
		}
	}
	fmt.Printf("ScummVM folders: %d games; %d planned; %d renamed; %d failed; %d unchanged; %d custom paths preserved.\n", result.Games, len(result.Changes), result.Renamed, result.Failed, result.Unchanged, result.SkippedCustom)
	if result.PlanPath != "" {
		fmt.Printf("Rename plan: %s\n", result.PlanPath)
	}
	if !*apply && err == nil {
		fmt.Println("Dry run; add --apply to rename folders with all metadata and media.")
	}
	return err
}
