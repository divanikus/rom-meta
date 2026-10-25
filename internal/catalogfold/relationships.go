package catalogfold

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
	"rommeta/internal/sourceyaml"
)

type sourceRelationship struct {
	Source, ID   string
	Relationship sourceyaml.Relationship
}

func collectSourceRelationships(game *catalogGame, source string, match *yaml.Node) error {
	// Cross-provider bridge IDs are not evidence for edition identity.
	fields := foldEvidenceFields(source)
	if len(fields) != 1 {
		return nil
	}
	id := catalogyaml.Scalar(catalogyaml.Value(match, fields[0]))
	if id == "" {
		return nil
	}
	// Existing unscoped IGDB matches were deliberately retained only as text.
	if source == "igdb" && len(catalogyaml.Strings(catalogyaml.Value(match, "platforms"))) == 0 {
		return nil
	}
	game.RelationshipIDs = append(game.RelationshipIDs, Evidence{Source: source, Field: fields[0], Value: id})
	return catalogyaml.Sequence(catalogyaml.Value(match, "relationships"), func(node *yaml.Node) error {
		var relationship sourceyaml.Relationship
		if err := node.Decode(&relationship); err != nil {
			return fmt.Errorf("%s relationship: %w", source, err)
		}
		if relationship.TargetID != "" && relationship.TargetID != id {
			game.Relationships = append(game.Relationships, sourceRelationship{source, id, relationship})
		}
		return nil
	})
}

// withEditionEvidence resolves source-scoped, explicit edition chains within
// one catalog platform. Conflicting parents and cycles produce no family.
func withEditionEvidence(games []catalogGame, only bool) []catalogGame {
	parents := map[string]string{}
	present := map[string]bool{}
	conflicts := map[string]bool{}
	key := func(source, id string) string { return source + "\x00" + id }
	for _, game := range games {
		allowed := map[string]bool{}
		for _, e := range game.Evidence {
			allowed[key(e.Source, e.Value)] = true
		}
		for _, e := range game.RelationshipIDs {
			if allowed[key(e.Source, e.Value)] {
				present[key(e.Source, e.Value)] = true
			}
		}
		for _, r := range game.Relationships {
			if !r.Relationship.CanFoldEdition() {
				continue
			}
			from, to := key(r.Source, r.ID), key(r.Source, r.Relationship.TargetID)
			if !allowed[from] {
				continue
			}
			if existing := parents[from]; existing != "" && existing != to {
				conflicts[from] = true
			}
			parents[from] = to
		}
	}
	rootOf := func(id string) string {
		seen := map[string]bool{}
		for {
			if seen[id] || conflicts[id] {
				return ""
			}
			seen[id] = true
			parent := parents[id]
			if parent == "" || !present[parent] {
				return id
			}
			id = parent
		}
	}
	roots := map[string]bool{}
	for id, parent := range parents {
		if !present[parent] {
			continue
		}
		if root := rootOf(id); root != "" && present[root] {
			roots[root] = true
		}
	}
	for i := range games {
		var families []Evidence
		scoped := map[string]bool{}
		for _, e := range games[i].RelationshipIDs {
			scoped[key(e.Source, e.Value)] = true
		}
		seen := map[string]bool{}
		for _, e := range games[i].Evidence {
			id := key(e.Source, e.Value)
			root := rootOf(id)
			if !scoped[id] || !roots[root] || seen[root] {
				continue
			}
			seen[root] = true
			parts := strings.SplitN(root, "\x00", 2)
			family := Evidence{Source: parts[0], Field: "edition_family", Value: parts[1]}
			if len(filterIgnoredEvidence([]Evidence{family}, games[i].IgnoredEvidence)) == 0 {
				continue
			}
			families = append(families, family)
			if id == root {
				games[i].EditionBase = true
			}
		}
		if only {
			games[i].Evidence = families
		} else {
			games[i].Evidence = append(games[i].Evidence, families...)
		}
	}
	return games
}
