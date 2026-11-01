package frontend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source describes generated metadata and asset proofs without copying or
// downloading media during preview. Direct apply reads only needed assets.
type Source struct {
	Files map[string]SourceFile
	Check func() error
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
	var result []packageEntry
	if p.source == nil {
		entries, err := os.ReadDir(filepath.Join(p.Package, filepath.FromSlash(relative)))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			result = append(result, packageEntry{e.Name(), e.IsDir()})
		}
		return result, nil
	}
	prefix := relative + "/"
	seen := map[string]bool{}
	for name := range p.source.Files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		tail := strings.TrimPrefix(name, prefix)
		first, _, directory := strings.Cut(tail, "/")
		if first != "" {
			seen[first] = seen[first] || directory
		}
	}
	if len(seen) == 0 {
		return nil, os.ErrNotExist
	}
	for name, directory := range seen {
		result = append(result, packageEntry{name, directory})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].name < result[j].name })
	return result, nil
}
func (p *Plan) packageProof(name string) (fingerprint, error) {
	if p.source == nil {
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
