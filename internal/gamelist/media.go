package gamelist

import (
	"fmt"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
)

type failedMedia struct{ tags, files, roles map[string]bool }

func newFailedMedia() *failedMedia {
	return &failedMedia{map[string]bool{}, map[string]bool{}, map[string]bool{}}
}
func (failed *failedMedia) keep(tag, file, role string) {
	failed.tags[tag] = true
	failed.files[file] = true
	failed.roles[role] = true
}

// Reconcile only this importer's candidates in one observation. Other source
// observations and owner candidates remain independently referenced.
func reconcileMedia(old, current *yaml.Node, failed *failedMedia) (*yaml.Node, error) {
	if current == nil {
		current = mapping()
	}
	err := catalogyaml.Mapping(old, func(role string, list *yaml.Node) error {
		return catalogyaml.Sequence(list, func(candidate *yaml.Node) error {
			imported := catalogyaml.Scalar(catalogyaml.Value(candidate, "importer")) == "es-de"
			tag := catalogyaml.Scalar(catalogyaml.Value(candidate, "source_tag"))
			file := catalogyaml.Scalar(catalogyaml.Value(candidate, "source_file"))
			keep := !imported || tag != "" && failed.tags[tag] || tag == "" && (failed.files[file] || failed.roles[role])
			if !keep {
				return nil
			}
			target := catalogyaml.Value(current, role)
			if target == nil {
				target = &yaml.Node{Kind: yaml.SequenceNode}
				if err := catalogyaml.Set(current, role, target); err != nil {
					return err
				}
			}
			if target.Kind != yaml.SequenceNode {
				return fmt.Errorf("media role must contain candidates")
			}
			for _, existing := range target.Content {
				if sameNode(existing, candidate) {
					return nil
				}
			}
			target.Content = append(target.Content, candidate)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	// Keep the ordering of surviving roles/candidates. A temporarily missing
	// optional file must not cause a formatting-only catalog rewrite.
	ordered := mapping()
	order := func(role string, list *yaml.Node) error {
		value := catalogyaml.Value(current, role)
		if value == nil {
			return nil
		}
		result := &yaml.Node{Kind: yaml.SequenceNode}
		used := map[int]bool{}
		if list != nil && list.Kind == yaml.SequenceNode {
			for _, before := range list.Content {
				for i, after := range value.Content {
					if used[i] {
						continue
					}
					equal := true
					for _, key := range []string{"url", "source_file", "importer"} {
						if key == "url" && catalogyaml.Scalar(catalogyaml.Value(before, "sha256")) != "" && catalogyaml.Scalar(catalogyaml.Value(before, "sha256")) == catalogyaml.Scalar(catalogyaml.Value(after, "sha256")) {
							continue
						}
						if catalogyaml.Scalar(catalogyaml.Value(before, key)) != catalogyaml.Scalar(catalogyaml.Value(after, key)) {
							equal = false
						}
					}
					if equal {
						result.Content = append(result.Content, after)
						used[i] = true
						break
					}
				}
			}
		}
		for i, candidate := range value.Content {
			if !used[i] {
				result.Content = append(result.Content, candidate)
			}
		}
		return catalogyaml.Set(ordered, role, result)
	}
	if err = catalogyaml.Mapping(old, order); err != nil {
		return nil, err
	}
	if err = catalogyaml.Mapping(current, func(role string, list *yaml.Node) error {
		if catalogyaml.Value(ordered, role) != nil {
			return nil
		}
		return catalogyaml.Set(ordered, role, list)
	}); err != nil {
		return nil, err
	}
	return ordered, nil
}
