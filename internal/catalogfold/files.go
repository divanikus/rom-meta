package catalogfold

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
)

// FileChange describes a move into the surviving card. Relocated media keep
// their basename so provider/type recognition continues to work in serve.
type FileChange struct {
	FromFolder string
	Path       string
	Target     string
	Action     string // move, deduplicate, relocate
}

type foldFile struct {
	path string
	rel  string
	dir  bool
	size int64
}

func planGroupFiles(root string, group Group) ([]FileChange, string, error) {
	platform := filepath.Join(root, "platforms", group.Platform)
	refs := append([]GameRef{group.Keep}, group.Merge...)
	occupied := map[string]foldFile{}
	hashes := map[string]string{}
	seenReleases := map[string]any{}
	var changes []FileChange
	key := func(path string) string { return strings.ToLower(filepath.ToSlash(path)) }
	for index, ref := range refs {
		gamePath := filepath.Join(platform, ref.Folder)
		if err := ensureGamePath(platform, gamePath); err != nil {
			return nil, "", err
		}
		if err := validateReviewedContent(gamePath, seenReleases); err != nil {
			return nil, fmt.Sprintf("%s: %s", ref.Folder, err), nil
		}
		err := catalogfs.WalkDir(gamePath, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == gamePath {
				return nil
			}
			rel, err := filepath.Rel(gamePath, path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return filePlanHold("unsupported file type: " + ref.Folder + "/" + rel)
			}
			// Only the two root catalog files are rendered separately. Nested
			// files with the same basename are ordinary extras and must survive.
			if key(rel) == "game.yaml" || key(rel) == "fingerprints.yaml" {
				return nil
			}
			item := foldFile{path: path, rel: rel, dir: info.IsDir(), size: info.Size()}
			existing, exists := occupied[key(rel)]
			if index == 0 {
				if exists {
					return filePlanHold("case-insensitive path collision: " + rel)
				}
				occupied[key(rel)] = item
				return nil
			}
			if item.dir {
				if exists && !existing.dir {
					return filePlanHold("file/directory collision: " + rel)
				}
				if !exists {
					occupied[key(rel)] = item
				}
				return nil
			}
			change := FileChange{FromFolder: ref.Folder, Path: rel, Target: rel, Action: "move"}
			if exists {
				if existing.dir {
					return filePlanHold("file/directory collision: " + rel)
				}
				same, err := identicalFoldFiles(item, existing, hashes)
				if err != nil {
					return err
				}
				if same {
					change.Action, change.Target = "deduplicate", existing.rel
				} else {
					if !strings.HasPrefix(key(rel), "media/") {
						return filePlanHold("different non-media files share path: " + rel)
					}
					change.Action = "relocate"
					for suffix := 1; ; suffix++ {
						folder := ref.Folder
						if suffix > 1 {
							folder = fmt.Sprintf("%s~%d", folder, suffix)
						}
						change.Target = filepath.Join("media", "_fold", folder, rel[len("media")+1:])
						other, taken := occupied[key(change.Target)]
						if !taken {
							break
						}
						if !other.dir {
							same, err := identicalFoldFiles(item, other, hashes)
							if err != nil {
								return err
							}
							if same {
								change.Action, change.Target = "deduplicate", other.rel
								break
							}
						}
					}
				}
			}
			for parent := filepath.Dir(change.Target); parent != "."; parent = filepath.Dir(parent) {
				if other, ok := occupied[key(parent)]; ok && !other.dir {
					return filePlanHold("file/directory collision: " + parent)
				}
				occupied[key(parent)] = foldFile{rel: parent, dir: true}
			}
			if change.Action != "deduplicate" {
				item.rel = change.Target
				occupied[key(change.Target)] = item
			}
			changes = append(changes, change)
			return nil
		})
		if hold, ok := err.(filePlanHold); ok {
			return nil, string(hold), nil
		} else if err != nil {
			return nil, "", err
		}
	}
	return changes, "", nil
}

type filePlanHold string

func (e filePlanHold) Error() string { return string(e) }

func identicalFoldFiles(a, b foldFile, hashes map[string]string) (bool, error) {
	if a.size != b.size {
		return false, nil
	}
	for _, file := range []foldFile{a, b} {
		if _, ok := hashes[file.path]; ok {
			continue
		}
		f, err := catalogfs.Open(file.path)
		if err != nil {
			return false, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return false, err
		}
		if closeErr != nil {
			return false, closeErr
		}
		hashes[file.path] = fmt.Sprintf("%x", h.Sum(nil))
	}
	return hashes[a.path] == hashes[b.path], nil
}

func applyFileChanges(platform, keepPath string, changes []FileChange) error {
	for _, change := range changes {
		if change.Action == "deduplicate" {
			continue
		}
		source := filepath.Join(platform, change.FromFolder, change.Path)
		target := filepath.Join(keepPath, change.Target)
		if err := catalogfs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// Link or copy exclusively: never replace an unexpected target. All
		// work happens inside the platform transaction, before publication.
		if err := catalogfs.Link(source, target); err == nil {
			continue
		}
		if err := copyFoldFileExclusive(source, target); err != nil {
			return err
		}
	}
	return nil
}

func copyFoldFileExclusive(source, target string) error {
	in, err := catalogfs.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := catalogfs.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
