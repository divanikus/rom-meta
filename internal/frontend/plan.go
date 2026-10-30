package frontend

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type Options struct {
	Target, PackageRoot, Destination, MediaRoot string
	Source                                      *Source
}
type Change struct {
	Platform  string   `json:"platform"`
	Title     string   `json:"title"`
	ID        string   `json:"id"`
	Action    string   `json:"action"`
	Fields    []string `json:"filled_fields,omitempty"`
	Versions  int      `json:"added_versions"`
	Media     int      `json:"added_media"`
	Playlists int      `json:"added_playlists"`
}
type Plan struct {
	Progress   func(stage, name string, current, total int) `json:"-"`
	Target     string                                       `json:"target"`
	MediaRoot  string                                       `json:"media_root,omitempty"`
	source     *Source
	Package    string   `json:"package,omitempty"`
	Root       string   `json:"destination"`
	Changes    []Change `json:"games"`
	Conflicts  []string `json:"conflicts"`
	Warnings   []string `json:"warnings"`
	Files      []string `json:"changed_files"`
	files      []fileChange
	guards     map[string]fingerprint
	external   map[string]fingerprint
	identities map[string]string
}

// Build uses the same additive plan and apply contract for each native adapter.
func Build(options Options) (*Plan, error) {
	if options.Target != "es-de" && options.Target != "launchbox" {
		return nil, fmt.Errorf("unsupported frontend target: %s", options.Target)
	}
	root, err := canonicalRoot(options.Destination)
	if err != nil {
		return nil, err
	}
	var pkg string
	if options.Source == nil {
		pkg, err = canonicalRoot(options.PackageRoot)
	} else {
		pkg, err = filepath.Abs(options.PackageRoot)
	}
	if err != nil {
		return nil, err
	}
	if inside(root, pkg) || inside(pkg, root) {
		return nil, fmt.Errorf("package and destination directories must not overlap")
	}
	p := &Plan{Target: options.Target, Root: root, Package: pkg, source: options.Source, guards: map[string]fingerprint{}, external: map[string]fingerprint{}, identities: map[string]string{}, Changes: []Change{}, Conflicts: []string{}, Warnings: []string{}, Files: []string{}}
	if options.MediaRoot != "" && p.Target != "es-de" {
		return nil, fmt.Errorf("--media-root is only supported for ES-DE")
	}
	if options.MediaRoot != "" {
		p.MediaRoot, err = prospectiveRoot(options.MediaRoot)
		if err != nil {
			return nil, err
		}
	}
	body, err := p.read(pkg, "manifest.json", false)
	if err != nil {
		return nil, err
	}
	var header struct {
		Schema int    `json:"schema_version"`
		Target string `json:"target"`
	}
	if err = json.Unmarshal(body, &header); err != nil {
		return nil, err
	}
	if header.Schema != 1 || header.Target != p.Target {
		return nil, fmt.Errorf("expected a version-1 %s export package", p.Target)
	}
	if p.Target == "launchbox" {
		err = buildLaunchBox(p)
	} else {
		err = buildESDE(p, body)
	}
	if err != nil {
		return nil, err
	}
	if err = p.protectSourceTrees(body); err != nil {
		return nil, err
	}
	rank := func(name string) int {
		if name == statePath {
			return 2
		}
		if strings.HasPrefix(name, "Data/") || strings.HasPrefix(name, "gamelists/") {
			return 1
		}
		return 0
	}
	sort.SliceStable(p.files, func(i, j int) bool { return rank(p.files[i].path) < rank(p.files[j].path) })
	return p, nil
}

func (p *Plan) Generated() bool { return p.source != nil }
func (p *Plan) protectSourceTrees(body []byte) error {
	type inputTrees struct {
		DataRoot string            `json:"data_root"`
		ROMRoot  string            `json:"rom_root"`
		Systems  []json.RawMessage `json:"systems"`
	}
	var visit func([]byte) error
	visit = func(body []byte) error {
		var trees inputTrees
		if err := json.Unmarshal(body, &trees); err != nil {
			return err
		}
		for _, name := range []string{trees.DataRoot, trees.ROMRoot} {
			if name == "" {
				continue
			}
			root, err := filepath.Abs(name)
			if err != nil {
				return err
			}
			for _, file := range p.files {
				target := filepath.Join(file.root, filepath.FromSlash(file.path))
				if inside(root, target) {
					return fmt.Errorf("destination output overlaps a source catalog/ROM tree: %s", target)
				}
			}
		}
		for _, system := range trees.Systems {
			if err := visit(system); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(body)
}
