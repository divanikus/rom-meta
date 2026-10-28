package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/platformnames"
)

type gameChoice struct {
	Platform string   `json:"platform"`
	Game     string   `json:"game"`
	Title    string   `json:"title,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	NewGame  *newGame `json:"new_game,omitempty"`
	expected map[string]string
	names    []string
}
type outputOwner struct {
	Game             *gameChoice
	Variant, Release string
	File             fingerprint
}
type discoveryCatalog struct {
	options     options
	registry    platformnames.Registry
	platforms   []string
	loaded      map[string]bool
	games       map[string]*gameChoice
	full, names map[string][]*gameChoice
	owners      map[string][]outputOwner
	files       []*copyFile
}

func gameID(p, g string) string { return p + "/" + g }
func identityKey(p string, f fingerprint) string {
	n := f.Size
	if f.Kind == "chd-combined-sha1" {
		n = 0
	}
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", p, f.Kind, n, strings.ToLower(f.SHA1))
}
func titleBase(s string) string {
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
func uniqueGames(xs []*gameChoice) []*gameChoice {
	out := []*gameChoice{}
	seen := map[string]bool{}
	for _, x := range xs {
		if !seen[gameID(x.Platform, x.Game)] {
			seen[gameID(x.Platform, x.Game)] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return gameID(out[i].Platform, out[i].Game) < gameID(out[j].Platform, out[j].Game)
	})
	return out
}
func openDiscoveryCatalog(o options) (*discoveryCatalog, error) {
	reg, _, err := platformnames.LoadRegistry(o.Catalog)
	if err != nil {
		return nil, err
	}
	xs, err := catalogfs.ReadDir(filepath.Join(o.Catalog, "platforms"))
	if err != nil {
		return nil, err
	}
	c := &discoveryCatalog{options: o, registry: reg, loaded: map[string]bool{}, games: map[string]*gameChoice{}, full: map[string][]*gameChoice{}, names: map[string][]*gameChoice{}, owners: map[string][]outputOwner{}}
	for _, x := range xs {
		if x.IsDir() && safeName(x.Name()) && !strings.HasPrefix(x.Name(), ".") && !strings.HasPrefix(x.Name(), "_") {
			c.platforms = append(c.platforms, x.Name())
		}
	}
	sort.Strings(c.platforms)
	return c, nil
}

// Publisher platform labels are a small adapter table, separate from game
// matching. The catalog registry still resolves user/local platform aliases.
var tenPlatforms = map[string][]string{
	"Nintendo - Famicom":                       {"nes", "Nintendo - Nintendo Entertainment System"},
	"Nintendo - Nintendo Entertainment System": {"nes"},
	"Nintendo - Super Famicom":                 {"snes"},
	"Nintendo - Family Computer Disk System":   {"fds"},
	"Panasonic - 3DO Interactive Multiplayer":  {"3do"},
	"NEC - PC Engine":                          {"pce"},
	"NEC - PC Engine CD":                       {"pcecd"},
	"Sega - Mega Drive":                        {"megadrive"},
	"Sega - Master System":                     {"mastersystem"},
	"Sega - SG-1000":                           {"sg1000"},
}

var errCatalogPlatformMissing = errors.New("catalog platform not found")

func (c *discoveryCatalog) platform(s string) (string, error) {
	for _, p := range c.platforms {
		if strings.EqualFold(p, s) {
			return p, nil
		}
	}
	names := append([]string{s}, tenPlatforms[s]...)
	found := []string{}
	for _, p := range c.platforms {
		for _, candidate := range append([]string{p}, tenPlatforms[p]...) {
			for _, name := range names {
				if c.registry.ResolveCLI(candidate) == c.registry.ResolveCLI(name) {
					found = appendUnique(found, p)
				}
			}
		}
	}
	if len(found) == 0 {
		return "", fmt.Errorf("%w: %s", errCatalogPlatformMissing, s)
	}
	if len(found) != 1 {
		return "", fmt.Errorf("catalog platform not found or ambiguous: %s; import its platform or configure config/platforms.yaml", s)
	}
	return found[0], nil
}
func (c *discoveryCatalog) load(p string) error {
	if c.loaded[p] {
		return nil
	}
	if !safeName(p) {
		return fmt.Errorf("unsafe platform")
	}
	xs, err := catalogfs.ReadDir(filepath.Join(c.options.Catalog, "platforms", p))
	if err != nil {
		return err
	}
	add := func(m map[string][]*gameChoice, g *gameChoice, s string) {
		if strings.TrimSpace(s) != "" {
			m[p+"\x00"+tokenName(s)] = append(m[p+"\x00"+tokenName(s)], g)
			g.names = append(g.names, s)
		}
	}
	for _, x := range xs {
		if !x.IsDir() || !safeName(x.Name()) || strings.HasPrefix(x.Name(), "_") || strings.HasPrefix(x.Name(), ".") {
			continue
		}
		key := filepath.Join("platforms", p, x.Name())
		gf, err := planFile(c.options.Catalog, filepath.Join(key, "game.yaml"), true)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		ff, err := planFile(c.options.Catalog, filepath.Join(key, "fingerprints.yaml"), true)
		if err != nil {
			return err
		}
		gd, err := catalogyaml.Parse(gf.data)
		if err != nil {
			return err
		}
		fd, err := catalogyaml.Parse(ff.data)
		if err != nil {
			return err
		}
		for _, d := range []*catalogyaml.Document{gd, fd} {
			if err := rejectDuplicateYAML(d.Root()); err != nil {
				return err
			}
		}
		if err := checkBundle(gd, fd); err != nil {
			return err
		}
		g := &gameChoice{Platform: p, Game: x.Name(), Title: catalogyaml.Scalar(catalogyaml.Value(gd.Root(), "title")), expected: map[string]string{"game.yaml": gf.SourceSHA256, "fingerprints.yaml": ff.SourceSHA256}}
		c.games[gameID(p, x.Name())] = g
		c.files = append(c.files, gf, ff)
		add(c.names, g, titleBase(g.Game))
		add(c.names, g, titleBase(g.Title))
		for _, k := range []string{"aliases", "alternate_titles"} {
			for _, a := range catalogyaml.Strings(catalogyaml.Value(gd.Root(), k)) {
				add(c.names, g, titleBase(a))
			}
		}
		err = catalogyaml.Mapping(catalogyaml.Value(gd.Root(), "releases"), func(name string, n *yaml.Node) error {
			for _, a := range []string{name, catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(n, "authority"), "entry_name"))} {
				add(c.full, g, a)
				add(c.names, g, titleBase(a))
			}
			return nil
		})
		if err != nil {
			return err
		}
		// Provider aliases are only considered when the stored platform agrees with
		// this card's source-platform aliases. Foreign matches cannot bridge games.
		err = catalogyaml.Mapping(catalogyaml.Value(gd.Root(), "sources"), func(source string, n *yaml.Node) error {
			return catalogyaml.Sequence(catalogyaml.Value(n, "matches"), func(m *yaml.Node) error {
				platform := catalogyaml.Scalar(catalogyaml.Value(m, "platform"))
				agree := false
				for _, a := range c.registry.CandidatesForSource(p, source) {
					if tokenName(a) == tokenName(platform) {
						agree = true
					}
				}
				if platform == "" || !agree {
					return nil
				}
				add(c.names, g, catalogyaml.Scalar(catalogyaml.Value(m, "title")))
				for _, a := range catalogyaml.Strings(catalogyaml.Value(m, "alternate_titles")) {
					add(c.names, g, a)
				}
				return catalogyaml.Sequence(catalogyaml.Value(m, "titles"), func(t *yaml.Node) error {
					add(c.names, g, catalogyaml.Scalar(catalogyaml.Value(t, "title")))
					return nil
				})
			})
		})
		if err != nil {
			return err
		}
		items := catalogyaml.Value(fd.Root(), "items")
		if items == nil {
			items = catalogyaml.Value(fd.Root(), "roms")
		}
		err = catalogyaml.Sequence(items, func(n *yaml.Node) error {
			f := catalogyaml.Value(n, "file")
			value := func(k string) string { return catalogyaml.Scalar(catalogyaml.Value(f, k)) }
			fp := fingerprint{Kind: value("kind"), Name: value("name"), Size: catalogyaml.Int64(catalogyaml.Value(f, "size")), SHA1: value("sha1"), SHA256: value("sha256"), MD5: value("md5"), CRC32: value("crc32")}
			owner := outputOwner{g, catalogyaml.Scalar(catalogyaml.Value(n, "variant")), catalogyaml.Scalar(catalogyaml.Value(n, "release")), fp}
			for _, k := range ownerKeys(p, fp) {
				c.owners[k] = append(c.owners[k], owner)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	c.loaded[p] = true
	return nil
}
func (c *discoveryCatalog) match(p, name string) ([]*gameChoice, string) {
	if xs := uniqueGames(c.full[p+"\x00"+tokenName(name)]); len(xs) > 0 {
		return xs, "complete catalog release name"
	}
	return uniqueGames(c.names[p+"\x00"+tokenName(titleBase(name))]), "catalog game title or same-platform alias"
}
func (c *discoveryCatalog) search(p, text string) []*gameChoice {
	key := literalName(text)
	out := []*gameChoice{}
	for _, g := range c.games {
		if g.Platform != p {
			continue
		}
		for _, n := range append([]string{g.Game, g.Title}, g.names...) {
			if strings.Contains(literalName(n), key) {
				out = append(out, g)
				break
			}
		}
	}
	return uniqueGames(out)
}
func (c *discoveryCatalog) fresh() error {
	for _, f := range c.files {
		now, err := catalogfs.Stat(f.source)
		if err != nil {
			return err
		}
		if !catalogfs.SameFile(f.info, now) || f.info.Size() != now.Size() || catalogfs.CacheStamp(f.info) != catalogfs.CacheStamp(now) {
			return fmt.Errorf("catalog changed during discovery: %s", f.Path)
		}
	}
	return nil
}
func safeTitleKey(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, s)
	return strings.Trim(strings.Join(strings.Fields(s), " "), " .")
}

// Staging also checks untouched bundles: an output cannot silently acquire a
// second owner between discovery and materialization.
func checkPublishedOwnership(o options, es []entry) error {
	c, err := openDiscoveryCatalog(o)
	if err != nil {
		return err
	}
	for _, e := range es {
		if err := c.load(e.Platform); err != nil {
			return err
		}
		for _, owner := range c.outputOwners(e.Platform, e.File) {
			if owner.Game.Game != e.Game || owner.Variant != e.Variant.Key || owner.Release != "" || !outputAgrees(e.File, owner.File) {
				return fmt.Errorf("modified output already belongs to %s / %s", owner.Game.Game, owner.Variant)
			}
		}
	}
	return nil
}

func ownerKeys(p string, f fingerprint) []string {
	keys := []string{}
	if f.SHA1 != "" {
		keys = append(keys, identityKey(p, f))
	}
	if f.SHA256 != "" {
		copy := f
		copy.SHA1 = "sha256:" + f.SHA256
		keys = append(keys, identityKey(p, copy))
	}
	return keys
}
func (c *discoveryCatalog) outputOwners(p string, f fingerprint) []outputOwner {
	out := []outputOwner{}
	seen := map[string]bool{}
	for _, k := range ownerKeys(p, f) {
		for _, o := range c.owners[k] {
			id := gameID(o.Game.Platform, o.Game.Game) + "\x00" + o.Variant + "\x00" + o.Release
			if !seen[id] {
				seen[id] = true
				out = append(out, o)
			}
		}
	}
	return out
}
