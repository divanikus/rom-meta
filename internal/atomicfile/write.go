// Package atomicfile writes catalog files through a flushed sibling temporary
// file and one rename. It never truncates an existing file or its hardlinks.
package atomicfile

import (
	"os"
	"path/filepath"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogfs"
)

// TempPattern avoids Samba assigning Hidden to a dot-prefixed temporary file
// and retaining that attribute after it replaces a visible catalog file.
const TempPattern = "rommeta-*.tmp"

// WriteFile replaces path only after the complete contents have been written,
// flushed and closed. Rename guarantees depend on the underlying filesystem.
func WriteFile(path string, contents []byte, mode os.FileMode) error {
	if err := catalogchanges.Touch(path); err != nil {
		return err
	}
	if packed, err := catalogfs.Packed(path); err != nil {
		return err
	} else if packed {
		return catalogfs.WriteFile(path, contents, mode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), TempPattern)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
