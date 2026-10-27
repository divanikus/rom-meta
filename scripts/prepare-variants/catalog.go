package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
)

func yamlNode(v any) (*yaml.Node, error) { n := &yaml.Node{}; err := n.Encode(v); return n, err }
func rejectDuplicateYAML(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
				return fmt.Errorf("duplicate, merged or non-string YAML key %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("YAML aliases require separate manual preparation")
	}
	for _, child := range n.Content {
		if err := rejectDuplicateYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func addEntry(g, f *catalogyaml.Document, e entry) (bool, error) {
	variants, err := catalogyaml.EnsureMapping(g.Root(), "variants")
	if err != nil {
		return false, err
	}
	v := map[string]any{"kind": e.Variant.Kind}
	if e.Variant.BasedOn != "" {
		v["based_on"] = e.Variant.BasedOn
	}
	if e.Variant.Title != "" {
		v["title"] = e.Variant.Title
	}
	if len(e.Variant.Languages) > 0 {
		v["languages"] = e.Variant.Languages
	}
	if e.Variant.Group != "" {
		v["group"] = e.Variant.Group
	}
	if e.Variant.Version != "" {
		v["version"] = e.Variant.Version
	}
	source := map[string]any{"name": "prepared-variants", "relationship": e.Evidence.Relationship, "links": e.Evidence.Links}
	// An explicitly prepared display label is also a complete metadata-only alias.
	// Payload lookup continues to require the modified hash.
	if e.Variant.Title != "" {
		source["entry_name"] = e.Variant.Title
	}
	if len(e.Evidence.Normalization) > 0 {
		source["normalization"] = e.Evidence.Normalization
	}
	if e.Evidence.Relationship == "source-release-name" {
		source["base_binary_identity"] = "not-proven-by-base-hash"
	}
	if e.Variant.BasedOn == "" {
		source["relationship_scope"] = "game"
	}
	v["source"] = source
	node, err := yamlNode(v)
	if err != nil {
		return false, err
	}
	changed := false
	if old := catalogyaml.Value(variants, e.Variant.Key); old != nil {
		if !sameYAML(old, node) {
			return false, fmt.Errorf("variant key already has a different definition")
		}
	} else {
		if err := catalogyaml.Set(variants, e.Variant.Key, node); err != nil {
			return false, err
		}
		changed = true
	}
	items := catalogyaml.Value(f.Root(), "items")
	if items != nil && catalogyaml.Value(f.Root(), "roms") != nil {
		return false, fmt.Errorf("fingerprints has both items and roms")
	}
	if items == nil {
		items = catalogyaml.Value(f.Root(), "roms")
	}
	if items == nil {
		items = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if err := catalogyaml.Set(f.Root(), "items", items); err != nil {
			return false, err
		}
	}
	if items.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("fingerprint items must be a sequence")
	}
	file := map[string]any{"name": e.File.Name}
	if e.File.Kind != "" {
		file["kind"] = e.File.Kind
	}
	if e.File.Size != 0 {
		file["size"] = e.File.Size
	}
	for k, value := range map[string]string{"sha1": e.File.SHA1, "sha256": e.File.SHA256, "md5": e.File.MD5, "crc32": e.File.CRC32} {
		if value != "" {
			file[k] = strings.ToLower(value)
		}
	}
	fp, err := yamlNode(map[string]any{"variant": e.Variant.Key, "source": source, "file": file})
	if err != nil {
		return false, err
	}
	found := false
	for _, existing := range items.Content {
		owner := catalogyaml.Scalar(catalogyaml.Value(existing, "variant"))
		existingFile := catalogyaml.Value(existing, "file")
		if catalogyaml.Scalar(catalogyaml.Value(existingFile, "kind")) != e.File.Kind {
			continue
		}
		same := false
		for _, k := range []string{"sha1", "sha256"} {
			want := strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(fp, "file"), k)))
			got := strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(existingFile, k)))
			same = same || want != "" && want == got && (e.File.Kind == "chd-combined-sha1" || e.File.Size == catalogyaml.Int64(catalogyaml.Value(existingFile, "size")))
		}
		if !same {
			continue
		}
		if owner != e.Variant.Key || catalogyaml.Scalar(catalogyaml.Value(existing, "release")) != "" {
			return false, fmt.Errorf("payload already owned by release %q / variant %q", catalogyaml.Scalar(catalogyaml.Value(existing, "release")), owner)
		}
		if !sameYAML(existing, fp) {
			return false, fmt.Errorf("existing variant fingerprint has different constraints or provenance")
		}
		found = true
	}
	if !found {
		items.Content = append(items.Content, fp)
		changed = true
	}
	return changed, nil
}
func sameYAML(a, b *yaml.Node) bool {
	var x, y any
	if a.Decode(&x) != nil || b.Decode(&y) != nil {
		return false
	}
	xb, e1 := json.Marshal(x)
	yb, e2 := json.Marshal(y)
	return e1 == nil && e2 == nil && string(xb) == string(yb)
}

func checkBundle(g, f *catalogyaml.Document) error {
	if _, err := catalogyaml.Variants(g.Root()); err != nil {
		return err
	}
	items := catalogyaml.Value(f.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(f.Root(), "roms")
	}
	return catalogyaml.Sequence(items, func(item *yaml.Node) error { _, _, err := catalogyaml.FingerprintTarget(g.Root(), item); return err })
}
