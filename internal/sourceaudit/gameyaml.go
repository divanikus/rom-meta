package sourceaudit

import (
	"strconv"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

func readGameYAML(path string) (catalogGame, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return catalogGame{}, err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return catalogGame{}, err
	}
	root := document.Root()
	game := catalogGame{
		GameRef:  GameRef{Title: catalogyaml.Scalar(catalogyaml.Value(root, "title"))},
		Releases: map[string]bool{},
		Sources:  map[string]sourceRecord{},
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "releases"), func(name string, _ *yaml.Node) error {
		game.Releases[name] = true
		return nil
	}); err != nil {
		return catalogGame{}, err
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "sources"), func(source string, stanza *yaml.Node) error {
		record := sourceRecord{}
		if err := catalogyaml.Sequence(catalogyaml.Value(stanza, "matches"), func(matchNode *yaml.Node) error {
			match := sourceMatch{}
			match.Releases = appendUniqueString(match.Releases, catalogyaml.Scalar(catalogyaml.Value(matchNode, "release")))
			for _, release := range catalogyaml.Strings(catalogyaml.Value(matchNode, "releases")) {
				match.Releases = appendUniqueString(match.Releases, release)
			}
			releasesNode := catalogyaml.Value(matchNode, "releases")
			if releasesNode != nil && releasesNode.Kind == yaml.SequenceNode {
				for _, item := range releasesNode.Content {
					if item.Kind == yaml.MappingNode {
						match.Releases = appendUniqueString(match.Releases, catalogyaml.Scalar(catalogyaml.Value(item, "release")))
					}
				}
			}
			if rawScore := catalogyaml.Scalar(catalogyaml.Value(matchNode, "match_score")); rawScore != "" {
				if score, err := strconv.Atoi(rawScore); err == nil {
					match.Score = score
					match.HasScore = true
				}
			}
			record.Matches = append(record.Matches, match)
			return nil
		}); err != nil {
			return err
		}
		game.Sources[source] = record
		return nil
	}); err != nil {
		return catalogGame{}, err
	}
	return game, nil
}
