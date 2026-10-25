package catalogfold

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Fold moves fragments between documents, where anchor names have no shared
// meaning. Resolve references in their original tree before splitting it.
func literalFoldYAML(root *yaml.Node) (*yaml.Node, error) {
	if !hasFoldYAMLReferences(root) {
		return root, nil
	}
	const maxAliasNodes = 100000
	const maxAliasBytes = 16 << 20
	const maxDepth = 256
	active := map[*yaml.Node]bool{}
	nodes, bytes := 0, 0
	var clone func(*yaml.Node, int, bool) (*yaml.Node, error)
	clone = func(node *yaml.Node, depth int, expanded bool) (*yaml.Node, error) {
		if node == nil {
			return nil, nil
		}
		if depth > maxDepth {
			return nil, fmt.Errorf("fold YAML reference nesting exceeds %d levels", maxDepth)
		}
		if active[node] {
			return nil, fmt.Errorf("recursive YAML alias at line %d", node.Line)
		}
		if expanded {
			nodes++
			bytes += len(node.Value) + len(node.Tag) + len(node.HeadComment) + len(node.LineComment) + len(node.FootComment)
			if nodes > maxAliasNodes || bytes > maxAliasBytes {
				return nil, fmt.Errorf("fold YAML alias expansion exceeds limit")
			}
		}
		active[node] = true
		defer delete(active, node)
		if node.Kind == yaml.AliasNode {
			if node.Alias == nil {
				return nil, fmt.Errorf("unresolved YAML alias %q", node.Value)
			}
			result, err := clone(node.Alias, depth+1, true)
			if err == nil {
				if node.HeadComment != "" {
					result.HeadComment = node.HeadComment
				}
				if node.LineComment != "" {
					result.LineComment = node.LineComment
				}
				if node.FootComment != "" {
					result.FootComment = node.FootComment
				}
			}
			return result, err
		}
		copy := *node
		copy.Anchor = ""
		copy.Alias = nil
		copy.Content = nil
		for _, child := range node.Content {
			literal, err := clone(child, depth+1, expanded)
			if err != nil {
				return nil, err
			}
			copy.Content = append(copy.Content, literal)
		}
		if copy.Kind == yaml.MappingNode {
			if err := flattenFoldYAMLMerges(&copy); err != nil {
				return nil, err
			}
		}
		return &copy, nil
	}
	return clone(root, 0, false)
}

func hasFoldYAMLReferences(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" || node.Tag == "!!merge" {
		return true
	}
	for _, child := range node.Content {
		if hasFoldYAMLReferences(child) {
			return true
		}
	}
	return false
}

// YAML merge keys inherit mappings; explicit keys override every inherited key,
// and the first mapping in a merge sequence wins. Keep that meaning as literals.
func flattenFoldYAMLMerges(node *yaml.Node) error {
	merges := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Tag == "!!merge" {
			merges = true
			break
		}
	}
	if !merges {
		return nil
	}
	seen := map[string]bool{}
	keyOf := func(key *yaml.Node) (string, error) {
		if key.Kind != yaml.ScalarNode {
			return "", fmt.Errorf("fold YAML merge requires scalar keys")
		}
		return key.Tag + "\x00" + key.Value, nil
	}
	var result []*yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Tag == "!!merge" {
			continue
		}
		identity, err := keyOf(key)
		if err != nil {
			return err
		}
		seen[identity] = true
		result = append(result, key, value)
	}
	var inherit func(*yaml.Node) error
	inherit = func(source *yaml.Node) error {
		if source.Kind == yaml.SequenceNode {
			for _, item := range source.Content {
				if item.Kind != yaml.MappingNode {
					return fmt.Errorf("fold YAML merge sequence requires mappings")
				}
				if err := inherit(item); err != nil {
					return err
				}
			}
			return nil
		}
		if source.Kind != yaml.MappingNode {
			return fmt.Errorf("fold YAML merge requires a mapping")
		}
		for i := 0; i+1 < len(source.Content); i += 2 {
			key, value := source.Content[i], source.Content[i+1]
			identity, err := keyOf(key)
			if err != nil {
				return err
			}
			if !seen[identity] {
				seen[identity] = true
				result = append(result, key, value)
			}
		}
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Tag == "!!merge" {
			if err := inherit(node.Content[i+1]); err != nil {
				return err
			}
		}
	}
	node.Content = result
	return nil
}
