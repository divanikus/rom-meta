package exporter

import (
	"io/fs"
	"os"
	"path/filepath"

	"rommeta/internal/catalogfs"
)

type sourceMediaFile struct{ path, parent string }

// One inventory per selection operation, shared by its games. Only names are
// retained: selected files still receive fresh path/type/size checks.
// Resolver entry points clear it after every request, including failed requests.
type mediaInventory struct{ files map[string][]sourceMediaFile }

func (m *mediaInventory) clear() { m.files = nil }

func (g *game) sourceMedia() ([]sourceMediaFile, error) {
	if g.context != nil {
		if err := g.context.Err(); err != nil {
			return nil, err
		}
	}
	if g.media == nil {
		g.media = &mediaInventory{}
	}
	if files, ok := g.media.files[g.path]; ok {
		return files, nil
	}
	root := filepath.Join(g.path, "media")
	var files []sourceMediaFile
	err := catalogfs.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if g.context != nil {
			if err := g.context.Err(); err != nil {
				return err
			}
		}
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, sourceMediaFile{path, filepath.Dir(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if g.media.files == nil {
		g.media.files = map[string][]sourceMediaFile{}
	}
	g.media.files[g.path] = files
	return files, nil
}
