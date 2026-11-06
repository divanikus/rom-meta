package screenscraper

import "rommeta/internal/platformnames"

func discoverSystemID(platform string, systems []screenScraperSystem, registry *platformnames.Registry) (string, error) {
	if registry == nil {
		local := platformnames.DefaultRegistry()
		registry = &local
	}
	// Explicit IDs are authoritative but still must exist in the provider response.
	if id, ok := registry.SourceID(platform, platformnames.SourceScreenScraper); ok {
		for _, system := range systems {
			if system.ID == id {
				return id, nil
			}
		}
		return "", &platformnames.ResolutionError{Platform: platform, Source: platformnames.SourceScreenScraper}
	}
	// Several names of the same provider ID are aliases, not several platforms.
	matches := map[string]bool{}
	for _, system := range systems {
		names := append([]string{system.Name}, system.Names...)
		for _, name := range names {
			if _, err := registry.Discover(platform, platformnames.SourceScreenScraper, []string{name}); err == nil && system.ID != "" {
				matches[system.ID] = true
			}
		}
	}
	if len(matches) == 1 {
		for id := range matches {
			return id, nil
		}
	}
	return "", &platformnames.ResolutionError{Platform: platform, Source: platformnames.SourceScreenScraper, Ambiguous: len(matches) > 1}
}
