package exporter

import (
	"fmt"
	"strings"

	"rommeta/internal/catalogyaml"
)

// DescriptionFallback allows reviewed reuse of another release's prose within
// the same card. It cannot change identity, other fields, or existing prose.
type DescriptionFallback struct {
	Game        string `yaml:"game" json:"game"`
	Release     string `yaml:"release" json:"release"`
	FromRelease string `yaml:"from_release" json:"from_release"`
}

func validateDescriptionFallbacks(p Profile) error {
	seen := map[string]bool{}
	for _, choice := range p.DescriptionFallbacks {
		if !safeMediaRelative(choice.Game) || !strings.HasPrefix(choice.Game, "platforms/") || strings.Count(choice.Game, "/") < 2 ||
			strings.TrimSpace(choice.Release) == "" || strings.TrimSpace(choice.FromRelease) == "" || choice.Release == choice.FromRelease {
			return fmt.Errorf("description fallback needs a catalog game and two distinct exact releases")
		}
		key := choice.Game + "\x00" + choice.Release
		if seen[key] {
			return fmt.Errorf("duplicate description fallback for %s / %s", choice.Game, choice.Release)
		}
		seen[key] = true
	}
	return nil
}

func applyDescriptionFallback(g *game, record *Record, profile Profile) error {
	if record.Release == "" || record.Fields["description"].Value != "" {
		return nil
	}
	for _, choice := range profile.DescriptionFallbacks {
		if choice.Game != record.Game || choice.Release != record.Release {
			continue
		}
		releases := catalogyaml.Value(g.root, "releases")
		if catalogyaml.Value(releases, choice.FromRelease) == nil {
			return fmt.Errorf("description fallback references missing release: %s / %s", choice.Game, choice.FromRelease)
		}
		donorProfile := profile
		donorProfile.Selection.Description.Scope = "release"
		value := resolveFields(g, choice.FromRelease, donorProfile)["description"]
		if value.Value == "" {
			return fmt.Errorf("description fallback has no eligible text: %s / %s", choice.Game, choice.FromRelease)
		}
		value.Selection, value.FromRelease = "reviewed-fallback", choice.FromRelease
		record.Fields["description"] = value
		record.Warnings = append(record.Warnings, "Description uses reviewed same-card release: "+choice.FromRelease)
		return nil
	}
	return nil
}
