package serveui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
)

var errOperationBusy = errors.New("operation is busy")

// One fetch writer and one export reader may share the process-owned catalog
// lock only when their physical catalog scopes are disjoint. Other web and CLI
// writers remain excluded until the last participant releases it.
type operationAccess struct {
	mu                              sync.Mutex
	unlock                          func()
	fetching, exporting             bool
	fetchPlatforms, exportPlatforms []string
}

func (s *server) exportScope(d exportDestination) ([]string, error) {
	if d.All {
		return nil, nil
	} // nil means the whole catalog
	selector, err := exporter.LoadPlatformSelector(s.root)
	if err != nil {
		return nil, err
	}
	_, folder, err := selector.Single(d.Platform, d.ROMRoot)
	if err != nil {
		return nil, err
	}
	return selector.Scope(folder), nil
}

func (s *server) exportAccess(d exportDestination) (func(), error) {
	platforms, err := s.exportScope(d)
	if err != nil {
		return nil, err
	}
	return s.acquireOperationAccess(false, platforms)
}

func (s *server) acquireOperationAccess(fetch bool, platforms []string) (func(), error) {
	a := &s.operationAccess
	a.mu.Lock()
	defer a.mu.Unlock()
	conflict := ""
	if fetch && a.fetching || !fetch && a.exporting {
		conflict = "another operation of this kind is running"
	} else if fetch && a.exporting || !fetch && a.fetching {
		other := a.fetchPlatforms
		if fetch {
			other = a.exportPlatforms
		}
		if len(platforms) == 0 || len(other) == 0 {
			conflict = "export of all platforms must wait for metadata fetching to finish"
		} else {
			for _, p := range platforms {
				for _, q := range other {
					if s.sameCatalogPlatform(p, q) {
						conflict = fmt.Sprintf("export and metadata fetching use the same platform (%s); wait for the active operation to finish", p)
					}
				}
			}
		}
	}
	if conflict != "" {
		return nil, fmt.Errorf("%w: %s", errOperationBusy, conflict)
	}
	if a.unlock == nil {
		if !s.editMu.TryLock() {
			return nil, fmt.Errorf("%w: another library operation is running; try again when it finishes", errOperationBusy)
		}
		unlock, err := catalogedit.Lock(s.root)
		if err != nil {
			s.editMu.Unlock()
			return nil, err
		}
		a.unlock = unlock
	}
	if fetch {
		a.fetching, a.fetchPlatforms = true, append([]string(nil), platforms...)
	} else {
		a.exporting, a.exportPlatforms = true, append([]string(nil), platforms...)
	}
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if fetch {
			a.fetching, a.fetchPlatforms = false, nil
		} else {
			a.exporting, a.exportPlatforms = false, nil
		}
		if !a.fetching && !a.exporting {
			a.unlock()
			a.unlock = nil
			s.editMu.Unlock()
		}
	}, nil
}

func (s *server) sameCatalogPlatform(left, right string) bool {
	path := func(p string) string {
		full := filepath.Join(s.root, "platforms", p)
		if resolved, err := catalogfs.EvalSymlinks(full); err == nil {
			return resolved
		}
		return full
	}
	return strings.EqualFold(path(left), path(right))
}
