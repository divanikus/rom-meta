// Package librarysettings edits library configuration without exposing credentials.
package librarysettings

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
)

type Field struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Help    string   `json:"help,omitempty"`
	Value   any      `json:"value"`
	Choices []string `json:"choices,omitempty"`
	Path    []string `json:"-"`
}
type Section struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Help     string  `json:"help"`
	File     string  `json:"file"`
	Revision string  `json:"revision"`
	Fields   []Field `json:"fields"`
	Status   string  `json:"status,omitempty"`
}
type Request struct {
	Section  string                     `json:"section"`
	Revision string                     `json:"revision"`
	Values   map[string]json.RawMessage `json:"values"`
}

func pathFor(root, name string) (string, error) {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(base, filepath.FromSlash(name))
	// Configuration is a small fixed set of ordinary files; never follow links
	// into unrelated roots or replace a symlink with a new document.
	for path := candidate; path != base; path = filepath.Dir(path) {
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", fmt.Errorf("cannot access library configuration")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("configuration symlinks cannot be edited in the browser")
		}
	}
	return candidate, nil
}
func read(root, name string) ([]byte, os.FileMode, error) {
	path, err := pathFor(root, name)
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, 0644, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("cannot read %s", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	return data, info.Mode().Perm(), nil
}
func parse(data []byte) (*catalogyaml.Document, error) {
	if strings.TrimSpace(string(data)) == "" {
		data = []byte("{}\n")
	}
	doc, err := catalogyaml.Parse(data)
	if err == io.EOF {
		return catalogyaml.Parse([]byte("{}\n"))
	}
	return doc, err
}
func value(root *yaml.Node, path []string) *yaml.Node {
	for _, key := range path {
		root = catalogyaml.Value(root, key)
	}
	return root
}
func yamlText(node *yaml.Node) string {
	if node == nil || node.Tag == "!!null" {
		return "{}\n"
	}
	data, _ := yaml.Marshal(node)
	return string(data)
}
func textField(id, label, kind, help string, path ...string) Field {
	return Field{ID: id, Label: label, Kind: kind, Help: help, Path: path}
}
func Load(root string) ([]Section, error) {
	if _, err := pathFor(root, "config/sources.yaml"); err != nil {
		return nil, err
	}
	profile, err := exporter.ExportProfile(root, "schema_version: 1\n")
	if err != nil {
		return nil, fmt.Errorf("cannot read source priorities; check config/sources.yaml")
	}
	defaults := map[string][]string{"metadata": profile.Selection.Description.Sources, "artwork": profile.Selection.Media["cover"].Sources, "video": profile.Selection.Media["video"].Sources}
	choices := []string{"launchbox", "screenscraper", "openvgdb", "igdb", "emumovies"}
	policy, _, err := read(root, "source-policies.yaml")
	if err != nil {
		return nil, err
	}
	if len(policy) > 0 {
		doc, e := parse(policy)
		if e != nil {
			return nil, fmt.Errorf("check source-policies.yaml YAML")
		}
		_ = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "sources"), func(k string, _ *yaml.Node) error { choices = append(choices, k); return nil })
	}
	result := []Section{
		{ID: "priorities", Title: "Source priorities", Help: "Choose which saved source wins when resolving descriptions, fields and media. Export profiles can override this order.", File: "config/sources.yaml"},
		{ID: "fetch", Title: "Enrichment", Help: "Choose which sources automatic enrichment uses, in order. These settings affect the next CLI or browser operation; saving does not start downloads.", File: "config/sources.yaml"},
		{ID: "fold", Title: "Folding & regions", Help: "Choose the preferred regional release and normalize regional names. Changes affect new import/fold plans, not already saved cards.", File: "config/catalog.yaml", Fields: []Field{
			textField("regions", "Preferred regions", "list", "First region has highest priority.", "fold", "region_priority"),
			textField("groups", "Region groups", "yaml", "Map a region to its alternate names, for example usa: [USA, US].", "fold", "region_groups"),
			textField("platforms", "Platform overrides", "yaml", "Map a platform to region_priority, for example segacd: {region_priority: [japan, usa]}.", "fold", "platforms")}},
		{ID: "platforms", Title: "Platform names & mappings", Help: "Local aliases and provider names/IDs extend the built-in platform registry.", File: "config/platforms.yaml", Fields: []Field{textField("document", "Local platform mappings", "yaml", "Example: Nintendo - Game Boy:\n  aliases: [My handheld]\n  sources:\n    screenscraper: {id: 9, name: Game Boy}")}},
		{ID: "legacy-aliases", Title: "Legacy platform aliases", Help: "Existing enrichment aliases in sources.yaml. Prefer Platform names & mappings for new aliases.", File: "config/sources.yaml", Fields: []Field{textField("aliases", "Aliases", "yaml", "Map an alias to a canonical platform name.", "platforms", "aliases")}},
		{ID: "policies", Title: "Media policies", Help: "Source attribution and media-sharing policy records. Editing a policy does not grant rights or change source ownership.", File: "source-policies.yaml", Fields: []Field{textField("document", "Source media policies", "yaml", "Keep schema_version, default and sources mappings. These records accompany source data.")}},
	}
	for _, category := range []string{"metadata", "artwork", "video"} {
		f := textField(category, strings.ToUpper(category[:1])+category[1:], "list", "Top source first. An empty list disables automatic source selection.", "priority", category)
		f.Value = defaults[category]
		f.Choices = choices
		result[0].Fields = append(result[0].Fields, f)
		f = textField(category, strings.ToUpper(category[:1])+category[1:], "list", "Only listed sources participate in automatic acquisition.", "fetch", category)
		f.Choices = []string{"launchbox", "openvgdb", "hasheous", "igdb", "screenscraper", "emumovies"}
		result[1].Fields = append(result[1].Fields, f)
	}
	result[1].Fields = append(result[1].Fields, textField("concurrency", "Parallel jobs", "number", "0 uses the CLI default. Provider-specific and account limits still apply.", "fetch", "concurrency"))
	sources, _, err := read(root, "config/sources.yaml")
	if err != nil {
		return nil, err
	}
	sourcesDoc, err := parse(sources)
	if err != nil {
		return nil, fmt.Errorf("check config/sources.yaml YAML")
	}
	var names []string
	if err = catalogyaml.Mapping(catalogyaml.Value(sourcesDoc.Root(), "sources"), func(k string, _ *yaml.Node) error { names = append(names, k); return nil }); err != nil {
		return nil, err
	}
	sort.Strings(names)
	config, configErr := store.LoadSources(root)
	for _, name := range names {
		section := Section{ID: "source:" + name, Title: "Connection · " + name, File: "config/sources.yaml", Help: "Connection settings for this source. Leaving a value empty uses its normal default. Credentials remain private."}
		for _, spec := range []struct {
			id, label, kind, help string
			path                  []string
		}{
			{"concurrency", "Parallel jobs", "number", "0 uses provider default.", []string{"concurrency"}},
			{"rate", "Requests per second", "number", "0 uses provider default.", []string{"api", "rate_limit_per_second"}},
			{"requests", "Parallel API requests", "number", "0 uses provider default.", []string{"api", "max_concurrent_requests"}},
			{"endpoint", "API / FTP address", "text", "", []string{"api", "base_url"}},
			{"snapshot", "Local snapshot path", "text", "Relative paths are resolved in the library.", []string{"snapshot", "path"}},
			{"download", "Snapshot download URL", "text", "", []string{"snapshot", "url"}},
			{"token", "OAuth token URL", "text", "", []string{"oauth", "token_url"}},
			{"grant", "OAuth grant type", "text", "", []string{"oauth", "grant_type"}},
			{"extractor", "Archive extractor", "text", "auto or the installed extractor path.", []string{"tools", "archive_extractor"}},
		} {
			path := append([]string{"sources", name}, spec.path...)
			// Existing options, plus provider-relevant optional limits.
			relevant := value(sourcesDoc.Root(), path) != nil || spec.id == "concurrency" ||
				((spec.id == "rate" || spec.id == "endpoint") && (name == "igdb" || name == "screenscraper" || name == "hasheous")) ||
				(spec.id == "requests" && name == "igdb")
			if relevant {
				section.Fields = append(section.Fields, textField(spec.id, spec.label, spec.kind, spec.help, path...))
			}
		}
		section.Status = "Credential status unavailable; check library source configuration."
		if configErr == nil {
			section.Status = "No credentials required by this connector."
			for _, source := range config.Sources {
				if source.Name == name {
					present, missing := store.CredentialStatus(config, source)
					if len(present)+len(missing) > 0 {
						section.Status = fmt.Sprintf("Credentials: %d of %d configured.", len(present), len(present)+len(missing))
						if len(missing) > 0 {
							section.Status += " Configure missing values in config/secrets.yaml or the referenced environment variables."
						}
						section.Status += " Stored values are never sent to the browser."
					}
				}
			}
		}
		result = append(result, section)
	}
	fold, err := store.LoadCatalogConfig(root)
	if err != nil {
		return nil, err
	}
	result[2].Fields[0].Value = fold.Fold.RegionPriority
	for i := range result {
		section := &result[i]
		data, _, err := read(root, section.File)
		if err != nil {
			return nil, err
		}
		section.Revision = catalogedit.Hash(data)
		doc, err := parse(data)
		if err != nil {
			return nil, fmt.Errorf("check %s YAML", section.File)
		}
		for j := range section.Fields {
			f := &section.Fields[j]
			node := value(doc.Root(), f.Path)
			if len(f.Path) == 0 {
				f.Value = string(data)
				continue
			}
			switch f.Kind {
			case "yaml":
				f.Value = yamlText(node)
			case "list":
				if node != nil {
					f.Value = catalogyaml.Strings(node)
				}
				if f.Value == nil {
					f.Value = []string{}
				}
				if list, ok := f.Value.([]string); ok && list == nil {
					f.Value = []string{}
				}
			case "number":
				f.Value = 0
				if node != nil {
					f.Value, _ = strconv.Atoi(catalogyaml.Scalar(node))
				}
			default:
				f.Value = catalogyaml.Scalar(node)
			}
		}
	}
	return result, nil
}
func stringList(n *yaml.Node) error {
	if n == nil || n.Kind != yaml.SequenceNode {
		return fmt.Errorf("expected a list")
	}
	seen := map[string]bool{}
	for _, v := range n.Content {
		if v.Kind != yaml.ScalarNode || (v.Tag != "" && v.Tag != "!!str") || strings.TrimSpace(v.Value) == "" || len(v.Value) > 200 || strings.ContainsAny(v.Value, "\r\n\t") {
			return fmt.Errorf("use nonempty source/region names")
		}
		if seen[v.Value] {
			return fmt.Errorf("duplicate name: %s", v.Value)
		}
		seen[v.Value] = true
	}
	return nil
}
func validateYAML(node *yaml.Node, depth int) error {
	if depth > 40 {
		return fmt.Errorf("YAML is too deeply nested")
	}
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("use explicit values rather than YAML aliases")
	}
	if node.Kind == yaml.MappingNode {
		keys := map[string]bool{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" || keys[key.Value] {
				return fmt.Errorf("use unique nonempty mapping keys")
			}
			keys[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAML(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func fieldNode(section string, f Field, raw json.RawMessage) (*yaml.Node, error) {
	switch f.Kind {
	case "list":
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil || v == nil {
			return nil, fmt.Errorf("expected a list")
		}
		if len(v) > 100 {
			return nil, fmt.Errorf("at most 100 entries")
		}
		n := catalogyaml.StringSequence(v)
		if err := stringList(n); err != nil {
			return nil, err
		}
		return n, nil
	case "number":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil || n < 0 || n > 1000 {
			return nil, fmt.Errorf("use a whole number between 0 and 1000")
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}, nil
	case "text":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil || len(v) > 4096 || strings.ContainsAny(v, "\x00\r\n") {
			return nil, fmt.Errorf("invalid text value")
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(v)}, nil
	case "yaml":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("expected YAML text")
		}
		doc, err := parse([]byte(v))
		if err != nil {
			return nil, fmt.Errorf("use one YAML mapping")
		}
		n := doc.Root()
		if err := validateYAML(n, 0); err != nil {
			return nil, err
		}
		n.Style = 0
		if section == "fold" {
			err = catalogyaml.Mapping(n, func(k string, item *yaml.Node) error {
				if f.ID == "groups" {
					return stringList(item)
				}
				item.Style = 0
				p := catalogyaml.Value(item, "region_priority")
				if p == nil {
					return fmt.Errorf("%s needs region_priority", k)
				}
				return stringList(p)
			})
		} else if section == "legacy-aliases" {
			err = catalogyaml.Mapping(n, func(_ string, v *yaml.Node) error {
				if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || v.Value == "" {
					return fmt.Errorf("alias targets must be names")
				}
				return nil
			})
		} else if section == "platforms" {
			err = catalogyaml.Mapping(n, func(_ string, p *yaml.Node) error {
				if p.Kind != yaml.MappingNode {
					return fmt.Errorf("each platform needs a mapping")
				}
				p.Style = 0
				if sources := catalogyaml.Value(p, "sources"); sources != nil {
					sources.Style = 0
				}
				if a := catalogyaml.Value(p, "aliases"); a != nil {
					if e := stringList(a); e != nil {
						return e
					}
				}
				return catalogyaml.Mapping(catalogyaml.Value(p, "sources"), func(_ string, ref *yaml.Node) error {
					if ref.Kind == yaml.MappingNode {
						ref.Style = yaml.FlowStyle
						return nil
					}
					if ref.Kind != yaml.ScalarNode {
						return fmt.Errorf("provider reference must be a name or {id, name}")
					}
					return nil
				})
			})
		}
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	return nil, fmt.Errorf("unknown setting type")
}

// Save requires the caller to hold the shared catalog writer lock.
func Save(root string, input Request) (Section, error) {
	sections, err := Load(root)
	if err != nil {
		return Section{}, err
	}
	var section *Section
	for i := range sections {
		if sections[i].ID == input.Section {
			section = &sections[i]
			break
		}
	}
	if section == nil {
		return Section{}, fmt.Errorf("unknown settings section")
	}
	data, mode, err := read(root, section.File)
	if err != nil {
		return Section{}, err
	}
	if input.Revision != catalogedit.Hash(data) {
		return Section{}, fmt.Errorf("settings changed; reload this section before saving")
	}
	if len(input.Values) == 0 {
		return *section, nil
	}
	doc, err := parse(data)
	if err != nil {
		return Section{}, err
	}
	for id, raw := range input.Values {
		var field *Field
		for i := range section.Fields {
			if section.Fields[i].ID == id {
				field = &section.Fields[i]
				break
			}
		}
		if field == nil {
			return Section{}, fmt.Errorf("unknown setting %q", id)
		}
		node, e := fieldNode(section.ID, *field, raw)
		if e != nil {
			return Section{}, fmt.Errorf("%s: %w", field.Label, e)
		}
		if len(field.Path) == 0 {
			*doc.Root() = *node
			continue
		}
		parent := doc.Root()
		parent.Style = 0
		for _, key := range field.Path[:len(field.Path)-1] {
			parent, e = catalogyaml.EnsureMapping(parent, key)
			if e != nil {
				return Section{}, e
			}
			parent.Style = 0
		}
		parent.Style = 0
		old := catalogyaml.Value(parent, field.Path[len(field.Path)-1])
		if old != nil {
			node.HeadComment = old.HeadComment
			node.LineComment = old.LineComment
			node.FootComment = old.FootComment
		}
		if e = catalogyaml.Set(parent, field.Path[len(field.Path)-1], node); e != nil {
			return Section{}, e
		}
	}
	updated, err := doc.Marshal()
	if err != nil {
		return Section{}, err
	}
	if section.ID == "platforms" {
		if len(doc.Root().Content) == 0 {
			updated = []byte("# Local platform overrides\n")
		} else if _, err = platformnames.ParseRegistryYAML(string(updated)); err != nil {
			return Section{}, err
		}
	}
	// Compare once more immediately before the atomic replacement.
	current, _, err := read(root, section.File)
	if err != nil {
		return Section{}, err
	}
	if catalogedit.Hash(current) != input.Revision {
		return Section{}, fmt.Errorf("settings changed; reload before saving")
	}
	path, err := pathFor(root, section.File)
	if err != nil {
		return Section{}, err
	}
	if err = atomicfile.WriteFile(path, updated, mode); err != nil {
		return Section{}, fmt.Errorf("cannot save %s; check service-user write permissions", section.File)
	}
	section.Revision = catalogedit.Hash(updated)
	return *section, nil
}
