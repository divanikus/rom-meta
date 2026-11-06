package enrichment

import "rommeta/internal/store"

// ConfigurationReason is deliberately local and cheap. Platform support is
// resolved from the provider list/snapshot by the job, with normal progress and
// retry handling. Opening a wizard never downloads databases or blocks on APIs.
func ConfigurationReason(job Job, config store.SourcesConfig) string {
	for _, source := range config.Sources {
		if source.Name == job.Source {
			return ""
		}
	}
	return "source is not configured in config/sources.yaml"
}
