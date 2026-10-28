package exporter

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// DiscObservation is bounded container evidence, never a whole-file or DAT hash.
// Header contains the actual 124-byte CHD v5 header, encoded as hex. Stat checks
// assume an ordinary immutable library; same-size/same-mtime payload edits are not detected.
type DiscObservation struct {
	Kind      string     `json:"kind"`
	Directory bool       `json:"directory"`
	Files     []DiscFile `json:"files"`
}
type DiscFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MtimeNS int64  `json:"mtime_ns"`
	Header  string `json:"header"`
}

func DiscLookupSystem(system string) bool {
	// UMD header metadata, independent of launch readers.
	if system == "psp" {
		return true
	}
	spec, ok := systems[system]
	return ok && spec.Disc && payloadExtension(system, ".chd")
}

var discNumber = regexp.MustCompile(`(?i) \(Disc ([0-9]+)(?: of ([0-9]+))?\)`)

func discEdition(name string) (string, int, int) {
	name = filepath.Base(name)
	if ext := filepath.Ext(name); nameExtension(strings.ToLower(ext)) {
		name = strings.TrimSuffix(name, ext)
	}
	match := discNumber.FindStringSubmatch(name)
	if match == nil {
		return name, 0, 0
	}
	number, _ := strconv.Atoi(match[1])
	total, _ := strconv.Atoi(match[2])
	return strings.TrimSpace(discNumber.ReplaceAllString(name, "")), number, total
}

func validCHDHeader(text string, size int64) bool {
	h, err := hex.DecodeString(text)
	if err != nil || len(h) != 124 || string(h[:8]) != "MComprHD" || binary.BigEndian.Uint32(h[8:12]) != 124 || binary.BigEndian.Uint32(h[12:16]) != 5 {
		return false
	}
	logical := binary.BigEndian.Uint64(h[32:40])
	mapOffset := binary.BigEndian.Uint64(h[40:48])
	metadata := binary.BigEndian.Uint64(h[48:56])
	hunk := binary.BigEndian.Uint32(h[56:60])
	unit := binary.BigEndian.Uint32(h[60:64])
	// Parent-dependent CHDs require a separate reader; do not pretend they are standalone.
	return size >= 124 && logical > 0 && hunk > 0 && hunk <= 64<<20 && unit > 0 && hunk%unit == 0 && logical%uint64(unit) == 0 && mapOffset >= 124 && mapOffset < uint64(size) && (metadata == 0 || metadata >= 124 && metadata < uint64(size)) && strings.Trim(text[208:248], "0") == "" && strings.Trim(text[128:168], "0") != ""
}

func ValidDiscObservation(rom string, size int64, d *DiscObservation) bool {
	if !ValidInputPath(rom) || d == nil || d.Kind != "chd-header-stat-v1" || len(d.Files) == 0 || len(d.Files) > 32 || (!d.Directory && (len(d.Files) != 1 || d.Files[0].Name != filepath.Base(rom))) || d.Directory && len(d.Files) < 2 {
		return false
	}
	var sum int64
	edition := ""
	seen := map[string]bool{}
	for i, f := range d.Files {
		if !ValidInputPath(f.Name) || strings.Contains(f.Name, "/") || !strings.EqualFold(filepath.Ext(f.Name), ".chd") || f.Size <= 0 || f.Size > 32<<30 || f.MtimeNS <= 0 || seen[strings.ToLower(f.Name)] || !validCHDHeader(f.Header, f.Size) {
			return false
		}
		seen[strings.ToLower(f.Name)] = true
		sum += f.Size
		if d.Directory {
			key, number, total := discEdition(f.Name)
			if number != i+1 || (total != 0 && total != len(d.Files)) || (edition != "" && key != edition) {
				return false
			}
			edition = key
		}
	}
	return sum == size
}

func observeDisc(root, system, rom string) (ObservedInput, error) {
	result := ObservedInput{ROM: rom}
	if !DiscLookupSystem(system) || !ValidInputPath(rom) {
		return result, fmt.Errorf("unsupported CHD system or native path")
	}
	base, err := realPath(root)
	if err != nil {
		return result, err
	}
	cursor := base
	for _, part := range strings.Split(rom, "/") {
		cursor = filepath.Join(cursor, part)
		info, e := catalogfs.Lstat(cursor)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return result, fmt.Errorf("CHD input contains unavailable or symlink component")
		}
	}
	actual, err := realPath(cursor)
	if err != nil || !within(base, actual) {
		return result, fmt.Errorf("CHD input escapes native library")
	}
	info, err := catalogfs.Stat(actual)
	if err != nil {
		return result, err
	}
	d := &DiscObservation{Kind: "chd-header-stat-v1", Directory: info.IsDir(), Files: []DiscFile{}}
	names := []string{filepath.Base(actual)}
	if d.Directory {
		entries, e := catalogfs.ReadDir(actual)
		if e != nil {
			return result, e
		}
		names = nil
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".chd") {
				return result, fmt.Errorf("disc folder must contain only ordinary CHD files")
			}
			names = append(names, entry.Name())
		}
		sort.Slice(names, func(i, j int) bool {
			_, a, _ := discEdition(names[i])
			_, b, _ := discEdition(names[j])
			if a == b {
				return names[i] < names[j]
			}
			return a < b
		})
	}
	if len(names) == 0 || len(names) > 32 {
		return result, fmt.Errorf("CHD input must contain 1..32 discs")
	}
	for _, name := range names {
		path := actual
		if d.Directory {
			path = filepath.Join(actual, name)
		}
		before, e := catalogfs.Lstat(path)
		if e != nil || !before.Mode().IsRegular() || before.Size() < 124 || before.Size() > 32<<30 {
			return result, fmt.Errorf("CHD is not a bounded regular file")
		}
		f, e := catalogfs.Open(path)
		if e != nil {
			return result, e
		}
		header := make([]byte, 124)
		_, e = io.ReadFull(f, header)
		f.Close()
		after, statErr := catalogfs.Stat(path)
		if e != nil || statErr != nil || !catalogfs.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return result, fmt.Errorf("CHD changed while reading header")
		}
		d.Files = append(d.Files, DiscFile{Name: name, Size: before.Size(), MtimeNS: before.ModTime().UnixNano(), Header: hex.EncodeToString(header)})
		result.Outer.Size += before.Size()
	}
	result.Disc = d
	if !ValidDiscObservation(rom, result.Outer.Size, d) {
		return result, fmt.Errorf("invalid CHD v5 header, mixed edition or missing/duplicate disc numbers")
	}
	return result, nil
}

func (resolver *LookupResolver) bindDisc(input ObservedInput, allowGame bool) (Record, error) {
	record := Record{ROM: input.ROM, Size: input.Outer.Size, Disc: input.Disc, Representation: "chd-header-stat", Status: "unsupported", Fields: map[string]Value{}, Media: map[string]Media{}}
	if !DiscLookupSystem(resolver.Platform.System) || !allowGame {
		record.Reason = "CHD metadata requires explicit game_binding; no DAT verification is available"
		return record, nil
	}
	matched, err := bindVariantCHD(resolver.Catalog.Root, resolver.index, &record, resolver.Catalog.Profile, resolver.Platform.System)
	if err != nil || matched {
		return record, err
	}
	if translationInput(&record) {
		record.Reason = "translated CHD identity is deferred"
		return record, nil
	}
	if resolver.Platform.System == "psp" {
		matched, err := resolver.bindUMDHeader(&record)
		if matched || err != nil {
			return record, err
		}
	}

	var first *Record
	for _, file := range input.Disc.Files {
		part := Record{ROM: file.Name, Status: "unmatched", Fields: map[string]Value{}, Media: map[string]Media{}}
		if err := resolver.bindDiscName(&part); err != nil {
			return record, err
		}
		if part.Status != "matched" {
			record.Status, record.Reason = part.Status, "CHD member lacks one unique catalog game: "+file.Name
			record.Candidates = part.Candidates
			return record, nil
		}
		if first == nil {
			copy := part
			first = &copy
		} else if first.Game != part.Game {
			record.Status, record.Reason = "ambiguous", "CHD folder mixes catalog games"
			return record, nil
		}
	}
	// A contiguous subset is still incomplete when the catalog lists another disc of this edition.
	if input.Disc.Directory {
		edition, _, _ := discEdition(input.Disc.Files[0].Name)
		for _, bucket := range resolver.index.ByName {
			for _, fp := range bucket {
				key, n, _ := discEdition(fp.release)
				if fp.game.path == filepath.Join(resolver.Catalog.Root, filepath.FromSlash(first.Game)) && key == edition && n > len(input.Disc.Files) {
					record.Reason = "CHD folder lacks a disc listed for this catalog edition"
					return record, nil
				}
			}
		}
	}
	record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "CHD member titles identify one catalog game; container and dump remain unverified"
	record.Game, record.MetadataRelease, record.Fields, record.Media = first.Game, first.MetadataRelease, first.Fields, first.Media
	record.Warnings = append(first.Warnings, "Only CHD headers, file sizes and mtimes observed; no full-file hash, decompression, Redump or integrity verification")
	return record, nil
}

// A UMD ISO is one 2048-byte-sector stream. Its declared CHD raw SHA1/size
// can select a metadata basis, but is not a verified hash of the observed file.
func (resolver *LookupResolver) bindUMDHeader(record *Record) (bool, error) {
	if record.Disc.Directory || len(record.Disc.Files) != 1 {
		return false, nil
	}
	h, err := hex.DecodeString(record.Disc.Files[0].Header)
	if err != nil || len(h) != 124 {
		return false, fmt.Errorf("invalid UMD CHD header")
	}
	if binary.BigEndian.Uint32(h[60:64]) != 2048 {
		return false, nil
	}
	size := int64(binary.BigEndian.Uint64(h[32:40]))
	sha1 := hex.EncodeToString(h[64:84])
	matches := map[string]fingerprint{}
	for _, fp := range resolver.index.BySize[size] {
		if fp.variant != "" || fp.chd || fp.sha1 != sha1 {
			continue
		}
		if err := loadGameDocument(resolver.Catalog.Root, fp.game); err != nil {
			return false, err
		}
		release := catalogyaml.Value(catalogyaml.Value(fp.game.root, "releases"), fp.release)
		if field(catalogyaml.Value(release, "authority"), "source") == "redump" {
			matches[fp.targetKey()] = fp
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	if len(matches) > 1 {
		record.Status, record.Reason = "ambiguous", "declared UMD CHD raw SHA1/size identifies multiple Redump releases"
		return true, nil
	}
	for _, fp := range matches {
		record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "declared UMD CHD raw SHA1/size selects one Redump metadata basis; payload remains unverified"
		if err := attachRecord(resolver.Catalog.Root, fp.game, fp.release, record, resolver.Catalog.Profile, resolver.Platform.System); err != nil {
			return true, err
		}
		record.Release = ""
		record.Warnings = append(record.Warnings, "Declared CHD header identity only; no decompression, payload integrity or Redump verification")
	}
	return true, nil
}

// A complete release name is considered before broad title aliases. It selects
// a metadata basis, never a verified raw-disc release from a CHD container.
func (resolver *LookupResolver) bindDiscName(record *Record) error {
	exact := map[string]fingerprint{}
	for _, fp := range resolver.index.ByName[nameKey(record.ROM)] {
		if fp.variant == "" {
			exact[fp.targetKey()] = fp
		}
	}
	if len(exact) > 1 {
		record.Status, record.Reason = "ambiguous", "complete CHD name identifies multiple catalog releases"
		return nil
	}
	for _, fp := range exact {
		record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "complete CHD release name selects a metadata basis; dump remains unverified"
		if err := attachRecord(resolver.Catalog.Root, fp.game, fp.release, record, resolver.Catalog.Profile, resolver.Platform.System); err != nil {
			return err
		}
		record.Release = ""
		record.Warnings = append(record.Warnings, "Complete filename selects metadata only; no raw-disc or Redump verification")
		return nil
	}
	return resolver.bindGame(record)
}
