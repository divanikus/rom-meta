package arcaderepair

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// ApplySelected restricts an existing prepared snapshot projection to exact groups.
// Unselected set bindings are restored before the usual manifest-bound apply.
func ApplySelected(root, reviewFile string, targets []string) (string, error) {
	data, err := catalogfs.ReadFile(reviewFile)
	if err != nil {
		return "", err
	}
	var report Report
	if err = json.Unmarshal(data, &report); err != nil {
		return "", err
	}
	selected := map[string]bool{}
	for _, target := range targets {
		if selected[target] {
			return "", fmt.Errorf("duplicate MAME target")
		}
		selected[target] = true
	}
	var groups []Group
	sources := map[string]string{}
	for _, group := range report.Groups {
		if selected[group.Target] {
			if group.Conflict != "" {
				return "", fmt.Errorf("%s", group.Conflict)
			}
			groups = append(groups, group)
			for _, source := range group.Sources {
				sources[source] = group.Target
			}
		}
	}
	if len(groups) != len(targets) || len(groups) == 0 {
		return "", fmt.Errorf("unknown or empty MAME selection")
	}
	binding := filepath.Join("bindings", "romsets", "mame", report.Snapshot, "catalog.yaml")
	original, err := readDocument(filepath.Join(filepath.Dir(reviewFile), "original", binding))
	if err != nil {
		return "", err
	}
	sets := catalogyaml.Value(original.Root(), "sets")
	if err := catalogyaml.Mapping(sets, func(_ string, node *yaml.Node) error {
		from := catalogyaml.Scalar(catalogyaml.Value(node, "game"))
		if to := sources[from]; to != "" {
			return catalogyaml.Set(node, "game", scalar(to))
		}
		return nil
	}); err != nil {
		return "", err
	}
	next, err := original.Marshal()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(reviewFile), "staged", binding)
	if err := catalogfs.WriteFile(path, next, 0644); err != nil {
		return "", err
	}
	report.BindingAfter, err = fileHash(path)
	if err != nil {
		return "", err
	}
	report.Groups = groups
	report.Conflicts = nil
	data, err = json.Marshal(report)
	if err != nil {
		return "", err
	}
	if err := catalogfs.WriteFile(reviewFile, data, 0644); err != nil {
		return "", err
	}
	return Apply(root, reviewFile)
}
