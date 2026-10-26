package catalogfold

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
	"rommeta/internal/contentidentity"
)

func parseGameYAML(contents []byte) (catalogGame, error) {
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return catalogGame{}, err
	}
	root, err := literalFoldYAML(document.Root())
	if err != nil {
		return catalogGame{}, err
	}
	if _, err := catalogyaml.Variants(root); err != nil {
		return catalogGame{}, err
	}
	game := catalogGame{
		Title:           catalogyaml.Scalar(catalogyaml.Value(root, "title")),
		ContentKey:      catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(root, "fold"), "content_key")),
		DefaultRelease:  catalogyaml.Scalar(catalogyaml.Value(root, "default_release")),
		SourceBlocks:    map[string]sourceBlock{},
		IgnoredEvidence: catalogyaml.Strings(catalogyaml.Value(catalogyaml.Value(root, "fold"), "ignore_evidence")),
		Aliases:         catalogyaml.Strings(catalogyaml.Value(root, "aliases")),
		AlternateTitles: catalogyaml.Strings(catalogyaml.Value(root, "alternate_titles")),
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "releases"), func(key string, value *yaml.Node) error {
		text, err := renderMappingEntry(key, value, 2, true)
		if err != nil {
			return err
		}
		profile := contentidentity.Release(key, game.Title, catalogyaml.Strings(catalogyaml.Value(value, "tags"))...)
		game.Content = append(game.Content, profile)
		game.Releases = append(game.Releases, block{Key: key, Text: text})
		collectRedumpSerial(&game, value)
		collectPS3Serials(&game, key, value)
		regions := catalogyaml.Strings(catalogyaml.Value(value, "regions"))
		if len(regions) == 0 {
			regions = releaseRegions(key)
		}
		for _, region := range regions {
			game.Regions = appendUniqueStrings(game.Regions, normalizeAliasRegion(region))
		}
		if catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(value, "mame"), "snapshot")) != "" {
			game.HasRomsetBindings = true
		}
		return nil
	}); err != nil {
		return catalogGame{}, err
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "sources"), func(source string, stanza *yaml.Node) error {
		raw, err := renderMappingEntry(source, stanza, 2, false)
		if err != nil {
			return err
		}
		item := sourceBlock{Name: source, Raw: raw}
		matches := catalogyaml.Value(stanza, "matches")
		if matches != nil {
			if matches.Kind != yaml.SequenceNode {
				return fmt.Errorf("source %s matches must be a sequence", source)
			}
			item.HasMatches = true
			for _, match := range matches.Content {
				text, err := renderSequenceItem(match, 6)
				if err != nil {
					return err
				}
				item.MatchBlocks = append(item.MatchBlocks, text)
				if err := collectSourceRelationships(&game, source, match); err != nil {
					return err
				}
				evidence := sourceEvidenceFromMatch(source, strings.Split(text, "\n"), game.Title)
				if source == "igdb" && len(catalogyaml.Strings(catalogyaml.Value(match, "platforms"))) == 0 {
					evidence = nil
				}
				if len(evidence) > 0 {
					kind := contentidentity.SourceKind(catalogyaml.Scalar(catalogyaml.Value(match, "game_type")))
					if kind != "" {
						for i, release := range game.Releases {
							if !catalogyaml.MatchIncludesRelease(match, release.Key) {
								continue
							}
							game.Content = contentidentity.ApplyKind(game.Content, i, kind)
						}
					}
				}
				game.Evidence = append(game.Evidence, evidence...)
				if err := collectSourceAliases(&game, source, match, evidence); err != nil {
					return err
				}
			}
		}
		game.SourceBlocks[source] = item
		return nil
	}); err != nil {
		return catalogGame{}, err
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "variants"), func(key string, value *yaml.Node) error {
		text, err := renderMappingEntry(key, value, 2, true)
		if err != nil {
			return err
		}
		game.Variants = append(game.Variants, block{Key: key, Text: text})
		return nil
	}); err != nil {
		return catalogGame{}, err
	}
	game.IgnoredEvidence = appendEvidenceTokens(nil, game.IgnoredEvidence...)
	game.Evidence = filterIgnoredEvidence(game.Evidence, game.IgnoredEvidence)
	return game, nil
}

func renderMappingEntry(key string, value *yaml.Node, indent int, quoteKey bool) (string, error) {
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	if quoteKey {
		keyNode.Style = yaml.DoubleQuotedStyle
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		keyNode, value,
	}}
	contents, err := yaml.Marshal(root)
	if err != nil {
		return "", err
	}
	return indentYAML(string(contents), indent), nil
}

func renderSequenceItem(value *yaml.Node, indent int) (string, error) {
	root := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{value}}
	contents, err := yaml.Marshal(root)
	if err != nil {
		return "", err
	}
	return indentYAML(string(contents), indent), nil
}

func indentYAML(value string, indent int) string {
	prefix := strings.Repeat(" ", indent)
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return strings.Join(lines, "\n") + "\n"
}
