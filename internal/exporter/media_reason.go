package exporter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
)

// MediaReason is presentation-only evidence from the shared selector. It is
// returned by catalog preview, never added to native/API identity or media proof.
type MediaReason struct {
	Variant      string   `json:"variant,omitempty"`
	Summary      string   `json:"summary"`
	Details      []string `json:"details,omitempty"`
	Order        []string `json:"order,omitempty"`
	ComparedWith string   `json:"compared_with,omitempty"`
}

func mediaReasonKey(role, path string) string { return role + "\x00" + path }

func mediaRegionName(region string) string {
	if name := map[string]string{"usa": "USA", "europe": "Europe", "japan": "Japan", "world": "Worldwide", "unspecified": "Unspecified"}[region]; name != "" {
		return name
	}
	return region
}
func mediaSourceName(source string) string {
	if name := map[string]string{"launchbox": "LaunchBox", "screenscraper": "ScreenScraper", "emumovies": "EmuMovies", "igdb": "IGDB", "openvgdb": "OpenVGDB", "local": "Saved local artwork"}[source]; name != "" {
		return name
	}
	return source
}
func mediaVariantName(variant string) string {
	if name := map[string]string{"original": "Original artwork", "reconstructed": "Reconstructed artwork", "unknown": "Unclassified artwork", "fanart": "Fan art"}[variant]; name != "" {
		return name
	}
	return variant
}
func mediaRankDimensions(rule Rule) []string {
	dimensions := []string{}
	if position(rule.Order, "scope") < 0 {
		dimensions = append(dimensions, "scope")
	}
	dimensions = append(dimensions, rule.Order...)
	return append(dimensions, "number")
}

func explainMediaChoice(ctx context.Context, root, role string, rule Rule, regions []string, release string, candidates []mediaCandidate, index int) MediaReason {
	selected := candidates[index]
	reason := MediaReason{Summary: "No other usable candidate under this profile."}
	if role != "video" && role != "manual" {
		reason.Variant = selected.variant
	}
	scope := "Shared game artwork."
	if selected.fromRelease != "" {
		scope = "Artwork stored for edition: " + selected.fromRelease + "."
		if selected.fromRelease != release {
			scope = "Artwork borrowed from edition: " + selected.fromRelease + "."
		}
	}
	reason.Details = append(reason.Details, scope)
	affinity := rankRegion(rule.Regions, selected.region, regions)
	if affinity >= 0 && rule.Regions[affinity] == "$release" {
		reason.Details = append(reason.Details, "Region matches the selected release: "+mediaRegionName(selected.region)+".")
	} else {
		reason.Details = append(reason.Details, "Allowed fallback region: "+mediaRegionName(selected.region)+".")
	}
	if reason.Variant != "" {
		reason.Details = append(reason.Details, "Artwork type comes from the saved filename label; image quality is not assessed.")
	}
	dimensions := mediaRankDimensions(rule)
	labels := map[string]string{"scope": "Edition / shared scope", "region": "Release region", "region-preference": "Preferred country", "variant": "Artwork type", "source": "Source", "format": "Format", "language": "Language", "style": "Artwork style", "number": "File number"}
	for _, dimension := range dimensions {
		reason.Order = append(reason.Order, labels[dimension])
	}
	reason.Order = append(reason.Order, "Filename")
	// The inventory and ranking already exist. Inspect only the next usable
	// candidate; no payload reads, hashing or second directory scan is needed.
	for _, other := range candidates[index+1:] {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		real, err := realPath(other.path)
		if err != nil || !within(root, real) {
			continue
		}
		info, err := catalogfs.Stat(real)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		reason.ComparedWith = filepath.Base(other.path)
		reason.Summary = "Alphabetically first filename after all other priorities tie."
		for i, dimension := range dimensions {
			if selected.rank[i] == other.rank[i] {
				continue
			}
			switch dimension {
			case "scope":
				reason.Summary = "Edition-specific artwork is preferred to shared artwork."
				if selected.fromRelease == "" {
					reason.Summary = "Shared game artwork is preferred to borrowed edition artwork."
				}
			case "region":
				reason.Summary = mediaRegionName(selected.region) + " artwork has higher regional priority than " + mediaRegionName(other.region) + "."
			case "region-preference":
				reason.Summary = "Preferred country within this release: " + mediaRegionName(selected.region) + "."
			case "variant":
				reason.Summary = mediaVariantName(selected.variant) + " is preferred to " + strings.ToLower(mediaVariantName(other.variant)) + "."
			case "source":
				reason.Summary = mediaSourceName(selected.source) + " is preferred to " + mediaSourceName(other.source) + "."
			case "format":
				reason.Summary = strings.ToUpper(selected.format) + " is preferred to " + strings.ToUpper(other.format) + "."
			case "language":
				reason.Summary = "Preferred artwork language: " + strings.ToUpper(selected.language) + "."
			case "style":
				reason.Summary = "Preferred artwork style: " + selected.style.style + "."
			case "number":
				reason.Summary = fmt.Sprintf("File number %d comes before %d after other priorities tie.", selected.rank[i], other.rank[i])
				if selected.rank[i] == 0 {
					reason.Summary = "Unnumbered artwork comes first after other priorities tie."
				}
			}
			break
		}
		break
	}
	return reason
}
