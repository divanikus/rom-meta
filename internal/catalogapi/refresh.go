package catalogapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/exporter"
)

// Signature checks are small. Normal RomMeta writes publish the revision only
// after command-level index maintenance; profile edits also invalidate the view.
func (s *LookupServer) signature() (string, error) {
	h := sha256.New()
	marker := filepath.Join(s.catalog.Root, catalogchanges.RevisionFile)
	info, err := os.Lstat(marker)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 256 {
			return "", fmt.Errorf("invalid catalog revision")
		}
		data, err := os.ReadFile(marker)
		if err != nil {
			return "", err
		}
		h.Write(data)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for _, name := range []string{s.options.Export.ProfilePath, filepath.Join(s.catalog.Root, "config", "sources.yaml")} {
		if name == "" {
			continue
		}
		info, err := os.Stat(name)
		if os.IsNotExist(err) {
			fmt.Fprintf(h, "|%s:missing", name)
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "|%s:%d:%d", name, info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Called under mu before catalog metadata requests. Catalog editing and native
// import are sequential workflows; existing drift checks still reject overlap.
func (s *LookupServer) refresh() error {
	version, err := s.signature()
	if err != nil {
		return err
	}
	if version == s.version {
		return nil
	}
	catalog, err := exporter.NewLookupCatalog(s.catalog.Root, s.options.Export.ProfilePath)
	if err != nil {
		return err
	}
	after, err := s.signature()
	if err != nil {
		return err
	}
	if version != after {
		return fmt.Errorf("catalog changed during refresh; retry after the write completes")
	}
	catalog.CacheDir = s.options.LookupCacheDir
	s.catalog = catalog
	s.health.Store(catalog.Platforms)
	s.resolver = nil
	s.exportResolver = nil
	s.exportKey = ""
	s.cache = map[string]boundFile{}
	s.assets = map[string]boundFile{}
	s.version = version
	return nil
}
