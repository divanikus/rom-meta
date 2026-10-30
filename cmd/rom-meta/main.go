package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"rommeta/internal/appinfo"
	"rommeta/internal/catalog"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
)

func main() { os.Exit(runCommand()) }

func runCommand() (code int) {
	changes := catalogchanges.Begin(func(root, platform string) error {
		fmt.Fprintf(os.Stderr, "Updating catalog index: %s\n", platform)
		return exporter.RefreshPlatformIndex(root, platform)
	})
	defer catalogfs.Cleanup()
	defer func() {
		if err := changes.Finish(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if code == 0 {
				code = 2
			}
		}
	}()
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "help" {
		args = append(append([]string{}, args[1:]...), "--help")
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		printUsage()
		return
	}

	switch args[0] {
	case "version", "--version":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "version accepts no arguments")
			return 2
		}
		fmt.Println(appinfo.DisplayName())
	case "catalog":
		if err := runCatalog(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "gamelist":
		if err := runGamelist(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	case "scummvm":
		if err := runScummVM(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "dat":
		if err := runDAT(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "romm":
		if err := runRomM(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "launchbox":
		if err := runLaunchBox(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	case "export":
		if err := runExport(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "fetch":
		if err := runFetch(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "init":
		if err := runInit(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "platforms":
		if err := runPlatforms(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "sources":
		if err := runSources(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "serve":
		if err := runServe(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
	case "validate":
		if len(args) > 1 && isHelpArg(args[1]) {
			printValidateUsage()
			return
		}
		if len(args) > 2 || len(args) > 1 && strings.HasPrefix(args[1], "-") {
			fmt.Fprintln(os.Stderr, "usage: rom-meta validate [root]")
			return 2
		}
		root := "."
		if len(args) > 1 {
			root = args[1]
		}
		progress := newTerminalProgress(os.Stdout)
		issues, err := catalog.ValidateWithOptions(root, catalog.ValidateOptions{
			Progress: func(snapshot catalog.ValidateProgress) {
				progress.UpdateTask("validate", validateProgressStage(snapshot), snapshot.Current, snapshot.Total, validateProgressLabel(snapshot))
			},
		})
		progress.Done()
		if err != nil {
			fmt.Fprintf(os.Stderr, "validation failed: %v\n", err)
			return 2
		}
		if len(issues) > 0 {
			for _, issue := range issues {
				fmt.Fprintf(os.Stderr, "%s: %s\n", issue.Path, issue.Message)
			}
			return 1
		}
		fmt.Println("Validation passed. Catalog looks consistent.")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage()
		catalogfs.Cleanup()
		return 2
	}
	return
}

func isHelpArg(value string) bool {
	return value == "help" || value == "-h" || value == "--help"
}

func formatCount(value int) string {
	text := strconv.Itoa(value)
	if value < 0 {
		text = strings.TrimPrefix(text, "-")
	}
	if len(text) <= 3 {
		if value < 0 {
			return "-" + text
		}
		return text
	}
	var parts []string
	for len(text) > 3 {
		parts = append([]string{text[len(text)-3:]}, parts...)
		text = text[:len(text)-3]
	}
	parts = append([]string{text}, parts...)
	result := strings.Join(parts, ",")
	if value < 0 {
		return "-" + result
	}
	return result
}

func printUsage() {
	fmt.Println(`RomMeta builds and reads portable ROM metadata catalogs.

Usage:
  rom-meta <command> [options]
  rom-meta <command> --help
  rom-meta --version

First catalog:
  rom-meta init <catalog>
  rom-meta dat plan <platform.dat-or-zip>
  rom-meta dat import <platform.dat-or-zip> --data-root <catalog>
  rom-meta dat import <platform.dat-or-zip> --data-root <catalog> --apply
  rom-meta serve --data-root <catalog>

Commands:
  init       Initialize catalog folders and source configuration.
  dat        Plan and import DAT/XML releases and fingerprints.
  scummvm    Import game cards from official ScummVM data.
  gamelist   Import private ES-DE metadata and referenced media.
  fetch      Collect configured metadata, artwork and video.
  sources    Inspect configuration, credentials readiness and coverage.
  platforms  List platform aliases and source mappings.
  validate   Check catalog layout and metadata; usage: validate [root].
  catalog    Inspect status, prune placeholders, label media, fold or split cards.
  serve      Start one catalog browser, API and OpenAPI/Swagger service.
  export     Generate a staged offline ES-DE or LaunchBox package.
  launchbox  Merge an exported package into a LaunchBox library.
  romm       JSON protocol helpers for the RomM server importer example.
  version    Print the build version.

Catalog mutations default to preview; --apply writes changes.
init creates a data root. HTTP metadata is read-only; lookup may maintain optional indexes.
Run catalog preparation and consumer imports sequentially.
Main workflow: dat import -> fetch -> catalog fold -> export.
Detailed flags: rom-meta <command> --help or rom-meta help <command>.`)
}

func printCatalogUsage() {
	fmt.Println(`rom-meta catalog inspects and cleans the local portable catalog.

Usage:
  rom-meta catalog status [--data-root <root>] [--platform <name>] [--details]
  rom-meta catalog prune-empty [--data-root <root>] [--platform <name>] [--details] [--apply]
  rom-meta catalog media-names [--data-root <root>] [--platform <name>] [--source launchbox] [--match <text>] [--details] [--apply]
  rom-meta catalog split --data-root <root> --review-plan <json> [--details] [--apply]
  rom-meta catalog fold [--data-root <root>] [--platform <name>] [--match <text>] [--editions-only] [--review-plan <json>] [--details] [--apply|--interactive]

Commands:
  status       Report platform, game, release, metadata coverage, media, and empty-game counts.
  prune-empty  Delete empty games only when --apply is passed.
  media-names  Label existing artwork variants offline using saved source categories.
  fold         Preview or merge game folders using source IDs or accepted MAME relationships.

Options:
  --data-root <root>  Data root containing platforms/.
  --platform <name>   Limit to one platform. Short aliases such as n64 or pcengine are accepted.
  --details           Include detailed status, cleanup, or fold candidates.
  --apply             Apply prune-empty deletion or fold merges. Without it, mutating commands are dry runs.
  --interactive       Review fold candidates one by one. yes applies; no saves a rejection marker.`)
}

func printCatalogStatusUsage() {
	fmt.Println(`rom-meta catalog status reports the current local catalog shape.

Usage:
  rom-meta catalog status [--data-root <root>] [--platform <name>] [--details]

Options:
  --data-root <root>  Data root containing platforms/.
  --platform <name>   Limit to one platform. Short aliases such as n64 or segacd are accepted.
  --details           Include empty-game details.`)
}

func printCatalogPruneEmptyUsage() {
	fmt.Println(`rom-meta catalog prune-empty removes empty games with no metadata or media.

Usage:
  rom-meta catalog prune-empty [--data-root <root>] [--platform <name>] [--details] [--apply]

Options:
  --data-root <root>  Data root containing platforms/.
  --platform <name>   Limit to one platform. Short aliases such as n64 or segacd are accepted.
  --details           Include candidate folders.
  --apply             Delete candidates. Without this flag, the command is a dry run.`)
}

func printCatalogFoldUsage() {
	fmt.Println(`rom-meta catalog fold merges compatible game bundles while preserving metadata.

Usage:
  rom-meta catalog split --data-root <root> --review-plan <json> [--details] [--apply]
  rom-meta catalog fold [--data-root <root>] [--platform <name>] [--match <text>] [--editions-only] [--review-plan <json>] [--details] [--apply|--interactive]

Options:
  --data-root <root>  Data root containing platforms/.
  --platform <name>   Limit to one platform. Short aliases such as n64 or segacd are accepted.
  --match <text>      Only show or apply candidate groups whose titles/evidence contain this text.
  --review-plan       Use reviewed partitions and SHA256 checks; exclusive of filters/interactive.
  --editions-only     Inspect explicit edition/port families; different content stays separate.
  --details           Include fold candidates, target names and identity evidence.
  --apply             Merge eligible candidates; held groups remain unchanged. Default: dry run.
  --interactive       Ask "Apply fold? yes/no/skip/quit" for each candidate.

Regional titles and existing files:
  Saved provider aliases can link regional titles on the same platform.
  Regional merges require compatible content and pairwise identity confirmation.
  Different titles need two independent providers or an anchored, region-matched alias.
  Demos, supplements, different editions and mixed legacy cards remain separate.
  --details explains held groups and media collisions; no provider requests are made.
  PS3 disc serials use a bundled, reviewed identity registry; unknown or mixed cards stay separate.
  Xbox 360 Redump serials discover additional candidates, never automatic identities.
  Serial-only links need --interactive confirmation; conflicts remain held.
  Existing keep-separate markers and provider matching safeguards remain in effect.
  Identical colliding files are deduplicated; distinct media variants are retained.
  Original platform backups are kept and their paths printed. Stop writers before --apply.

MAME:
  Accepted MAME sets use the same grouping rules as DAT import, preserving set bindings.
  New imports already fold by default; running fold again normally has nothing to change.
  Preview with --platform Arcade --details; add --apply to merge or rename existing cards.
  Metadata, media and original backups are preserved. Stop catalog writers before applying.
  One snapshot is supported, including partial imports; splits and conflicting data are held.
  Source-ID interactive/review-plan modes do not change MAME bindings; --editions-only excludes them.

Config:
  config/catalog.yaml controls the region priority used to choose the kept folder.
  fold.region_priority is the global default, and fold.platforms.<platform>.region_priority can override it.
  fold.region_groups maps region aliases such as USA, US, JAP, JA, China, CN, ZH, Germany, France, or Russia into priority groups.
  game.yaml can disable reviewed bad source identities with fold.ignore_evidence: ["source.field=value"].`)
}

func printValidateUsage() {
	fmt.Println(`rom-meta validate checks catalog layout and minimal metadata contracts.

Usage:
  rom-meta validate [root]

Arguments:
  root  Data root to validate. Defaults to the current directory.`)
}
