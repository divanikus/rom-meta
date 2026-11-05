package catalogedit

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/curation"
	"strings"
)

// Source + indices address the exact saved observation under a card revision.
// Match -1 selects source-level fields. Kind manual creates a scoped record.
type Target struct {
	Kind         string `json:"kind"`
	Source       string `json:"source,omitempty"`
	Match        int    `json:"match"`
	ReleaseIndex *int   `json:"release_index,omitempty"`
	Scope        string `json:"scope,omitempty"`
}
type SourceRecord struct {
	Target Target `json:"target"`
	Label  string `json:"label"`
	Fields Patch  `json:"fields"`
}

func recordFields(node *yaml.Node, skip string) (Patch, error) {
	fields := Patch{}
	err := catalogyaml.Mapping(node, func(k string, n *yaml.Node) error {
		if k == skip {
			return nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return err
		}
		fields[k] = v
		return nil
	})
	return fields, err
}
func Records(data []byte) ([]SourceRecord, []string, error) {
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	result := []SourceRecord{}
	releases := []string{}
	err = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "releases"), func(k string, _ *yaml.Node) error { releases = append(releases, k); return nil })
	if err != nil {
		return nil, nil, err
	}
	err = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "sources"), func(source string, stanza *yaml.Node) error {
		fields, e := recordFields(stanza, "matches")
		if e != nil {
			return e
		}
		result = append(result, SourceRecord{Target: Target{Kind: "source", Source: source, Match: -1}, Label: "Source details", Fields: fields})
		matches := catalogyaml.Value(stanza, "matches")
		if matches == nil {
			return nil
		}
		if matches.Kind != yaml.SequenceNode {
			return fmt.Errorf("source matches must be a list")
		}
		for i, match := range matches.Content {
			fields, e := recordFields(match, "")
			if e != nil {
				return e
			}
			label := fmt.Sprintf("Record %d", i+1)
			if title := catalogyaml.Scalar(catalogyaml.Value(match, "title")); title != "" {
				label += " · " + title
			}
			if scope := catalogyaml.Scalar(catalogyaml.Value(match, "release")); scope != "" {
				label += " · " + scope
			}
			result = append(result, SourceRecord{Target: Target{Kind: "source", Source: source, Match: i}, Label: label, Fields: fields})
			nested := catalogyaml.Value(match, "releases")
			if nested != nil && nested.Kind == yaml.SequenceNode {
				for j, n := range nested.Content {
					if n.Kind != yaml.MappingNode {
						continue
					}
					values, e := recordFields(n, "")
					if e != nil {
						return e
					}
					index := j
					result = append(result, SourceRecord{Target: Target{Kind: "source", Source: source, Match: i, ReleaseIndex: &index}, Label: label + " / " + catalogyaml.Scalar(catalogyaml.Value(n, "release")), Fields: values})
				}
			}
		}
		return nil
	})
	return result, releases, err
}
func targetNode(root *yaml.Node, t Target) (*yaml.Node, error) {
	sources := catalogyaml.Value(root, "sources")
	if t.Kind == "manual" {
		if t.Scope != "" && catalogyaml.Value(catalogyaml.Value(root, "releases"), t.Scope) == nil {
			return nil, fmt.Errorf("choose an existing release")
		}
		var err error
		sources, err = catalogyaml.EnsureMapping(root, "sources")
		if err != nil {
			return nil, err
		}
		stanza, err := catalogyaml.EnsureMapping(sources, t.Source)
		if err != nil {
			return nil, err
		}
		matches := catalogyaml.Value(stanza, "matches")
		if matches == nil {
			matches = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			catalogyaml.Set(stanza, "matches", matches)
		}
		if matches.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("source matches must be a list")
		}
		for _, m := range matches.Content {
			if catalogyaml.Scalar(catalogyaml.Value(m, "release")) == t.Scope && catalogyaml.Value(m, "releases") == nil {
				return nil, fmt.Errorf("this source already has a record for that scope; choose its existing record")
			}
		}
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if t.Scope != "" {
			catalogyaml.Set(n, "release", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t.Scope})
		}
		matches.Content = append(matches.Content, n)
		return n, nil
	}
	if t.Kind != "source" || t.Source == "" {
		return nil, fmt.Errorf("choose a source record")
	}
	node := catalogyaml.Value(sources, t.Source)
	if node == nil {
		return nil, fmt.Errorf("source no longer exists")
	}
	if t.Match == -1 {
		if t.ReleaseIndex != nil {
			return nil, fmt.Errorf("choose a match first")
		}
		return node, nil
	}
	matches := catalogyaml.Value(node, "matches")
	if matches == nil || matches.Kind != yaml.SequenceNode || t.Match < 0 || t.Match >= len(matches.Content) {
		return nil, fmt.Errorf("source record no longer exists")
	}
	node = matches.Content[t.Match]
	if t.ReleaseIndex != nil {
		nested := catalogyaml.Value(node, "releases")
		i := *t.ReleaseIndex
		if nested == nil || nested.Kind != yaml.SequenceNode || i < 0 || i >= len(nested.Content) {
			return nil, fmt.Errorf("source release record no longer exists")
		}
		node = nested.Content[i]
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("source record must be a mapping")
	}
	return node, nil
}
func validValue(v any, depth int) error {
	if depth > 20 {
		return fmt.Errorf("value is too deeply nested")
	}
	switch n := v.(type) {
	case nil, bool, float64, int, int64, string:
		if s, ok := n.(string); ok && (len(s) > 1<<20 || strings.ContainsRune(s, 0)) {
			return fmt.Errorf("invalid text")
		}
	case []any:
		if len(n) > 2048 {
			return fmt.Errorf("too many values")
		}
		for _, x := range n {
			if err := validValue(x, depth+1); err != nil {
				return err
			}
		}
	case []string:
		for _, x := range n {
			if err := validValue(x, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if len(n) > 2048 {
			return fmt.Errorf("too many fields")
		}
		for k, x := range n {
			if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "\x00\r\n") {
				return fmt.Errorf("invalid field name")
			}
			if err := validValue(x, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported value")
	}
	return nil
}

func validRecordValue(key string, value any) error {
	if err := validValue(value, 0); err != nil {
		return err
	}
	if value == nil {
		return nil
	}
	switch key {
	case "descriptions":
		m, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("use an object of language codes and text")
		}
		for lang, v := range m {
			if strings.TrimSpace(lang) == "" {
				return fmt.Errorf("language code is required")
			}
			if _, ok := v.(string); !ok {
				return fmt.Errorf("description %s must be text", lang)
			}
		}
	case "title", "release", "description", "developer", "publisher", "release_date":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("use text")
		}
	case "genres", "themes", "collections", "franchises", "alternate_titles", "aliases":
		switch list := value.(type) {
		case []string:
		case []any:
			for _, v := range list {
				if _, ok := v.(string); !ok {
					return fmt.Errorf("use a list of text values")
				}
			}
		default:
			return fmt.Errorf("use a list of text values")
		}
	case "releases":
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("use a list of release names or records")
		}
		for _, v := range list {
			switch n := v.(type) {
			case string:
			case map[string]any:
				for k, x := range n {
					if err := validRecordValue(k, x); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("invalid release record")
			}
		}
	}
	return nil
}

func ApplyTarget(data []byte, t Target, patch Patch) ([]byte, error) {
	if t.Kind == "" || t.Kind == "library" {
		return Apply(data, patch)
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, err
	}
	node, err := targetNode(doc.Root(), t)
	if err != nil {
		return nil, err
	}
	for k, v := range patch {
		if strings.TrimSpace(k) == "" || len(k) > 200 || strings.ContainsAny(k, "\x00\r\n") || k == "matches" || k == "__proto__" || k == "constructor" || k == "prototype" {
			return nil, fmt.Errorf("invalid source field %q", k)
		}
		if err = validRecordValue(k, v); err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		if v == nil {
			Delete(node, k)
			continue
		}
		n := &yaml.Node{}
		if err = n.Encode(v); err != nil {
			return nil, err
		}
		if old := catalogyaml.Value(node, k); old != nil {
			n.HeadComment = old.HeadComment
			n.LineComment = old.LineComment
			n.FootComment = old.FootComment
		}
		if err = catalogyaml.Set(node, k, n); err != nil {
			return nil, err
		}
	}
	updated, err := doc.Marshal()
	if err != nil {
		return nil, err
	}
	if _, _, err = Records(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

type Edit struct {
	Target Target `json:"target"`
	Fields Patch  `json:"fields"`
}

func ApplyEdits(data []byte, edits []Edit) ([]byte, error) {
	if len(edits) > 128 {
		return nil, fmt.Errorf("too many edited records")
	}
	var err error
	for _, edit := range edits {
		data, err = ApplyTarget(data, edit.Target, edit.Fields)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}
func SaveEdits(root, platform, folder, revision string, edits []Edit) (Card, error) {
	registry, err := curation.Load(root)
	if err != nil {
		return Card{}, err
	}
	for _, edit := range edits {
		if edit.Target.Kind == "manual" && !registry.Contains(edit.Target.Source) {
			return Card{}, fmt.Errorf("create or select a named source first")
		}
	}
	path, err := GamePath(root, platform, folder)
	if err != nil {
		return Card{}, err
	}
	file := filepath.Join(path, "game.yaml")
	data, err := catalogfs.ReadFile(file)
	if err != nil {
		return Card{}, err
	}
	if revision == "" || Hash(data) != revision {
		return Card{}, fmt.Errorf("card changed since it was opened; reload before saving")
	}
	updated, err := ApplyEdits(data, edits)
	if err != nil {
		return Card{}, err
	}
	if len(edits) == 0 {
		return Inspect(data)
	}
	info, err := catalogfs.Stat(file)
	if err != nil {
		return Card{}, err
	}
	if err = atomicfile.WriteFile(file, updated, info.Mode().Perm()); err != nil {
		return Card{}, err
	}
	return Inspect(updated)
}
