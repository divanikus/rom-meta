package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/platformnames"
	"rommeta/internal/sourceassets"
	"rommeta/internal/taskprogress"
)

func runCatalogMediaNames(args []string) error {
	flags := flag.NewFlagSet("catalog media-names", flag.ContinueOnError)
	options := sourceassets.MediaNamesOptions{}
	flags.StringVar(&options.DataRoot, "data-root", ".", "Data root containing platforms/.")
	flags.StringVar(&options.Platform, "platform", "", "Optional platform name or alias.")
	flags.StringVar(&options.Source, "source", "launchbox", "Source whose saved media types label filenames.")
	flags.StringVar(&options.Match, "match", "", "Optional case-insensitive game folder substring.")
	flags.BoolVar(&options.Apply, "apply", false, "Rename local files; default is a dry run.")
	details := flags.Bool("details", false, "Print each old and new path.")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: rom-meta catalog media-names [--data-root <root>] [--platform <name>] [--source launchbox] [--match <text>] [--details] [--apply]")
		fmt.Fprintln(flags.Output(), "Offline naming upgrade using saved source categories and downloader candidate order. No downloads or overwrites; unknown files stay unchanged. Exact path overrides must follow renamed files.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	var err error
	options.Platform, err = catalogPlatformDirectory(options.DataRoot, options.Platform)
	if err != nil {
		return err
	}
	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(snapshot taskprogress.Snapshot) {
		progress.UpdateTask("media names", snapshot.Stage, snapshot.Current, snapshot.Total, snapshot.Item)
	}
	result, err := sourceassets.NameMedia(options)
	progress.Done()
	if *details {
		for _, change := range result.Changes {
			fmt.Printf("%s -> %s\n", change.From, change.To)
		}
	}
	fmt.Printf("Media names: %d planned, %d renamed, %d already named, %d conflicts skipped.\n", result.Planned, result.Renamed, result.Existing, result.Conflicts)
	return err
}

// catalogPlatformDirectory binds a filter to a real directory before applying aliases.
// A legacy catalog name must not disappear into an absent registry default.
func catalogPlatformDirectory(root, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}
	entries, err := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), input) {
			return entry.Name(), nil
		}
	}
	registry, _, err := platformnames.LoadRegistry(root)
	if err != nil {
		return "", err
	}
	want := registry.ResolveCLI(input)
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() && registry.ResolveCLI(entry.Name()) == want {
			matches = append(matches, entry.Name())
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no catalog platform directory matches %q", input)
	default:
		return "", fmt.Errorf("platform alias %q matches multiple directories: %s; use an exact directory name", input, strings.Join(matches, ", "))
	}
}
