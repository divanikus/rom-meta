package catalogedit

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/curation"
)

// ValidateRaw checks the submitted document without normalizing its text.
// Unknown fields and comments remain exactly as the owner entered them.
func ValidateRaw(text string) ([]byte, error) {
	if len(text) > 3<<20 || !utf8.ValidString(text) {
		return nil, fmt.Errorf("YAML must be UTF-8 and at most 3 MiB")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return nil, fmt.Errorf("YAML contains a control character")
		}
	}
	doc, err := catalogyaml.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	if err = validateRawNode(doc.Root(), "$", 0); err != nil {
		return nil, err
	}
	root := doc.Root()
	title := catalogyaml.Value(root, "title")
	if title == nil || title.Tag != "!!str" || strings.TrimSpace(title.Value) == "" {
		return nil, fmt.Errorf("title must be nonempty text")
	}
	if version := catalogyaml.Value(root, "schema_version"); version != nil && (version.Tag != "!!int" || version.Value != "1") {
		return nil, fmt.Errorf("schema_version must be 1")
	}
	if platform := catalogyaml.Value(root, "platform"); platform != nil && platform.Tag != "!!str" {
		return nil, fmt.Errorf("platform must be text")
	}
	for _, key := range Fields {
		n := catalogyaml.Value(root, key)
		if n == nil {
			continue
		}
		var v any
		if err = n.Decode(&v); err != nil {
			return nil, err
		}
		if err = validRecordValue(key, v); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	releases := catalogyaml.Value(root, "releases")
	if err = catalogyaml.Mapping(releases, func(name string, n *yaml.Node) error {
		if strings.TrimSpace(name) == "" || n.Kind != yaml.MappingNode {
			return fmt.Errorf("releases.%s must be a mapping", name)
		}
		for _, key := range Fields {
			if field := catalogyaml.Value(n, key); field != nil {
				var value any
				if e := field.Decode(&value); e != nil {
					return e
				}
				if e := validRecordValue(key, value); e != nil {
					return fmt.Errorf("releases.%s.%s: %w", name, key, e)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if n := catalogyaml.Value(root, "default_release"); n != nil && (n.Tag != "!!str" || catalogyaml.Value(releases, n.Value) == nil) {
		return nil, fmt.Errorf("default_release must name an existing release")
	}
	if _, err = catalogyaml.Variants(root); err != nil {
		return nil, err
	}
	records, _, err := Records([]byte(text))
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		for k, v := range r.Fields {
			if err = validRecordValue(k, v); err != nil {
				return nil, fmt.Errorf("sources.%s / %s / %s: %w", r.Target.Source, r.Label, k, err)
			}
		}
		if scopes, ok := r.Fields["releases"].([]any); ok {
			for _, item := range scopes {
				if scope, ok := item.(string); ok && catalogyaml.Value(releases, scope) == nil {
					return nil, fmt.Errorf("sources.%s references unknown release %q", r.Target.Source, scope)
				}
			}
		}
		if scope, ok := r.Fields["release"].(string); ok && scope != "" && catalogyaml.Value(releases, scope) == nil {
			return nil, fmt.Errorf("sources.%s references unknown release %q", r.Target.Source, scope)
		}
	}
	return []byte(text), nil
}
func validateRawNode(n *yaml.Node, path string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s: YAML is too deeply nested", path)
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || strings.TrimSpace(k.Value) == "" {
				return fmt.Errorf("line %d: use nonempty text keys", k.Line)
			}
			if seen[k.Value] {
				return fmt.Errorf("line %d: duplicate key %s.%s", k.Line, path, k.Value)
			}
			if k.Value == "__proto__" || k.Value == "constructor" || k.Value == "prototype" {
				return fmt.Errorf("line %d: reserved key %s", k.Line, k.Value)
			}
			seen[k.Value] = true
			if err := validateRawNode(n.Content[i+1], path+"."+k.Value, depth+1); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := validateRawNode(c, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if n.Tag == "!!float" {
			var value float64
			if err := n.Decode(&value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("line %d: use a finite number", n.Line)
			}
		}
		switch n.Tag {
		case "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp":
		default:
			return fmt.Errorf("line %d: unsupported YAML tag %s", n.Line, n.Tag)
		}
	default:
		return fmt.Errorf("line %d: use explicit values instead of YAML aliases", n.Line)
	}
	return nil
}

// CheckRawReferences validates whole-card edits and optional paired-document
// drafts. Ordinary source-field edits retain their existing behavior.
func CheckRawReferences(root, platform, folder string, edits []Edit, data []byte, suppliedFingerprints ...[]byte) error {
	raw := false
	for _, e := range edits {
		raw = raw || e.Target.Kind == "raw"
	}
	if !raw && len(suppliedFingerprints) == 0 {
		return nil
	}
	path, err := GamePath(root, platform, folder)
	if err != nil {
		return err
	}
	if _, err = ValidateRaw(string(data)); err != nil {
		return err
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return err
	}
	registry, err := curation.Load(root)
	if err != nil {
		return err
	}
	old, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
	if err != nil {
		return err
	}
	oldDoc, err := catalogyaml.Parse(old)
	if err != nil {
		return err
	}
	if err = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "sources"), func(id string, _ *yaml.Node) error {
		if curation.IsManual(id) && !registry.Contains(id) && catalogyaml.Value(catalogyaml.Value(oldDoc.Root(), "sources"), id) == nil {
			return fmt.Errorf("create a named variant before adding source %q", id)
		}
		return nil
	}); err != nil {
		return err
	}
	var fp []byte
	if len(suppliedFingerprints) > 0 {
		fp = suppliedFingerprints[0]
	} else {
		fp, err = catalogfs.ReadFile(filepath.Join(path, "fingerprints.yaml"))
	}
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(fp) == 0 {
		return nil
	}
	fingerprints, err := catalogyaml.Parse(fp)
	if err != nil {
		return err
	}
	items := catalogyaml.Value(fingerprints.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(fingerprints.Root(), "roms")
	}
	return catalogyaml.Sequence(items, func(n *yaml.Node) error { _, _, err := catalogyaml.FingerprintTarget(doc.Root(), n); return err })
}
