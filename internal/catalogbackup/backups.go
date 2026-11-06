// Package catalogbackup lists and removes retained RomMeta originals.
// Callers hold the shared catalog writer lock during inspection and deletion.
package catalogbackup

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	ID       string    `json:"id"`
	Platform string    `json:"platform"`
	Kind     string    `json:"kind"`
	Modified time.Time `json:"modified"`
	Bytes    int64     `json:"bytes"`
	Files    int       `json:"files"`
	Revision string    `json:"revision"`
	Error    string    `json:"error,omitempty"`
}

var foldName = regexp.MustCompile(`^(.+)\.fold-backup-[0-9]+$`)
var arcadeName = regexp.MustCompile(`^arcade-repair-[0-9]+$`)

func identity(id string) (string, string, error) {
	if id == "" || strings.ContainsAny(id, "/\\:\x00") || filepath.Base(id) != id {
		return "", "", fmt.Errorf("invalid backup name")
	}
	if m := foldName.FindStringSubmatch(id); m != nil {
		return m[1], "Platform", nil
	}
	if arcadeName.MatchString(id) {
		return "Arcade", "MAME", nil
	}
	return "", "", fmt.Errorf("not a RomMeta backup")
}
func parent(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "backups")
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("backup folder must be a real directory")
	}
	return path, nil
}
func inspect(dir, id string) (Entry, error) {
	platform, kind, err := identity(id)
	entry := Entry{ID: id, Platform: platform, Kind: kind}
	if err != nil {
		return entry, err
	}
	path := filepath.Join(dir, id)
	info, err := os.Lstat(path)
	if err != nil {
		return entry, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return entry, fmt.Errorf("backup must be a real directory")
	}
	entry.Modified = info.ModTime()
	digest := sha256.New()
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("backup contains a link or special file")
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		fmt.Fprintf(digest, "%q %d %d %d\n", rel, info.Size(), info.ModTime().UnixNano(), info.Mode())
		if !info.IsDir() {
			entry.Bytes += info.Size()
			entry.Files++
		}
		return nil
	})
	if err != nil {
		return entry, err
	}
	entry.Revision = fmt.Sprintf("%x", digest.Sum(nil))
	return entry, nil
}
func List(root string) ([]Entry, error) {
	result := []Entry{}
	dir, err := parent(root)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range entries {
		if _, _, err := identity(item.Name()); err != nil {
			continue
		}
		entry, err := inspect(dir, item.Name())
		if err != nil {
			entry.Error = err.Error()
			entry.Revision = ""
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Modified.After(result[j].Modified) })
	return result, nil
}
func Inspect(root, id string) (Entry, error) {
	if _, _, err := identity(id); err != nil {
		return Entry{}, err
	}
	dir, err := parent(root)
	if err != nil {
		return Entry{}, err
	}
	return inspect(dir, id)
}
func Delete(root, id, revision string) error {
	entry, err := Inspect(root, id)
	if err != nil {
		return err
	}
	if revision == "" || revision != entry.Revision {
		return fmt.Errorf("backup changed; refresh the list before deleting")
	}
	dir, err := parent(root)
	if err != nil {
		return err
	}
	// Names, parent, every descendant and the reviewed revision are checked above.
	return os.RemoveAll(filepath.Join(dir, id))
}
