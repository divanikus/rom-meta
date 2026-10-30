package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/exportclient"
	"rommeta/internal/exporter"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
)

type platformListOptions struct {
	API, ROMRoot string
	DataRoot     string
	Format       string
	OutPath      string
	Full         bool
	Details      bool
}

type platformListResult struct {
	Selector             *exporter.PlatformSelector
	Folders              []exporter.FolderSelection
	Skipped              []exporter.SkippedSystem
	DataRoot             string
	ConfigPath           string
	LocalConfigLoaded    bool
	Full                 bool
	Platforms            []platformnames.Platform
	UnknownPlatformCount int
}

func runPlatforms(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printPlatformsUsage()
		return nil
	}
	switch args[0] {
	case "list":
		return runPlatformsList(args[1:])
	default:
		return fmt.Errorf("unknown platforms command: %s", args[0])
	}
}

func runPlatformsList(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printPlatformsUsage()
		return nil
	}
	options := platformListOptions{DataRoot: ".", Format: "text"}
	dataRootExplicit := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--api" || arg == "--rom-root":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", arg)
			}
			i++
			if arg == "--api" {
				options.API = args[i]
			} else {
				options.ROMRoot = args[i]
			}
		case strings.HasPrefix(arg, "--api="):
			options.API = strings.TrimPrefix(arg, "--api=")
		case strings.HasPrefix(arg, "--rom-root="):
			options.ROMRoot = strings.TrimPrefix(arg, "--rom-root=")
		case arg == "--data-root":
			dataRootExplicit = true
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
			dataRootExplicit = true
			options.DataRoot = strings.TrimPrefix(arg, "--data-root=")
		case arg == "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires a value")
			}
			i++
			options.Format = args[i]
		case strings.HasPrefix(arg, "--format="):
			options.Format = strings.TrimPrefix(arg, "--format=")
		case arg == "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("--out requires a value")
			}
			i++
			options.OutPath = args[i]
		case strings.HasPrefix(arg, "--out="):
			options.OutPath = strings.TrimPrefix(arg, "--out=")
		case arg == "--full":
			options.Full = true
		case arg == "--details":
			options.Details = true
		default:
			return fmt.Errorf("unknown option: %s", arg)
		}
	}
	if options.API != "" && dataRootExplicit {
		return fmt.Errorf("choose --api or --data-root, not both")
	}
	options.Format = strings.ToLower(strings.TrimSpace(options.Format))
	if options.Format == "" {
		options.Format = "text"
	}
	if options.Format != "text" && options.Format != "yaml" && options.Format != "json" {
		return fmt.Errorf("--format must be text, yaml or json")
	}
	result, err := buildPlatformList(options)
	if err != nil {
		return err
	}
	var output string
	switch options.Format {
	case "yaml":
		output = renderPlatformListYAML(result.Platforms)
	case "json":
		value := struct {
			SchemaVersion  int                        `json:"schema_version"`
			Source         string                     `json:"source"`
			CatalogChecked bool                       `json:"catalog_checked"`
			Platforms      []exporter.PlatformOption  `json:"platforms"`
			Folders        []exporter.FolderSelection `json:"rom_folders,omitempty"`
			Skipped        []exporter.SkippedSystem   `json:"skipped,omitempty"`
		}{1, result.DataRoot, result.Selector.CatalogKnown, platformListRows(result), result.Folders, result.Skipped}
		body, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		output = string(body) + "\n"
	default:
		output = renderPlatformListText(result, options.Details, strings.TrimSpace(options.OutPath) == "" && outputColorEnabled(os.Stdout))
	}
	if strings.TrimSpace(options.OutPath) != "" {
		outputRoot := result.DataRoot
		if options.API != "" {
			outputRoot = "."
		}
		outPath := store.ResolveDataRootPath(outputRoot, options.OutPath)
		if err := catalogfs.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := catalogfs.WriteFile(outPath, []byte(output), 0o644); err != nil {
			return err
		}
		fmt.Printf("Wrote %s\n", outPath)
		return nil
	}
	fmt.Print(output)
	return nil
}

func buildPlatformList(options platformListOptions) (platformListResult, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return platformListResult{}, err
	}
	var selector *exporter.PlatformSelector
	if options.API != "" {
		if options.DataRoot != "" && options.DataRoot != "." {
			return platformListResult{}, fmt.Errorf("choose --api or --data-root, not both")
		}
		selector, err = exportclient.ListPlatforms(context.Background(), exportclient.Options{API: options.API, Token: os.Getenv("ROMMETA_API_TOKEN")})
		dataRoot = options.API
	} else {
		var names platformnames.Registry
		names, err = exporter.LoadSelectorNames(dataRoot)
		if err == nil {
			selector = exporter.NewPlatformSelector(names, nil)
			if _, statErr := catalogfs.Stat(filepath.Join(dataRoot, "platforms")); statErr == nil {
				selector, err = exporter.LoadPlatformSelector(dataRoot)
			} else if !os.IsNotExist(statErr) {
				err = statErr
			}
		}
	}
	if err != nil {
		return platformListResult{}, err
	}
	result := platformListResult{DataRoot: dataRoot, ConfigPath: filepath.Join(dataRoot, "config", "platforms.yaml"), Full: options.Full || !selector.CatalogKnown, Selector: selector}
	if options.API == "" {
		_, err := catalogfs.Stat(result.ConfigPath)
		result.LocalConfigLoaded = err == nil
	}
	if result.Full {
		result.Platforms = selector.Names.Platforms()
	} else {
		seen := map[string]bool{}
		for _, c := range selector.Catalogs {
			if seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			if entry, ok := selector.Names.Entry(c.Name); ok {
				result.Platforms = append(result.Platforms, entry)
			} else {
				result.UnknownPlatformCount++
				result.Platforms = append(result.Platforms, platformnames.Platform{Name: c.Name})
			}
		}
		sort.Slice(result.Platforms, func(i, j int) bool { return result.Platforms[i].Name < result.Platforms[j].Name })
	}
	if options.ROMRoot != "" {
		romRoot, e := filepath.Abs(options.ROMRoot)
		if e != nil {
			return platformListResult{}, e
		}
		romRoot, e = filepath.EvalSymlinks(romRoot)
		if e != nil {
			return platformListResult{}, e
		}
		result.Folders, result.Skipped, err = selector.Folders(romRoot, nil, true)
		if err != nil {
			return platformListResult{}, err
		}
	}
	return result, nil
}
func platformListRows(result platformListResult) []exporter.PlatformOption {
	selector := result.Selector
	if selector == nil {
		selector = exporter.NewPlatformSelector(platformnames.DefaultRegistry(), nil)
	}
	byName := map[string]exporter.PlatformOption{}
	for _, row := range selector.Options() {
		byName[row.Name] = row
	}
	rows := []exporter.PlatformOption{}
	for _, platform := range result.Platforms {
		row, ok := byName[platform.Name]
		if !ok {
			row = exporter.PlatformOption{Name: platform.Name, Reason: "unknown registry entry"}
		}
		rows = append(rows, row)
	}
	return rows
}

func renderPlatformListText(result platformListResult, details, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf("Platform registry", ansiCyan, color))
	fmt.Fprintf(&b, "%s: %s\n", catalogLabel("Library", color), result.DataRoot)
	source := "built-in"
	if strings.HasPrefix(result.DataRoot, "http://") || strings.HasPrefix(result.DataRoot, "https://") {
		source += " + API platform names"
	}
	if result.LocalConfigLoaded {
		source += " + config/platforms.yaml"
	}
	fmt.Fprintf(&b, "%s: %s\n", catalogLabel("Registry", color), source)
	if result.Full {
		fmt.Fprintf(&b, "%s: %s (full registry)\n", catalogLabel("Platforms", color), formatCount(len(result.Platforms)))
	} else {
		fmt.Fprintf(&b, "%s: %s", catalogLabel("Platforms", color), formatCount(len(result.Platforms)))
		if result.UnknownPlatformCount > 0 {
			fmt.Fprintf(&b, "; unknown: %s", formatCount(result.UnknownPlatformCount))
		}
		fmt.Fprintln(&b)
	}
	if len(result.Platforms) == 0 {
		return b.String()
	}
	fmt.Fprintln(&b, "Source mappings are explicit registry entries; providers may also match by name.")
	fmt.Fprintln(&b, "Missing mappings do not establish source availability or downloaded coverage.")
	fmt.Fprintln(&b)
	rows := platformListRows(result)
	for index, platform := range result.Platforms {
		row := rows[index]
		aliases := strings.Join(platform.Aliases, ", ")
		if result.Selector != nil {
			aliases = strings.Join(row.Aliases, ", ")
		}
		sources := strings.Join(platformSourceNames(platform), ", ")
		if aliases == "" {
			aliases = "-"
		}
		if sources == "" {
			sources = "-"
		}
		fmt.Fprintf(&b, "%s\n", colorizeIf(platform.Name, ansiCyan, color))
		fmt.Fprintf(&b, "  code: %s\n", row.Code)
		fmt.Fprintf(&b, "  catalog status: %s\n", row.CatalogStatus)
		fmt.Fprintf(&b, "  export: %s; readers: %s\n", strings.Join(row.ExportTargets, ", "), strings.Join(row.ExportSystems, ", "))
		if row.Reason != "" {
			fmt.Fprintf(&b, "  export status: %s\n", row.Reason)
		}
		if result.Selector != nil && result.Selector.CatalogKnown {
			fmt.Fprintf(&b, "  catalog folders: %s\n", strings.Join(row.Catalogs, ", "))
		} else {
			fmt.Fprintln(&b, "  catalog: not checked")
		}
		if len(row.Extensions) > 0 {
			fmt.Fprintf(&b, "  payload formats: %s\n", strings.Join(row.Extensions, ", "))
		}
		fmt.Fprintf(&b, "  %s: %s\n", catalogLabel("aliases", color), aliases)
		fmt.Fprintf(&b, "  %s: %s\n", catalogLabel("source mappings", color), sources)
		if details {
			for _, source := range sortedPlatformSources(platform.Sources) {
				ref := platform.Sources[source]
				fmt.Fprintf(&b, "    %s: %s\n", colorizeIf(source, ansiYellow, color), platformSourceRefText(ref))
			}
		}
		fmt.Fprintln(&b)
	}
	for _, folder := range result.Folders {
		selection := folder.Selection
		fmt.Fprintf(&b, "Folder %s => %s (%s), reader %s; %s\n", selection.Folder, selection.Code, selection.Name, selection.System, selection.MatchedBy)
	}
	for _, skip := range result.Skipped {
		fmt.Fprintf(&b, "Skipped %s: %s\n", skip.System, skip.Reason)
	}
	return b.String()
}

func renderPlatformListYAML(platforms []platformnames.Platform) string {
	var b strings.Builder
	for index, platform := range platforms {
		if index > 0 {
			fmt.Fprintln(&b)
		}
		fmt.Fprintf(&b, "%s:\n", yamlString(platform.Name))
		if len(platform.Aliases) > 0 {
			fmt.Fprintf(&b, "  aliases: %s\n", yamlInlineList(platform.Aliases))
		}
		if len(platform.Sources) > 0 {
			fmt.Fprintln(&b, "  sources:")
			for _, source := range sortedPlatformSources(platform.Sources) {
				fmt.Fprintf(&b, "    %s: %s\n", source, yamlSourceRef(platform.Sources[source]))
			}
		}
		if len(platform.Aliases) == 0 && len(platform.Sources) == 0 {
			fmt.Fprintln(&b, "  aliases: []")
		}
	}
	return b.String()
}

func platformSourceNames(platform platformnames.Platform) []string {
	return sortedPlatformSources(platform.Sources)
}

func sortedPlatformSources(sources map[string]platformnames.SourceRef) []string {
	names := make([]string, 0, len(sources))
	for source := range sources {
		names = append(names, source)
	}
	sort.Strings(names)
	return names
}

func platformSourceRefText(ref platformnames.SourceRef) string {
	var parts []string
	if ref.ID != "" {
		parts = append(parts, "id="+ref.ID)
	}
	if ref.Name != "" {
		parts = append(parts, ref.Name)
	}
	for _, name := range ref.Names {
		if name != ref.Name {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func yamlSourceRef(ref platformnames.SourceRef) string {
	if ref.ID == "" && len(ref.Names) == 0 {
		return yamlString(ref.Name)
	}
	var fields []string
	if ref.ID != "" {
		fields = append(fields, "id: "+yamlString(ref.ID))
	}
	if ref.Name != "" {
		fields = append(fields, "name: "+yamlString(ref.Name))
	}
	if len(ref.Names) > 0 {
		fields = append(fields, "names: "+yamlInlineList(ref.Names))
	}
	return "{ " + strings.Join(fields, ", ") + " }"
}

func yamlInlineList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, yamlString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func printPlatformsUsage() {
	fmt.Println(`rom-meta platforms inspects platform name mappings.

Usage:
  rom-meta platforms list [--data-root <root> | --api <origin>] [--rom-root <collection>] [--format text|yaml|json] [--out <path>] [--full] [--details]

Commands:
  list  List codes, names, accepted aliases, export support and catalog availability.

Options:
  --data-root <root>  Data root containing platforms/.
  --api <origin>     Read the catalog platform capabilities from an API.
  --rom-root <root>  Explain immediate platform folders by name; no ROM hashing.
  --format <format>   Output: text or json (unified listing), yaml (registry configuration).
  --out <path>        Write output to a path, Relative to data root, or the working directory with --api.
  --full              List the full built-in registry plus local config/platforms.yaml.
  --details           In text output, include source-specific names and IDs.

Config:
  config/platforms.yaml can add local aliases and source-specific platform overrides.
  Without a local catalog, list uses the full built-in registry.
  The same codes/names/aliases work in export --platform and --platforms.`)
}
