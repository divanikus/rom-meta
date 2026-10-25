package serveui

import (
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
)

func parseSourceMatches(raw string) []sourceMatch {
	document, err := catalogyaml.Parse([]byte(raw))
	if err != nil {
		return nil
	}
	root := document.Root()
	var sourceNode *yaml.Node
	_ = catalogyaml.Mapping(root, func(_ string, value *yaml.Node) error {
		if sourceNode == nil {
			sourceNode = value
		}
		return nil
	})
	return parseSourceMatchesNode(sourceNode)
}

func parseSourceMatchesNode(sourceNode *yaml.Node) []sourceMatch {
	var matches []sourceMatch
	_ = catalogyaml.Sequence(catalogyaml.Value(sourceNode, "matches"), func(node *yaml.Node) error {
		match := sourceMatch{
			Release:         scalarField(node, "release"),
			Releases:        catalogyaml.Strings(catalogyaml.Value(node, "releases")),
			Title:           scalarField(node, "title"),
			Region:          scalarField(node, "region"),
			Platform:        scalarField(node, "platform"),
			ReleaseDate:     scalarField(node, "release_date"),
			Developer:       scalarField(node, "developer"),
			Publisher:       scalarField(node, "publisher"),
			Genres:          catalogyaml.Strings(catalogyaml.Value(node, "genres")),
			Themes:          catalogyaml.Strings(catalogyaml.Value(node, "themes")),
			Collections:     catalogyaml.Strings(catalogyaml.Value(node, "collections")),
			Franchises:      catalogyaml.Strings(catalogyaml.Value(node, "franchises")),
			Platforms:       catalogyaml.Strings(catalogyaml.Value(node, "platforms")),
			MaxPlayers:      scalarField(node, "max_players"),
			Players:         scalarField(node, "players"),
			Rating:          scalarField(node, "rating"),
			CommunityRating: scalarField(node, "community_rating"),
			RatingCount:     scalarField(node, "rating_count"),
			AlternateTitles: catalogyaml.Strings(catalogyaml.Value(node, "alternate_titles")),
			MatchedQuery:    scalarField(node, "matched_query"),
			MatchScore:      scalarField(node, "match_score"),
		}
		match.Titles = parseTitleVariants(catalogyaml.Value(node, "titles"))
		match.Descriptions = stringMap(catalogyaml.Value(node, "descriptions"), true)
		match.References = stringMap(catalogyaml.Value(node, "references"), false)
		match.MediaCounts = mediaCounts(catalogyaml.Value(node, "media"), match.MediaCounts)
		releasesNode := catalogyaml.Value(node, "releases")
		if releasesNode != nil && releasesNode.Kind == yaml.SequenceNode {
			for _, releaseNode := range releasesNode.Content {
				if releaseNode.Kind != yaml.MappingNode {
					continue
				}
				match.Releases = appendUniqueStrings(match.Releases, scalarField(releaseNode, "release"))
				match.MediaCounts = mediaCounts(catalogyaml.Value(releaseNode, "media"), match.MediaCounts)
			}
		}
		matches = append(matches, match)
		return nil
	})
	return compactMatches(matches)
}

func scalarField(node *yaml.Node, key string) string {
	return catalogyaml.Scalar(catalogyaml.Value(node, key))
}

func parseTitleVariants(node *yaml.Node) []titleVariant {
	var result []titleVariant
	_ = catalogyaml.Sequence(node, func(item *yaml.Node) error {
		title := titleVariant{
			Title:    scalarField(item, "title"),
			Region:   scalarField(item, "region"),
			Language: scalarField(item, "language"),
		}
		if title.Title != "" {
			result = append(result, title)
		}
		return nil
	})
	return result
}

func stringMap(node *yaml.Node, lowerKeys bool) map[string]string {
	result := map[string]string{}
	_ = catalogyaml.Mapping(node, func(key string, value *yaml.Node) error {
		if lowerKeys {
			key = strings.ToLower(strings.TrimSpace(key))
		}
		if text := strings.TrimSpace(catalogyaml.Scalar(value)); key != "" && text != "" {
			result[key] = text
		}
		return nil
	})
	if len(result) == 0 {
		return nil
	}
	return result
}

func mediaCounts(node *yaml.Node, counts map[string]int) map[string]int {
	_ = catalogyaml.Mapping(node, func(mediaType string, candidates *yaml.Node) error {
		if candidates != nil && candidates.Kind == yaml.SequenceNode {
			if counts == nil {
				counts = map[string]int{}
			}
			counts[mediaType] += len(candidates.Content)
		}
		return nil
	})
	return counts
}
