package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
)

// inputStamp excludes access times and platform-specific stat caches.
type inputStamp struct {
	Size, MtimeNS int64
	Mode          os.FileMode
}

// InputGuard keeps size/time/type observations in memory. Same-stat byte edits
// are deliberately outside apply validation; full inspection belongs to scan.
type InputGuard struct{ files map[string]inputStamp }

func CaptureInputGuard(root string, input ExportInput) (*InputGuard, error) {
	g := &InputGuard{files: map[string]inputStamp{}}
	var add func(string) error
	add = func(relative string) error {
		if !ValidInputPath(relative) {
			return fmt.Errorf("invalid observed input path")
		}
		name := filepath.Join(root, filepath.FromSlash(relative))
		if _, ok := g.files[name]; ok {
			return nil
		}
		info, err := catalogfs.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("ROM input changed: %s", relative)
		}
		g.files[name] = inputStamp{info.Size(), info.ModTime().UnixNano(), info.Mode()}
		return nil
	}
	var observe func(ExportInput) error
	observe = func(in ExportInput) error {
		if in.Kind == "unsupported" {
			return nil
		}
		if err := add(in.ROM); err != nil {
			return err
		}
		base := filepath.Dir(in.ROM)
		if in.Catalog != nil && in.Catalog.Directory || in.Disc != nil && in.Disc.Directory {
			base = in.ROM
		}
		addMember := func(name string) error {
			relative := filepath.ToSlash(filepath.Join(base, filepath.FromSlash(name)))
			if err := add(relative); err != nil {
				return err
			}
			// Directory stamps detect additions/removals without another directory walk.
			for dir := filepath.Dir(relative); dir != "." && dir != filepath.Dir(base); dir = filepath.Dir(dir) {
				if err := add(filepath.ToSlash(dir)); err != nil {
					return err
				}
				if dir == base {
					break
				}
			}
			return nil
		}
		if in.Catalog != nil {
			for _, f := range in.Catalog.Files {
				if err := addMember(f.Name); err != nil {
					return err
				}
			}
		}
		if in.Disc != nil {
			for _, f := range in.Disc.Files {
				if err := addMember(f.Name); err != nil {
					return err
				}
			}
		}
		if in.Kind == "playlist" {
			if err := add(filepath.ToSlash(filepath.Join(in.ROM, filepath.Base(in.ROM)))); err != nil {
				return err
			}
			for _, part := range in.Parts {
				if err := observe(part); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := observe(input); err != nil {
		return nil, err
	}
	return g, nil
}
func (g *InputGuard) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if g == nil {
		return fmt.Errorf("ROM observation guard is missing")
	}
	for name, before := range g.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		now, err := catalogfs.Lstat(name)
		if err != nil {
			return err
		}
		if now.Mode() != before.Mode || now.Size() != before.Size || now.ModTime().UnixNano() != before.MtimeNS {
			return fmt.Errorf("ROM changed since planning: %s", name)
		}
	}
	return nil
}
