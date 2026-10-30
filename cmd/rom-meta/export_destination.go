package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"rommeta/internal/exportclient"
	"rommeta/internal/exporter"
	"rommeta/internal/frontend"
)

func printDestinationPlan(plan *frontend.Plan) {
	fmt.Printf("Target: %s\nDestination: %s\n", plan.Target, plan.Root)
	if plan.MediaRoot != "" {
		fmt.Println("Media:", plan.MediaRoot)
	}
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
		progress.UpdateTask("export", stage, current, total, name)
	}
	backup, err := plan.Apply()
	if err != nil {
		return err
	}
	if backup == "" {
		fmt.Println("Destination already up to date; no files or backups created.")
	} else {
		fmt.Println("Updated. Backup and changed-file list:", backup)
	}
	return nil
}
func runPackageDestination(target, pkg, destination, mediaRoot, report string, apply bool) error {
	plan, err := frontend.Build(frontend.Options{Target: target, PackageRoot: pkg, Destination: destination, MediaRoot: mediaRoot})
	if err != nil {
		return err
	}
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
func transientOutput(options exporter.Options, destination, api string) (string, error) {
	entropy := make([]byte, 12)
	if _, err := rand.Read(entropy); err != nil {
		return "", err
	}
	for _, parent := range []string{os.TempDir(), filepath.Dir(destination)} {
		out, err := filepath.Abs(filepath.Join(parent, "rommeta-direct-"+hex.EncodeToString(entropy)))
		if err != nil {
			return "", err
		}
		roots := []string{options.ROMRoot, destination}
		if api == "" {
			roots = append(roots, options.DataRoot)
		}
		overlap := false
		for _, root := range roots {
			if root == "" {
				continue
			}
			abs, err := filepath.Abs(root)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(abs, out)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				overlap = true
			}
		}
		if !overlap {
			return out, nil
		}
	}
	return "", fmt.Errorf("cannot select temporary preparation outside the input and destination trees")
}
func runDirectDestination(options exporter.Options, api string, systems []string, all bool, destination, mediaRoot, report string) error {
	if mediaRoot != "" && options.Target != "es-de" {
		return fmt.Errorf("--media-root is only supported for ES-DE")
	}
	stage, err := transientOutput(options, destination, api)
	if err != nil {
		return err
	}
	options.Out = stage
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	options.Context = ctx
	progress := newTerminalProgress(os.Stdout)
	defer progress.Done()
	options.Progress = func(stage, name string, current, total int) {
		progress.UpdateTask("export", stage, current, total, name)
	}
	single := options.Platform != ""
	var plans []*exporter.Plan
	var skipped []exporter.SkippedSystem
	var materialize func() error
	if api != "" {
		job, err := exportclient.Build(ctx, exportclient.Options{BatchOptions: exporter.BatchOptions{Options: options, All: all, Systems: systems}, API: api, Token: os.Getenv("ROMMETA_API_TOKEN")})
		if err != nil {
			return err
		}
		plans, skipped, stage = job.Batch.Plans, job.Batch.Skipped, job.Batch.Out
		single = job.Single
		materialize = func() error { return job.Apply(ctx) }
	} else if single {
		plan, err := exporter.Build(options)
		if err != nil {
			return err
		}
		plans = []*exporter.Plan{plan}
		stage = plan.Out
		materialize = func() error { return exporter.Apply(plan, options.Progress) }
	} else {
		batch, err := exporter.BuildBatch(exporter.BatchOptions{Options: options, All: all, Systems: systems})
		if err != nil {
			return err
		}
		plans, skipped, stage = batch.Plans, batch.Skipped, batch.Out
		materialize = func() error { return exporter.ApplyBatch(batch, options.Progress) }
	}
	progress.Done()
	for _, plan := range plans {
		printExportSummary(plan)
	}
	for _, skip := range skipped {
		fmt.Printf("Skipped %s: %s\n", skip.System, skip.Reason)
	}
	files, err := exporter.PreviewFiles(plans, single)
	if err != nil {
		return err
	}
	source := &frontend.Source{Files: map[string]frontend.SourceFile{}}
	for name, file := range files {
		source.Files[name] = frontend.SourceFile{Data: file.Data, SHA256: file.SHA256, Size: file.Size}
	}
	nativeOptions := frontend.Options{Target: options.Target, PackageRoot: stage, Destination: destination, MediaRoot: mediaRoot, Source: source}
	plan, err := frontend.Build(nativeOptions)
	if err != nil {
		return err
	}
	printDestinationPlan(plan)
	if err = destinationReport(plan, report); err != nil {
		return err
	}
	if !options.Apply {
		fmt.Println("Preview only. Repeat with --apply to update this destination.")
		return nil
	}
	if err = plan.Check(); err != nil {
		return err
	}
	if len(plan.Files) == 0 {
		fmt.Println("Destination already up to date; no files or backups created.")
		return nil
	}
	if err = materialize(); err != nil {
		return err
	}
	// materialize publishes a new directory only on success; cleanup owns that result.
	defer os.RemoveAll(stage)
	if err = plan.Check(); err != nil {
		return err
	}
	nativeOptions.Source = nil
	installed, err := frontend.Build(nativeOptions)
	if err != nil {
		return err
	}
	return applyDestination(installed)
}
