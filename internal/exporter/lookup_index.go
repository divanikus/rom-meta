package exporter

// The optional index contains recognition summaries, not authoritative metadata.
// Source ZIP/YAML files remain independently checked when a selection is made.
import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogyaml"
)

type LookupIndexStats struct{ Games, Cached, Rebuilt int }
type lookupFingerprint struct {
	Release, Variant       string
	Kind                   string
	CHD                    bool
	Size                   int64
	SHA1, SHA256, MD5, CRC string
}
type lookupEntry struct {
	SchemaVersion                 int
	VariantNames                  map[string][]string
	Title                         string
	GameNames                     []string
	ReleaseNames                  map[string][]string
	Fingerprints                  []lookupFingerprint
	GameDigest, FingerprintDigest string
}
type lookupRow struct {
	stamp string
	entry lookupEntry
}
type lookupDisk struct {
	db   *sql.DB
	rows map[string]lookupRow
}

// Recognition summaries are portable with a platform. The optional external cache
// directory uses catalog-scoped filenames instead. The SQL table is versioned.
// Cache failures are optimization failures: callers fall back to source files.
func openLookupDisk(dir, root, platform string) *lookupDisk {
	if dir == "-" {
		return nil
	}
	var name string
	if dir == "" {
		name = filepath.Join(root, "platforms", platform, "_index.sqlite")
	} else {
		sum := sha256.Sum256([]byte(root + "\x00" + platform))
		name = filepath.Join(dir, "platform-v1-"+hex.EncodeToString(sum[:16])+".sqlite")
	}
	_ = os.MkdirAll(filepath.Dir(name), 0700)
	if info, err := os.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return nil
	}
	db, err := sql.Open("sqlite", name)
	if err != nil {
		return nil
	}
	db.SetMaxOpenConns(1)
	fail := func() *lookupDisk { db.Close(); return nil }
	if _, err = db.Exec("PRAGMA busy_timeout=250"); err != nil {
		return fail()
	}
	// Existing indexes can be used from a read-only catalog mount.
	var present int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='lookup_bundles_v1'").Scan(&present); err != nil {
		return fail()
	}
	if present == 0 {
		if _, err = db.Exec("CREATE TABLE lookup_bundles_v1 (folder TEXT PRIMARY KEY, stamp TEXT NOT NULL, payload BLOB NOT NULL, checksum TEXT NOT NULL)"); err != nil {
			return fail()
		}
	}
	rows, err := db.Query("SELECT folder, stamp, payload, checksum FROM lookup_bundles_v1")
	if err != nil {
		return fail()
	}
	c := &lookupDisk{db: db, rows: map[string]lookupRow{}}
	for rows.Next() {
		var folder, stamp, checksum string
		var payload []byte
		if err = rows.Scan(&folder, &stamp, &payload, &checksum); err != nil {
			break
		}
		digest := sha256.Sum256(payload)
		var entry lookupEntry
		if hex.EncodeToString(digest[:]) != checksum || json.Unmarshal(payload, &entry) != nil || entry.SchemaVersion != 2 {
			continue
		}
		c.rows[folder] = lookupRow{stamp, entry}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return fail()
	}
	return c
}
func (c *lookupDisk) save(updates map[string]lookupRow, seen map[string]bool) error {
	if c == nil {
		return fmt.Errorf("index storage unavailable")
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for folder, row := range updates {
		payload, err := json.Marshal(row.entry)
		if err != nil {
			return err
		}
		checksum := sha256.Sum256(payload)
		if _, err = tx.Exec("INSERT OR REPLACE INTO lookup_bundles_v1(folder,stamp,payload,checksum) VALUES(?,?,?,?)", folder, row.stamp, payload, hex.EncodeToString(checksum[:])); err != nil {
			return err
		}
	}
	for folder := range c.rows {
		if !seen[folder] {
			if _, err = tx.Exec("DELETE FROM lookup_bundles_v1 WHERE folder=?", folder); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// RefreshPlatformIndex is command-finalization maintenance, independent of
// runtime ROM reader capabilities. The payload catalog remains authoritative.
func RefreshPlatformIndex(root, platform string) error {
	actual, err := realPath(root)
	if err != nil {
		return err
	}
	c := LookupCatalog{Root: actual, requireIndexWrite: true, Platforms: []LookupPlatform{{Platform: platform, Supported: true}}}
	_, err = c.Open(platform)
	return err
}

// This is deliberately a stat pass, not a ZIP-directory pass. Fixed TorrentZip
// member dates are irrelevant: the physical metadata container is observed.
// Equal-size edits preserving physical mtime are outside this cache guard, as
// with the existing disposable catalog cache; selected documents remain SHA-bound.
func lookupBundleStamp(root, path string, packed bool) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%t", filepath.Base(path), packed)
	names := []string{"game.yaml", "fingerprints.yaml"}
	if packed {
		names = append([]string{"metadata.zip"}, names...)
	}
	for _, name := range names {
		info, err := os.Lstat(filepath.Join(path, name))
		if os.IsNotExist(err) {
			fmt.Fprintf(&b, "|%s:missing", name)
			continue
		}
		if err != nil {
			return "", err
		}
		if name != "metadata.zip" && info.Mode()&os.ModeSymlink != 0 {
			actual, err := filepath.EvalSymlinks(filepath.Join(path, name))
			if err != nil {
				return "", err
			}
			if !within(root, actual) {
				return "", fmt.Errorf("catalog document escapes root")
			}
			info, err = os.Stat(actual)
			if err != nil {
				return "", err
			}
			relative, _ := filepath.Rel(root, actual)
			fmt.Fprintf(&b, "|link:%s", filepath.ToSlash(relative))
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("non-regular metadata file: %s", filepath.Join(path, name))
		}
		fmt.Fprintf(&b, "|%s:%d:%d", name, info.Size(), info.ModTime().UnixNano())
	}
	return b.String(), nil
}

// Windows/SMB timestamps use 100 ns precision. Compare persisted Linux stamps
// at that precision so portable indexes do not rebuild solely for lost digits.
// Keep exact stamps for before/after write checks; selected YAML retains SHA checks.
func portableLookupStamp(stamp string) string {
	parts := strings.Split(stamp, "|")
	for i, part := range parts {
		fields := strings.Split(part, ":")
		if len(fields) != 3 || (fields[0] != "metadata.zip" && fields[0] != "game.yaml" && fields[0] != "fingerprints.yaml") {
			continue
		}
		ns, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		fields[2] = strconv.FormatInt(ns-ns%100, 10)
		parts[i] = strings.Join(fields, ":")
	}
	return strings.Join(parts, "|")
}

func lookupDocument(root, name string) (*yaml.Node, string, error) {
	actual, err := realPath(name)
	if err != nil {
		return nil, "", err
	}
	if !within(root, actual) {
		return nil, "", fmt.Errorf("catalog document escapes root")
	}
	data, err := catalogfs.ReadFile(actual)
	if err != nil {
		return nil, "", err
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return doc.Root(), hex.EncodeToString(sum[:]), nil
}
func readLookupEntry(root, path string) (lookupEntry, error) {
	entry := lookupEntry{SchemaVersion: 2, ReleaseNames: map[string][]string{}, VariantNames: map[string][]string{}}
	doc, digest, err := lookupDocument(root, filepath.Join(path, "game.yaml"))
	if os.IsNotExist(err) {
		return entry, nil
	}
	if err != nil {
		return entry, err
	}
	if _, err := catalogyaml.Variants(doc); err != nil {
		return entry, err
	}
	entry.GameDigest = digest
	g := &game{path: path, name: field(doc, "title"), root: doc}
	if g.name == "" {
		g.name = filepath.Base(path)
	}
	entry.Title = g.name
	index := newCatalogIndex()
	indexNames(index, g)
	fps, digest, err := lookupDocument(root, filepath.Join(path, "fingerprints.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return entry, err
	}
	if err == nil {
		entry.FingerprintDigest = digest
		if err = indexFingerprints(index, g, fps); err != nil {
			return entry, err
		}
	}
	for key, games := range index.GamesByName {
		for range games {
			entry.GameNames = append(entry.GameNames, key)
		}
	}
	for key, fps := range index.ByName {
		for _, fp := range fps {
			if fp.variant != "" {
				entry.VariantNames[key] = append(entry.VariantNames[key], fp.variant)
			} else {
				entry.ReleaseNames[key] = append(entry.ReleaseNames[key], fp.release)
			}
		}
	}
	for _, fps := range index.BySize {
		for _, fp := range fps {
			entry.Fingerprints = append(entry.Fingerprints, lookupFingerprint{Release: fp.release, Variant: fp.variant, Kind: fp.kind, CHD: fp.chd, Size: fp.size, SHA1: fp.sha1, SHA256: fp.sha256, MD5: fp.md5, CRC: fp.crc})
		}
	}
	for _, fps := range index.ByCHD {
		for _, fp := range fps {
			entry.Fingerprints = append(entry.Fingerprints, lookupFingerprint{Release: fp.release, Variant: fp.variant, Kind: fp.kind, CHD: true, Size: fp.size, SHA1: fp.sha1})
		}
	}
	return entry, nil
}
func newCatalogIndex() *catalogIndex {
	return &catalogIndex{BySize: map[int64][]fingerprint{}, ByName: map[string][]fingerprint{}, GamesByName: map[string][]*game{}}
}
func (e lookupEntry) add(index *catalogIndex, path string) *game {
	if e.GameDigest == "" {
		return nil
	}
	g := &game{path: path, name: e.Title, documentDigest: e.GameDigest}
	for _, key := range e.GameNames {
		index.GamesByName[key] = append(index.GamesByName[key], g)
	}
	for key, releases := range e.ReleaseNames {
		for _, release := range releases {
			index.ByName[key] = append(index.ByName[key], fingerprint{game: g, release: release})
		}
	}
	for name, variants := range e.VariantNames {
		for _, variant := range variants {
			index.ByName[name] = append(index.ByName[name], fingerprint{game: g, variant: variant})
		}
	}
	for _, fp := range e.Fingerprints {
		if fp.Kind == "chd-combined-sha1" {
			if index.ByCHD == nil {
				index.ByCHD = map[string][]fingerprint{}
			}
			index.ByCHD[fp.SHA1] = append(index.ByCHD[fp.SHA1], fingerprint{game: g, variant: fp.Variant, kind: fp.Kind, chd: true, size: fp.Size, sha1: fp.SHA1})
			continue
		}
		index.BySize[fp.Size] = append(index.BySize[fp.Size], fingerprint{game: g, release: fp.Release, variant: fp.Variant, chd: fp.CHD, size: fp.Size, sha1: fp.SHA1, sha256: fp.SHA256, md5: fp.MD5, crc: fp.CRC})
	}
	return g
}
func (catalog *LookupCatalog) openIndex(resolver *LookupResolver, folder string) error {
	packed, err := catalogfs.PlatformLayout(folder)
	if err != nil {
		return err
	}
	entries, err := catalogfs.ReadDir(folder)
	if err != nil {
		return err
	}
	cache := openLookupDisk(catalog.CacheDir, catalog.Root, filepath.Base(folder))
	if cache != nil {
		defer cache.db.Close()
	} else if catalog.requireIndexWrite {
		return fmt.Errorf("cannot create or open platform index")
	}
	updates := map[string]lookupRow{}
	seen := map[string]bool{}
	if resolver.index == nil {
		resolver.index = newCatalogIndex()
	}
	for _, item := range entries {
		if !item.IsDir() || strings.HasPrefix(item.Name(), ".") || strings.HasPrefix(item.Name(), "_") || catalogrules.IsIgnoredGameFolder(item.Name()) {
			continue
		}
		path, err := realPath(filepath.Join(folder, item.Name()))
		if err != nil {
			return err
		}
		if !within(catalog.Root, path) {
			return fmt.Errorf("game folder escapes catalog root")
		}
		before, err := lookupBundleStamp(catalog.Root, path, packed)
		if err != nil {
			return err
		}
		seen[item.Name()] = true
		resolver.IndexStats.Games++
		var entry lookupEntry
		row, ok := lookupRow{}, false
		if cache != nil {
			row, ok = cache.rows[item.Name()]
		}
		if ok && portableLookupStamp(row.stamp) == portableLookupStamp(before) {
			entry = row.entry
			resolver.IndexStats.Cached++
		} else {
			entry, err = readLookupEntry(catalog.Root, path)
			if err != nil {
				return fmt.Errorf("%s: %w", item.Name(), err)
			}
			after, err := lookupBundleStamp(catalog.Root, path, packed)
			if err != nil || before != after {
				return fmt.Errorf("metadata changed while indexing %s", item.Name())
			}
			updates[item.Name()] = lookupRow{before, entry}
			resolver.IndexStats.Rebuilt++
		}
		if game := entry.add(resolver.index, path); game != nil {
			resolver.games = append(resolver.games, game)
		}
		relative, _ := filepath.Rel(catalog.Root, path)
		if entry.GameDigest != "" {
			resolver.DocumentDigests[filepath.ToSlash(filepath.Join(relative, "game.yaml"))] = entry.GameDigest
		}
		if entry.FingerprintDigest != "" {
			resolver.DocumentDigests[filepath.ToSlash(filepath.Join(relative, "fingerprints.yaml"))] = entry.FingerprintDigest
		}
	}
	err = cache.save(updates, seen)
	if catalog.requireIndexWrite {
		return err
	}
	return nil
}
