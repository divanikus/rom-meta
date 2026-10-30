package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"rommeta/internal/launchbox"
)

func runLaunchBox(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Println("Usage: rom-meta launchbox merge --package <export> --launchbox-root <installation> [--report <new.json>] [--apply]\n\nPreview additive enrichment of an existing library. Close LaunchBox/Big Box before --apply.\nExisting IDs, nonempty fields, launch settings, favorites and history are preserved.")
		return nil
	}
	if args[0] != "merge" {
		return fmt.Errorf("unknown launchbox command: %s", args[0])
	}
	flags := flag.NewFlagSet("launchbox merge", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	var options launchbox.Options
	var apply bool
	var report string
	flags.StringVar(&options.PackageRoot, "package", "", "Reviewed LaunchBox export package")
	flags.StringVar(&options.LaunchBoxRoot, "launchbox-root", "", "LaunchBox installation containing Data/Platforms")
	flags.StringVar(&report, "report", "", "Write the plan to a new JSON report file (contains private paths)")
	flags.BoolVar(&apply, "apply", false, "Apply additive merge after validation and backup")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional launchbox merge arguments")
	}
	if options.PackageRoot == "" || options.LaunchBoxRoot == "" {
		return fmt.Errorf("--package and --launchbox-root are required")
	}
	plan, err := launchbox.Build(options)
	if err != nil {
		return err
	}
	fmt.Printf("LaunchBox: %s\nPackage: %s\n", plan.Root, plan.Package)
	for _, change := range plan.Changes {
		fmt.Printf("%s / %s: %s | %d fields | %d new versions | %d media | %d playlists\n", change.Platform, change.Title, change.Action, len(change.Fields), change.Versions, change.Media, change.Playlists)
	}
	for _, warning := range plan.Warnings {
		fmt.Println("Warning:", warning)
	}
	for _, conflict := range plan.Conflicts {
		fmt.Println("Conflict:", conflict)
	}
	fmt.Printf("%d files to write | %d conflicts\n", len(plan.Files), len(plan.Conflicts))
	if report != "" {
		body, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(append(body, '\n'))
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if !apply {
		fmt.Println("Preview only. Repeat with --apply to merge into this installation.")
		return nil
	}
	backup, err := plan.Apply()
	if err != nil {
		return err
	}
	if backup == "" {
		fmt.Println("Library already up to date; no files or backups created.")
	} else {
		fmt.Printf("Merged. Backup and changed-file list: %s\n", backup)
	}
	return nil
}
