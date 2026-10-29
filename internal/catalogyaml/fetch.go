package catalogyaml

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// PreservedSources lists owner-controlled source stanzas that acquisition must
// not replace. Notes inside generated source data do not establish this policy.
func PreservedSources(root *yaml.Node) ([]string, error) {
	fetch := Value(root, "fetch")
	if fetch == nil {
		return nil, nil
	}
	// Decode this small owner block so aliases and YAML merge keys keep their
	// ordinary meaning. Reject malformed controls rather than silently unlocking.
	var options map[string]any
	if err := fetch.Decode(&options); err != nil {
		return nil, fmt.Errorf("fetch must be a mapping: %w", err)
	}
	value, present := options["preserve_sources"]
	if !present {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("fetch.preserve_sources must be a list of source IDs")
	}
	var out []string
	seen := map[string]bool{}
	for _, item := range list {
		source, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("fetch.preserve_sources must contain source IDs")
		}
		source = strings.ToLower(strings.TrimSpace(source))
		if source == "" {
			return nil, fmt.Errorf("fetch.preserve_sources contains an empty source ID")
		}
		if !seen[source] {
			seen[source] = true
			out = append(out, source)
		}
	}
	return out, nil
}

func SourcePreserved(root *yaml.Node, source string) (bool, error) {
	sources, err := PreservedSources(root)
	if err != nil {
		return false, err
	}
	for _, s := range sources {
		if s == strings.ToLower(strings.TrimSpace(source)) {
			return true, nil
		}
	}
	return false, nil
}
