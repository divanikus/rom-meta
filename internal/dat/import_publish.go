package dat

import (
	"fmt"
	"os"
	"path/filepath"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogfs"
)

// publishImportedPlatform cleans up incomplete writes, but retains a complete
// stage when activation fails. The rename function is injected for fault tests.
func publishImportedPlatform(platformPath string, prepare func(string) (int, error), rename func(string, string) error) (int, error) {
	if err := catalogchanges.Touch(platformPath); err != nil {
		return 0, err
	}
	parent := filepath.Dir(platformPath)
	if err := catalogfs.MkdirAll(parent, 0o755); err != nil {
		return 0, err
	}
	stagePath, err := os.MkdirTemp(parent, "."+filepath.Base(platformPath)+".import-")
	if err != nil {
		return 0, err
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = catalogfs.RemoveAll(stagePath)
		}
	}()
	writes, err := prepare(stagePath)
	if err != nil {
		return 0, err
	}
	// MkdirTemp is private while preparing, but a published platform must remain
	// readable to the catalog's normal readers (including a separate API user).
	mode := os.FileMode(0755)
	if info, err := catalogfs.Stat(platformPath); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if err := os.Chmod(stagePath, mode); err != nil {
		return 0, err
	}
	prepared = true
	active, err := replacePlatformDirectory(platformPath, stagePath, rename)
	if err != nil {
		if !active {
			return 0, fmt.Errorf("%w; prepared platform retained at %s", err, stagePath)
		}
		return writes, err
	}
	return writes, nil
}

// The boolean distinguishes a failed activation from cleanup failure after a
// successful swap, so recovery messages never point at an already moved stage.
func replacePlatformDirectory(platformPath, stagePath string, rename func(string, string) error) (bool, error) {
	if _, err := catalogfs.Stat(platformPath); err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		if err := rename(stagePath, platformPath); err != nil {
			return false, fmt.Errorf("activate prepared platform: %w", err)
		}
		return true, nil
	}
	backupPath, err := reserveSiblingPath(platformPath, ".backup-")
	if err != nil {
		return false, err
	}
	if err := rename(platformPath, backupPath); err != nil {
		return false, fmt.Errorf("move existing platform to backup: %w", err)
	}
	if err := rename(stagePath, platformPath); err != nil {
		if rollbackErr := rename(backupPath, platformPath); rollbackErr != nil {
			return false, fmt.Errorf("activate prepared platform: %w; restore original platform from %s: %w", err, backupPath, rollbackErr)
		}
		return false, fmt.Errorf("activate prepared platform: %w; original platform restored", err)
	}
	if err := catalogfs.RemoveAll(backupPath); err != nil {
		return true, fmt.Errorf("new platform is active but old backup could not be removed: %s: %w", backupPath, err)
	}
	return true, nil
}

func reserveSiblingPath(path, suffix string) (string, error) {
	reserved, err := os.MkdirTemp(filepath.Dir(path), "."+filepath.Base(path)+suffix)
	if err != nil {
		return "", err
	}
	if err := catalogfs.Remove(reserved); err != nil {
		return "", err
	}
	return reserved, nil
}
