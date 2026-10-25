package arcaderepair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
)

// Apply publishes exactly a prepared projection after rechecking every input
// and output byte. Original directories and the old binding are retained on
// the same volume; failures attempt rollback and report the retained backup.
func Apply(root, reviewFile string) (backup string, err error) {
	return applyReview(root, reviewFile, catalogfs.Rename)
}

func applyReview(root, reviewFile string, rename func(string, string) error) (backup string, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	reviewFile, err = filepath.Abs(reviewFile)
	if err != nil {
		return "", err
	}
	data, err := catalogfs.ReadFile(reviewFile)
	if err != nil {
		return "", err
	}
	var r Report
	if err = json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	if r.Version != 1 || !strings.EqualFold(filepath.Clean(r.DataRoot), root) || !component(r.Snapshot) || len(r.Conflicts) > 0 {
		return "", fmt.Errorf("review root/version/snapshot mismatch or unresolved conflicts")
	}
	if len(r.Groups) == 0 {
		return "", nil
	}
	review := filepath.Dir(reviewFile)
	if within(root, review) || within(review, root) {
		return "", fmt.Errorf("review must be outside the catalog")
	}
	sources, targets := map[string]bool{}, map[string]bool{}
	for _, g := range r.Groups {
		if !component(g.Target) || len(g.Sources) == 0 || targets[strings.ToLower(g.Target)] || len(g.Before) != len(g.Sources) {
			return "", fmt.Errorf("invalid/duplicate repair group")
		}
		targets[strings.ToLower(g.Target)] = true
		for _, from := range g.Sources {
			if !component(from) || sources[strings.ToLower(from)] || len(g.Before[from]) == 0 {
				return "", fmt.Errorf("invalid/duplicate source card")
			}
			sources[strings.ToLower(from)] = true
		}
		if len(g.After) == 0 {
			return "", fmt.Errorf("empty output manifest")
		}
	}
	bindingRel := filepath.Join("bindings", "romsets", "mame", r.Snapshot, "catalog.yaml")
	preflight := func() error {
		for _, p := range []string{filepath.Join(root, "platforms", "Arcade"), filepath.Join(root, bindingRel)} {
			if e := noLinks(root, p); e != nil {
				return e
			}
		}
		if e := noLinks(review, filepath.Join(review, "staged", bindingRel)); e != nil {
			return e
		}
		if e := checkFile(filepath.Join(root, bindingRel), r.BindingBefore); e != nil {
			return e
		}
		if e := checkFile(filepath.Join(review, "staged", bindingRel), r.BindingAfter); e != nil {
			return e
		}
		for _, g := range r.Groups {
			if e := noLinks(review, filepath.Join(review, "staged", "platforms", "Arcade", g.Target)); e != nil {
				return e
			}
			for _, from := range g.Sources {
				if e := checkTree(filepath.Join(root, "platforms", "Arcade", from), g.Before[from]); e != nil {
					return e
				}
			}
			if e := checkTree(filepath.Join(review, "staged", "platforms", "Arcade", g.Target), g.After); e != nil {
				return e
			}
			if !sources[strings.ToLower(g.Target)] {
				if _, e := catalogfs.Lstat(filepath.Join(root, "platforms", "Arcade", g.Target)); !os.IsNotExist(e) {
					return fmt.Errorf("destination appeared: %s", g.Target)
				}
			}
		}
		return nil
	}
	if err = preflight(); err != nil {
		return "", err
	}
	parent := filepath.Join(root, "backups")
	if err = catalogfs.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	if info, e := catalogfs.Lstat(parent); e != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("unsafe backup directory")
	}
	backup, err = os.MkdirTemp(parent, "arcade-repair-")
	if err != nil {
		return "", err
	}
	if err = writeNew(filepath.Join(backup, "review.json"), data); err != nil {
		return backup, err
	}
	if err = copyFile(filepath.Join(root, bindingRel), filepath.Join(backup, "original", bindingRel)); err != nil {
		return backup, err
	}
	for _, g := range r.Groups {
		src := filepath.Join(review, "staged", "platforms", "Arcade", g.Target)
		dst := filepath.Join(backup, "prepared", "platforms", "Arcade", g.Target)
		for _, rel := range keys(g.After) {
			if !filepath.IsLocal(rel) {
				return backup, fmt.Errorf("unsafe output file")
			}
			if err = copyFile(filepath.Join(src, rel), filepath.Join(dst, rel)); err != nil {
				return backup, err
			}
		}
		if err = checkTree(dst, g.After); err != nil {
			return backup, err
		}
	}
	next, err := catalogfs.ReadFile(filepath.Join(review, "staged", bindingRel))
	if err != nil {
		return backup, err
	}
	if err = writeNew(filepath.Join(backup, "prepared", bindingRel), next); err != nil {
		return backup, err
	}
	if err = checkFile(filepath.Join(backup, "prepared", bindingRel), r.BindingAfter); err != nil {
		return backup, err
	}
	// Copying media can take time; reject intervening fetches before any move.
	if err = preflight(); err != nil {
		return backup, err
	}
	var moved, published []string
	bindingWritten := false
	defer func() {
		if err == nil {
			return
		}
		originalErr := err
		var rollback []string
		if bindingWritten {
			old, e := catalogfs.ReadFile(filepath.Join(backup, "original", bindingRel))
			if e == nil {
				e = atomicfile.WriteFile(filepath.Join(root, bindingRel), old, 0644)
			}
			if e != nil {
				rollback = append(rollback, e.Error())
			}
		}
		for i := len(published) - 1; i >= 0; i-- {
			dst := filepath.Join(backup, "failed-output", published[i])
			e := catalogfs.MkdirAll(filepath.Dir(dst), 0755)
			if e == nil {
				e = rename(filepath.Join(root, "platforms", "Arcade", published[i]), dst)
			}
			if e != nil {
				rollback = append(rollback, e.Error())
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			if e := rename(filepath.Join(backup, "original", "platforms", "Arcade", moved[i]), filepath.Join(root, "platforms", "Arcade", moved[i])); e != nil {
				rollback = append(rollback, e.Error())
			}
		}
		if len(rollback) > 0 {
			err = fmt.Errorf("%w; rollback failed: %s; retained backup: %s", originalErr, strings.Join(rollback, "; "), backup)
		} else {
			err = fmt.Errorf("%w; rolled back; retained backup: %s", originalErr, backup)
		}
	}()
	for _, g := range r.Groups {
		for _, from := range g.Sources {
			dst := filepath.Join(backup, "original", "platforms", "Arcade", from)
			if err = catalogfs.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return backup, err
			}
			if err = rename(filepath.Join(root, "platforms", "Arcade", from), dst); err != nil {
				return backup, err
			}
			moved = append(moved, from)
		}
	}
	for _, g := range r.Groups {
		if err = rename(filepath.Join(backup, "prepared", "platforms", "Arcade", g.Target), filepath.Join(root, "platforms", "Arcade", g.Target)); err != nil {
			return backup, err
		}
		published = append(published, g.Target)
	}
	// A write error may occur after replacement, so restore on any error here.
	bindingWritten = true
	if err = atomicfile.WriteFile(filepath.Join(root, bindingRel), next, 0644); err != nil {
		return backup, err
	}
	if err = writeNew(filepath.Join(backup, "APPLIED.txt"), []byte("Repair applied. Original cards and catalog binding are retained in original/. Stop all writers before any manual restoration.\n")); err != nil {
		return backup, err
	}
	return backup, nil
}

func noLinks(root, path string) error {
	if !within(root, path) {
		return fmt.Errorf("path escapes root")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	parts := []string{root}
	if rel != "." {
		parts = append(parts, strings.Split(rel, string(filepath.Separator))...)
	}
	current := ""
	for i, part := range parts {
		if i == 0 {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, e := catalogfs.Lstat(current)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not supported: %s", current)
		}
	}
	return nil
}

func checkFile(path, want string) error {
	have, e := fileHash(path)
	if e != nil {
		return e
	}
	if have != want {
		return fmt.Errorf("file changed since review: %s", path)
	}
	return nil
}
func checkTree(path string, want map[string]string) error {
	have, e := treeHashes(path)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(have, want) {
		return fmt.Errorf("card changed since review: %s", path)
	}
	return nil
}
