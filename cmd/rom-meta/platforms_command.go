package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
)

type platformListOptions struct {
	DataRoot string
	Format   string
	OutPath  string
	Full     bool
	Details  bool
}

type platformListResult struct {
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
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--data-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--data-root requires a value")
			}
			i++
			options.DataRoot = args[i]
		case strings.HasPrefix(arg, "--data-root="):
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
	options.Format = strings.ToLower(strings.TrimSpace(options.Format))
	if options.Format == "" {
		options.Format = "text"
	}
	if options.Format != "text" && options.Format != "yaml" {
		return fmt.Errorf("--format must be text or yaml")
	}
	result, err := buildPlatformList(options)
	if err != nil {
		return err
	}
	var output string
	switch options.Format {
	case "yaml":
		output = renderPlatformListYAML(result.Platforms)
	default:
		output = renderPlatformListText(result, options.Details, strings.TrimSpace(options.OutPath) == "" && outputColorEnabled(os.Stdout))
	}
	if strings.TrimSpace(options.OutPath) != "" {
		outPath := store.ResolveDataRootPath(result.DataRoot, options.OutPath)
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
	registry, loaded, err := platformnames.LoadRegistry(dataRoot)
	if err != nil {
		return platformListResult{}, err
	}
	if config, err := store.LoadSources(dataRoot); err == nil {
		registry.MergeAliases(config.PlatformAliases)
		dataRoot = config.Root
	}
	result := platformListResult{
		DataRoot:          dataRoot,
		ConfigPath:        filepath.Join(dataRoot, "config", "platforms.yaml"),
		LocalConfigLoaded: loaded,
		Full:              options.Full,
	}
	if options.Full {
		result.Platforms = registry.Platforms()
		return result, nil
	}
	names, err := catalogPlatformNames(dataRoot)
	if err != nil {
		return platformListResult{}, err
	}
	for _, name := range names {
		if platform, ok := registry.Entry(name); ok {
			result.Platforms = append(result.Platforms, platform)
			continue
		}
		result.UnknownPlatformCount++
		result.Platforms = append(result.Platforms, platformnames.Platform{Name: name, Sources: map[string]platformnames.SourceRef{}})
	}
	return result, nil
}

func catalogPlatformNames(dataRoot string) ([]string, error) {
	platformRoot := filepath.Join(dataRoot, "platforms")
	entries, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func renderPlatformListText(result platformListResult, details, color bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", colorizeIf("Platform registry", ansiCyan, color))
	fmt.Fprintf(&b, "%s: %s\n", catalogLabel("Library", color), result.DataRoot)
	source := "built-in"
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
	for _, platform := range result.Platforms {
		aliases := strings.Join(platform.Aliases, ", ")
		sources := strings.Join(platformSourceNames(platform), ", ")
		if aliases == "" {
			aliases = "-"
		}
		if sources == "" {
			sources = "-"
		}
		fmt.Fprintf(&b, "%s\n", colorizeIf(platform.Name, ansiCyan, color))
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
  rom-meta platforms list [--data-root <root>] [--format text|yaml] [--out <path>] [--full] [--details]

Commands:
  list  List catalog platforms and the names known for each source.

Options:
  --data-root <root>  Data root containing platforms/.
  --format <format>   Output format: text or yaml.
  --out <path>        Write output to a path, relative to the data root when not absolute.
  --full              List the full built-in registry plus local config/platforms.yaml.
  --details           In text output, include source-specific names and IDs.

Config:
  config/platforms.yaml can add local aliases and source-specific platform overrides.`)
}
