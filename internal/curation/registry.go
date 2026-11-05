// Package curation names user-authored sources without changing source priority.
package curation

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogyaml"
	"sort"
	"strings"
	"unicode/utf8"
)

type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Registry struct {
	Revision string   `json:"revision"`
	Sources  []Source `json:"sources"`
}

func hash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func IsManual(id string) bool { return strings.HasPrefix(id, "manual.") }
func read(root string) (string, []byte, os.FileMode, error) {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, 0, err
	}
	path := filepath.Join(base, "config", "curation.yaml")
	for _, p := range []string{filepath.Dir(path), path} {
		info, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", nil, 0, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, 0, fmt.Errorf("curation configuration cannot be a symlink")
		}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return path, nil, 0644, nil
	}
	if err != nil {
		return "", nil, 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, 0, err
	}
	return path, data, info.Mode().Perm(), nil
}
func document(data []byte) (*catalogyaml.Document, error) {
	if strings.TrimSpace(string(data)) == "" {
		data = []byte("schema_version: 1\nsources: {}\n")
	}
	return catalogyaml.Parse(data)
}
func Load(root string) (Registry, error) {
	_, data, _, err := read(root)
	if err != nil {
		return Registry{}, err
	}
	doc, err := document(data)
	if err != nil {
		return Registry{}, fmt.Errorf("check config/curation.yaml: %w", err)
	}
	r := Registry{Revision: hash(data), Sources: []Source{}}
	err = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "sources"), func(id string, n *yaml.Node) error {
		name := catalogyaml.Scalar(catalogyaml.Value(n, "name"))
		if !IsManual(id) || strings.TrimSpace(name) == "" {
			return fmt.Errorf("invalid named source %q", id)
		}
		r.Sources = append(r.Sources, Source{id, name})
		return nil
	})
	sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i].Name < r.Sources[j].Name })
	return r, err
}
func (r Registry) Contains(id string) bool {
	for _, s := range r.Sources {
		if s.ID == id {
			return true
		}
	}
	return false
}
func (r Registry) Labels() map[string]string {
	m := map[string]string{}
	for _, s := range r.Sources {
		m[s.ID] = s.Name
	}
	return m
}

// Save requires the catalog writer lock. It creates/renames a source, never
// copies card fields or enables that source for export.
func Save(root, revision, id, name string) (Registry, Source, error) {
	r, err := Load(root)
	if err != nil {
		return r, Source{}, err
	}
	if revision != r.Revision {
		return r, Source{}, fmt.Errorf("source names changed; reload before saving")
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 || strings.ContainsAny(name, "\r\n\t\x00") {
		return r, Source{}, fmt.Errorf("enter a source name of 1–120 characters")
	}
	for _, s := range r.Sources {
		if s.ID != id && strings.EqualFold(s.Name, name) {
			return r, Source{}, fmt.Errorf("a source with that name already exists")
		}
	}
	if id == "" {
		var nonce [12]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return r, Source{}, err
		}
		id = "manual." + hex.EncodeToString(nonce[:])
	} else if !r.Contains(id) {
		return r, Source{}, fmt.Errorf("unknown named source")
	}
	path, data, mode, err := read(root)
	if err != nil {
		return r, Source{}, err
	}
	if hash(data) != revision {
		return r, Source{}, fmt.Errorf("source names changed; reload before saving")
	}
	doc, err := document(data)
	if err != nil {
		return r, Source{}, err
	}
	sources, err := catalogyaml.EnsureMapping(doc.Root(), "sources")
	if err != nil {
		return r, Source{}, err
	}
	source, err := catalogyaml.EnsureMapping(sources, id)
	if err != nil {
		return r, Source{}, err
	}
	if err = catalogyaml.Set(source, "name", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}); err != nil {
		return r, Source{}, err
	}
	updated, err := doc.Marshal()
	if err != nil {
		return r, Source{}, err
	}
	if err = atomicfile.WriteFile(path, updated, mode); err != nil {
		return r, Source{}, err
	}
	r, err = Load(root)
	return r, Source{id, name}, err
}
