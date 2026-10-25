package catalogfold

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
	"rommeta/internal/contentidentity"
	"rommeta/internal/regions"
)

type sourceAlias struct {
	Identity Evidence
	Title    string
	Region   string
}

func collectSourceAliases(game *catalogGame, source string, match *yaml.Node, evidence []Evidence) error {
	if len(evidence) == 0 {
		return nil
	}
	if source == "igdb" && len(catalogyaml.Strings(catalogyaml.Value(match, "platforms"))) == 0 {
		return nil
	}
	// An alias must be anchored to the actual card title. A hash match can
	// describe a related demo/update with the base game's provider identity;
	// do not use that record to infer new title-to-title links.
	cardKey := regionalTitleKey(game.Title)
	anchored := cardKey != "" && cardKey == regionalTitleKey(catalogyaml.Scalar(catalogyaml.Value(match, "title")))
	if err := catalogyaml.Sequence(catalogyaml.Value(match, "titles"), func(node *yaml.Node) error {
		if cardKey != "" && cardKey == regionalTitleKey(catalogyaml.Scalar(catalogyaml.Value(node, "title"))) {
			anchored = true
		}
		return nil
	}); err != nil {
		return err
	}
	if !anchored {
		return nil
	}
	for _, identity := range evidence {
		for _, title := range catalogyaml.Strings(catalogyaml.Value(match, "alternate_titles")) {
			game.SourceAliases = append(game.SourceAliases, sourceAlias{Identity: identity, Title: title})
		}
		if err := catalogyaml.Sequence(catalogyaml.Value(match, "titles"), func(node *yaml.Node) error {
			title := catalogyaml.Scalar(catalogyaml.Value(node, "title"))
			if title != "" {
				game.SourceAliases = append(game.SourceAliases, sourceAlias{
					Identity: identity, Title: title,
					Region: normalizeAliasRegion(catalogyaml.Scalar(catalogyaml.Value(node, "region"))),
				})
			}
			return nil
		}); err != nil {
			return fmt.Errorf("%s titles: %w", source, err)
		}
	}
	return nil
}

// Normalize known transport qualifiers, punctuation and trailing articles.
// Edition names, numbers, subtitles and unknown qualifiers retain identity;
// explicit content roles also pass the separate content boundary guard.
func regionalTitleKey(title string) string {
	title, _ = contentidentity.Analyze(title)
	title = strings.ToLower(strings.TrimSpace(title))
	parts := strings.Split(title, " - ")
	for i, part := range parts {
		for _, article := range []string{"the", "a", "an"} {
			if strings.HasSuffix(part, ", "+article) {
				parts[i] = article + " " + strings.TrimSuffix(part, ", "+article)
				break
			}
		}
	}
	title = strings.Join(parts, " - ")
	var b strings.Builder
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func normalizeAliasRegion(value string) string {
	return normalizeRegionName(regions.NormalizeDisplay(value))
}

func releaseRegions(name string) []string {
	start := strings.IndexByte(name, '(')
	if start < 0 {
		return nil
	}
	end := strings.IndexByte(name[start:], ')')
	if end < 0 {
		return nil
	}
	var result []string
	for _, value := range strings.Split(name[start+1:start+end], ",") {
		if region, ok := regions.NormalizeDATDisplay(value); ok {
			result = append(result, region)
		}
	}
	return result
}

func aliasRegionMatches(region string, game catalogGame) bool {
	if region == "" || region == "unknown" {
		return false
	}
	for _, candidate := range game.Regions {
		if candidate == region || (region == "north america" && (candidate == "usa" || candidate == "canada")) {
			return true
		}
	}
	return false
}

func aliasEvidenceAllowed(game catalogGame, identity Evidence) bool {
	alias := identity
	alias.Field = "regional_alias"
	return len(filterIgnoredEvidence([]Evidence{identity, alias}, game.IgnoredEvidence)) == 2
}

func hasDirectAliasIdentity(game catalogGame, identity Evidence) bool {
	for _, item := range game.Evidence {
		if item.Source == identity.Source && item.Field == identity.Field && item.Value == identity.Value {
			return true
		}
	}
	return false
}

// Saved aliases extend candidate discovery, never the provider's primary
// matcher. Unscoped/ambiguous aliases remain visible but cannot auto-apply.
func withRegionalAliasEvidence(games []catalogGame) []catalogGame {
	type reference struct {
		index int
		alias sourceAlias
	}
	byTitle := map[string][]reference{}
	for index, game := range games {
		if game.HasRomsetBindings {
			continue
		}
		for _, alias := range game.SourceAliases {
			key := regionalTitleKey(alias.Title)
			if key == "" || key == regionalTitleKey(game.Title) || !aliasEvidenceAllowed(game, alias.Identity) {
				continue
			}
			byTitle[key] = append(byTitle[key], reference{index, alias})
		}
	}
	for index := range games {
		game := &games[index]
		if game.HasRomsetBindings {
			continue
		}
		refs := byTitle[regionalTitleKey(game.Title)]
		confirmed := map[string]bool{}
		identities := map[string]map[string]bool{}
		for _, ref := range refs {
			id := ref.alias.Identity
			if ref.index == index || !aliasEvidenceAllowed(*game, id) || hasDirectAliasIdentity(*game, id) {
				continue
			}
			if identities[id.Source] == nil {
				identities[id.Source] = map[string]bool{}
			}
			identities[id.Source][id.Value] = true
			if aliasRegionMatches(ref.alias.Region, *game) {
				confirmed[id.Source+"\x00"+id.Value] = true
			}
		}
		independent := 0
		for _, values := range identities {
			if len(values) == 1 {
				independent++
			}
		}
		for _, ref := range refs {
			id := ref.alias.Identity
			if ref.index == index || !aliasEvidenceAllowed(*game, id) || hasDirectAliasIdentity(*game, id) {
				continue
			}
			var reason string
			if len(identities[id.Source]) > 1 {
				reason = fmt.Sprintf("ambiguous %s regional alias for %q", id.Source, game.Title)
			} else if ref.alias.Region != "" && !aliasRegionMatches(ref.alias.Region, *game) {
				reason = fmt.Sprintf("alias for %q conflicts with its explicit release region", game.Title)
			} else if providers, _, _ := regionalProviderProof(games[ref.index], *game); !confirmed[id.Source+"\x00"+id.Value] && independent < 2 && len(providers) < 2 {
				reason = fmt.Sprintf("alias for %q has no matching explicit release region", game.Title)
			}
			if reason != "" {
				game.AliasHoldReasons = appendUniqueStrings(game.AliasHoldReasons, reason)
			}
			id.Field = "regional_alias"
			game.Evidence = append(game.Evidence, id)
			games[ref.index].Evidence = append(games[ref.index].Evidence, id)
		}
	}
	return games
}

func aliasGroupHoldReason(games []catalogGame, indexes []int) string {
	var reasons []string
	for _, index := range indexes {
		game := games[index]
		for _, reason := range game.AliasHoldReasons {
			reasons = appendUniqueStrings(reasons, reason)
		}
	}
	sort.Strings(reasons)
	return strings.Join(reasons, "; ")
}

func groupDirectIDConflicts(games []catalogGame, indexes []int) []string {
	ids := map[string]map[string]bool{}
	for _, index := range indexes {
		game := games[index]
		// Include weaker direct IDs too, and conflicts through a third card.
		for _, item := range game.RelationshipIDs {
			if ids[item.Source] == nil {
				ids[item.Source] = map[string]bool{}
			}
			ids[item.Source][item.Value] = true
		}
	}
	var reasons []string
	for source, values := range ids {
		if len(values) > 1 {
			var sorted []string
			for value := range values {
				sorted = append(sorted, value)
			}
			sort.Strings(sorted)
			reasons = append(reasons, "conflicting "+source+" IDs: "+strings.Join(sorted, ", "))
		}
	}
	sort.Strings(reasons)
	return reasons
}
