package catalogyaml

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// GamelistMediaReferences returns the currently referenced local assets for each
// gamelist collection. Retained files outside these sets are recovery material,
// not candidates for automatic selection. References from all observations count.
func GamelistMediaReferences(root *yaml.Node) (map[string]map[string]bool, error) {
	result := map[string]map[string]bool{}
	err := Mapping(Value(root, "sources"), func(source string, stanza *yaml.Node) error {
		if !strings.HasPrefix(source, "gamelist.") {
			return nil
		}
		paths := map[string]bool{}
		result[source] = paths
		return Sequence(Value(stanza, "matches"), func(match *yaml.Node) error {
			return Mapping(Value(match, "media"), func(_ string, candidates *yaml.Node) error {
				return Sequence(candidates, func(candidate *yaml.Node) error {
					value := Scalar(Value(candidate, "url"))
					if value == "" {
						return nil
					}
					if !strings.HasPrefix(value, "media/") || path.Clean(value) != value || strings.ContainsAny(value, "\\:\x00") {
						return fmt.Errorf("%s has an unsafe local media reference", source)
					}
					paths[value] = true
					return nil
				})
			})
		})
	})
	return result, err
}
