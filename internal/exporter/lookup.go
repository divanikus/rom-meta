package exporter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/regions"
)

// Observations are facts about caller-owned files, not paths the catalog may open.
type FileDigest struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type ObservedInput struct {
	ROM     string              `json:"rom"`
	Outer   FileDigest          `json:"outer"`
	Payload PayloadProof        `json:"payload"`
	Disc    *DiscObservation    `json:"disc,omitempty"`
	Catalog *CatalogObservation `json:"catalog,omitempty"`
}
type LookupPlatform struct {
	VariantBinding bool     `json:"variant_binding,omitempty"`
	ExportSystems  []string `json:"export_systems,omitempty"`
	Platform       string   `json:"platform"`
	System         string   `json:"system,omitempty"`
	Supported      bool     `json:"supported"`
	Reason         string   `json:"reason,omitempty"`
	GameBinding    bool     `json:"game_binding,omitempty"`
	NameBinding    bool     `json:"name_binding,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
}
type LookupCatalog struct {
	Root      string
	Profile   Profile
	Platforms []LookupPlatform
	// CacheDir is optional derived state; empty uses platform/_index.sqlite, '-' disables it.
	CacheDir          string
	requireIndexWrite bool
}
type LookupResolver struct {
	Catalog         *LookupCatalog
	Platform        LookupPlatform
	index           *catalogIndex
	games           []*game // Compact identities; parsed card trees live for one request.
	DocumentDigests map[string]string
	IndexStats      LookupIndexStats
}

func LookupSystem(system string) bool {
	// Metadata-only CHD reader; no ES-DE launch support.
	if system == "psp" {
		return true
	}
	spec, ok := systems[system]
	return ok && !spec.Arcade && (!spec.Disc || DiscLookupSystem(system))
}
func ValidInputPath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

var observationHex = regexp.MustCompile(`^[a-f0-9]+$`)

func ValidObservation(input ObservedInput) bool {
	if input.Catalog != nil {
		return input.Disc == nil && input.Outer.SHA256 == "" && input.Payload.Equal(PayloadProof{}) && ValidCatalogObservation(input.ROM, input.Outer.Size, input.Catalog)
	}
	if input.Disc != nil {
		return input.Outer.SHA256 == "" && input.Payload.Equal(PayloadProof{}) && ValidDiscObservation(input.ROM, input.Outer.Size, input.Disc)
	}
	hexValue := func(value string, length int) bool {
		return len(value) == length && observationHex.MatchString(value)
	}
	p := input.Payload
	if len(p.Components) > 15 {
		return false
	}
	seen := map[string]bool{}
	var total int64
	for _, part := range PayloadParts(p) {
		total += part.Size
		if len(part.Components) > 0 || part.Size <= 0 || part.Size > maxPayload || !hexValue(part.SHA256, 64) || !hexValue(part.SHA1, 40) || !hexValue(part.MD5, 32) || !hexValue(part.CRC32, 8) {
			return false
		}
		if len(p.Components) > 0 {
			key := strings.ToLower(part.Member)
			if !ValidInputPath(part.Member) || seen[key] {
				return false
			}
			seen[key] = true
		}
	}
	if !ValidInputPath(input.ROM) || input.Outer.Size <= 0 || input.Outer.Size > 32<<30 || p.Size <= 0 || p.Size > maxPayload || !hexValue(input.Outer.SHA256, 64) || !hexValue(p.SHA256, 64) || !hexValue(p.SHA1, 40) || !hexValue(p.MD5, 32) || !hexValue(p.CRC32, 8) {
		return false
	}
	if total > maxPayload {
		return false
	}
	if p.Member == "" && (input.Outer.Size != p.Size || input.Outer.SHA256 != p.SHA256) {
		return false
	}
	return p.Member == "" || strings.EqualFold(filepath.Ext(input.ROM), ".zip")
}
func NewLookupCatalog(dataRoot, profilePath string) (*LookupCatalog, error) {
	root, err := realPath(dataRoot)
	if err != nil {
		return nil, err
	}
	profile, err := loadProfile(root, profilePath)
	if err != nil {
		return nil, err
	}
	catalog := &LookupCatalog{Root: root, Profile: profile, Platforms: []LookupPlatform{}}
	entries, err := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		path, err := realPath(filepath.Join(root, "platforms", entry.Name()))
		if err != nil || !within(root, path) {
			return nil, fmt.Errorf("catalog platform escapes root")
		}
		platform := LookupPlatform{Platform: entry.Name()}
		doc, err := readDocument(filepath.Join(path, "_platform.yaml"))
		if os.IsNotExist(err) {
			platform.Reason = "catalog has no platform header"
		} else if err != nil {
			return nil, err
		} else {
			for _, system := range SupportedSystems() {
				if systems[system].Platform == field(doc, "name") {
					platform.ExportSystems = append(platform.ExportSystems, system)
				}
			}
			platform.System, err = systemFor(field(doc, "name"), "")
			if err != nil {
				platform.Reason = "no supported launch reader for this platform"
			} else if !LookupSystem(platform.System) {
				platform.Reason = "disc, track, package or arcade proof is not supported by catalog lookup"
			} else {
				platform.Supported = true
				platform.GameBinding = DiscLookupSystem(platform.System)
			}
		}
		if doc != nil && field(doc, "name") == "Sony - PlayStation Portable" {
			// Metadata observations are independent of ES-DE launch readers.
			platform.System, platform.Supported, platform.GameBinding, platform.Reason = "psp", true, true, ""
		}
		if doc != nil && field(doc, "name") != "" {
			platform.NameBinding = true
			platform.Aliases = []string{field(doc, "name"), field(doc, "display_name")}
			for alias, target := range map[string]string{
				"Atari - Atari 7800": "Atari - Atari 7800 (BIN)", "Atari - Atari Jaguar": "Atari - Atari Jaguar (J64)", "Atari - Atari Lynx": "Atari - Atari Lynx (LYX)",
				"Nintendo - Family Computer Disk System": "Nintendo - Family Computer Disk System (FDS)", "Fujitsu - FM-Towns": "Fujitsu - FM Towns series", "MAME": "Arcade", "Panasonic - 3DO Interactive Multiplayer": "3DO Interactive Multiplayer",
			} {
				if platform.Platform == target {
					platform.Aliases = append(platform.Aliases, alias)
				}
			}
			sort.Strings(platform.Aliases)
			if !platform.Supported {
				platform.System, platform.Supported, platform.GameBinding, platform.Reason = "catalog", true, true, ""
			}
		}
		platform.VariantBinding = platform.Supported && platform.Platform != "Arcade"
		catalog.Platforms = append(catalog.Platforms, platform)
	}
	return catalog, nil
}
func (catalog *LookupCatalog) Open(platform string) (*LookupResolver, error) {
	var selected *LookupPlatform
	for i := range catalog.Platforms {
		if catalog.Platforms[i].Platform == platform {
			selected = &catalog.Platforms[i]
			break
		}
	}
	if selected == nil || !selected.Supported {
		return nil, fmt.Errorf("platform has no supported catalog lookup reader")
	}
	folder := filepath.Join(catalog.Root, "platforms", platform)
	resolver := &LookupResolver{Catalog: catalog, Platform: *selected, DocumentDigests: map[string]string{}}
	err := catalog.openIndex(resolver, folder)
	return resolver, err
}
func (resolver *LookupResolver) Resolve(inputs []ObservedInput) (*Plan, error) {
	return resolver.ResolveBindings(inputs, false)
}

// ResolveBindings explicitly permits game-only identity, never dump verification.
// Existing exports and default exact API lookups retain their previous behavior.
func (resolver *LookupResolver) ResolveBindings(inputs []ObservedInput, allowGame bool) (*Plan, error) {
	defer func() {
		for _, game := range resolver.games {
			game.root = nil
			game.mediaStyles = nil
		}
	}()
	catalog := resolver.Catalog
	plan := &Plan{SchemaVersion: 1, Target: "es-de", Platform: resolver.Platform.Platform, System: resolver.Platform.System, DataRoot: catalog.Root, Profile: catalog.Profile, Records: []Record{}}
	seen := map[string]bool{}
	for _, input := range inputs {
		if !ValidObservation(input) || seen[input.ROM] {
			return nil, fmt.Errorf("invalid or duplicate input observation")
		}
		seen[input.ROM] = true
		if input.Catalog != nil {
			record, err := resolver.bindCatalog(input, allowGame)
			if err != nil {
				return nil, err
			}
			plan.Records = append(plan.Records, record)
			continue
		}
		if input.Disc != nil {
			record, err := resolver.bindDisc(input, allowGame)
			if err != nil {
				return nil, err
			}
			plan.Records = append(plan.Records, record)
			continue
		}
		p := input.Payload
		record := Record{ROM: input.ROM, Status: "unmatched", Size: p.Size, SHA1: p.SHA1, Member: p.Member, Payload: &p, Representation: "raw-file", Fields: map[string]Value{}, Media: map[string]Media{}}
		ext := strings.ToLower(filepath.Ext(input.ROM))
		payloadExt := ext
		if p.Member != "" {
			payloadExt = strings.ToLower(filepath.Ext(p.Member))
			record.Representation = "zip-payload"
		}
		if !payloadExtension(plan.System, payloadExt) || ext == ".zip" && p.Member == "" {
			record.Status = "unsupported"
			record.Reason = "observation is not a supported raw cartridge or single-payload ZIP"
		} else {
			if len(p.Components) > 0 {
				if err := bindCompound(catalog.Root, resolver.index, &record, catalog.Profile, plan.System); err != nil {
					return nil, err
				}
				plan.Records = append(plan.Records, record)
				continue
			}
			h := hashes{p.Size, p.SHA1, p.SHA256, p.MD5, p.CRC32}
			if err := bindObservedPayload(catalog.Root, resolver.index, &record, catalog.Profile, plan.System, h, ext, false); err != nil {
				return nil, err
			}
			if allowGame && record.Status == "unmatched" && !strings.Contains("/"+strings.ToLower(input.ROM), "/translations/") && !strings.Contains(strings.ToLower(input.ROM), "[t-") {
				if err := resolver.bindGame(&record); err != nil {
					return nil, err
				}
			}
		}
		plan.Records = append(plan.Records, record)
	}
	return plan, nil
}

func bindingTitle(name string) string {
	name = filepath.Base(name)
	if nameExtension(strings.ToLower(filepath.Ext(name))) {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if n := strings.Index(name, " ("); n >= 0 {
		name = name[:n]
	}
	if n := strings.Index(name, " ["); n >= 0 {
		name = name[:n]
	}
	return nameKey(name)
}

func (resolver *LookupResolver) bindGame(record *Record) error {
	// Exact normalized title/authority aliases only; no edit-distance, region,
	// revision or translation guessing. Archive/member disagreement is ambiguous.
	games := map[string]*game{}
	keys := map[string]bool{bindingTitle(record.ROM): true}
	if record.Member != "" {
		keys[bindingTitle(record.Member)] = true
	}
	for _, bucket := range resolver.index.ByName {
		for _, fp := range bucket {
			if keys[bindingTitle(fp.release)] || keys[bindingTitle(fp.game.name)] {
				games[fp.game.path] = fp.game
			}
		}
	}
	if len(games) == 1 {
		for _, g := range games {
			record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "unique catalog title/authority alias; dump is unverified"
			record.Warnings = append(record.Warnings, "Game identity only; no DAT or payload verification")
			if err := attachRecord(resolver.Catalog.Root, g, "", record, resolver.Catalog.Profile, resolver.Platform.System); err != nil {
				return err
			}
			// A DAT-style region label is a display hint, never a release proof.
			var hint []string
			if start := strings.Index(record.ROM, " ("); start >= 0 {
				if end := strings.Index(record.ROM[start+2:], ")"); end >= 0 {
					for _, part := range strings.Split(record.ROM[start+2:start+2+end], ",") {
						if region, ok := regions.NormalizeDATDisplay(part); ok {
							hint = append(hint, region)
						}
					}
				}
			}
			if len(hint) > 0 {
				media, warnings, err := resolveMedia(resolver.Catalog.Root, g, record.MetadataRelease, record.ROM, resolver.Catalog.Profile, false, resolver.Platform.System, hint)
				if err != nil {
					return err
				}
				record.Media = media
				record.Warnings = append(record.Warnings, warnings...)
			}
			return nil
		}
	}
	if len(games) > 1 {
		record.Status, record.Reason = "ambiguous", "title/authority alias identifies multiple catalog cards"
		for _, g := range games {
			record.Candidates = append(record.Candidates, g.name)
		}
		sort.Strings(record.Candidates)
	}
	return nil
}

// ObserveInput uses the shared single-payload reader, without extracting files.
func ObserveInput(root, system, rom string) (ObservedInput, error) {
	if DiscLookupSystem(system) {
		return observeDisc(root, system, rom)
	}
	result := ObservedInput{ROM: rom}
	if !LookupSystem(system) || !ValidInputPath(rom) {
		return result, fmt.Errorf("unsupported system or invalid native path")
	}
	ext := strings.ToLower(filepath.Ext(rom))
	if ext != ".zip" && !payloadExtension(system, ext) {
		return result, fmt.Errorf("format is not a supported raw cartridge or single-payload ZIP")
	}
	base, err := realPath(root)
	if err != nil {
		return result, err
	}
	lexical := filepath.Join(base, filepath.FromSlash(rom))
	actual, err := realPath(lexical)
	if err != nil || !within(base, actual) {
		return result, fmt.Errorf("native input unavailable or outside library")
	}
	// Reject symlinks, including intermediate components; native mounts are ordinary roots.
	cursor := base
	for _, part := range strings.Split(rom, "/") {
		cursor = filepath.Join(cursor, part)
		info, e := catalogfs.Lstat(cursor)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return result, fmt.Errorf("native input contains symlink or unavailable component")
		}
	}
	before, err := catalogfs.Stat(actual)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 32<<30 || ext != ".zip" && before.Size() > maxPayload {
		return result, fmt.Errorf("input is not a bounded regular cartridge file")
	}
	result.Payload, err = observePayload(actual, system)
	if err != nil {
		return result, err
	}
	result.Outer = FileDigest{Size: result.Payload.Size, SHA256: result.Payload.SHA256}
	if ext == ".zip" {
		f, e := catalogfs.Open(actual)
		if e != nil {
			return result, e
		}
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(f, (32<<30)+1))
		f.Close()
		if e != nil || n != before.Size() {
			return result, fmt.Errorf("archive changed while reading")
		}
		result.Outer = FileDigest{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}
	}
	after, err := catalogfs.Stat(actual)
	if err != nil || !catalogfs.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !ValidObservation(result) {
		return result, fmt.Errorf("native input changed while reading")
	}
	return result, nil
}
