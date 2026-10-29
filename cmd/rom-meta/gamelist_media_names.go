package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"rommeta/internal/gamelist"
)

func runGamelistMediaNames(args []string) error {
	flags := flag.NewFlagSet("gamelist media-names", flag.ContinueOnError)
	var o gamelist.MediaNamesOptions
	var report string
	flags.StringVar(&o.DataRoot, "data-root", ".", "Catalog root")
	flags.StringVar(&o.Platform, "platform", "", "Optional exact catalog platform")
	flags.StringVar(&o.Collection, "collection", "", "Collection identifier (required)")
	flags.StringVar(&report, "report", "", "Write the private rename report")
	flags.BoolVar(&o.Apply, "apply", false, "Apply the reviewed renames")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected media-names argument")
	}
	progress := newTerminalProgress(os.Stdout)
	o.Progress = func(name string, current, total int) {
		progress.UpdateTask("media names", "renaming imported media", current, total, name)
	}
	result, err := gamelist.NameMedia(o)
	progress.Done()
	if err != nil {
		return err
	}
	if report != "" {
		body, e := json.MarshalIndent(result, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(report, body, 0600); e != nil {
			return e
		}
	}
	fmt.Printf("Cards: %d; renamed media paths: %d\n", result.Cards, result.Renamed)
	if result.Backup != "" {
		fmt.Printf("Backups and rename receipt: %s\n", result.Backup)
	}
	if !o.Apply {
		fmt.Println("Dry run; add --apply to rename files and update YAML references.")
	}
	return nil
}
