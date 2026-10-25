package main

import (
	"encoding/json"
	"fmt"
	"os"
	"rommeta/internal/catalogfold"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"strings"
)

func runCatalogSplit(args []string) error {
	options := catalogfold.SplitOptions{DataRoot: "."}
	details := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			fmt.Println("rom-meta catalog split --data-root <root> --review-plan <json> [--details] [--apply]\nSplit explicitly reviewed content partitions, preserving releases, fingerprints and files.\nDefault is read-only preflight. Every input extra file needs an explicit target.\nOriginal platform backup is retained. Stop catalog writers before --apply.")
			return nil
		case "--data-root", "--review-plan":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return fmt.Errorf("%s requires a value", args[i])
			}
			flag := args[i]
			i++
			if flag == "--data-root" {
				options.DataRoot = args[i]
			} else {
				options.ReviewPlan = args[i]
			}
		case "--apply":
			options.Apply = true
		case "--details":
			details = true
		default:
			return fmt.Errorf("unknown catalog split option: %s", args[i])
		}
	}
	if options.ReviewPlan == "" {
		return fmt.Errorf("catalog split requires --review-plan")
	}
	config, err := store.LoadCatalogConfig(options.DataRoot)
	if err != nil {
		return err
	}
	options.DataRoot = config.Root
	options.RegionPriority = config.Fold.RegionPriority
	options.RegionGroups = config.Fold.RegionGroups
	options.Progress = func(p taskprogress.Snapshot) {
		if p.Current == 1 || p.Current%100 == 0 || p.Current == p.Total {
			fmt.Fprintf(os.Stderr, "progress: %s %d/%d | %s\n", p.Stage, p.Current, p.Total, p.Item)
		}
	}
	result, err := catalogfold.Split(options)
	if err != nil {
		return err
	}
	if details {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Printf("Split: %s; %d reviewed cards, %d new cards; %d releases, %d fingerprints, %d files; apply=%t\n", result.Platform, result.Groups, result.NewCards, result.Releases, result.Fingerprints, result.Files, result.Apply)
	if result.Backup != "" {
		fmt.Println("Backup:", result.Backup)
	}
	return nil
}
