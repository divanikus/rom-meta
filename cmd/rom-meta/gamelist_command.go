package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"rommeta/internal/gamelist"
)

func runGamelist(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printGamelistUsage()
		return nil
	}
	if args[0] != "import" {
		return fmt.Errorf("unknown gamelist command: %s", args[0])
	}
	options, report, help, err := parseGamelistOptions(args[1:])
	if err != nil {
		return err
	}
	if help {
		printGamelistUsage()
		return nil
	}
	progress := newTerminalProgress(os.Stdout)
	options.Progress = func(name string, current, total int) {
		progress.UpdateTask("gamelist import", "reading private curation", current, total, name)
	}
	result, err := gamelist.Import(options)
	progress.Done()
	if err != nil {
		return err
	}
	mode := "Dry run"
	if options.Apply {
		mode = "Applied"
	}
	fmt.Printf("%s: %s / %s\nEntries: %d; matched: %d; unmatched: %d; ambiguous: %d\nChanged cards: %d; new media: %d (%d bytes); missing media: %d\n", mode, result.Platform, result.Source, result.Entries, result.Matched, result.Unmatched, result.Ambiguous, result.Updated, result.MediaAdded, result.MediaBytes, result.MissingMedia)
	if result.Backup != "" {
		fmt.Printf("Backups and receipt: %s\n", result.Backup)
	}
	if report != "" {
		body, e := json.MarshalIndent(result, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(report, body, 0600); e != nil {
			return e
		}
		fmt.Printf("Report: %s\n", report)
	}
	if !options.Apply {
		fmt.Println("Add --apply to import exact release-name matches.")
	}
	if result.MissingMedia > 0 {
		return fmt.Errorf("import has %d unavailable media references; see report", result.MissingMedia)
	}
	return nil
}
func parseGamelistOptions(args []string) (o gamelist.Options, report string, help bool, err error) {
	o.DataRoot = "."
	for i := 0; i < len(args); i++ {
		a := args[i]
		if isHelpArg(a) {
			return o, report, true, nil
		}
		if a == "--apply" {
			o.Apply = true
			continue
		}
		if !strings.HasPrefix(a, "-") {
			if o.Input != "" {
				return o, report, false, fmt.Errorf("only one gamelist input is allowed")
			}
			o.Input = a
			continue
		}
		k, v, has := strings.Cut(a, "=")
		switch k {
		case "--data-root", "--platform", "--collection", "--language", "--member", "--origin", "--report":
		default:
			return o, report, false, fmt.Errorf("unknown gamelist option: %s", a)
		}
		if !has {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return o, report, false, fmt.Errorf("%s requires a value", k)
			}
			i++
			v = args[i]
		}
		if strings.TrimSpace(v) == "" {
			return o, report, false, fmt.Errorf("%s requires a value", k)
		}
		switch k {
		case "--data-root":
			o.DataRoot = v
		case "--platform":
			o.Platform = v
		case "--collection":
			o.Collection = v
		case "--language":
			o.Language = v
		case "--member":
			o.Member = v
		case "--origin":
			o.Origin = v
		case "--report":
			report = v
		}
	}
	if o.Input == "" || o.Collection == "" || o.Language == "" {
		err = fmt.Errorf("gamelist import requires input, --collection and --language")
	}
	return
}
func printGamelistUsage() {
	fmt.Println(`rom-meta gamelist import harvests existing ES-DE metadata and referenced media.

Usage:
  rom-meta gamelist import <gamelist.xml-or-pack.zip> --collection <name> --language <code> [options]

Options:
  --data-root <root>  Existing catalog (default: current directory).
  --platform <name>   Catalog platform; defaults to provider.system.
  --collection <id>   Lowercase private source name: gamelist.<id>.
  --language <code>   Language of descriptions, separate from ROM languages.
  --member <path>     Gamelist ZIP member when the archive has multiple lists.
  --origin <id>       Stable relative identifier for repeated imports of one list.
  --report <file>     Private JSON report, including unresolved paths.
  --apply             Write source candidates and referenced media.

Only unique complete release names and explicit release aliases are matched.
Unresolved entries remain in the report; no cards, releases or fingerprints are invented.
Imported curation is private by default. Existing canonical fields and other sources survive.
No ROM payload is read, extracted or copied.`)
}
