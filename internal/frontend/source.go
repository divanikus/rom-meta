package frontend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"rommeta/internal/frontenddata"
	"sort"
	"strings"
)

// Source describes generated metadata and asset proofs without copying or
// downloading media during preview. Direct apply reads only needed assets.
type Source struct {
	Library *frontenddata.Library
	Files   map[string]SourceFile
	Check   func() error
}
type SourceFile struct {
	Open   func() (io.ReadCloser, error)
	Data   []byte
	SHA256 string
	Size   int64
}
type packageEntry struct {
	name      string
	directory bool
}

func (e packageEntry) Name() string { return e.name }
func (e packageEntry) IsDir() bool  { return e.directory }
func (p *Plan) packageEntries(relative string) ([]packageEntry, error) {
	if _, err := safeJoin(p.Package, relative); err != nil {
		return nil, err
	}
	if p.packageDirs == nil {
		p.packageDirs = map[string][]packageEntry{}
		if p.source != nil {
			tree := map[string]map[string]bool{}
			add := func(name string) {
				parts := strings.Split(name, "/")
				for i, part := range parts {
					dir := strings.Join(parts[:i], "/")
					if tree[dir] == nil {
						tree[dir] = map[string]bool{}
					}
					tree[dir][part] = i < len(parts)-1
				}
			}
			for name := range p.source.Files {
				add(name)
			}
			if p.source.Library != nil {
				for name := range p.source.Library.Native {
					add(name)
				}
			}
			for dir, children := range tree {
				entries := []packageEntry{}
				for name, directory := range children {
					entries = append(entries, packageEntry{name, directory})
				}
				sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
				p.packageDirs[dir] = entries
			}
		}
	}
	if entries, ok := p.packageDirs[relative]; ok {
		return entries, nil
	}
	if p.source != nil {
		return nil, os.ErrNotExist
	}
	entries, err := os.ReadDir(filepath.Join(p.Package, filepath.FromSlash(relative)))
	if os.IsNotExist(err) {
		p.packageDirs[relative] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]packageEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, packageEntry{e.Name(), e.IsDir()})
	}
	p.packageDirs[relative] = result
	return result, nil
}

func (p *Plan) packageMedia(relative, game string) ([]packageEntry, error) {
	if p.sourceMedia == nil {
		p.sourceMedia = map[string]map[string][]packageEntry{}
	}
	if groups, ok := p.sourceMedia[relative]; ok {
		return groups[strings.ToLower(game)], nil
	}
	entries, err := p.packageEntries(relative)
	if err != nil {
		return nil, err
	}
	groups := map[string][]packageEntry{}
	for _, e := range entries {
		if !e.IsDir() && len(e.name) > 37 && e.name[36] == '-' {
			key := strings.ToLower(e.name[:36])
			groups[key] = append(groups[key], e)
		}
	}
	p.sourceMedia[relative] = groups
	return groups[strings.ToLower(game)], nil
}

func (p *Plan) packageProof(name string) (fingerprint, error) {
	if p.source == nil {
		if previous, ok := p.guards[name]; ok {
			return previous, nil
		}
		return digest(name)
	}
	file, err := p.sourceFile(name)
	if err != nil {
		return fingerprint{}, err
	}
	hash := file.SHA256
	if file.Data != nil {
		hash = hashBytes(file.Data)
	}
	if hash == "" {
		hash = "deferred:" + name
	}
	return fingerprint{true, hash}, nil
}

func (p *Plan) sourceFile(name string) (SourceFile, error) {
	relative, err := filepath.Rel(p.Package, name)
	if err != nil {
		return SourceFile{}, err
	}
	relative = filepath.ToSlash(relative)
	if _, err = safeJoin(p.Package, relative); err != nil {
		return SourceFile{}, err
	}
	file, ok := p.source.Files[relative]
	if !ok {
		return SourceFile{}, fmt.Errorf("missing generated asset: %s", relative)
	}
	return file, nil
}

func prospectiveRoot(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	current := abs
	var suffix []string
	for {
		if _, err = os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func (p *Plan) incoming(relative, root string) (*document, error) {
	if p.source != nil && p.source.Library != nil {
		entries, ok := p.source.Library.Native[relative]
		if !ok {
			return nil, fmt.Errorf("missing native input: %s", relative)
		}
		doc := &document{root: root}
		for _, e := range entries {
			doc.children = append(doc.children, element{name: e.Name, values: e.Fields})
		}
		return doc, nil
	}
	body, err := p.read(p.Package, relative, false)
	if err != nil {
		return nil, err
	}
	if root == "gameList" {
		_, list, _, _, err := gamelist(body)
		return list, err
	}
	return parse(body, root)
}
