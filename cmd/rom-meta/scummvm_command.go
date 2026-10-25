package main

import (
	"fmt"
	"os"
	"strings"

	"rommeta/internal/scummvm"
	"rommeta/internal/taskprogress"
)

func runScummVM(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printScummVMUsage()
		return nil
	}
	if args[0] == "rename-folders" {
		return runScummVMRenameFolders(args[1:])
	}
	if args[0] != "import" {
		return fmt.Errorf("unknown scummvm command: %s", args[0])
	}
	options, help, err := parseScummVMImportOptions(args[1:])
	if err != nil {
		return err
	}
	if help {
		printScummVMUsage()
		return nil
	}
	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(snapshot taskprogress.Snapshot) {
		progress.UpdateTask("scummvm import", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	result, err := scummvm.Import(options)
	progress.Done()
	if err != nil {
		return err
	}
	mode := "Dry run"
	if result.Apply {
		mode = "Applied"
	}
	fmt.Printf("%s: ScummVM catalog\nVersion: %s\n", mode, result.Version)
	if result.Official != nil {
		fmt.Printf("Official source: %s\nRevision: %s\nRelease: %s; excluded broken/untested/disabled entries: %s\n", result.Official.URL, result.Official.Revision, result.Official.Release, formatCount(result.Official.Excluded))
	}
	fmt.Printf("List: %s entries; %s selected; %s generic engine entries skipped\n",
		formatCount(result.Supported), formatCount(result.Selected), formatCount(result.SkippedGeneric))
	fmt.Printf("Cards: %s new; %s updated; %s unchanged\n",
		formatCount(result.Added), formatCount(result.Updated), formatCount(result.Unchanged))
	fmt.Printf("Snapshot: %s\n", result.SnapshotPath)
	if result.Apply {
		fmt.Printf("Written files: %s\n", formatCount(result.WrittenFiles))
	} else {
		fmt.Println("Add --apply to write the cards and source snapshot.")
	}
	fmt.Println("Next: rom-meta fetch --data-root <root> --platform scummvm")
	return nil
}

func parseScummVMImportOptions(args []string) (scummvm.ImportOptions, bool, error) {
	options := scummvm.ImportOptions{DataRoot: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isHelpArg(arg) {
			return options, true, nil
		}
		if arg == "--offline" {
			options.Offline = true
			continue
		}
		if arg == "--apply" {
			options.Apply = true
			continue
		}
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--data-root", "--scummvm", "--list-file", "--version", "--engine", "--release", "--revision":
		default:
			return options, false, fmt.Errorf("unknown scummvm import option: %s", arg)
		}
		if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return options, false, fmt.Errorf("%s requires a value", key)
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return options, false, fmt.Errorf("%s requires a value", key)
		}
		switch key {
		case "--data-root":
			options.DataRoot = value
		case "--release":
			options.Release = value
		case "--revision":
			options.Revision = value
		case "--scummvm":
			options.Executable = value
		case "--list-file":
			options.ListFile = value
		case "--version":
			options.Version = value
		case "--engine":
			options.Engines = append(options.Engines, value)
		}
	}
	if options.ListFile != "" && options.Executable != "" {
		return options, false, fmt.Errorf("--list-file and --scummvm are mutually exclusive")
	}
	return options, false, nil
}

func printScummVMUsage() {
	fmt.Println(`rom-meta scummvm import creates cards from official ScummVM data.

Usage:
  rom-meta scummvm rename-folders [--data-root <root>] [--details] [--apply]
  rom-meta scummvm import [--data-root <root>] [--release <version>] [--revision <sha>] [--engine <ids>] [--offline] [--apply]
  rom-meta scummvm import --scummvm <executable> [--data-root <root>] [--engine <ids>] [--apply]
  rom-meta scummvm import --list-file <games.txt> --version <version> [--data-root <root>] [--engine <ids>] [--apply]

Options:
  --data-root <root> Catalog root. Default: current directory.
  --release <ver>    Stable website release. Default: latest published release.
  --revision <sha>   Pin official website data to a full commit SHA.
  --offline         Reuse the last applied official snapshot without network.
  --engine <ids>     Optional comma-separated engine IDs, for example scumm,sci.
  --scummvm <path>   Explicit alternative: read an installed ScummVM executable.
  --list-file <file> Explicit alternative: saved scummvm --list-games output.
  --version <text>   Required with --list-file.
  --apply            Write cards and snapshot. Default: read-only preview.

Default import downloads structured data from scummvm/scummvm-web; no ScummVM
installation or game files are needed. It selects the stable release's enabled
games with good, excellent or bugged support. Broken/untested entries are excluded.
The website covers curated titles and engine categories, not every engine's
detectable fan game. Known generic categories are skipped.
New card folders use the game title; a qualified ID is added only for name collisions.
rename-folders previews removal of legacy suffixes; --apply moves whole bundles,
preserves custom paths and saves a private rename plan. Stop other writers first.
Source commit, raw YAML, checksums and normalized game list are saved on apply.
--release, --revision and --offline cannot be combined with explicit local input.
Repeat import preserves curation, source metadata and media; omitted IDs remain.
Stop other writers before applying. Repeat an interrupted import to resume.
Then use ordinary fetch --platform scummvm for metadata, artwork and video.
See docs/scummvm.md for source setup, commands, coverage and limitations.`)
}
