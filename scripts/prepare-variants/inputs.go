package main

import (
	"archive/zip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/zipnames"
)

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func resolved(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	// Resolve the existing ancestor for new output/report paths.
	probe := abs
	var tail []string
	for {
		real, e := filepath.EvalSymlinks(probe)
		if e == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", e
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}

func checkOutput(o options) error {
	if o.Catalog == "" || o.ROMRoot == "" || o.Links == "" || o.Out == "" {
		return fmt.Errorf("--catalog, --rom-root, --links and --out required")
	}
	if o.Media != "artwork" && o.Media != "all" && o.Media != "none" {
		return fmt.Errorf("--media must be artwork, all or none")
	}
	out, err := resolved(o.Out)
	if err != nil {
		return err
	}
	for _, name := range []string{o.Catalog, o.ROMRoot, o.Links, o.Sources} {
		if name == "" {
			continue
		}
		input, err := resolved(name)
		if err != nil {
			return err
		}
		if within(input, out) || within(out, input) {
			return fmt.Errorf("output overlaps input %s", name)
		}
	}
	return nil
}
func reportPath(o options) (string, error) {
	name, err := resolved(o.Report)
	if err != nil {
		return "", err
	}
	for _, root := range []string{o.Catalog, o.ROMRoot, o.Sources, o.Links, o.Out} {
		if root == "" {
			continue
		}
		abs, e := resolved(root)
		if e != nil {
			return "", e
		}
		if within(abs, name) || within(name, abs) {
			return "", fmt.Errorf("report overlaps inputs or output")
		}
	}
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		return "", fmt.Errorf("report must be a new file")
	}
	return name, nil
}

func regularInput(root, relative string) (string, error) {
	if !safeRelative(relative) {
		return "", fmt.Errorf("unsafe relative input %q", relative)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	name := filepath.Join(root, filepath.FromSlash(relative))
	real, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", err
	}
	if !within(root, real) {
		return "", fmt.Errorf("input escapes its root: %s", relative)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("input is not a regular file: %s", relative)
	}
	return real, nil
}

func checkSnapshots(m *manifest, o options) error {
	if len(m.Snapshots) > 0 && o.Sources == "" {
		return fmt.Errorf("--sources required for pinned snapshots")
	}
	seen := map[string]bool{}
	for _, s := range m.Snapshots {
		if seen[s.Path] || !validHash(s.SHA256, 32) {
			return fmt.Errorf("duplicate snapshot or invalid SHA256: %s", s.Path)
		}
		seen[s.Path] = true
		name, err := regularInput(o.Sources, s.Path)
		if err != nil {
			return err
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		ce := f.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
		if !strings.EqualFold(fmt.Sprintf("%x", h.Sum(nil)), s.SHA256) {
			return fmt.Errorf("source snapshot changed: %s", s.Path)
		}
	}
	return nil
}

func verifyInput(root string, in input, want fingerprint) (os.FileInfo, error) {
	name, err := regularInput(root, in.Path)
	if err != nil {
		return nil, err
	}
	before, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if in.Size != 0 && before.Size() != in.Size || in.MTimeNS != 0 && before.ModTime().UnixNano() != in.MTimeNS {
		return nil, fmt.Errorf("input stat changed: %s", in.Path)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	var member io.ReadCloser
	if in.Member != "" {
		z, err := zip.NewReader(f, before.Size())
		if err != nil {
			return nil, err
		}
		count := 0
		var selected *zip.File
		for _, entry := range z.File {
			if zipnames.Name(entry) != in.Member {
				continue
			}
			count++
			if count > 1 {
				return nil, fmt.Errorf("duplicate ZIP member: %s", in.Member)
			}
			if entry.UncompressedSize64 != uint64(want.Size) {
				return nil, fmt.Errorf("ZIP member size mismatch: %s", in.Member)
			}
			selected = entry
		}
		if selected == nil {
			return nil, fmt.Errorf("ZIP member missing: %s", in.Member)
		}
		member, err = selected.Open()
		if err != nil {
			return nil, err
		}
		defer member.Close()
		r = member
	} else if before.Size() != want.Size {
		return nil, fmt.Errorf("raw input size mismatch: %s", in.Path)
	}
	s1, s256, m, c := sha1.New(), sha256.New(), md5.New(), crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(s1, s256, m, c), io.LimitReader(r, want.Size+1))
	if err != nil {
		return nil, err
	}
	if n != want.Size {
		return nil, fmt.Errorf("payload size mismatch: %s", in.Path)
	}
	for _, pair := range [][2]string{{want.SHA1, fmt.Sprintf("%x", s1.Sum(nil))}, {want.SHA256, fmt.Sprintf("%x", s256.Sum(nil))}, {want.MD5, fmt.Sprintf("%x", m.Sum(nil))}, {want.CRC32, fmt.Sprintf("%08x", c.Sum32())}} {
		if pair[0] != "" && !strings.EqualFold(pair[0], pair[1]) {
			return nil, fmt.Errorf("payload hash mismatch: %s", in.Path)
		}
	}
	after, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !sameStat(before, after) {
		return nil, fmt.Errorf("input changed while hashing: %s", in.Path)
	}
	return after, nil
}
func sameStat(a, b os.FileInfo) bool {
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && os.SameFile(a, b)
}
