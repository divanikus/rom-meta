package catalogfs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bodgit/torrentzip"
	"rommeta/internal/catalogchanges"
)

var writers sync.Map

func writerLock(game string) (func(), error) {
	value, _ := writers.LoadOrStore(game, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	if err := os.MkdirAll(game, 0755); err != nil {
		mu.Unlock()
		return nil, err
	}
	lock := filepath.Join(game, ".rommeta-write-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("game writer busy (check stale lock before removing %s): %w", lock, err)
	}
	return func() { os.Remove(lock); mu.Unlock() }, nil
}

// PutFiles batches paths from ordinary staging files into their affected packs.
// TorrentZip normalizes the affected packs, including unchanged members. Callers keep multi-game
// operations inside their existing staged platform/backup transaction.
func PutFiles(game string, files map[string]string, remove []string) error {
	changes := map[string]func() (io.ReadCloser, error){}
	for member, file := range files {
		file := file
		changes[member] = func() (io.ReadCloser, error) { return os.Open(file) }
	}
	return update(game, changes, remove)
}
func update(game string, changes map[string]func() (io.ReadCloser, error), remove []string) error {
	if err := catalogchanges.Touch(game); err != nil {
		return err
	}
	for n := range changes {
		if _, e := PackName(n); e != nil {
			return e
		}
	}
	for _, n := range remove {
		if _, e := PackName(n); e != nil {
			return e
		}
	}
	unlock, err := writerLock(game)
	if err != nil {
		return err
	}
	defer unlock()
	idx, err := inventory(game)
	if err != nil {
		return err
	}
	next := map[string]bool{}
	for n := range idx.members {
		next[n] = true
	}
	for _, n := range remove {
		delete(next, n)
	}
	for n := range changes {
		next[n] = true
	}
	folded := map[string]string{}
	dirs := map[string]bool{}
	for n := range next {
		for q := n; q != "."; q = pathpkg.Dir(q) {
			key := strings.ToLower(q)
			if old, ok := folded[key]; ok && old != q {
				return fmt.Errorf("case-colliding write paths: %s / %s", old, q)
			}
			folded[key] = q
			if q != n {
				dirs[q] = true
			}
		}
	}
	for n := range next {
		if dirs[n] {
			return fmt.Errorf("write file/directory collision: %s", n)
		}
	}
	groups := map[string]map[string]func() (io.ReadCloser, error){}
	add := func(n string, open func() (io.ReadCloser, error)) error {
		pack, e := PackName(n)
		if e != nil {
			return e
		}
		if _, e = os.Lstat(filepath.Join(game, filepath.FromSlash(n))); e == nil {
			return fmt.Errorf("loose file in packed write destination: %s", n)
		} else if !os.IsNotExist(e) {
			return e
		}
		if groups[pack] == nil {
			groups[pack] = map[string]func() (io.ReadCloser, error){}
		}
		groups[pack][n] = open
		return nil
	}
	for n, open := range changes {
		if err = add(n, open); err != nil {
			return err
		}
	}
	for _, n := range remove {
		if err = add(n, nil); err != nil {
			return err
		}
	}
	packs := make([]string, 0, len(groups))
	for pack := range groups {
		packs = append(packs, pack)
	}
	sort.Strings(packs)
	for _, pack := range packs {
		if err = writePack(game, pack, idx.packs[pack], groups[pack]); err != nil {
			return err
		}
	}
	indexes.Delete(game)
	return nil
}
func writePack(game, pack string, before os.FileInfo, changes map[string]func() (io.ReadCloser, error)) error {
	name := filepath.Join(game, pack)
	var old *zip.ReadCloser
	var err error
	if before != nil {
		old, err = zip.OpenReader(name)
		if err != nil {
			return err
		}
		defer old.Close()
	}
	tmp, err := os.CreateTemp(game, "rommeta-pack-*.tmp")
	if err != nil {
		return err
	}
	defer func() { tmp.Close(); os.Remove(tmp.Name()) }()
	z := torrentzip.NewStreamWriter(tmp)
	existing := map[string]*zip.File{}
	if old != nil {
		for _, f := range old.File {
			if !f.FileInfo().IsDir() {
				f.Name = zipName(f)
				existing[f.Name] = f
			}
		}
	}
	names := map[string]bool{}
	for n := range existing {
		names[n] = true
	}
	for n := range changes {
		names[n] = true
	}
	order := make([]string, 0, len(names))
	for n := range names {
		order = append(order, n)
	}
	sort.Slice(order, func(i, j int) bool { return torrentzip.CompareNames(order[i], order[j]) < 0 })
	count := 0
	for _, n := range order {
		open, changed := changes[n]
		if changed && open == nil {
			continue
		}
		if !changed {
			if err = z.Copy(existing[n]); err != nil {
				z.Close()
				return err
			}
			count++
			continue
		}
		r, e := open()
		if e != nil {
			z.Close()
			return e
		}
		h := &torrentzip.FileHeader{Name: n}
		w, e := z.CreateHeader(h)
		if e == nil {
			var size int64
			size, e = io.Copy(w, io.LimitReader(r, MaxMemberSize+1))
			if e == nil && size > MaxMemberSize {
				e = fmt.Errorf("oversized member: %s", n)
			}
		}
		ce := r.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			z.Close()
			return e
		}
		count++
	}
	if err = z.Close(); err != nil {
		return err
	}
	if err = packAttributes(tmp, before); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	check, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return err
	}
	err = check.Close()
	if err != nil {
		return err
	}
	now, e := os.Stat(name)
	if before != nil {
		if e != nil || !samePhysical(before, now) {
			return fmt.Errorf("ZIP changed during write: %s", name)
		}
	} else if e == nil || !os.IsNotExist(e) {
		return fmt.Errorf("ZIP destination appeared during write: %s", name)
	}
	// Close the old archive before replacement on Windows/SMB.
	if old != nil {
		if err = old.Close(); err != nil {
			return err
		}
		old = nil
	}
	if count == 0 {
		if before == nil {
			return nil
		}
		return os.Remove(name)
	}
	return os.Rename(tmp.Name(), name)
}
func WriteFile(name string, data []byte, mode os.FileMode) error {
	if err := catalogchanges.Touch(name); err != nil {
		return err
	}
	l, err := locate(name)
	if err != nil {
		return err
	}
	if !l.packed || l.member == "" || isPack(l.member) {
		return os.WriteFile(name, data, mode)
	}
	return update(l.game, map[string]func() (io.ReadCloser, error){l.member: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}, nil)
}

func Rename(from, to string) error {
	if err := catalogchanges.Touch(from, to); err != nil {
		return err
	}
	if filepath.Clean(from) == filepath.Clean(to) {
		_, e := Stat(from)
		return e
	}
	a, err := locate(from)
	if err != nil {
		return err
	}
	b, err := locate(to)
	if err != nil {
		return err
	}
	if (!a.packed || a.member == "" || isPack(a.member)) && (!b.packed || b.member == "" || isPack(b.member)) {
		return os.Rename(from, to)
	}
	info, err := Stat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("logical directory rename requires a staged platform operation: %s", from)
	}
	// Downloaders stage ordinary *.tmp files beside their logical targets.
	_, virtual := info.(*fileInfo)
	if b.packed && b.member != "" {
		if virtual {
			in, e := Open(from)
			if e != nil {
				return e
			}
			tmp, e := os.CreateTemp(b.game, "rommeta-move-*.tmp")
			if e != nil {
				in.Close()
				return e
			}
			defer os.Remove(tmp.Name())
			_, e = io.Copy(tmp, in)
			ce := in.Close()
			if e == nil {
				e = ce
			}
			ce = tmp.Close()
			if e == nil {
				e = ce
			}
			if e != nil {
				return e
			}
			if a.game == b.game {
				return update(b.game, map[string]func() (io.ReadCloser, error){b.member: func() (io.ReadCloser, error) { return os.Open(tmp.Name()) }}, []string{a.member})
			}
			if e = PutFiles(b.game, map[string]string{b.member: tmp.Name()}, nil); e != nil {
				return e
			}
			return Remove(from)
		}
		if err = PutFiles(b.game, map[string]string{b.member: from}, nil); err != nil {
			return err
		}
		return Remove(from)
	}
	in, err := Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(to), "rommeta-copy-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, err = io.Copy(out, in)
	ce := out.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(out.Name(), to); err != nil {
		return err
	}
	return Remove(from)
}
func Remove(name string) error {
	if err := catalogchanges.Touch(name); err != nil {
		return err
	}
	info, err := Lstat(name)
	if err != nil {
		return err
	}
	if v, ok := info.(*fileInfo); ok {
		if v.IsDir() {
			entries, e := ReadDir(name)
			if e != nil {
				return e
			}
			if len(entries) > 0 {
				return fmt.Errorf("directory not empty: %s", name)
			}
			return nil
		}
		l, e := locate(name)
		if e != nil {
			return e
		}
		return update(l.game, nil, []string{l.member})
	}
	return os.Remove(name)
}
func RemoveAll(name string) error {
	if err := catalogchanges.Touch(name); err != nil {
		return err
	}
	l, err := locate(name)
	if err != nil {
		return err
	}
	if !l.packed || l.member == "" {
		indexes.Delete(l.game)
		return os.RemoveAll(name)
	}
	idx, err := inventory(l.game)
	if err != nil {
		return err
	}
	var remove []string
	for n := range idx.members {
		if n == l.member || strings.HasPrefix(n, l.member+"/") {
			remove = append(remove, n)
		}
	}
	if len(remove) > 0 {
		if err = update(l.game, nil, remove); err != nil {
			return err
		}
	}
	return os.RemoveAll(name)
}
func MkdirAll(name string, mode os.FileMode) error {
	if err := catalogchanges.Touch(name); err != nil {
		return err
	}
	return os.MkdirAll(name, mode)
}
func Link(from, to string) error {
	// SQLite caches are updated transactionally in place; stage a separate file.
	if filepath.Base(from) == "_index.sqlite" || filepath.Base(to) == "_index.sqlite" {
		return &os.LinkError{Op: "link", Old: from, New: to, Err: fs.ErrInvalid}
	}
	if err := catalogchanges.Touch(to); err != nil {
		return err
	}
	a, e := Packed(from)
	if e != nil {
		return e
	}
	b, e := Packed(to)
	if e != nil {
		return e
	}
	if a || b {
		return &os.LinkError{Op: "link", Old: from, New: to, Err: fs.ErrInvalid}
	}
	return os.Link(from, to)
}
func Chtimes(name string, atime, mtime time.Time) error {
	if err := catalogchanges.Touch(name); err != nil {
		return err
	}
	info, e := Lstat(name)
	if e != nil {
		return e
	}
	if _, ok := info.(*fileInfo); ok {
		return nil
	}
	return os.Chtimes(name, atime, mtime)
}
