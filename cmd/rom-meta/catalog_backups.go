package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"rommeta/internal/catalogbackup"
	"rommeta/internal/catalogedit"
)

func runCatalogBackups(args []string) error {
	flags := flag.NewFlagSet("catalog backups", flag.ContinueOnError)
	root := flags.String("data-root", ".", "Library root")
	remove := flags.String("delete", "", "Exact backup name to remove (preview unless --apply)")
	apply := flags.Bool("apply", false, "Confirm deletion of the named backup")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *apply && *remove == "" {
		return fmt.Errorf("--apply requires --delete <backup name>")
	}
	unlock, err := catalogedit.Lock(*root)
	if err != nil {
		return err
	}
	defer unlock()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if *remove == "" {
		entries, err := catalogbackup.List(*root)
		if err != nil {
			return err
		}
		return encoder.Encode(entries)
	}
	entry, err := catalogbackup.Inspect(*root, *remove)
	if err != nil {
		return err
	}
	if !*apply {
		fmt.Println("Backup to delete (add --apply to confirm):")
		return encoder.Encode(entry)
	}
	if err := catalogbackup.Delete(*root, *remove, entry.Revision); err != nil {
		return err
	}
	fmt.Println("Deleted backup:", *remove)
	return nil
}
