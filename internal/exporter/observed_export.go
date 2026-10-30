package exporter

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/platformnames"
)

// ExportInput contains only observations of caller-owned files. No caller path
// is opened by the catalog server. Parts are bounded playlist members.
type ExportInput struct {
	Catalog        *CatalogObservation `json:"catalog,omitempty"`
	Disc           *DiscObservation    `json:"disc,omitempty"`
	Representation string              `json:"representation,omitempty"`
	ROM            string              `json:"rom"`
	Kind           string              `json:"kind"`
	Proof          PayloadProof        `json:"proof"`
	Parts          []ExportInput       `json:"parts,omitempty"`
	Serial         string              `json:"serial,omitempty"`
	Reason         string              `json:"reason,omitempty"`
}

type ExportResolver struct {
	Root, Platform, System string
	PlatformName           string
	DocumentDigests        map[string]string
	index                  *catalogIndex
	arcade                 *arcadeIndex
}

// OpenExport retains the complete local export semantics, separate from strict
// consumer lookup. Persisted indexes remain optional and selected YAML is checked.
func (catalog *LookupCatalog) OpenExport(platform, system, snapshot string) (*ExportResolver, error) {
	found := false
	for _, p := range catalog.Platforms {
		if p.Platform == platform {
			if p.header != p.family && p.family != "" {
				if _, err := systemFor(p.header, system); err == nil {
					found = true
				}
			}
			for _, id := range p.ExportSystems {
				if id == system {
					found = true
				}
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("platform has no export reader for %q", system)
	}
	r := &ExportResolver{Root: catalog.Root, Platform: platform, System: system}
	for _, p := range catalog.Platforms {
		if p.Platform == platform {
			r.PlatformName = p.Name
			break
		}
	}
	var err error
	if systems[system].Arcade {
		r.arcade, err = loadArcadeIndex(catalog.Root, snapshot)
	} else {
		lookup := &LookupResolver{Catalog: catalog, Platform: LookupPlatform{Platform: platform, System: system}, DocumentDigests: map[string]string{}}
		for _, folder := range catalog.scope(platform) {
			err = catalog.openIndex(lookup, filepath.Join(catalog.Root, "platforms", folder))
			if err != nil {
				return nil, err
			}
		}
		r.index = lookup.index
		r.DocumentDigests = lookup.DocumentDigests
	}
	return r, err
}

func exportRecord(input ExportInput) Record {
	r := Record{Representation: input.Representation, ROM: input.ROM, Status: "unmatched", Fields: map[string]Value{}, Media: map[string]Media{}}
	r.Fields["title"] = Value{Value: strings.TrimSuffix(filepath.Base(input.ROM), filepath.Ext(input.ROM)), Source: "filename"}
	return r
}

func validExportInput(input ExportInput, system string, part bool) bool {
	if !validSystemID(system) || !ValidInputPath(input.ROM) || len(input.ROM) > 8192 || len(input.Reason) > 4096 || len(input.Parts) > 32 {
		return false
	}
	if input.Catalog != nil {
		return !part && (input.Representation == "" || input.Representation == "romset-single-zip") && input.Kind == "catalog" && input.Disc == nil && input.Proof.Equal(PayloadProof{}) && len(input.Parts) == 0 && input.Serial == "" && ValidCatalogObservation(input.ROM, catalogSize(input.Catalog), input.Catalog)
	}
	if input.Kind == "catalog" {
		return false
	}
	if input.Disc != nil && input.Kind != "payload" {
		return false
	}
	if !ValidInputPath(input.ROM) || len(input.ROM) > 8192 || len(input.Reason) > 4096 || len(input.Parts) > 32 {
		return false
	}
	if input.Kind == "unsupported" {
		if input.Representation != "" && input.Representation != "playlist-directory" && input.Representation != "ps3-serial-file" {
			return false
		}
		return !part && input.Reason != "" && len(input.Parts) == 0 && input.Serial == "" && input.Proof.Equal(PayloadProof{})
	}
	p := input.Proof
	if input.Disc != nil && (input.Kind != "payload" || !DiscLookupSystem(system) || input.Disc.Directory || !ValidDiscObservation(input.ROM, p.Size, input.Disc)) {
		return false
	}
	validHex := func(s string, n int) bool { return len(s) == n && observationHex.MatchString(s) }
	if p.Size <= 0 || !validHex(p.SHA1, 40) || !validHex(p.SHA256, 64) || !validHex(p.MD5, 32) || !validHex(p.CRC32, 8) || len(p.Components) > 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(input.ROM))
	switch input.Kind {
	case "payload":
		if len(input.Parts) > 0 || input.Serial != "" || systems[system].Arcade {
			return false
		}
		if part && ext != ".chd" && ext != ".iso" {
			return false
		}
		if !systems[system].Disc && !part && p.Size > maxPayload {
			return false
		}
		if p.Member != "" {
			return !part && ext == ".zip" && ValidInputPath(p.Member) && (payloadExtension(system, strings.ToLower(filepath.Ext(p.Member))) || input.Representation == "romset-payload" && genericPayloadExtension(strings.ToLower(filepath.Ext(p.Member))))
		}
		return ext != ".zip" && !(system == "ps3" && ext == ".ps3") && (payloadExtension(system, ext) || part && (ext == ".chd" || ext == ".iso") || input.Representation == "romset-payload" && genericPayloadExtension(ext))
	case "arcade":
		return !part && systems[system].Arcade && payloadExtension(system, ext) && p.Member == "" && len(input.Parts) == 0 && input.Serial == ""
	case "serial":
		return !part && system == "ps3" && ext == ".ps3" && p.Size <= 64 && p.Member == "" && len(input.Parts) == 0 && ps3SerialPattern.MatchString(input.Serial)
	case "playlist":
		if part || !playlistSystem(system) || ext != ".m3u" || p.Size > 65536 || p.Member != "" || input.Serial != "" || len(input.Parts) == 0 {
			return false
		}
		seen := map[string]bool{}
		for _, member := range input.Parts {
			if member.Kind != "payload" || !strings.HasPrefix(member.ROM, input.ROM+"/") || seen[strings.ToLower(member.ROM)] || !validExportInput(member, system, true) {
				return false
			}
			seen[strings.ToLower(member.ROM)] = true
		}
		return true
	}
	return false
}

// Resolve uses the same binding and output naming rules as local Build.
func (r *ExportResolver) Resolve(inputs []ExportInput, profile Profile) (*Plan, error) {
	plan := &Plan{PlatformName: r.PlatformName, SchemaVersion: 1, Target: "es-de", Platform: r.Platform, System: r.System, DataRoot: r.Root, Profile: profile, Records: []Record{}}
	seen := map[string]bool{}
	for _, input := range inputs {
		if !validExportInput(input, r.System, false) || seen[input.ROM] {
			return nil, fmt.Errorf("invalid or duplicate export observation: %q", input.ROM)
		}
		seen[input.ROM] = true
		record, err := r.resolve(input, profile)
		if err != nil {
			return nil, err
		}
		plan.Records = append(plan.Records, record)
	}
	if err := finishExportPlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (r *ExportResolver) resolve(input ExportInput, profile Profile) (Record, error) {
	record := exportRecord(input)
	if input.Kind == "unsupported" {
		record.Status, record.Reason = "unsupported", input.Reason
		return record, nil
	}
	p := input.Proof
	record.Size, record.SHA1, record.Member = p.Size, p.SHA1, p.Member
	ext := strings.ToLower(filepath.Ext(input.ROM))
	switch input.Kind {
	case "catalog":
		return r.resolveCatalogExport(input, profile)
	case "playlist":
		return r.resolvePlaylist(input, profile)
	case "serial":
		record.Representation = "ps3-serial-file"
		err := nameFallback(r.Root, r.index, &record, profile, r.System)
		record.Warnings = append(record.Warnings, "PS3 serial-file title binding does not validate the serial against a PSN catalog or installed RPCS3 game")
		return record, err
	case "arcade":
		record.Representation = "arcade-container"
		return record, bindArcade(r.Root, r.arcade, &record, profile, r.System)
	}
	record.Payload = &p
	record.Representation = "raw-file"
	if p.Member != "" {
		record.Representation = "zip-payload"
	}
	if input.Representation == "romset-payload" {
		record.Representation = input.Representation
	}
	if ext == ".chd" && input.Disc != nil {
		record.Representation = "chd-container"
		record.Disc = input.Disc
		matched, err := bindVariantCHD(r.Root, r.index, &record, profile, r.System)
		if err != nil || matched {
			return record, err
		}
	}
	if ext == ".chd" {
		record.Representation = "chd-container"
		record.Warnings = append(record.Warnings, "CHD container hashes are not raw-disc or Redump track hashes; no decompression/integrity check is performed")
	}
	if r.System == "ps3" || r.System == "xbox" || r.System == "xbox360" {
		record.Warnings = append(record.Warnings, "Container metadata binding does not verify emulator launchability, decryption, firmware or XISO conversion")
	}
	return record, bindPayload(r.Root, r.index, &record, profile, r.System, hashes{p.Size, p.SHA1, p.SHA256, p.MD5, p.CRC32}, ext)
}

func finishExportPlan(plan *Plan) error {
	if err := applyExportTitles(plan); err != nil {
		return err
	}
	destinations := map[string]string{}
	for _, record := range plan.Records {
		switch record.Status {
		case "matched":
			plan.Matched++
			switch record.MatchedBy {
			case "hash", "playlist-hash":
				plan.HashMatches++
			case "name", "catalog-name", "arcade-set-name", "playlist-name":
				plan.NameMatches++
			default:
				plan.GameOnly++
			}
		case "ambiguous":
			plan.Ambiguous++
		case "unsupported":
			plan.Unsupported++
		default:
			plan.Unmatched++
		}
		for _, media := range record.Media {
			key := strings.ToLower(media.OutputPath)
			if previous, ok := destinations[key]; ok {
				return fmt.Errorf("media output collision: %s and %s -> %s", previous, record.ROM, media.OutputPath)
			}
			destinations[key] = record.ROM
			plan.MediaFiles++
			plan.MediaBytes += media.Size
		}
	}
	return nil
}

// ObserveExport enumerates existing files without any catalog or frontend access.
func ObserveExport(options Options) ([]ExportInput, error) {
	if !validSystemID(options.System) || options.Limit < 0 {
		return nil, fmt.Errorf("invalid export reader or limit")
	}
	root, err := realPath(options.ROMRoot)
	if err != nil {
		return nil, err
	}
	total, err := countInputs(options, root)
	if err != nil {
		return nil, err
	}
	inputs := []ExportInput{}
	err = catalogfs.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if options.Context != nil {
			if err := options.Context.Err(); err != nil {
				return err
			}
		}
		if walkErr != nil {
			return walkErr
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		playlist := entry.IsDir() && playlistSystem(options.System) && ext == ".m3u"
		group, err := romsetEntry(root, path, entry, options.LegacyOnly)
		group = group && !playlist
		if err != nil {
			return err
		}
		if entry.IsDir() && !playlist && !group || !entry.IsDir() && !inputExtension(options.System, ext) || catalogrules.IsBIOSName(entry.Name()) {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if options.Match != "" && !strings.Contains(strings.ToLower(rel), strings.ToLower(options.Match)) {
			if playlist || group {
				return fs.SkipDir
			}
			return nil
		}
		if options.Limit > 0 && len(inputs) >= options.Limit {
			return fs.SkipAll
		}
		if options.Progress != nil {
			options.Progress("ROMs", rel, len(inputs)+1, total)
		}
		input := ExportInput{ROM: rel, Kind: "unsupported"}
		switch {
		case !playlist && !group && !entry.Type().IsRegular() && entry.Type()&os.ModeSymlink == 0:
			input.Reason = "ROM input must be a regular file"
		case entry.Type()&os.ModeSymlink != 0:
			input.Reason = "ROM symlinks are not followed"
		case group:
			input, e = observeROMSet(options.Context, root, path, options.System)
		case playlist:
			input, e = observePlaylist(options.Context, root, path, options.System)
		case options.System == "ps3" && ext == ".ps3":
			input, e = observeSerial(root, path)
		case systems[options.System].Arcade:
			if !payloadExtension(options.System, ext) {
				input.Reason = "arcade launch reader accepts ZIP/7z set archives"
			} else {
				input.Kind = "arcade"
				input.Proof, e = inspectROMContext(options.Context, path, options.System)
			}
		case !options.LegacyOnly && (genericSystem(options.System) || ext != ".zip" && !payloadExtension(options.System, ext)):
			input, e = observeROMSet(options.Context, root, path, options.System)
		case ext == ".7z" || ext == ".rar" || systems[options.System].Disc && !payloadExtension(options.System, ext):
			input.Reason = "reader accepts raw cartridge files/single-payload ZIPs or standalone CHD/ISO discs"
		default:
			input.Kind = "payload"
			input.Proof, e = inspectROMContext(options.Context, path, options.System)
			if e != nil {
				input.Kind, input.Reason = "unsupported", e.Error()
				input.Proof = PayloadProof{}
				e = nil
			}
		}
		if e == nil && input.Kind == "unsupported" && !options.LegacyOnly && entry.Type().IsRegular() && !systems[options.System].Arcade {
			input, e = observeROMSet(options.Context, root, path, options.System)
		}
		if e == nil && input.Kind == "payload" && ext == ".chd" {
			observed, observeErr := observeDisc(root, options.System, rel)
			if observeErr == nil {
				input.Disc = observed.Disc
			}
		}
		if e != nil {
			return e
		}
		if !validExportInput(input, options.System, false) {
			input = ExportInput{ROM: rel, Kind: "unsupported", Reason: "invalid or unsafe launch observation"}
		}
		inputs = append(inputs, input)
		if playlist || group {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, errNoInputs
	}
	return inputs, nil
}

// ExportOutputPath derives destinations locally rather than trusting API paths.
func ExportOutputPath(system string, record Record, role, extension string) (string, error) {
	ok := validSystemID(system)
	layout, known := roles[role]
	if !ok || !known || !ValidInputPath(record.ROM) || len(extension) < 2 || extension[0] != '.' || strings.ContainsAny(extension[1:], "./\\:") {
		return "", fmt.Errorf("invalid media destination")
	}
	stem := strings.TrimSuffix(record.ROM, filepath.Ext(record.ROM))
	if record.Representation == "playlist-directory" {
		stem = record.ROM
	}
	return filepath.ToSlash(filepath.Join("downloaded_media", system, layout.Dir, filepath.FromSlash(stem)+extension)), nil
}

// CheckDocuments pins selected catalog evidence even when a cached game tree was
// already loaded. External edits require a fresh resolver rather than stale output.
func (r *ExportResolver) CheckDocuments(plan *Plan) error {
	checked := map[string]bool{}
	for _, record := range plan.Records {
		if record.Game == "" {
			continue
		}
		for _, file := range []string{"game.yaml", "fingerprints.yaml"} {
			relative := record.Game + "/" + file
			if checked[relative] {
				continue
			}
			checked[relative] = true
			expected := r.DocumentDigests[relative]
			if r.arcade != nil && file == "game.yaml" {
				if g := r.arcade.Games[filepath.Join(r.Root, filepath.FromSlash(record.Game))]; g != nil {
					expected = g.documentDigest
				}
			}
			if expected == "" {
				continue
			}
			_, digest, err := lookupDocument(r.Root, filepath.Join(r.Root, filepath.FromSlash(relative)))
			if err != nil {
				return err
			}
			if digest != expected {
				return fmt.Errorf("catalog changed behind export index; finish writes and restart API")
			}
		}
	}
	return nil
}

func SystemPlatform(system string) (string, bool) {
	if spec, ok := systems[system]; ok {
		return spec.Platform, true
	}
	names := platformnames.DefaultRegistry()
	entry, ok := names.Entry(system)
	return entry.Name, ok && !exportExcluded(entry.Name)
}

// FinalizeExport recomputes public counts without trusting network aggregates.
func FinalizeExport(plan *Plan) error {
	plan.Matched, plan.HashMatches, plan.NameMatches, plan.GameOnly = 0, 0, 0, 0
	plan.Ambiguous, plan.Unmatched, plan.Unsupported, plan.MediaFiles = 0, 0, 0, 0
	plan.MediaBytes = 0
	return finishExportPlan(plan)
}

// IsEmptyExport identifies a directory without inputs for its selected reader.
func IsEmptyExport(err error) bool { return errors.Is(err, errNoInputs) }

// NeedsArcadeSnapshot identifies an unresolved saved MAME catalog selection.
func NeedsArcadeSnapshot(err error) bool { return errors.Is(err, errArcadeSnapshot) }
