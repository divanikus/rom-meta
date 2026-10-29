// Package catalogfs keeps catalog paths logical while platform game bundles use
// ordinary ZIP files. Non-catalog paths and unmarked platforms retain OS behavior.
package catalogfs

import (
	"archive/zip"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"rommeta/internal/medianames"
	"rommeta/internal/zipnames"
)

const Marker = "catalog-storage.yaml"
const MarkerContent = "version: 1\nlayout: game-source-zip\narchive: torrentzip\n"
const MaxMemberSize int64 = 512 << 20

type location struct {
	platform, game, member string
	packed                 bool
}

// PlatformLayout validates a platform's explicit marker. Absent means loose.
func PlatformLayout(platform string) (bool, error) {
	if _, e := os.Lstat(filepath.Join(platform, "catalog-conversion.pending")); e == nil {
		return false, fmt.Errorf("catalog conversion incomplete: %s", platform)
	} else if !os.IsNotExist(e) {
		return false, e
	}
	name := filepath.Join(platform, Marker)
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return false, fmt.Errorf("invalid storage marker: %s", name)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return false, err
	}
	var m struct {
		Version int    `yaml:"version"`
		Layout  string `yaml:"layout"`
		Archive string `yaml:"archive"`
	}
	if err = yaml.Unmarshal(b, &m); err != nil {
		return false, err
	}
	if m.Version != 1 || m.Layout != "game-source-zip" || m.Archive != "" && m.Archive != "torrentzip" {
		return false, fmt.Errorf("unsupported catalog storage in %s", name)
	}
	return true, nil
}

func locate(name string) (location, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return location{}, err
	}
	// All catalog callers use <root>/platforms/<platform>/<game>/... . This
	// also covers their existing private platform transaction directories.
	slash := filepath.ToSlash(abs)
	i := strings.LastIndex(slash, "/platforms/")
	if i < 0 {
		return location{}, nil
	}
	base := slash[:i+len("/platforms/")]
	parts := strings.Split(slash[len(base):], "/")
	if len(parts) < 2 {
		return location{}, nil
	}
	platform := filepath.FromSlash(base + parts[0])
	packed, err := PlatformLayout(platform)
	if err != nil {
		return location{}, err
	}
	member := strings.Join(parts[2:], "/")
	return location{platform: platform, game: filepath.Join(platform, parts[1]), member: member, packed: packed}, nil
}

// Packed reports whether a logical game file belongs to a marked platform.
func Packed(name string) (bool, error) {
	l, e := locate(name)
	return l.packed && l.member != "" && !isPack(l.member), e
}

func zipName(f *zip.File) string { return zipnames.Name(f) }

func safeMember(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		if p == "" || p == "." || p == ".." || strings.TrimRight(p, " .") != p {
			return false
		}
	}
	return true
}
func isPack(name string) bool {
	if name == "metadata.zip" {
		return true
	}
	for _, prefix := range []string{"artwork.", "video.", "media."} {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".zip") {
			return true
		}
	}
	return false
}

// PackName retains the existing filename's source token; unlabelled content
// gets an explicitly unattributed pack, never an invented free/local source.
func PackName(member string) (string, error) {
	if !safeMember(member) || isPack(member) || member == Marker {
		return "", fmt.Errorf("reserved or unsafe member %q", member)
	}
	if !strings.HasPrefix(member, "media/") {
		return "metadata.zip", nil
	}
	name := path.Base(member)
	stem := strings.TrimSuffix(name, path.Ext(name))
	parts := medianames.SourceParts(strings.Split(stem, "."))
	source := "unattributed"
	if len(parts) > 1 {
		source = parts[len(parts)-1]
	}
	if source == "" {
		return "", fmt.Errorf("empty media source: %s", member)
	}
	for _, r := range strings.TrimPrefix(source, "gamelist.") {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("unsupported source token in %s", member)
		}
	}
	kind := strings.Split(parts[0], "-")[0]
	group := "artwork"
	if strings.Contains(parts[0], "video") {
		kind = "video"
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".webm", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".mpg", ".mpeg":
		kind = "video"
	}
	switch kind {
	case "video":
		group = "video"
	case "manual", "audio", "music", "soundtrack":
		group = "media"
	}
	// Unknown non-image content stays in the generic media family.
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
	default:
		if group == "artwork" {
			group = "media"
		}
	}
	return group + "." + source + ".zip", nil
}

type member struct {
	name, pack string
	header     zip.FileHeader
	offset     int64 // Negative for media: resolve its local header only on Open.
	physical   os.FileInfo
}
type gameIndex struct {
	members map[string]*member
	dirs    map[string]bool
	packs   map[string]os.FileInfo
}

func samePhysical(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func inventory(game string, only ...string) (*gameIndex, error) {
	packs := map[string]os.FileInfo{}
	key := game
	if len(only) != 0 {
		key += "\x00" + only[0]
		info, err := os.Lstat(filepath.Join(game, only[0]))
		if err == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("non-regular pack: %s", only[0])
			}
			packs[only[0]] = info
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(game)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !isPack(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("non-regular pack: %s", e.Name())
			}
			packs[e.Name()] = info
		}
	}
	if old, ok := indexes.Load(key); ok {
		idx := old.(*gameIndex)
		equal := len(packs) == len(idx.packs)
		for n, s := range packs {
			if !samePhysical(s, idx.packs[n]) {
				equal = false
			}
		}
		if equal {
			return idx, nil
		}
	}
	idx := &gameIndex{members: map[string]*member{}, dirs: map[string]bool{"": true}, packs: packs}
	folded := map[string]string{}
	components := map[string]string{}
	names := make([]string, 0, len(packs))
	for n := range packs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		z, err := zip.OpenReader(filepath.Join(game, name))
		if err != nil {
			return nil, err
		}
		parseErr := func() error {
			for _, f := range z.File {
				n := strings.TrimSuffix(zipName(f), "/")
				if !safeMember(n) || !f.Mode().IsRegular() && !f.FileInfo().IsDir() {
					return fmt.Errorf("unsafe ZIP member %q in %s", f.Name, name)
				}
				if f.FileInfo().IsDir() {
					idx.dirs[n] = true
					continue
				}
				want, err := PackName(n)
				if err != nil {
					return err
				}
				if want != name {
					return fmt.Errorf("member %s belongs in %s, found in %s", n, want, name)
				}
				for q := n; q != "."; q = path.Dir(q) {
					key := strings.ToLower(q)
					if prior, ok := components[key]; ok && prior != q {
						return fmt.Errorf("case-colliding ZIP paths: %s / %s", prior, q)
					}
					components[key] = q
				}
				key := strings.ToLower(n)
				if prev, ok := folded[key]; ok {
					return fmt.Errorf("duplicate/case-colliding ZIP members: %s / %s", prev, n)
				}
				folded[key] = n
				if f.Method != zip.Store && f.Method != zip.Deflate {
					return fmt.Errorf("unsupported ZIP method %d for %s", f.Method, n)
				}
				if f.Flags&1 != 0 || f.UncompressedSize64 > uint64(MaxMemberSize) {
					return fmt.Errorf("encrypted or oversized ZIP member: %s", n)
				}
				// Media inventories need central-directory metadata only. Seeking
				// to every local header amplifies HDD/ZFS reads across large packs.
				// Keep metadata offsets/stamps stable for existing recognition indexes.
				off := int64(-1)
				if name == "metadata.zip" {
					off, err = f.DataOffset()
					if err != nil {
						return err
					}
				}
				idx.members[n] = &member{name: n, pack: name, header: f.FileHeader, offset: off, physical: packs[name]}
				for d := path.Dir(n); d != "."; d = path.Dir(d) {
					idx.dirs[d] = true
				}
			}
			return nil
		}()
		closeErr := z.Close()
		if parseErr != nil {
			return nil, parseErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	for n := range idx.members {
		if idx.dirs[n] {
			return nil, fmt.Errorf("ZIP file/directory collision: %s", n)
		}
	}
	indexes.Store(key, idx)
	return idx, nil
}

type fileInfo struct {
	m        *member
	name     string
	dir      bool
	modified time.Time
}

func (f *fileInfo) Name() string { return f.name }
func (f *fileInfo) Size() int64 {
	if f.m == nil {
		return 0
	}
	return int64(f.m.header.UncompressedSize64)
}
func (f *fileInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0755
	}
	return 0644
}
func (f *fileInfo) ModTime() time.Time {
	if f.m != nil {
		return f.m.physical.ModTime()
	}
	return f.modified
}
func (f *fileInfo) IsDir() bool { return f.dir }
func (f *fileInfo) Sys() any    { return nil }

// SameFile binds a logical member to its container identity and directory entry.
func SameFile(a, b os.FileInfo) bool {
	x, xok := a.(*fileInfo)
	y, yok := b.(*fileInfo)
	if !xok && !yok {
		return os.SameFile(a, b)
	}
	if !xok || !yok || x.m == nil || y.m == nil {
		return false
	}
	return x.m.name == y.m.name && x.m.pack == y.m.pack && x.m.header.CRC32 == y.m.header.CRC32 && x.m.header.UncompressedSize64 == y.m.header.UncompressedSize64 && x.m.offset == y.m.offset && samePhysical(x.m.physical, y.m.physical)
}

// CacheStamp includes container/member stamps; ZIP member dates may be fixed.
func CacheStamp(info os.FileInfo) int64 {
	f, ok := info.(*fileInfo)
	if !ok || f.m == nil {
		return info.ModTime().UnixNano()
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "zip-v1:%s:%d:%d:%d:%d:%d", f.m.pack, f.m.physical.Size(), f.m.physical.ModTime().UnixNano(), f.m.offset, f.m.header.CRC32, f.m.header.UncompressedSize64)
	return int64(h.Sum64())
}

func stat(name string, follow bool) (os.FileInfo, error) {
	l, err := locate(name)
	if err != nil {
		return nil, err
	}
	if !l.packed || l.member == "" {
		if follow {
			return os.Stat(name)
		}
		return os.Lstat(name)
	}
	idx, err := memberInventory(l)
	if err != nil {
		return nil, err
	}
	physical, e := os.Lstat(name)
	if m, ok := idx.members[l.member]; ok {
		if e == nil {
			return nil, fmt.Errorf("loose/ZIP duplicate: %s", name)
		}
		if !os.IsNotExist(e) {
			return nil, e
		}
		return &fileInfo{m: m, name: path.Base(l.member)}, nil
	}
	if idx.dirs[l.member] {
		if e == nil && !physical.IsDir() {
			return nil, fmt.Errorf("loose/ZIP directory collision: %s", name)
		}
		return &fileInfo{name: path.Base(l.member), dir: true}, nil
	}
	if e == nil {
		if follow {
			return os.Stat(name)
		}
		return physical, nil
	}
	return nil, e
}
func Stat(name string) (os.FileInfo, error)  { return stat(name, true) }
func Lstat(name string) (os.FileInfo, error) { return stat(name, false) }

func ReadDir(name string) ([]os.DirEntry, error) {
	l, err := locate(name)
	if err != nil {
		return nil, err
	}
	if !l.packed {
		entries, err := os.ReadDir(name)
		if err != nil {
			return nil, err
		}
		if filepath.Base(filepath.Clean(name)) == "platforms" {
			out := entries[:0]
			for _, e := range entries {
				if IsBackupPlatform(e.Name()) {
					continue
				}
				if e.IsDir() {
					if _, pendingErr := os.Lstat(filepath.Join(name, e.Name(), "catalog-conversion.pending")); pendingErr == nil {
						continue
					} else if !os.IsNotExist(pendingErr) {
						return nil, pendingErr
					}
				}
				out = append(out, e)
			}
			entries = out
		}
		return entries, nil
	}
	idx, err := inventory(l.game)
	if err != nil {
		return nil, err
	}
	if l.member != "" && !idx.dirs[l.member] {
		return os.ReadDir(name)
	}
	found := map[string]os.DirEntry{}
	physical, err := os.ReadDir(name)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range physical {
		if l.member == "" && (isPack(e.Name()) || e.Name() == ".rommeta-write-lock" || strings.HasPrefix(e.Name(), "rommeta-pack-")) {
			continue
		}
		found[e.Name()] = e
	}
	prefix := l.member
	if prefix != "" {
		prefix += "/"
	}
	add := func(n string, info os.FileInfo) error {
		if !strings.HasPrefix(n, prefix) {
			return nil
		}
		tail := strings.TrimPrefix(n, prefix)
		if tail == "" || strings.Contains(tail, "/") {
			return nil
		}
		if old, ok := found[tail]; ok && (!old.IsDir() || !info.IsDir()) {
			return fmt.Errorf("loose/ZIP duplicate: %s", filepath.Join(name, tail))
		}
		found[tail] = fs.FileInfoToDirEntry(info)
		return nil
	}
	for n, m := range idx.members {
		if err := add(n, &fileInfo{m: m, name: path.Base(n)}); err != nil {
			return nil, err
		}
	}
	for n := range idx.dirs {
		if err := add(n, &fileInfo{name: path.Base(n), dir: true}); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]os.DirEntry, 0, len(names))
	for _, n := range names {
		out = append(out, found[n])
	}
	return out, nil
}
func IsBackupPlatform(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".old") || strings.Contains(n, ".old.") || strings.HasSuffix(n, ".converting")
}

func WalkDir(root string, fn fs.WalkDirFunc) error {
	info, err := Lstat(root)
	if err != nil {
		return fn(root, nil, err)
	}
	var walk func(string, fs.DirEntry) error
	walk = func(n string, d fs.DirEntry) error {
		err := fn(n, d, nil)
		if err != nil {
			if err == fs.SkipDir && d.IsDir() {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		entries, e := ReadDir(n)
		if e != nil {
			return fn(n, d, e)
		}
		for _, entry := range entries {
			e = walk(filepath.Join(n, entry.Name()), entry)
			if e == fs.SkipDir {
				break
			}
			if e != nil {
				return e
			}
		}
		return nil
	}
	err = walk(root, fs.FileInfoToDirEntry(info))
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

// EvalSymlinks resolves the real game directory and retains the checked member
// path. No ZIP member is ever expanded to an arbitrary filesystem destination.
func EvalSymlinks(name string) (string, error) {
	l, err := locate(name)
	if err != nil {
		return "", err
	}
	if !l.packed || l.member == "" {
		return filepath.EvalSymlinks(name)
	}
	info, err := Lstat(name)
	if err != nil {
		return "", err
	}
	if _, ok := info.(*fileInfo); !ok {
		return filepath.EvalSymlinks(name)
	}
	game, err := filepath.EvalSymlinks(l.game)
	if err != nil {
		return "", err
	}
	return filepath.Join(game, filepath.FromSlash(l.member)), nil
}
