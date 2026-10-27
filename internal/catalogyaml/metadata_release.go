package catalogyaml

import (
	"gopkg.in/yaml.v3"
	"rommeta/internal/releasepolicy"
)

// DefaultRelease selects the game's ordinary metadata basis, not a ROM identity
// or an assertion about the historical input of a derivative.
func DefaultRelease(root *yaml.Node) string {
	releases := Value(root, "releases")
	if name := Scalar(Value(root, "default_release")); name != "" && Value(releases, name) != nil {
		return name
	}
	var names []string
	_ = Mapping(releases, func(name string, _ *yaml.Node) error {
		names = append(names, name)
		return nil
	})
	return releasepolicy.Choose(names, nil, nil)
}
