package catalogfold

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"reflect"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogyaml"
	"sort"
	"strings"
)

type fieldConflicts struct{ Fields []string }

func (e *fieldConflicts) Error() string {
	return "conflicting saved fields: " + strings.Join(e.Fields, ", ")
}
func equalNode(a, b *yaml.Node) bool {
	var x, y any
	if a == nil || b == nil {
		return a == b
	}
	if a.Decode(&x) != nil || b.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
func mergeExtra(target, source *yaml.Node, path string, conflicts *[]string) error {
	if source == nil {
		return nil
	}
	return catalogyaml.Mapping(source, func(key string, value *yaml.Node) error {
		name := key
		if path != "" {
			name = path + "." + key
		}
		old := catalogyaml.Value(target, key)
		if old == nil {
			return catalogyaml.Set(target, key, value)
		}
		if equalNode(old, value) {
			return nil
		}
		if old.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			return mergeExtra(old, value, name, conflicts)
		}
		*conflicts = appendUniqueStrings(*conflicts, name)
		return nil
	})
}
func mergedDocument(games []catalogGame, regions regionRules, patch catalogedit.Patch) ([]byte, error) {
	base, err := catalogyaml.Parse([]byte(renderMergedGameYAML(games, regions)))
	if err != nil {
		return nil, err
	}
	extra := &yaml.Node{Kind: yaml.MappingNode}
	var conflicts []string
	for _, game := range games {
		if game.Document == nil {
			continue
		}
		root, err := literalFoldYAML(game.Document.Root())
		if err != nil {
			return nil, err
		}
		fragment := &yaml.Node{Kind: yaml.MappingNode}
		for i := 0; i+1 < len(root.Content); i += 2 {
			key, value := root.Content[i].Value, root.Content[i+1]
			switch key {
			case "title", "default_release", "aliases", "alternate_titles", "releases", "variants":
				continue
			case "sources":
				sources := &yaml.Node{Kind: yaml.MappingNode}
				if err := catalogyaml.Mapping(value, func(source string, stanza *yaml.Node) error {
					rest := &yaml.Node{Kind: yaml.MappingNode}
					if err := catalogyaml.Mapping(stanza, func(k string, v *yaml.Node) error {
						if k == "matches" || k == "status" || k == "checked_at" || (source == "hasheous" && k == "reason") {
							return nil
						}
						return catalogyaml.Set(rest, k, v)
					}); err != nil {
						return err
					}
					if len(rest.Content) > 0 {
						return catalogyaml.Set(sources, source, rest)
					}
					return nil
				}); err != nil {
					return nil, err
				}
				value = sources
			case "fold":
				rest := &yaml.Node{Kind: yaml.MappingNode}
				if err := catalogyaml.Mapping(value, func(k string, v *yaml.Node) error {
					if k == "content_key" || k == "ignore_evidence" {
						return nil
					}
					return catalogyaml.Set(rest, k, v)
				}); err != nil {
					return nil, err
				}
				value = rest
			default:
				if _, resolved := patch[key]; resolved {
					continue
				}
			}
			if err := catalogyaml.Set(fragment, key, value); err != nil {
				return nil, err
			}
		}
		if err := mergeExtra(extra, fragment, "", &conflicts); err != nil {
			return nil, err
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return nil, &fieldConflicts{Fields: conflicts}
	}
	if err := mergeExtra(base.Root(), extra, "", &conflicts); err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("cannot preserve %s", strings.Join(conflicts, ", "))
	}
	data, err := base.Marshal()
	if err != nil {
		return nil, err
	}
	return catalogedit.Apply(data, patch)
}
