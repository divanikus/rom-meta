package catalogedit

import (
	"fmt"
	"os"
	"path/filepath"
)

// Lock coordinates CLI and web writers. A leftover lock is reported, never stolen.
func Lock(root string) (func(), error) {
	dir := filepath.Join(root, ".rommeta")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("catalog runtime directory cannot be a symlink")
	}
	path := filepath.Join(dir, "catalog-write.lock")
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, fmt.Errorf("catalog writer is busy; wait for the other operation (lock: %s): %w", path, err)
	}
	return func() { _ = os.Remove(path) }, nil
}
