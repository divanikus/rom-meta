package dat

import (
	"strings"

	"rommeta/internal/platformnames"
	"rommeta/internal/store"
)

// LibraryOptions applies the same catalog policy for CLI and browser imports.
func LibraryOptions(options ImportOptions) (ImportOptions, error) {
	config, err := store.LoadCatalogConfig(options.DataRoot)
	if err != nil {
		return options, err
	}
	registry, _, err := platformnames.LoadRegistry(config.Root)
	if err != nil {
		return options, err
	}
	options.DataRoot = config.Root
	options.PlatformOverride = registry.ResolveCLI(options.PlatformOverride)
	options.RegionPriority = config.Fold.RegionPriority
	options.RegionGroups = config.Fold.RegionGroups
	if len(config.Fold.Platforms) > 0 {
		options.PlatformRegionPriority = map[string][]string{}
		for name, value := range config.Fold.Platforms {
			if resolved := registry.ResolveCLI(name); strings.TrimSpace(resolved) != "" {
				options.PlatformRegionPriority[resolved] = value.RegionPriority
			}
		}
	}
	return options, nil
}
