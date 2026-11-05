package enrichment

import (
	"fmt"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
	"strings"
)

func MappingReason(job Job, platform string, registry platformnames.Registry, config store.SourcesConfig) string {
	configured := false
	for i := range config.Sources {
		if config.Sources[i].Name == job.Source {
			configured = true
			break
		}
	}
	if !configured {
		return "source is not configured in config/sources.yaml"
	}
	// Hasheous identifies ROMs by evidence, independently of platform mappings.
	if job.Source == "hasheous" {
		return ""
	}
	entry, known := registry.Entry(platform)
	if known {
		ref := entry.Sources[job.Source]
		if strings.TrimSpace(ref.Name) != "" || len(ref.Names) > 0 || (job.Source == "screenscraper" && strings.TrimSpace(ref.ID) != "") {
			return ""
		}
	}
	return fmt.Sprintf("no %s platform mapping for %q; add one in config/platforms.yaml or select --source explicitly", job.Source, platform)
}
