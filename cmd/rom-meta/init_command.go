package main

import (
	"fmt"
	"strings"

	"rommeta/internal/store"
)

func runInit(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printInitUsage()
		return nil
	}
	options := store.InitOptions{}
	var root string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--force-config":
			options.ForceConfig = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown option: %s", arg)
			}
			if root != "" {
				return fmt.Errorf("unexpected argument: %s", arg)
			}
			root = arg
		}
	}
	if root == "" {
		printInitUsage()
		return fmt.Errorf("data root is required")
	}

	result, err := store.Init(root, options)
	if err != nil {
		return err
	}
	printInitResult(result)
	return nil
}

func printInitResult(result store.InitResult) {
	fmt.Println("RomMeta library initialized")
	fmt.Printf("Library: %s\n", result.Root)
	fmt.Printf("Platforms: %s\n", result.PlatformsPath)
	fmt.Printf("Config: %s\n", result.ConfigPath)
	fmt.Printf("Runtime state: %s\n", result.RuntimePath)
	fmt.Printf("source config: %s\n", result.SourcesConfigPath)
	fmt.Printf("catalog config: %s\n", result.CatalogConfigPath)
	fmt.Printf("secrets example: %s\n", result.SecretsExamplePath)
	fmt.Printf("source policies: %s\n", result.SourcePoliciesPath)
	fmt.Printf("created directories: %d\n", result.CreatedDirectoryCount)
	if result.SourcesConfigCreated {
		fmt.Println("sources.yaml: created")
	} else if result.SourcesConfigOverwritten {
		fmt.Println("sources.yaml: overwritten")
	} else {
		fmt.Println("sources.yaml: kept")
	}
	if result.CatalogConfigCreated {
		fmt.Println("catalog.yaml: created")
	} else {
		fmt.Println("catalog.yaml: kept")
	}
	if result.SourcePoliciesCreated {
		fmt.Println("source-policies.yaml: created")
	} else {
		fmt.Println("source-policies.yaml: kept")
	}
	if result.SecretsExampleCreated {
		fmt.Println("secrets.example.yaml: created")
	} else {
		fmt.Println("secrets.example.yaml: kept")
	}
}

func printInitUsage() {
	fmt.Println(`rom-meta init creates the standard local data-root layout.

Usage:
  rom-meta init <data-root> [--force-config]

Options:
  --force-config  Regenerate config/sources.yaml. Other user-editable config files are kept.

Creates:
  platforms/
  config/sources.yaml
  config/catalog.yaml
  config/secrets.example.yaml
  source-policies.yaml
  .rommeta/ runtime cache and download folders`)
}
