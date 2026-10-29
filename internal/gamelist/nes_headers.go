package gamelist

import (
	"archive/zip"
	"crypto/md5"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxNESFile = 32 << 20

// legacyNESMD5 hashes one conventional older header representation in memory.
// This is an association aid, not ROM conversion or proof about the source ROM.
// Extended mapper/size and nonstandard console fields are not guessed.
func legacyNESMD5(body []byte) string {
	if len(body) < 16 || string(body[:4]) != "NES\x1a" || body[7]&0x0c != 0x08 ||
		body[8]&0x0f != 0 || body[9] != 0 || body[7]&3 != 0 {
		return ""
	}
	header := append([]byte(nil), body[:16]...)
	header[7] &^= 0x0c
	clear(header[8:16])
	h := md5.New()
	_, _ = h.Write(header)
	_, _ = h.Write(body[16:])
	return fmt.Sprintf("%x", h.Sum(nil))
}

func readBoundedNES(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxNESFile+1))
	if err == nil && len(body) > maxNESFile {
		err = fmt.Errorf("NES input exceeds 32 MiB")
	}
	return body, err
}

func readLocalNES(name string) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(name), ".nes") {
		file, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if info.Size() > maxNESFile {
			return nil, nil
		}
		return readBoundedNES(file)
	}
	archive, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	var member *zip.File
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(file.Name), ".nes") {
			continue
		}
		if member != nil {
			return nil, nil // No whole-file claim for a multi-ROM archive.
		}
		if !safeRelative(file.Name) || file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe local NES ZIP member")
		}
		member = file
	}
	if member == nil {
		return nil, nil
	}
	if member.UncompressedSize64 > maxNESFile {
		return nil, nil // A large multicart cannot use the bounded legacy-header aid.
	}
	reader, err := member.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return readBoundedNES(reader)
}

func addNESHeaderTargets(root string, canonical map[string][]target, sizes map[target]int64) (map[string][]target, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("--nes-rom-root must be an existing directory")
	}
	index := map[string][]target{}
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink inside local NES root")
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".nes" && ext != ".zip" {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("unavailable regular local NES input")
		}
		body, err := readLocalNES(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		// A matching label is insufficient: verify the complete existing local
		// file against an eligible registered fingerprint before deriving a key.
		targets := canonical[fmt.Sprintf("%x", md5.Sum(body))]
		if len(targets) == 0 {
			return nil
		}
		hash := legacyNESMD5(body)
		for _, t := range targets {
			if sizes[t] != int64(len(body)) {
				continue
			}
			addTarget(index, hash, t)
		}
		return nil
	})
	return index, err
}
