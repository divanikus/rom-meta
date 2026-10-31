package exporter

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/zipnames"
)

const maxPayload = 512 << 20

type game struct {
	context        context.Context
	mediaStyles    map[string]mediaStyle
	mediaReasons   map[string]MediaReason
	path, name     string
	root           *yaml.Node
	documentDigest string
}
type fingerprint struct {
	chd                    bool
	game                   *game
	release, variant       string
	kind                   string
	size                   int64
	sha1, sha256, md5, crc string
}
type hashes struct {
	size                   int64
	sha1, sha256, md5, crc string
}

func Build(options Options) (*Plan, error) {
	target, err := NormalizeTarget(options.Target)
	if err != nil {
		return nil, err
	}
	if options.ROMRoot == "" || options.Out == "" || options.Platform == "" {
		return nil, fmt.Errorf("--rom-root, --out and --platform are required")
	}
	if options.Limit < 0 {
		return nil, fmt.Errorf("--limit must be non-negative")
	}
	root, err := realPath(options.DataRoot)
	if err != nil {
		return nil, err
	}
	romRoot, err := realPath(options.ROMRoot)
	if err != nil {
		return nil, err
	}
	if info, err := catalogfs.Stat(romRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("ROM root must be a directory")
	}
	out, err := prospectivePath(options.Out)
	if err != nil {
		return nil, err
	}
	if within(root, out) || within(out, root) || within(romRoot, out) || within(out, romRoot) {
		return nil, fmt.Errorf("output must be separate from the catalog and ROM trees")
	}
	selector := options.selector
	if selector == nil {
		selector, err = LoadPlatformSelector(root)
		if err != nil {
			return nil, err
		}
	}
	selection, platform := options.selection, options.selectedCatalog
	if selection == nil {
		selection, platform, err = selector.Single(options.Platform, romRoot)
		if err != nil {
			return nil, err
		}
	}
	system := selection.System
	if options.System != "" {
		system, err = systemFor(selection.Name, options.System)
		if err != nil {
			return nil, err
		}
		copySelection := *selection
		copySelection.System = system
		selection = &copySelection
	}
	platformReal, err := realPath(filepath.Join(root, "platforms", platform))
	if err != nil {
		return nil, err
	}
	if !within(root, platformReal) {
		return nil, fmt.Errorf("catalog platform escapes data root")
	}
	options.System = system
	profile, err := loadProfile(root, options.ProfilePath)
	if err != nil {
		return nil, err
	}
	var index *catalogIndex
	var arcade *arcadeIndex
	if systems[system].Arcade {
		arcade, err = loadArcadeIndex(root, options.ArcadeSnapshot)
	} else {
		index = newCatalogIndex()
		for _, folder := range selector.Scope(platform) {
			part, e := buildIndex(root, filepath.Join(root, "platforms", folder), options.Progress)
			if e != nil {
				return nil, e
			}
			mergeCatalogIndex(index, part)
		}
	}
	if err != nil {
		return nil, err
	}
	inputs, err := ObserveExport(options)
	if err != nil {
		return nil, err
	}
	resolver := &ExportResolver{Root: root, Platform: platform, PlatformName: selection.Name, System: system, index: index, arcade: arcade}
	plan, err := resolver.Resolve(inputs, profile)
	if err != nil {
		return nil, err
	}
	upgraded, changed, err := HashRequiredInputs(options, inputs, plan.Records)
	if err != nil {
		return nil, err
	}
	if changed {
		plan, err = resolver.Resolve(upgraded, profile)
		if err != nil {
			return nil, err
		}
	}
	plan.ROMRoot, plan.Out, plan.Context = romRoot, out, options.Context
	plan.PlatformSelection = selection
	if err := ProjectTarget(plan, target); err != nil {
		return nil, err
	}
	return plan, nil
}

func buildIndex(root, platform string, progress func(string, string, int, int)) (*catalogIndex, error) {
	index := &catalogIndex{BySize: map[int64][]fingerprint{}, ByName: map[string][]fingerprint{}, GamesByName: map[string][]*game{}}
	entries, err := catalogfs.ReadDir(platform)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || catalogrules.IsIgnoredGameFolder(entry.Name()) {
			continue
		}
		if progress != nil {
			progress("catalog", entry.Name(), i+1, len(entries))
		}
		path := filepath.Join(platform, entry.Name())
		real, err := realPath(path)
		if err != nil {
			return nil, err
		}
		if !within(root, real) {
			return nil, fmt.Errorf("game folder escapes catalog root")
		}
		path = real
		doc, err := readDocument(filepath.Join(path, "game.yaml"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if _, err := catalogyaml.Variants(doc); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		g := &game{path: path, name: field(doc, "title"), root: doc}
		if g.name == "" {
			g.name = entry.Name()
		}
		indexNames(index, g)
		fingerprints, err := readDocument(filepath.Join(path, "fingerprints.yaml"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		err = indexFingerprints(index, g, fingerprints)
		if err != nil {
			return nil, err
		}
	}
	return index, nil
}

func readDocument(path string) (*yaml.Node, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := catalogyaml.Parse(contents)
	if err != nil {
		return nil, err
	}
	return doc.Root(), nil
}
func field(node *yaml.Node, key string) string {
	return strings.TrimSpace(catalogyaml.Scalar(catalogyaml.Value(node, key)))
}

func fingerprintMatches(f fingerprint, h hashes) bool {
	return (f.sha1 != "" || f.sha256 != "") && (f.sha1 == "" || f.sha1 == h.sha1) && (f.sha256 == "" || f.sha256 == h.sha256) && (f.md5 == "" || f.md5 == h.md5) && (f.crc == "" || f.crc == h.crc)
}
func hashROM(path, system string) (hashes, string, error) {
	return hashROMContext(context.Background(), path, system)
}
func hashROMContext(ctx context.Context, path, system string) (hashes, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if strings.EqualFold(filepath.Ext(path), ".zip") && !systems[system].Arcade {
		archive, err := zip.OpenReader(path)
		if err != nil {
			return hashes{}, "", err
		}
		defer archive.Close()
		var payload *zip.File
		for _, member := range archive.File {
			if !member.FileInfo().IsDir() && payloadExtension(system, strings.ToLower(filepath.Ext(zipnames.Name(member)))) {
				if payload != nil {
					return hashes{}, "", fmt.Errorf("ZIP contains multiple platform payloads")
				}
				payload = member
			}
		}
		if payload == nil {
			return hashes{}, "", fmt.Errorf("ZIP has no platform payload")
		}
		if payload.UncompressedSize64 > maxPayload {
			return hashes{}, zipnames.Name(payload), fmt.Errorf("payload exceeds 512 MiB limit")
		}
		input, err := payload.Open()
		if err != nil {
			return hashes{}, zipnames.Name(payload), err
		}
		defer input.Close()
		h, err := hashStream(contextReader{ctx, input}, maxPayload)
		return h, zipnames.Name(payload), err
	}
	input, err := catalogfs.Open(path)
	if err != nil {
		return hashes{}, "", err
	}
	defer input.Close()
	limit := int64(maxPayload)
	if systems[system].Disc || systems[system].Arcade || genericSystem(system) && (strings.EqualFold(filepath.Ext(path), ".chd") || strings.EqualFold(filepath.Ext(path), ".iso")) {
		limit = 0
	}
	h, err := hashStream(contextReader{ctx, input}, limit)
	return h, "", err
}
func hashStream(input io.Reader, limit int64) (hashes, error) {
	a, b, c, d := sha1.New(), sha256.New(), md5.New(), crc32.NewIEEE()
	if limit > 0 {
		input = io.LimitReader(input, limit+1)
	}
	size, err := io.Copy(io.MultiWriter(a, b, c, d), input)
	if err != nil {
		return hashes{}, err
	}
	if limit > 0 && size > limit {
		return hashes{}, fmt.Errorf("payload exceeds 512 MiB limit")
	}
	return hashes{size: size, sha1: fmt.Sprintf("%x", a.Sum(nil)), sha256: fmt.Sprintf("%x", b.Sum(nil)), md5: fmt.Sprintf("%x", c.Sum(nil)), crc: fmt.Sprintf("%08x", d.Sum32())}, nil
}

func countInputs(options Options, root string) (int, error) {
	count := 0
	err := catalogfs.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		playlistDir := entry.IsDir() && playlistSystem(options.System) && ext == ".m3u"
		group, e := romsetEntry(root, path, entry, options.LegacyOnly)
		if e != nil {
			return e
		}
		if entry.IsDir() && !playlistDir && !group {
			return nil
		}
		if !playlistDir && !group && !inputExtension(options.System, ext) || catalogrules.IsBIOSName(entry.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if options.Match != "" && !strings.Contains(strings.ToLower(rel), strings.ToLower(options.Match)) {
			if playlistDir || group {
				return fs.SkipDir
			}
			return nil
		}
		count++
		if options.Limit > 0 && count >= options.Limit {
			return fs.SkipAll
		}
		if playlistDir || group {
			return fs.SkipDir
		}
		return nil
	})
	return count, err
}

func indexFingerprints(index *catalogIndex, g *game, fingerprints *yaml.Node) error {
	if _, err := catalogyaml.Variants(g.root); err != nil {
		return err
	}
	items := catalogyaml.Value(fingerprints, "items")
	if items == nil {
		items = catalogyaml.Value(fingerprints, "roms")
	}
	return catalogyaml.Sequence(items, func(node *yaml.Node) error {
		release, variant, err := catalogyaml.FingerprintTarget(g.root, node)
		if err != nil {
			return err
		}
		file := catalogyaml.Value(node, "file")
		fp := fingerprint{game: g, release: release, variant: variant, kind: field(file, "kind"),
			chd:  strings.EqualFold(filepath.Ext(field(file, "name")), ".chd"),
			size: catalogyaml.Int64(catalogyaml.Value(file, "size")),
			sha1: strings.ToLower(field(file, "sha1")), sha256: strings.ToLower(field(file, "sha256")),
			md5: strings.ToLower(field(file, "md5")), crc: strings.ToLower(field(file, "crc32"))}
		if variant == "" {
			addReleaseName(index, field(file, "name"), g, release)
			addReleaseName(index, field(catalogyaml.Value(node, "source"), "entry_name"), g, release)
		} else {
			addVariantName(index, field(file, "name"), g, variant)
			addVariantName(index, field(catalogyaml.Value(node, "source"), "entry_name"), g, variant)
		}
		if fp.kind == "chd-combined-sha1" {
			if index.ByCHD == nil {
				index.ByCHD = map[string][]fingerprint{}
			}
			index.ByCHD[fp.sha1] = append(index.ByCHD[fp.sha1], fp)
		} else if fp.size > 0 && (fp.sha1 != "" || fp.sha256 != "") {
			index.BySize[fp.size] = append(index.BySize[fp.size], fp)
		}
		return nil
	})
}
