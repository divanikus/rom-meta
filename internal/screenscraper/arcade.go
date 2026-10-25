package screenscraper

import "strings"

func mameQueries(game catalogGame) []screenScraperQuery {
	var queries []screenScraperQuery
	seen := map[string]bool{}
	add := func(name, set string) {
		if set == "" || strings.ContainsAny(set, "/\\ .") || seen[set] {
			return
		}
		seen[set] = true
		queries = append(queries, screenScraperQuery{Release: name, Term: set, ROMName: set + ".zip"})
	}
	for _, release := range game.MAMEReleases {
		if release.Name == game.DefaultRelease {
			add(release.Name, release.MAMESet)
		}
	}
	for _, release := range game.MAMEReleases {
		add(release.Name, release.MAMESet)
	}
	if len(queries) > maxSearchTerms {
		queries = queries[:maxSearchTerms]
	}
	return queries
}

func metadataMatchesMAMESet(metadata gameMetadata, query screenScraperQuery, systemID string) bool {
	// MAME (75) covers child systems such as Neo-Geo (142).
	if metadata.SystemID != systemID && metadata.SystemParentID != systemID {
		return false
	}
	if metadata.MatchedROM != "" {
		return strings.EqualFold(metadata.MatchedROM, query.ROMName)
	}
	for _, rom := range metadata.ROMs {
		if strings.EqualFold(rom.Filename, query.ROMName) {
			return true
		}
	}
	return false
}
