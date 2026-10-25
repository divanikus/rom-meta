package igdb

import (
	"regexp"
	"strings"

	"rommeta/internal/catalogrules"
)

func arcadeSearchTerms(game catalogGame) []string {
	var terms []string
	add := func(title string) {
		for _, name := range strings.Split(stripTags(title), " / ") {
			terms = appendTermVariants(terms, titleShape(name))
		}
	}
	// The release description retains '/' regional aliases that folder labels lose.
	for _, release := range game.MAMEReleases {
		if release.Name == game.DefaultRelease {
			add(release.Title)
		}
	}
	add(game.Title)
	for _, release := range game.MAMEReleases {
		add(release.Title)
	}
	for _, alias := range game.Aliases {
		add(alias)
	}
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	return terms
}

// Broad IGDB game records can cover Arcade and several home ports. A home-only
// record is never evidence for an imported arcade title, even with a Hasheous ID.
func arcadePlatformMatches(game catalogGame, candidate apiGame) bool {
	familyName := map[string]string{
		"Arcade - SNK - Neo Geo":  "Neo Geo MVS",
		"Arcade - Sega - Naomi":   "Sega NAOMI",
		"Arcade - Sega - Naomi 2": "Sega NAOMI 2",
	}[game.ArcadePlatform]
	for _, platform := range candidate.Platforms {
		if strings.EqualFold(platform.Name, "Arcade") || (familyName != "" && strings.EqualFold(platform.Name, familyName)) {
			return true
		}
	}
	return false
}

func arcadeCandidateAllowed(game catalogGame, candidate apiGame) bool {
	names := []string{candidate.Name}
	for _, alias := range candidate.AlternativeNames {
		names = append(names, alias.Name)
	}
	allUnofficial := true
	anyUnofficial := false
	for _, release := range game.MAMEReleases {
		unofficial := catalogrules.HasUnofficialTag(release.Tags)
		for _, tag := range release.Tags {
			if tag == "bootleg" || tag == "hack" {
				unofficial = true
			}
		}
		if !unofficial {
			allUnofficial = false
		}
		anyUnofficial = anyUnofficial || unofficial
	}
	marked := false
	for _, name := range names {
		if hasUnofficialTitleMarker(name) || arcadeUnofficialMarker.MatchString(name) {
			marked = true
		}
	}
	if (allUnofficial && !marked) || (!anyUnofficial && marked) {
		return false
	}
	for _, port := range []string{"mega tech", "mega play", "playchoice"} {
		local := strings.Contains(looseTitleKey(game.ArcadePlatform+" "+game.Title), port)
		for _, release := range game.MAMEReleases {
			local = local || strings.Contains(looseTitleKey(release.Title), port)
		}
		remote := false
		for _, name := range names {
			remote = remote || strings.Contains(looseTitleKey(name), port)
		}
		for _, platform := range candidate.Platforms {
			remote = remote || strings.Contains(looseTitleKey(platform.Name), port)
		}
		if local != remote {
			return false
		}
	}
	return true
}

var arcadeUnofficialMarker = regexp.MustCompile(`(?i)\b(?:bootleg|hack)\b`)
