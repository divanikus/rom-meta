package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
	"rommeta/internal/frontend"
)

func printDestinationPlan(plan *frontend.Plan) {
	fmt.Printf("Target: %s\nDestination: %s\n", plan.Target, plan.Root)
	if plan.MediaRoot != "" {
		fmt.Println("Media:", plan.MediaRoot)
	}
	for _, change := range plan.Changes {
		fmt.Printf("%s / %s: %s | %d fields | %d new versions | %d media | %d playlists\n", change.Platform, change.Title, change.Action, len(change.Fields), change.Versions, change.Media, change.Playlists)
		if change.Consolidated > 0 {
			fmt.Printf("  %d obsolete RomMeta cards consolidated\n", change.Consolidated)
		}
	}
	for _, warning := range plan.Warnings {
		fmt.Println("Warning:", warning)
	}
	for _, conflict := range plan.Conflicts {
		fmt.Println("Conflict:", conflict)
	}
	fmt.Printf("%d files to write | %d conflicts\n", len(plan.Files), len(plan.Conflicts))
}
func destinationReport(plan *frontend.Plan, name string) error {
	if name == "" {
		return nil
	}
	copy := *plan
	if copy.Generated() {
		copy.Package = ""
	}
	body, err := json.MarshalIndent(&copy, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(body, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
func applyDestination(plan *frontend.Plan) error {
	progress := newTerminalProgress(os.Stdout)
	defer progress.Done()
	plan.Progress = func(stage, name string, current, total int) {
		if stage == "check files" {
			if name != "" {
				progress.UpdateTask("export", "write", 0, len(plan.Files), "checking "+name)
			}
			return
		}
		progress.UpdateTask("export", "write", current, total, name)
	}
	progress.UpdateTask("export", "write", 0, len(plan.Files), "checking inputs")
	err := plan.Apply()
	if err != nil {
		return err
	}
	progress.Done()
	if len(plan.Files) == 0 {
		fmt.Println("Destination already up to date; no files written.")
	} else {
		fmt.Println("Updated.")
	}
	return nil
}
func runPackageDestination(target, pkg, destination, mediaRoot, report string, apply bool) error {
	progress := newTerminalProgress(os.Stdout)
	defer progress.Done()
	notify := func(stage, name string, current, total int) {
		updateExportProgress(progress, stage, name, current, total)
	}
	plan, err := frontend.Build(frontend.Options{Progress: notify, Target: target, PackageRoot: pkg, Destination: destination, MediaRoot: mediaRoot})
	if err != nil {
		return err
	}
	progress.Done()
	printDestinationPlan(plan)
	if err = destinationReport(plan, report); err != nil {
		return err
	}
	if !apply {
		fmt.Println("Preview only. Repeat with --apply to update this destination.")
		return nil
	}
	return applyDestination(plan)
}
func runDirectDestination(options exporter.Options, api string, systems []string, all bool, destination, mediaRoot, report string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	options.Context = ctx
	progress := newTerminalProgress(os.Stdout)
	defer progress.Done()
	options.Progress = func(stage, name string, current, total int) {
		updateExportProgress(progress, stage, name, current, total)
	}
	job, err := exportjob.Build(options, api, systems, all, destination, mediaRoot, os.Getenv("ROMMETA_API_TOKEN"))
	if err != nil {
		return err
	}
	progress.Done()
	for _, plan := range job.Plans {
		printExportSummary(plan)
	}
	for _, skip := range job.Skipped {
		fmt.Printf("Skipped %s: %s\n", skip.System, skip.Reason)
	}
	printDestinationPlan(job.Native)
	if err = destinationReport(job.Native, report); err != nil {
		return err
	}
	if !options.Apply {
		fmt.Println("Preview only. Repeat with --apply to update this destination.")
		return nil
	}
	return applyDestination(job.Native)
}

func updateExportProgress(progress *terminalProgress, stage, name string, current, total int) {
	original := stage
	if stage == "write" || strings.HasSuffix(stage, " / write") || stage == "download" || strings.Contains(stage, " / download") {
		stage = "write"
	} else {
		stage = "scan"
	}
	if name == "" {
		name = original
	} else if original == "index" || original == "reindexing" || strings.HasSuffix(original, " / index") || strings.HasSuffix(original, " / reindexing") {
		name = original + ": " + name
	}
	progress.UpdateTask("export", stage, current, total, name)
}
