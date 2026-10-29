package gamelist

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

var importedMediaName = regexp.MustCompile(`^(box-front|screenshot|gameplay-video|logo|fanart|manual)(?:-([0-9]+))?(\.unspecified\.und\.[a-f0-9]{12})?$`)

type mediaFile struct {
	name, role, hash string
	size             int64
	asset            asset
	existing, legacy bool
}
type mediaVariant struct {
	files []*mediaFile
	dest  string
}

// normalizeMedia allocates readable names per release, role and collection.
// Existing readable names stay stable as additional content arrives.
func normalizeMedia(root, game, source string, c *card) (added int, addedBytes int64, err error) {
	files := map[string]*mediaFile{}
	classify := func(name string) (string, bool, bool) {
		ext := path.Ext(name)
		if !strings.Contains("|.png|.jpg|.jpeg|.webp|.mp4|.pdf|", "|"+strings.ToLower(ext)+"|") {
			return "", false, false
		}
		base := strings.TrimSuffix(path.Base(name), ext)
		suffix := "." + source
		if !strings.HasSuffix(base, suffix) {
			return "", false, false
		}
		m := importedMediaName.FindStringSubmatch(strings.TrimSuffix(base, suffix))
		if m == nil {
			return "", false, false
		}
		return m[1], m[3] != "", true
	}
	var walk func(string) error
	walk = func(dir string) error {
		entries, e := catalogfs.ReadDir(filepath.Join(game, filepath.FromSlash(dir)))
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		for _, entry := range entries {
			name := path.Join(dir, entry.Name())
			if entry.IsDir() {
				if e = walk(name); e != nil {
					return e
				}
				continue
			}
			role, legacy, ok := classify(name)
			if !ok {
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("media is a symlink: %s", name)
			}
			hash, size, e := catalogDigest(filepath.Join(game, filepath.FromSlash(name)))
			if e != nil {
				return e
			}
			files[name] = &mediaFile{name: name, role: role, hash: hash, size: size, existing: true, legacy: legacy, asset: asset{input: filepath.Join(game, filepath.FromSlash(name)), digest: hash, size: size, catalog: true}}
		}
		return nil
	}
	if err = walk("media"); err != nil {
		return
	}
	for name, a := range c.media {
		role, legacy, ok := classify(name)
		if !ok {
			return 0, 0, fmt.Errorf("invalid imported media name: %s", name)
		}
		if old := files[name]; old != nil {
			if old.hash != a.digest {
				return 0, 0, fmt.Errorf("media collision: %s", name)
			}
			continue
		}
		files[name] = &mediaFile{name: name, role: role, hash: a.digest, size: a.size, asset: a, legacy: legacy}
	}
	groups := map[string][]*mediaFile{}
	for _, f := range files {
		key := path.Join(path.Dir(f.name), f.role)
		groups[key] = append(groups[key], f)
	}
	c.media = map[string]asset{}
	c.renames = map[string]string{}
	c.mediaHashes = map[string]string{}
	for name, f := range files {
		if f.existing {
			c.mediaHashes[name] = f.hash
		}
	}
	c.remove = nil
	rewrite := map[string]string{}
	for key, list := range groups {
		sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
		variants := []*mediaVariant{}
		byContent := map[string]*mediaVariant{}
		for _, f := range list {
			identity := f.hash + strings.ToLower(path.Ext(f.name))
			v := byContent[identity]
			if v == nil {
				v = &mediaVariant{}
				byContent[identity] = v
				variants = append(variants, v)
			}
			v.files = append(v.files, f)
		}
		used := map[string]bool{}
		ordinals := map[int]bool{}
		if len(variants) > 1 {
			for _, v := range variants {
				for _, f := range v.files {
					if f.existing && !f.legacy {
						stem := strings.TrimSuffix(path.Base(f.name), path.Ext(f.name))
						stem = strings.TrimSuffix(stem, "."+source)
						ordinal := 1
						if parts := importedMediaName.FindStringSubmatch(stem); parts != nil && parts[2] != "" {
							ordinal, _ = strconv.Atoi(parts[2])
						}
						if ordinals[ordinal] {
							continue
						}
						ordinals[ordinal] = true
						v.dest = f.name
						used[strings.ToLower(f.name)] = true
						break
					}
				}
			}
		}
		for _, v := range variants {
			sample := v.files[0]
			ext := strings.ToLower(path.Ext(sample.name))
			if v.dest == "" {
				if len(variants) == 1 {
					v.dest = key + "." + source + ext
				} else {
					for n := 1; ; n++ {
						candidate := fmt.Sprintf("%s-%02d.%s%s", key, n, source, ext)
						if !ordinals[n] && !used[strings.ToLower(candidate)] {
							v.dest = candidate
							ordinals[n] = true
							break
						}
					}
				}
				used[strings.ToLower(v.dest)] = true
			}
			if err = checkParents(root, filepath.Dir(filepath.Join(game, filepath.FromSlash(v.dest)))); err != nil {
				return
			}
			if info, e := catalogfs.Lstat(filepath.Join(game, filepath.FromSlash(v.dest))); e == nil && !info.Mode().IsRegular() {
				return 0, 0, fmt.Errorf("media destination is not a regular file: %s", v.dest)
			} else if e != nil && !os.IsNotExist(e) {
				return 0, 0, e
			}
			exists := false
			for _, f := range v.files {
				if f.existing {
					exists = true
				}
				rewrite[f.name] = v.dest
				if f.existing && f.name != v.dest {
					c.renames[f.name] = v.dest
				}
			}
			if !exists {
				added++
				addedBytes += sample.size
			}
			if dest := files[v.dest]; dest == nil || dest.hash != sample.hash {
				c.media[v.dest] = sample.asset
			}
		}
	}
	final := map[string]bool{}
	for _, dest := range rewrite {
		final[dest] = true
	}
	for old := range c.renames {
		if !final[old] {
			c.remove = append(c.remove, old)
		}
	}
	sort.Strings(c.remove)
	gameRel, e := filepath.Rel(root, game)
	if e != nil {
		return 0, 0, e
	}
	prefix := filepath.ToSlash(gameRel) + "/"
	var update func(*yaml.Node)
	update = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "source_file" || n.Content[i].Value == "source_path" {
					continue
				}
				update(n.Content[i+1])
			}
		} else if n.Kind == yaml.SequenceNode {
			for _, child := range n.Content {
				update(child)
			}
		} else if n.Kind == yaml.ScalarNode {
			if dest, ok := rewrite[n.Value]; ok {
				n.Value = dest
			} else if strings.HasPrefix(n.Value, prefix) {
				if dest, ok := rewrite[strings.TrimPrefix(n.Value, prefix)]; ok {
					n.Value = prefix + dest
				}
			}
		}
	}
	update(c.doc.Root())
	return
}

type MediaNamesOptions struct {
	DataRoot, Platform, Collection string
	Apply                          bool
	Progress                       func(string, int, int)
}
type MediaNamesResult struct {
	Inventory map[string]map[string]string `json:"inventory,omitempty"`
	Cards     int                          `json:"cards"`
	Renamed   int                          `json:"renamed"`
	Changes   map[string]map[string]string `json:"changes,omitempty"`
	Backup    string                       `json:"backup,omitempty"`
}

// NameMedia migrates only recognized media in the selected collection namespace.
func NameMedia(o MediaNamesOptions) (result MediaNamesResult, err error) {
	if !collectionPattern.MatchString(o.Collection) {
		return result, fmt.Errorf("--collection is required")
	}
	root, err := filepath.Abs(o.DataRoot)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	platforms, err := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if err != nil {
		return result, err
	}
	cards := map[string]*card{}
	updates := map[string][]byte{}
	result.Changes = map[string]map[string]string{}
	result.Inventory = map[string]map[string]string{}
	for pi, p := range platforms {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") || o.Platform != "" && p.Name() != o.Platform {
			continue
		}
		if o.Progress != nil {
			o.Progress(p.Name(), pi+1, len(platforms))
		}
		dir := filepath.Join(root, "platforms", p.Name())
		games, e := catalogfs.ReadDir(dir)
		if e != nil {
			return result, e
		}
		for _, g := range games {
			if !g.IsDir() || strings.HasPrefix(g.Name(), ".") {
				continue
			}
			game := filepath.Join(dir, g.Name())
			body, e := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return result, e
			}
			doc, e := catalogyaml.Parse(body)
			if e != nil {
				return result, e
			}
			source := "gamelist." + o.Collection
			if catalogyaml.Value(catalogyaml.Value(doc.Root(), "sources"), source) == nil {
				continue
			}
			c := &card{original: body, doc: doc, media: map[string]asset{}}
			if _, _, e = normalizeMedia(root, game, source, c); e != nil {
				return result, fmt.Errorf("%s: %w", game, e)
			}
			rel, _ := filepath.Rel(root, game)
			if len(c.mediaHashes) > 0 {
				result.Inventory[filepath.ToSlash(rel)] = c.mediaHashes
			}
			if len(c.renames) == 0 {
				continue
			}
			body, e = doc.Marshal()
			if e != nil {
				return result, e
			}
			cards[game] = c
			updates[game] = body
			result.Changes[filepath.ToSlash(rel)] = c.renames
			result.Cards++
			result.Renamed += len(c.renames)
		}
	}
	if o.Apply && len(updates) > 0 {
		result.Backup, err = publishCards(root, cards, updates, nil, o.Progress, nil)
		if err != nil {
			return result, err
		}
	}
	return
}
