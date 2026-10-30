package exporter

import (
	"archive/zip"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/zipnames"
)

// Format policy is shared by platforms. New catalog headers do not require Go
// registrations. Sidecars never become launches; opaque containers stay stat/name
// evidence and are not decompressed or claimed to be directly emulator-ready.
func genericCandidateExtension(ext string) bool {
	if len(ext) < 2 || len(ext) > 20 || ext[0] != '.' {
		return false
	}
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return !strings.Contains("|.txt|.nfo|.pdf|.jpg|.jpeg|.png|.gif|.webp|.mp4|.avi|.mkv|.xml|.yaml|.yml|.json|.ini|.cfg|.key|.rap|.pup|.log|.sfv|.md5|.sha1|.sha256|", "|"+ext+"|")
}
func genericPayloadExtension(ext string) bool {
	return genericCandidateExtension(ext) && !strings.Contains("|.zip|.7z|.rar|.chd|.rvz|.wux|.wua|.gcz|.wbfs|.cso|.zso|.pbp|.pkg|.cia|.vpk|.wad|.nsp|.xci|.cue|.gdi|.m3u|.app|.h3|.tik|.tmd|", "|"+ext+"|")
}
func structuralROMDirectory(name string) bool {
	name = strings.ToLower(name)
	return name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || strings.Contains("|retail|aftermarket|unlicensed|translations|hacks|homebrew|support|bios|firmware|roms|games|digital|", "|"+name+"|")
}

// Group explicit installed-game layouts, without confusing category/language
// directories or ordinary disc subdirectories with an installed title.
func romsetDirectory(path string) (bool, error) {
	if structuralROMDirectory(filepath.Base(path)) {
		return false, nil
	}
	entries, err := catalogfs.ReadDir(path)
	if err != nil {
		return false, err
	}
	dataDirs := false
	dataFiles := false
	files := 0
	allLaunches := true
	allDiscs := len(entries) > 0
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if entry.IsDir() {
			allDiscs = false
			if name == "ps3_game" || name == "sce_sys" || name == "code" || name == "content" || name == "data" {
				dataDirs = true
			}
			continue
		}
		files++
		ext := strings.ToLower(filepath.Ext(name))
		if !strings.Contains("|.chd|.iso|.zip|.7z|.rar|.rvz|.wux|.wua|.gcz|.wbfs|.cso|.pbp|", "|"+ext+"|") {
			allLaunches = false
		}
		if ext != ".chd" && ext != ".iso" {
			allDiscs = false
		}
		if ext == ".app" || ext == ".h3" || ext == ".tmd" || ext == ".tik" {
			dataFiles = true
		}
	}
	namedSet := strings.Contains(filepath.Base(path), " (") && files > 1 && !allLaunches
	return !allDiscs && (dataDirs || dataFiles || namedSet), nil
}
func catalogSize(c *CatalogObservation) int64 {
	var size int64
	if c != nil {
		for _, file := range c.Files {
			size += file.Size
		}
	}
	return size
}
func observeROMSet(ctx context.Context, root, path, system string) (ExportInput, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ExportInput{}, err
	}
	input := ExportInput{ROM: filepath.ToSlash(rel), Kind: "unsupported"}
	stat := func() (ExportInput, error) {
		observed, err := ObserveCatalogInput(root, input.ROM)
		if err != nil {
			return input, err
		}
		input.Kind = "catalog"
		input.Catalog = observed.Catalog
		return input, nil
	}
	info, err := catalogfs.Lstat(path)
	if err != nil {
		return input, err
	}
	if !info.Mode().IsRegular() {
		if info.IsDir() {
			return stat()
		}
		input.Reason = "ROM input must be a regular file or an installed-game directory"
		return input, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		archive, err := zip.OpenReader(path)
		if err != nil {
			input.Reason = "cannot inspect ZIP directory: " + err.Error()
			return input, nil
		}
		count := 0
		opaque := false
		var bytes uint64
		seen := map[string]bool{}
		for _, file := range archive.File {
			if file.FileInfo().IsDir() {
				continue
			}
			name := zipnames.Name(file)
			key := strings.ToLower(name)
			if !ValidInputPath(name) || seen[key] || file.Mode()&os.ModeSymlink != 0 || file.Flags&1 != 0 {
				archive.Close()
				input.Reason = "unsafe, duplicate, symlink or encrypted ZIP member"
				return input, nil
			}
			seen[key] = true
			memberExt := strings.ToLower(filepath.Ext(name))
			if genericCandidateExtension(memberExt) && !genericPayloadExtension(memberExt) {
				opaque = true
			}
			if genericPayloadExtension(memberExt) {
				count++
				bytes = file.UncompressedSize64
			}
		}
		archive.Close()
		// Multi-file disc/package archives keep the original set identity. Hashing
		// a random member must never become a whole-release/variant proof.
		if opaque || count != 1 {
			return stat()
		}
		if bytes > maxPayload {
			input.Representation = "romset-single-zip"
			return stat()
		}
	} else if !genericPayloadExtension(ext) || info.Size() > maxPayload {
		return stat()
	}
	proof, err := inspectROMContext(ctx, path, "romset")
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return input, ctx.Err()
		}
		input.Reason = err.Error()
		return input, nil
	}
	input.Kind, input.Representation, input.Proof = "payload", "romset-payload", proof
	return input, nil
}
func (r *ExportResolver) resolveCatalogExport(input ExportInput, profile Profile) (Record, error) {
	lookup := &LookupResolver{Catalog: &LookupCatalog{Root: r.Root, Profile: profile}, Platform: LookupPlatform{Platform: r.Platform, System: r.System}, index: r.index}
	record, err := lookup.bindCatalog(ObservedInput{ROM: input.ROM, Outer: FileDigest{Size: catalogSize(input.Catalog)}, Catalog: input.Catalog}, true)
	if err != nil {
		return record, err
	}
	record.SourceOnly = ROMSetSourceOnly(input)
	record.Warnings = append(record.Warnings, "Original ROM-set container/folder reference; extraction, installation or native emulator configuration may be required. Launch/dependency readiness is not verified")
	return record, nil
}

// CheckROMSetObservation detects membership additions/removals as well as stat
// changes, including nested files. Stat facts are not content-integrity hashes.
func CheckROMSetObservation(root, rom string, expected *CatalogObservation) error {
	observed, err := ObserveCatalogInput(root, rom)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, observed.Catalog) {
		return fmt.Errorf("ROM-set input changed since planning: %s", rom)
	}
	return nil
}
func romsetEntry(root, path string, entry fs.DirEntry, legacyOnly bool) (bool, error) {
	if legacyOnly || path == root || !entry.IsDir() {
		return false, nil
	}
	return romsetDirectory(path)
}

// ROMSetSourceOnly separates source packages/directories from native launch files.
func ROMSetSourceOnly(input ExportInput) bool {
	if input.Catalog == nil || input.Representation == "romset-single-zip" {
		return false
	}
	if input.Catalog.Directory {
		return true
	}
	ext := strings.ToLower(filepath.Ext(input.ROM))
	return strings.Contains("|.zip|.7z|.rar|.pkg|.cia|.vpk|.wad|.nsp|.app|.h3|.tik|.tmd|", "|"+ext+"|")
}
func recordDescription(record Record) string {
	text := record.Fields["description"].Value
	if record.SourceOnly {
		if text != "" {
			text += "\n\n"
		}
		text += "RomMeta: source ROM set; extract/install and configure its launch before playing."
	}
	return text
}
