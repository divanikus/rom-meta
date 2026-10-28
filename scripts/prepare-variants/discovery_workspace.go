package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"rommeta/internal/atomicfile"
)

const discoveryMarker = ".variant-discovery.json"

type discoverySession struct {
	SchemaVersion   int               `json:"schema_version"`
	Catalog         string            `json:"catalog"`
	Sources         string            `json:"sources"`
	SourcesConfig   discoveryOptions  `json:"source_config"`
	DecisionsSHA256 string            `json:"decisions_sha256,omitempty"`
	Generated       map[string]string `json:"generated,omitempty"`
}

func strictJSON(b []byte, v any) error {
	if err := checkJSONKeys(b); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func jsonBytes(v any) ([]byte, error) {
	b, e := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), e
}
func workFile(root, name string) (string, error) {
	p := filepath.Join(root, name)
	info, err := os.Lstat(p)
	if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return "", fmt.Errorf("workspace file is not regular: %s", name)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return p, nil
}
func validateWorkspace(root string, s *discoverySession) error {
	w, err := resolved(root)
	if err != nil {
		return err
	}
	for _, r := range []string{s.Catalog, s.Sources} {
		p, e := resolved(r)
		if e != nil {
			return e
		}
		if within(w, p) || within(p, w) {
			return fmt.Errorf("discovery workspace overlaps input")
		}
	}
	return nil
}
func loadSession(root string) (*discoverySession, error) {
	p, err := workFile(root, discoveryMarker)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	s := &discoverySession{}
	if err = strictJSON(b, s); err != nil {
		return nil, err
	}
	if s.SchemaVersion != 1 || s.Catalog == "" || s.Sources == "" {
		return nil, fmt.Errorf("invalid discovery workspace")
	}
	if err = validateWorkspace(root, s); err != nil {
		return nil, err
	}
	return s, nil
}
func validateGenerated(root string, s *discoverySession) error {
	for name, hash := range s.Generated {
		if name != "links.json" && name != "report.json" {
			return fmt.Errorf("unexpected generated workspace file")
		}
		p, err := workFile(root, name)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if digest(b) != hash {
			return fmt.Errorf("generated %s was modified; preserve it and use a new workspace", name)
		}
	}
	return nil
}
func loadDecisions(root string) (map[string]reviewDecision, []byte, error) {
	p, err := workFile(root, "decisions.json")
	if err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		b = []byte("{}\n")
		err = nil
	}
	if err != nil {
		return nil, nil, err
	}
	out := map[string]reviewDecision{}
	if err = strictJSON(b, &out); err != nil {
		return nil, nil, err
	}
	if out == nil {
		return nil, nil, fmt.Errorf("decisions must be an object")
	}
	for id, d := range out {
		if !validHash(id, 32) {
			return nil, nil, fmt.Errorf("invalid decision fingerprint key")
		}
		if d.Exclude {
			if d.Platform != "" || d.Game != "" || d.NewGame != nil {
				return nil, nil, fmt.Errorf("excluded decision also selects a game")
			}
			continue
		}
		if !safeName(d.Platform) || !safeName(d.Game) {
			return nil, nil, fmt.Errorf("decision needs safe platform and game")
		}
	}
	return out, b, nil
}
func saveDecisions(root string, ds map[string]reviewDecision) error {
	p, err := workFile(root, "decisions.json")
	if err != nil {
		return err
	}
	b, err := jsonBytes(ds)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(p, b, 0600)
}
func saveDiscovery(root string, s *discoverySession, m *manifest, r *discoveryReport) error {
	_, b, err := loadDecisions(root)
	if err != nil {
		return err
	}
	s.DecisionsSHA256 = digest(b)
	s.Generated = map[string]string{}
	for _, item := range []struct {
		name  string
		value any
	}{{"links.json", m}, {"report.json", r}} {
		p, err := workFile(root, item.name)
		if err != nil {
			return err
		}
		b, err := jsonBytes(item.value)
		if err != nil {
			return err
		}
		if err = atomicfile.WriteFile(p, b, 0600); err != nil {
			return err
		}
		s.Generated[item.name] = digest(b)
	}
	b, err = jsonBytes(s)
	if err != nil {
		return err
	}
	p, err := workFile(root, discoveryMarker)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(p, b, 0600)
}
func lockWorkspace(root string) (func(), error) {
	p := filepath.Join(root, ".discovery-lock")
	if err := os.Mkdir(p, 0700); err != nil {
		return nil, fmt.Errorf("workspace is locked; if no helper is running remove .discovery-lock: %w", err)
	}
	return func() { _ = os.Remove(p) }, nil
}
func initializeDiscovery(root string, o options, d discoveryOptions) (*discoverySession, error) {
	if s, err := loadSession(root); err == nil {
		if o.Catalog != "" {
			p, e := resolved(o.Catalog)
			if e != nil || p != s.Catalog {
				return nil, fmt.Errorf("workspace catalog differs; use a new workspace")
			}
		}
		if o.Sources != "" {
			p, e := resolved(o.Sources)
			if e != nil || p != s.Sources {
				return nil, fmt.Errorf("workspace sources differ; use a new workspace")
			}
		}
		if len(d.DAT) > 0 || len(d.TEN) > 0 || d.GA != "" || d.RHA != "" || d.HackHash != "" {
			if !sameSourceSelection(d, s.SourcesConfig) {
				return nil, fmt.Errorf("source selection differs; use a new workspace")
			}
		}
		return s, validateGenerated(root, s)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if o.Catalog == "" || o.Sources == "" {
		return nil, fmt.Errorf("new discovery needs --catalog and --sources; translation DATs are found automatically")
	}
	cat, err := resolved(o.Catalog)
	if err != nil {
		return nil, err
	}
	src, err := resolved(o.Sources)
	if err != nil {
		return nil, err
	}
	s := &discoverySession{SchemaVersion: 1, Catalog: cat, Sources: src, SourcesConfig: d}
	if err = validateWorkspace(root, s); err != nil {
		return nil, err
	}
	xs, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(xs) > 0 {
		return nil, fmt.Errorf("new discovery workspace must be empty")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	b, _ := jsonBytes(s)
	p, _ := workFile(root, discoveryMarker)
	if err = atomicfile.WriteFile(p, b, 0600); err != nil {
		return nil, err
	}
	return s, nil
}
func discoveryCLI(root string, o options, d discoveryOptions, review bool, in io.Reader, out io.Writer) error {
	if o.ROMRoot != "" || o.Links != "" || o.Out != "" || o.Report != "" || o.Apply {
		return fmt.Errorf("discovery/review writes analysis only; stage separately with --workspace")
	}
	s, err := initializeDiscovery(root, o, d)
	if err != nil {
		return err
	}
	unlock, err := lockWorkspace(root)
	if err != nil {
		return err
	}
	defer unlock()
	if err = validateGenerated(root, s); err != nil {
		return err
	}
	ds, _, err := loadDecisions(root)
	if err != nil {
		return err
	}
	o.Catalog = s.Catalog
	o.Sources = s.Sources
	m, r, c, err := discover(o, s.SourcesConfig, ds)
	if err != nil {
		return err
	}
	if review {
		if err = reviewRows(root, o, r, c, ds, in, out); err != nil {
			return err
		}
		m, r, _, err = discover(o, s.SourcesConfig, ds)
		if err != nil {
			return err
		}
	}
	if err = saveDecisions(root, ds); err != nil {
		return err
	}
	if err = saveDiscovery(root, s, m, r); err != nil {
		return err
	}
	fmt.Fprintf(out, "Discovery: %d ready, %d review, %d already owned, %d unsupported, %d excluded\nWorkspace: %s\n", r.Counts["ready"], r.Counts["review"], r.Counts["already-owned"], r.Counts["unsupported"], r.Counts["excluded"], root)
	if r.Counts["skipped-platform"] > 0 {
		fmt.Fprintf(out, "Skipped %d DATs whose platforms are absent from the catalog; details: report.json\n", r.Counts["skipped-platform"])
	}
	return nil
}
func workspaceOptions(root string, o options) (options, error) {
	if o.Catalog != "" || o.Sources != "" || o.Links != "" || o.ROMRoot != "" {
		return o, fmt.Errorf("--workspace supplies catalog, sources and links")
	}
	s, err := loadSession(root)
	if err != nil {
		return o, err
	}
	if err = validateGenerated(root, s); err != nil {
		return o, err
	}
	_, b, err := loadDecisions(root)
	if err != nil {
		return o, err
	}
	if digest(b) != s.DecisionsSHA256 {
		return o, fmt.Errorf("decisions changed; rerun --discover or --review")
	}
	if len(s.Generated) != 2 {
		return o, fmt.Errorf("discovery has not completed")
	}
	o.Catalog = s.Catalog
	o.Sources = s.Sources
	o.Links = filepath.Join(root, "links.json")
	raw, err := os.ReadFile(o.Links)
	if err != nil {
		return o, err
	}
	var m manifest
	if err = strictJSON(raw, &m); err != nil {
		return o, err
	}
	if len(m.Entries) == 0 {
		return o, fmt.Errorf("no ready outputs; inspect report.json and run --review")
	}
	if o.Out != "" {
		w, e := resolved(root)
		if e != nil {
			return o, e
		}
		p, e := resolved(o.Out)
		if e != nil {
			return o, e
		}
		if within(w, p) || within(p, w) {
			return o, fmt.Errorf("stage must be outside discovery workspace")
		}
	}
	return o, nil
}

func sameSourceSelection(a, b discoveryOptions) bool {
	normalize := func(d discoveryOptions) discoveryOptions {
		files := append(append([]string{}, d.DAT...), d.TEN...)
		d.DAT = []string{}
		d.TEN = nil
		for _, file := range files {
			if file == "." {
				d.DAT = []string{}
				return d
			}
			d.DAT = appendUnique(d.DAT, file)
		}
		sort.Strings(d.DAT)
		return d
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}
