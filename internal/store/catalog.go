package store

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
)

// CatalogConfig is parsed from config/catalog.yaml.
type CatalogConfig struct {
	Root string
	Path string
	Fold CatalogFoldConfig
}

// CatalogFoldConfig controls post-metadata catalog folding.
type CatalogFoldConfig struct {
	RegionPriority []string
	RegionGroups   map[string][]string
	Platforms      map[string]CatalogPlatformFoldConfig
}

// CatalogPlatformFoldConfig overrides fold behavior for one platform.
type CatalogPlatformFoldConfig struct {
	RegionPriority []string
}

// LoadCatalogConfig reads optional config/catalog.yaml from a data root.
func LoadCatalogConfig(root string) (CatalogConfig, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return CatalogConfig{}, err
	}
	path := filepath.Join(absRoot, "config", "catalog.yaml")
	config := CatalogConfig{
		Root: absRoot,
		Path: path,
		Fold: CatalogFoldConfig{
			RegionPriority: []string{"usa", "world", "europe", "japan", "asia", "russia"},
			RegionGroups:   map[string][]string{},
			Platforms:      map[string]CatalogPlatformFoldConfig{},
		},
	}

	file, err := catalogfs.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return CatalogConfig{}, err
	}
	defer file.Close()

	var list catalogConfigListContext
	inFold := false
	section := ""
	currentPlatform := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := trimSourceComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := sourceIndent(line)
		value := strings.TrimSpace(line)
		if list.Kind != "" && indent == list.Indent && strings.HasPrefix(value, "- ") {
			appendCatalogConfigListItem(&config, list, strings.TrimSpace(strings.TrimPrefix(value, "- ")))
			continue
		}
		list = catalogConfigListContext{}

		if indent == 0 {
			inFold = value == "fold:"
			section = ""
			currentPlatform = ""
			continue
		}
		if !inFold {
			continue
		}

		if indent == 2 {
			key, raw, ok := splitSourceKV(value)
			if ok && key == "region_priority" {
				if strings.TrimSpace(raw) == "" {
					config.Fold.RegionPriority = nil
					list = catalogConfigListContext{Kind: "global-priority", Indent: 4}
					continue
				}
				config.Fold.RegionPriority = parseCatalogConfigList(raw)
				continue
			}
			if strings.HasSuffix(value, ":") {
				section = strings.TrimSuffix(value, ":")
				currentPlatform = ""
				continue
			}
			continue
		}

		switch section {
		case "region_groups":
			if indent != 4 {
				continue
			}
			group, raw, ok := splitSourceKV(value)
			if !ok {
				continue
			}
			group = parseSourceScalar(group)
			if strings.TrimSpace(raw) == "" {
				if config.Fold.RegionGroups[group] == nil {
					config.Fold.RegionGroups[group] = nil
				}
				list = catalogConfigListContext{Kind: "region-group", Group: group, Indent: 6}
				continue
			}
			config.Fold.RegionGroups[group] = parseCatalogConfigList(raw)
		case "platforms":
			if indent == 4 && strings.HasSuffix(value, ":") {
				currentPlatform = parseSourceScalar(strings.TrimSuffix(value, ":"))
				if _, ok := config.Fold.Platforms[currentPlatform]; !ok {
					config.Fold.Platforms[currentPlatform] = CatalogPlatformFoldConfig{}
				}
				continue
			}
			if indent != 6 || currentPlatform == "" {
				continue
			}
			key, raw, ok := splitSourceKV(value)
			if !ok || key != "region_priority" {
				continue
			}
			if strings.TrimSpace(raw) == "" {
				platform := config.Fold.Platforms[currentPlatform]
				platform.RegionPriority = nil
				config.Fold.Platforms[currentPlatform] = platform
				list = catalogConfigListContext{Kind: "platform-priority", Platform: currentPlatform, Indent: 8}
				continue
			}
			platform := config.Fold.Platforms[currentPlatform]
			platform.RegionPriority = parseCatalogConfigList(raw)
			config.Fold.Platforms[currentPlatform] = platform
		}
	}
	if err := scanner.Err(); err != nil {
		return CatalogConfig{}, err
	}
	return config, nil
}

type catalogConfigListContext struct {
	Kind     string
	Group    string
	Platform string
	Indent   int
}

func appendCatalogConfigListItem(config *CatalogConfig, list catalogConfigListContext, raw string) {
	value := parseSourceScalar(raw)
	if value == "" {
		return
	}
	switch list.Kind {
	case "global-priority":
		config.Fold.RegionPriority = append(config.Fold.RegionPriority, value)
	case "region-group":
		config.Fold.RegionGroups[list.Group] = append(config.Fold.RegionGroups[list.Group], value)
	case "platform-priority":
		platform := config.Fold.Platforms[list.Platform]
		platform.RegionPriority = append(platform.RegionPriority, value)
		config.Fold.Platforms[list.Platform] = platform
	}
}

func parseCatalogConfigList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	}
	var result []string
	for _, item := range splitCatalogConfigList(raw) {
		value := parseSourceScalar(item)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func splitCatalogConfigList(raw string) []string {
	var result []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for _, r := range raw {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && inDouble:
			current.WriteRune(r)
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			current.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			current.WriteRune(r)
		case r == ',' && !inSingle && !inDouble:
			result = append(result, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		result = append(result, strings.TrimSpace(current.String()))
	}
	return result
}

const defaultCatalogConfig = `schema_version: 0

# Local catalog behavior. This file is safe to edit and share.
fold:
  # Determines which release folder stays as the main game bundle after
  # source-ID-backed regional variants are folded together.
  region_priority: [usa, world, europe, japan, asia, russia]

  # Region groups normalize source/DAT variants before applying the priority
  # above. Add local aliases here when a DAT or source uses a different token.
  region_groups:
    usa: [USA, US, U.S.A., United States, United States of America, North America]
    world: [World, International]
    europe: [Europe, EU, European Union, Germany, Deutschland, France, Spain, Italy, Netherlands, Sweden, Denmark, Finland, Norway, Portugal, United Kingdom, UK, GB, Great Britain, Australia]
    japan: [Japan, JP, JPN, JAP, JA]
    asia: [Asia, China, PRC, CN, CHN, ZH, Hong Kong, HK, HKG, Taiwan, TW, TWN, Korea, KR, KOR, South Korea, Singapore]
    russia: [Russia, Russian Federation, RU, RUS]

  # Optional platform overrides. Keys may use canonical platform names or
  # configured platform aliases.
  platforms:
    # "Nintendo - Nintendo Entertainment System":
    #   region_priority: [japan, usa, world, europe, asia, russia]
`
