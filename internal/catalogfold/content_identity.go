package catalogfold

import (
	"fmt"
	"strings"

	"rommeta/internal/contentidentity"
)

// contentGroupHoldReason is shared by discovery, reviewed plans and the staged
// writer. Explicit review approves identity, never incompatible content.
func contentGroupHoldReason(games []catalogGame) string {
	profiles := make([][]contentidentity.Profile, 0, len(games))
	keys := map[string]bool{}
	for _, game := range games {
		if game.ContentKey != "" {
			keys[game.ContentKey] = true
		}
	}
	if len(keys) > 1 {
		return "different reviewed content identities"
	}
	for _, game := range games {
		profiles = append(profiles, game.Content)
	}
	if reason := contentidentity.Boundary(profiles...); reason != "" {
		return reason
	}
	return contentRelationshipHoldReason(games)
}

func contentRelationshipHoldReason(games []catalogGame) string {
	for _, game := range games {
		for _, relation := range game.Relationships {
			if relation.Relationship.Type == "port_of" || relation.Relationship.Type == "related_to" {
				continue
			}
			for _, other := range games {
				if other.Folder == game.Folder {
					continue
				}
				for _, id := range other.RelationshipIDs {
					if id.Source == relation.Source && id.Value == relation.Relationship.TargetID {
						return "different content linked by " + relation.Relationship.Type
					}
				}
			}
		}
	}
	return ""
}

func identityGroupHoldReason(games []catalogGame, indexes []int) string {
	selected := make([]catalogGame, 0, len(indexes))
	for _, index := range indexes {
		selected = append(selected, games[index])
	}
	if reason := contentGroupHoldReason(selected); reason != "" {
		return reason
	}
	if reason := aliasGroupHoldReason(games, indexes); reason != "" {
		return reason
	}
	if conflicts := groupDirectIDConflicts(games, indexes); len(conflicts) > 0 {
		return strings.Join(conflicts, "; ")
	}
	for i, left := range selected {
		for _, right := range selected[i+1:] {
			if !confirmedRegionalPair(left, right) {
				return fmt.Sprintf("no independent regional identity confirmation for %q and %q; transitive links and edition relationships are discovery only", left.Folder, right.Folder)
			}
		}
	}
	return ""
}

func confirmedRegionalPair(left, right catalogGame) bool { return regionalPairProof(left, right, true) }

func regionalPairProof(left, right catalogGame, serialHint bool) bool {
	providers, direct, regional := regionalProviderProof(left, right)
	if (direct && regionalTitleKey(left.Title) == regionalTitleKey(right.Title)) || len(providers) >= 2 || regional {
		return true
	}
	// Existing checked families have complete-coverage guards. Product serials
	// are discovery hints and still pass the separate interactive review gate.
	for _, a := range left.Evidence {
		for _, b := range right.Evidence {
			if evidenceKey(a) == evidenceKey(b) && (a.Field == "disc_family" || (serialHint && a.Field == "xbox360_serial")) {
				return true
			}
		}
	}
	return false
}

func regionalProviderProof(left, right catalogGame) (map[string]bool, bool, bool) {
	providers := map[string]bool{}
	direct, regional := false, false
	for _, a := range left.Evidence {
		for _, b := range right.Evidence {
			if evidenceKey(a) != evidenceKey(b) {
				continue
			}
			switch a.Field {
			case "database_id", "screenscraper_id", "igdb_id":
				providers[a.Source] = true
				direct = true
			}
		}
	}
	for _, pair := range [][2]catalogGame{{left, right}, {right, left}} {
		for _, alias := range pair[0].SourceAliases {
			if regionalTitleKey(alias.Title) != regionalTitleKey(pair[1].Title) || !aliasEvidenceAllowed(pair[0], alias.Identity) || !aliasEvidenceAllowed(pair[1], alias.Identity) {
				continue
			}
			providers[alias.Identity.Source] = true
			regional = regional || aliasRegionMatches(alias.Region, pair[1])
		}
	}
	return providers, direct, regional
}

// reviewedDevelopmentGames accepts an explicit hash-bound parent decision.
// It can retire former release-stage keys, never merge another commercial card.
// Existing parent qualifiers remain untouched. Each early release must fit an
// edition profile already present in that parent; provider taxonomy can be
// inherited only when the early release has no conflicting product type.
func reviewedDevelopmentGames(games []catalogGame, parent string) ([]catalogGame, string) {
	if parent == "" {
		return games, contentGroupHoldReason(games)
	}
	parentIndex := -1
	for i, game := range games {
		if game.Folder == parent {
			parentIndex = i
		}
	}
	if parentIndex < 0 {
		return games, "reviewed development parent is not a member"
	}
	parentProfiles := games[parentIndex].Content
	if len(parentProfiles) == 0 {
		return games, "reviewed development parent has unknown releases"
	}
	for _, profile := range parentProfiles {
		switch profile.Kind {
		case "retail", "expanded edition", "remake", "remaster":
		default:
			if !profile.Development() {
				return games, "development parent contains a separate addon or supplement"
			}
		}
	}
	for i, game := range games {
		if i == parentIndex {
			continue
		}
		if len(game.Content) == 0 {
			return games, "reviewed development member has unknown releases"
		}
		for _, profile := range game.Content {
			if !profile.Development() {
				return games, "only confirmed development releases can retire a different reviewed content key"
			}
			compatible := false
			for _, anchor := range parentProfiles {
				if profile.Edition == anchor.Edition && (profile.Product == "" || profile.Product == anchor.Product || (profile.Product == anchor.Kind && !anchor.Development())) {
					compatible = true
				}
			}
			if !compatible {
				return games, "development release has a different edition or product qualifier"
			}
		}
	}
	checked := append([]catalogGame(nil), games...)
	for i := range checked {
		checked[i].ContentKey = games[parentIndex].ContentKey
	}
	return checked, contentRelationshipHoldReason(checked)
}
