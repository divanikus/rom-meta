package exporter

import (
	"sort"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
)

type descriptionNode struct {
	node     *yaml.Node
	release  string
	distance int
}

// Description scope is a display policy over an already bound catalog card.
// Provider match releases identify acquisition evidence, not distinct game prose.
func descriptionNodes(g *game, source, release, scope string) []descriptionNode {
	if scope != "game" {
		var result []descriptionNode
		for _, n := range sourceNodes(g.root, source, release) {
			result = append(result, descriptionNode{node: n, release: release})
		}
		return result
	}
	var result []descriptionNode
	releases := catalogyaml.Value(g.root, "releases")
	add := func(n *yaml.Node, scopes []string) {
		if len(scopes) == 0 {
			result = append(result, descriptionNode{node: n, distance: 1})
			return
		}
		var valid []string
		for _, r := range scopes {
			if r == release {
				result = append(result, descriptionNode{node: n, release: r})
				return
			}
			if catalogyaml.Value(releases, r) != nil {
				valid = append(valid, r)
			}
		}
		if len(valid) > 0 {
			sort.Strings(valid)
			from := valid[0]
			if position(valid, field(g.root, "default_release")) >= 0 {
				from = field(g.root, "default_release")
			}
			result = append(result, descriptionNode{node: n, release: from, distance: 2})
		}
	}
	parent := catalogyaml.Value(catalogyaml.Value(g.root, "sources"), source)
	_ = catalogyaml.Sequence(catalogyaml.Value(parent, "matches"), func(match *yaml.Node) error {
		scopes := catalogyaml.Strings(catalogyaml.Value(match, "releases"))
		if r := field(match, "release"); r != "" {
			scopes = append(scopes, r)
		}
		_ = catalogyaml.Sequence(catalogyaml.Value(match, "releases"), func(child *yaml.Node) error {
			if child.Kind == yaml.MappingNode {
				r := field(child, "release")
				scopes = append(scopes, r)
				add(child, []string{r})
			}
			return nil
		})
		add(match, scopes)
		return nil
	})
	return result
}
