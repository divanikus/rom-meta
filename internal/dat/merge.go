package dat

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/releasepolicy"
)

func mergeImportedGameYAML(existing, generated string, plan Plan, options ImportOptions) (string, error) {
	existingDocument, err := catalogyaml.Parse([]byte(existing))
	if err != nil {
		return "", err
	}
	generatedDocument, err := catalogyaml.Parse([]byte(generated))
	if err != nil {
		return "", err
	}
	existingReleases := catalogyaml.Value(existingDocument.Root(), "releases")
	generatedReleases, err := catalogyaml.EnsureMapping(generatedDocument.Root(), "releases")
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	if err := catalogyaml.Mapping(generatedReleases, func(key string, _ *yaml.Node) error {
		seen[key] = true
		return nil
	}); err != nil {
		return "", err
	}
	if err := catalogyaml.Mapping(existingReleases, func(key string, value *yaml.Node) error {
		if seen[key] {
			if plan.ArcadeSnapshot == "" {
				target := catalogyaml.Value(generatedReleases, key)
				if err := preserveUnownedFields(target, value, "title", "regions", "languages", "tags", "authority"); err != nil {
					return err
				}
				if err := preserveUnownedFields(catalogyaml.Value(target, "authority"), catalogyaml.Value(value, "authority"), "source", "entry_name", "entry_id", "cloneof_id", "serial", "version"); err != nil {
					return err
				}
			}
			if plan.ArcadeSnapshot != "" {
				target := catalogyaml.Value(generatedReleases, key)
				if err := catalogyaml.Mapping(value, func(field string, node *yaml.Node) error {
					if catalogyaml.Value(target, field) == nil {
						return catalogyaml.Set(target, field, node)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		}
		generatedReleases.Content = append(generatedReleases.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Style: yaml.DoubleQuotedStyle}, value,
		)
		seen[key] = true
		return nil
	}); err != nil {
		return "", err
	}
	if title := catalogyaml.Value(existingDocument.Root(), "title"); catalogyaml.Scalar(title) != "" {
		if err := catalogyaml.Set(generatedDocument.Root(), "title", title); err != nil {
			return "", err
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	priority := options.RegionPriority
	if platformPriority := options.PlatformRegionPriority[plan.PlatformName]; len(platformPriority) > 0 {
		priority = platformPriority
	}
	selected := releasepolicy.Choose(keys, priority, options.RegionGroups)
	if plan.ArcadeSnapshot != "" {
		selected = catalogyaml.Scalar(catalogyaml.Value(generatedDocument.Root(), "default_release"))
		if previous := catalogyaml.Scalar(catalogyaml.Value(existingDocument.Root(), "default_release")); seen[previous] {
			selected = previous
		}
	}
	if selected != "" {
		if err := catalogyaml.Set(generatedDocument.Root(), "default_release", &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: selected,
			Style: yaml.DoubleQuotedStyle,
		}); err != nil {
			return "", err
		}
	}
	owned := []string{"title", "default_release", "releases"}
	if plan.ArcadeSnapshot != "" {
		owned = append(owned, "arcade")
		target := catalogyaml.Value(generatedDocument.Root(), "arcade")
		if err := catalogyaml.Mapping(catalogyaml.Value(existingDocument.Root(), "arcade"), func(field string, node *yaml.Node) error {
			if catalogyaml.Value(target, field) == nil {
				return catalogyaml.Set(target, field, node)
			}
			return nil
		}); err != nil {
			return "", err
		}
	}
	merged, err := generatedDocument.Marshal()
	if err != nil {
		return "", err
	}
	return catalogyaml.PreserveUnknownTopLevel(existing, string(merged), owned...)
}

func mergeImportedFingerprintsYAML(existing, generated string) (string, error) {
	existingDocument, err := catalogyaml.Parse([]byte(existing))
	if err != nil {
		return "", err
	}
	generatedDocument, err := catalogyaml.Parse([]byte(generated))
	if err != nil {
		return "", err
	}
	generatedItems := catalogyaml.Value(generatedDocument.Root(), "items")
	if generatedItems == nil || generatedItems.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("generated fingerprints items must be a sequence")
	}
	seen := map[string]*yaml.Node{}
	for _, item := range generatedItems.Content {
		if key := fingerprintItemKey(item); key != "" {
			seen[key] = item
		}
	}
	existingItems := catalogyaml.Value(existingDocument.Root(), "items")
	if existingItems == nil {
		existingItems = catalogyaml.Value(existingDocument.Root(), "roms")
	}
	if err := catalogyaml.Sequence(existingItems, func(item *yaml.Node) error {
		key := fingerprintItemKey(item)
		if target := seen[key]; key != "" && target != nil {
			return preserveUnownedFields(target, item)
		}
		generatedItems.Content = append(generatedItems.Content, item)
		if key != "" {
			seen[key] = item
		}
		return nil
	}); err != nil {
		return "", err
	}
	contents, err := generatedDocument.Marshal()
	if err != nil {
		return "", err
	}
	return string(contents), nil
}

func fingerprintItemKey(item *yaml.Node) string {
	if id := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(item, "id"))); id != "" {
		return "id:" + id
	}
	release := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(item, "release")))
	file := catalogyaml.Value(item, "file")
	name := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(file, "name")))
	part := fingerprintNodeHash(item)
	return "fallback:" + strings.Join([]string{release, name, part}, "\x00")
}

// Fresh authority values win. Fields outside its schema survive refresh.
func preserveUnownedFields(target, existing *yaml.Node, owned ...string) error {
	if target == nil || existing == nil || target.Kind != yaml.MappingNode || existing.Kind != yaml.MappingNode {
		return nil
	}
	skip := map[string]bool{}
	for _, field := range owned {
		skip[field] = true
	}
	return catalogyaml.Mapping(existing, func(field string, node *yaml.Node) error {
		if skip[field] {
			return nil
		}
		current := catalogyaml.Value(target, field)
		if current == nil {
			return catalogyaml.Set(target, field, node)
		}
		return preserveUnownedFields(current, node)
	})
}
