package exporter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// CatalogObservation records file membership and stat facts for metadata binding.
// It deliberately contains no payload hashes or claims about dump integrity.
type CatalogObservation struct {
	Kind      string        `json:"kind"`
	Directory bool          `json:"directory"`
	Files     []CatalogFile `json:"files"`
}
type CatalogFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MtimeNS int64  `json:"mtime_ns"`
}

func ValidCatalogObservation(rom string, size int64, c *CatalogObservation) bool {
	if !ValidInputPath(rom) || c == nil || c.Kind != "name-stat-v1" || len(c.Files) == 0 || len(c.Files) > 65536 || size < 0 {
		return false
	}
	if !c.Directory && (len(c.Files) != 1 || c.Files[0].Name != filepath.Base(rom)) {
		return false
	}
	var sum int64
	previous := ""
	for _, f := range c.Files {
		if !ValidInputPath(f.Name) || f.Size < 0 || f.Size > 1<<44 || f.MtimeNS <= 0 || previous != "" && f.Name <= previous {
			return false
		}
		sum += f.Size
		previous = f.Name
	}
	return sum == size
}

// ObserveCatalogInput reads directory entries and file stats, never file bodies.
func ObserveCatalogInput(root, rom string) (ObservedInput, error) {
	result := ObservedInput{ROM: rom}
	if !ValidInputPath(rom) {
		return result, fmt.Errorf("invalid native catalog path")
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
			return result, fmt.Errorf("catalog input contains unavailable or symlink component")
		}
	}
	info, err := catalogfs.Lstat(cursor)
	if err != nil {
		return result, err
	}
	c := &CatalogObservation{Kind: "name-stat-v1", Directory: info.IsDir(), Files: []CatalogFile{}}
	add := func(path, name string, info fs.FileInfo) error {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("catalog input contains a nonregular file")
		}
		c.Files = append(c.Files, CatalogFile{Name: filepath.ToSlash(name), Size: info.Size(), MtimeNS: info.ModTime().UnixNano()})
		if len(c.Files) > 65536 {
			return fmt.Errorf("catalog folder exceeds 65536 files")
		}
		result.Outer.Size += info.Size()
		return nil
	}
	if c.Directory {
		err = catalogfs.WalkDir(cursor, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("catalog folder contains a symlink")
			}
			if entry.IsDir() {
				return nil
			}
			info, e := catalogfs.Lstat(path)
			if e != nil {
				return e
			}
			name, e := filepath.Rel(cursor, path)
			if e != nil {
				return e
			}
			return add(path, name, info)
		})
	} else {
		err = add(cursor, filepath.Base(cursor), info)
	}
	if err != nil {
		return result, err
	}
	sort.Slice(c.Files, func(i, j int) bool { return c.Files[i].Name < c.Files[j].Name })
	result.Catalog = c
	if !ValidCatalogObservation(rom, result.Outer.Size, c) {
		return result, fmt.Errorf("invalid or empty catalog observation")
	}
	return result, nil
}

func (resolver *LookupResolver) bindCatalog(input ObservedInput, allowGame bool) (Record, error) {
	record := Record{ROM: input.ROM, Size: input.Outer.Size, Catalog: input.Catalog, Representation: "catalog-name-stat", Status: "unsupported", Fields: map[string]Value{}, Media: map[string]Media{}}
	if !allowGame {
		record.Reason = "catalog names require explicit game_binding; dump identity is unverified"
		return record, nil
	}
	translated := translationInput(&record)
	name := filepath.Base(input.ROM)
	if !input.Catalog.Directory {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	// Match complete authority/release names including qualifiers. A name is not a hash.
	candidates := resolver.index.nameCandidates(name)
	unique := map[string]fingerprint{}
	for _, candidate := range candidates {
		// MAME short names are identifiers. Generic payload aliases and punctuation
		// normalization must not make another set compete with the exact authority.
		if resolver.Platform.Platform == "Arcade" {
			if candidate.variant != "" {
				continue
			}
			if err := loadGameDocument(resolver.Catalog.Root, candidate.game); err != nil {
				return record, err
			}
			release := catalogyaml.Value(catalogyaml.Value(candidate.game.root, "releases"), candidate.release)
			authority, mame := catalogyaml.Value(release, "authority"), catalogyaml.Value(release, "mame")
			if field(authority, "source") != "mame" || field(authority, "entry_name") != name || field(mame, "snapshot") == "" {
				continue
			}
		}
		if !translated || candidate.variant != "" {
			unique[candidate.targetKey()] = candidate
		}
	}
	if len(unique) == 1 {
		for _, candidate := range unique {
			record.Status, record.MatchedBy, record.Reason = "matched", "catalog-name", "unique complete catalog release name; payload remains unverified"
			record.Warnings = []string{"Filename/directory and stat evidence only; no dump, DAT, MAME set-version or integrity verification"}
			err := attachFingerprintRecord(resolver.Catalog.Root, candidate, &record, resolver.Catalog.Profile, resolver.Platform.System)
			if err == nil && resolver.Platform.Platform == "Arcade" {
				release := catalogyaml.Value(catalogyaml.Value(candidate.game.root, "releases"), candidate.release)
				record.ArcadeSnapshot = field(catalogyaml.Value(release, "mame"), "snapshot")
			}
			return record, err
		}
	}
	if len(unique) > 1 {
		record.Status, record.Reason = "ambiguous", "complete name identifies multiple catalog releases"
		return record, nil
	}
	if translated {
		record.Reason = "translated payload identity requires an explicit catalog binding"
		return record, nil
	}
	if resolver.Platform.Platform == "Arcade" {
		record.Status, record.Reason = "unmatched", "set short name is absent from current MAME release authority/snapshot"
		return record, nil
	}
	// A multidisc directory may omit the Disc qualifier. Every member must identify
	// one catalog card; no title-only or region/edition guessing is performed.
	if input.Catalog.Directory && len(input.Catalog.Files) > 1 && len(input.Catalog.Files) <= 32 {
		edition, _, _ := catalogMemberEdition(input.Catalog.Files[0].Name)
		sameEdition := true
		games := map[string]*game{}
		for i, file := range input.Catalog.Files {
			key, number, total := catalogMemberEdition(file.Name)
			if number != i+1 || total != 0 && total != len(input.Catalog.Files) || key != edition {
				sameEdition = false
				break
			}
			choices := resolver.index.nameCandidates(catalogMemberName(file.Name))
			if len(choices) == 0 {
				sameEdition = false
				break
			}
			for _, choice := range choices {
				games[choice.game.path] = choice.game
			}
		}
		if sameEdition && len(games) == 1 {
			for _, g := range games {
				record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "complete same-edition disc member names identify one catalog game; payload remains unverified"
				err := attachRecord(resolver.Catalog.Root, g, "", &record, resolver.Catalog.Profile, resolver.Platform.System)
				return record, err
			}
		}
	}
	record.Status, record.Reason = "unmatched", "no unique complete catalog release name"
	return record, nil
}

// Catalog members are files, including compressed optical formats without a launch reader.
func catalogMemberName(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// Play/install roles differ within one edition. Region, language, revision and
// all other qualifiers remain significant. The directory label supplies no identity.
func catalogMemberEdition(name string) (string, int, int) {
	edition, number, total := discEdition(catalogMemberName(name))
	for _, role := range []string{" (Play Disc)", " (Content Install Disc)"} {
		edition = strings.ReplaceAll(edition, role, "")
	}
	return edition, number, total
}
