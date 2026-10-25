// Package catalogchanges batches derived-index maintenance for one CLI command.
package catalogchanges

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const RevisionFile = "_catalog-revision"

type Batch struct {
	mu      sync.Mutex
	roots   map[string]map[string]bool
	known   map[string]bool
	refresh func(string, string) error
}

var active struct {
	sync.Mutex
	batch *Batch
}

// Begin installs command-scoped tracking; low-level library callers remain explicit.
func Begin(refresh func(string, string) error) *Batch {
	b := &Batch{roots: map[string]map[string]bool{}, known: map[string]bool{}, refresh: refresh}
	active.Lock()
	active.batch = b
	active.Unlock()
	return b
}

// Touch records writes before they occur, including partial operations that fail.
func Touch(paths ...string) error {
	active.Lock()
	b := active.batch
	active.Unlock()
	if b == nil {
		return nil
	}
	for _, name := range paths {
		abs, err := filepath.Abs(name)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(filepath.Clean(abs))
		root, platform := "", ""
		isConfig := false
		if i := strings.LastIndex(slash, "/platforms/"); i >= 0 {
			root = slash[:i]
			platform = strings.Split(slash[i+len("/platforms/"):], "/")[0]
			if strings.HasPrefix(platform, ".") || strings.HasPrefix(platform, "_") || strings.HasSuffix(platform, ".old") || strings.Contains(platform, ".old.") || strings.HasSuffix(platform, ".converting") {
				platform = ""
			}
		} else if strings.HasSuffix(slash, "/platforms") {
			root = strings.TrimSuffix(slash, "/platforms")
		} else if i := strings.LastIndex(slash, "/config/"); i >= 0 {
			tail := slash[i+len("/config/"):]
			if tail == "sources.yaml" || tail == "source-policies.yaml" || tail == "catalog.yaml" {
				root = slash[:i]
				isConfig = true
			}
		}
		if root == "" {
			continue
		}
		if strings.Contains(filepath.ToSlash(root), "/.rommeta/") {
			continue
		}
		b.mu.Lock()
		root = filepath.FromSlash(root)
		if !b.known[root] {
			_, configErr := os.Stat(filepath.Join(root, "config", "sources.yaml"))
			_, headerErr := os.Stat(filepath.Join(root, "platforms", platform, "_platform.yaml"))
			if isConfig || configErr == nil || platform != "" && headerErr == nil || filepath.Base(name) == "_platform.yaml" {
				b.known[root] = true
			}
		}
		if b.roots[root] == nil {
			b.roots[root] = map[string]bool{}
		}
		if platform != "" {
			b.roots[root][platform] = true
		}
		b.mu.Unlock()
	}
	return nil
}

// Finish runs once after payload work, even after partial failure. Cache/revision
// errors are reported without trying to undo already committed catalog payloads.
func (b *Batch) Finish() error {
	active.Lock()
	if active.batch == b {
		active.batch = nil
	}
	active.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	var first error
	roots := make([]string, 0, len(b.roots))
	for root := range b.roots {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		if !b.known[root] {
			continue
		}
		platforms := make([]string, 0, len(b.roots[root]))
		for p := range b.roots[root] {
			platforms = append(platforms, p)
		}
		sort.Strings(platforms)
		for _, p := range platforms {
			if _, err := os.Stat(filepath.Join(root, "platforms", p)); os.IsNotExist(err) {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, "platforms", p, "_platform.yaml")); os.IsNotExist(err) {
				continue
			}
			if err := b.refresh(root, p); err != nil && first == nil {
				first = fmt.Errorf("catalog written, index refresh %s: %w", p, err)
			}
		}
		if err := publishRevision(root); err != nil && first == nil {
			first = fmt.Errorf("catalog written, revision publication: %w", err)
		}
	}
	b.roots = map[string]map[string]bool{}
	return first
}
func publishRevision(root string) error {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, "rommeta-revision-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0644); err == nil {
		_, err = f.WriteString(hex.EncodeToString(token[:]) + "\n")
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(root, RevisionFile))
}
