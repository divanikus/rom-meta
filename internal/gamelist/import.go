package gamelist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/platformnames"
	"rommeta/internal/regions"
)

type Options struct {
	Input, Member, Origin, DataRoot, Platform, Collection, Language string
	NESROMRoot                                                      string
	Apply                                                           bool
	MatchMD5                                                        bool
	MediaRoles                                                      map[string]string
	MD5ReviewPaths                                                  []string
	Progress                                                        func(string, int, int)
}
type Observation struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	Game        string `json:"game,omitempty"`
	Release     string `json:"release,omitempty"`
	MatchMethod string `json:"match_method,omitempty"`
	MD5Status   string `json:"md5_status,omitempty"`
}
type Result struct {
	Collection   string        `json:"collection"`
	Source       string        `json:"source"`
	Platform     string        `json:"platform"`
	Origin       string        `json:"origin"`
	Apply        bool          `json:"apply"`
	Entries      int           `json:"entries"`
	Matched      int           `json:"matched"`
	Unmatched    int           `json:"unmatched"`
	Ambiguous    int           `json:"ambiguous"`
	Updated      int           `json:"updated"`
	MediaRenamed int           `json:"media_renamed"`
	MediaAdded   int           `json:"media_added"`
	MediaBytes   int64         `json:"media_bytes"`
	MissingMedia int           `json:"missing_media"`
	HashMatched  int           `json:"hash_matched"`
	HashReview   int           `json:"hash_review"`
	Warnings     []string      `json:"warnings,omitempty"`
	Observations []Observation `json:"observations"`
	Backup       string        `json:"backup,omitempty"`
}
type target struct{ game, release, variant string }
type card struct {
	original    []byte
	doc         *catalogyaml.Document
	media       map[string]asset
	renames     map[string]string
	mediaHashes map[string]string
	remove      []string
}
type asset struct {
	input, digest string
	size          int64
	catalog       bool
}

var romExtensions = func() map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.Fields(".zip .7z .chd .cue .bin .rom .nes .unf .unif .sfc .smc .gb .gbc .gba .n64 .z64 .v64 .pce .sgx .vec .ws .wsc .nds .gdi .cdi .iso .cso .rvz .wud .wux .wad .j64 .a78 .a26 .lnx .lyx .32x .sms .gg .md .gen .bs .m3u .ps3 .squashfs .scummvm .svm .sh .exe") {
		out[e] = true
	}
	return out
}()
var collectionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var languagePattern = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]+)*$`)

func nameKey(s string) string    { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func mapping() *yaml.Node        { return &yaml.Node{Kind: yaml.MappingNode} }
func encode(v any) *yaml.Node    { n := &yaml.Node{}; _ = n.Encode(v); return n }
func digest(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, 512<<20+1))
	if n > 512<<20 {
		return "", 0, fmt.Errorf("media exceeds 512 MiB")
	}
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func sourceDigest(in *input, n string) (string, int64, error) {
	r, e := in.open(n)
	if e != nil {
		return "", 0, e
	}
	defer r.Close()
	return digest(r)
}
func catalogDigest(n string) (string, int64, error) {
	r, e := catalogfs.Open(n)
	if e != nil {
		return "", 0, e
	}
	defer r.Close()
	return digest(r)
}
func sameNode(a, b *yaml.Node) bool {
	var x, y bytes.Buffer
	ea, eb := yaml.NewEncoder(&x), yaml.NewEncoder(&y)
	_ = ea.Encode(a)
	_ = eb.Encode(b)
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// Import attaches uniquely qualified release-name or guarded hash observations.
// Frontend IDs and reported hashes remain source evidence, never dump proof.
func Import(o Options) (result Result, err error) {
	if o.NESROMRoot != "" && !o.MatchMD5 {
		return result, fmt.Errorf("--nes-rom-root requires --match-md5")
	}
	for i, prefix := range o.MD5ReviewPaths {
		if !o.MatchMD5 {
			return result, fmt.Errorf("--md5-review-path requires --match-md5")
		}
		normalized, e := relativeRef(strings.TrimSuffix(prefix, "/"))
		if e != nil {
			return result, e
		}
		o.MD5ReviewPaths[i] = normalized
	}
	if err = validateMediaRoles(o.MediaRoles); err != nil {
		return result, err
	}
	if !collectionPattern.MatchString(o.Collection) {
		return result, fmt.Errorf("--collection must be a lowercase identifier (letters, digits, hyphen, underscore)")
	}
	if !languagePattern.MatchString(o.Language) {
		return result, fmt.Errorf("--language must identify the description language")
	}
	if o.DataRoot == "" {
		o.DataRoot = "."
	}
	root, err := filepath.Abs(o.DataRoot)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	in, err := openInput(o)
	if err != nil {
		return result, err
	}
	defer in.close()
	platform := o.Platform
	if platform == "" {
		platform = in.system
	}
	if platform == "" {
		return result, fmt.Errorf("--platform is required when provider.system is absent")
	}
	registry, _, err := platformnames.LoadRegistry(root)
	if err != nil {
		return result, err
	}
	platform = registry.ResolveCLI(platform)
	if !safeRelative(platform) || strings.Contains(platform, "/") {
		return result, fmt.Errorf("invalid catalog platform")
	}
	if o.NESROMRoot != "" && platform != "Nintendo - Nintendo Entertainment System" {
		return result, fmt.Errorf("--nes-rom-root is only supported for the NES platform")
	}
	platformRoot := filepath.Join(root, "platforms", platform)
	folders, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return result, err
	}
	source := "gamelist." + o.Collection
	result = Result{Collection: o.Collection, Source: source, Platform: platform, Origin: in.origin, Apply: o.Apply, Entries: len(in.entries), Observations: []Observation{}}
	index := map[string][]target{}
	legacyIndex := map[string][]target{}
	hashIndex := map[string][]target{}
	fingerprintSizes := map[target]int64{}
	cards := map[string]*card{}
	add := func(name string, t target) {
		k := nameKey(name)
		if k == "" {
			return
		}
		for _, old := range index[k] {
			if old == t {
				return
			}
		}
		index[k] = append(index[k], t)
		for _, key := range legacyNameKeys(name, true) {
			addTarget(legacyIndex, key, t)
		}
	}
	for _, folder := range folders {
		if !folder.IsDir() || strings.HasPrefix(folder.Name(), ".") {
			continue
		}
		game := filepath.Join(platformRoot, folder.Name())
		body, e := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return result, e
		}
		doc, e := catalogyaml.Parse(body)
		if e != nil {
			return result, fmt.Errorf("%s: %w", game, e)
		}
		cards[game] = &card{original: body, doc: doc, media: map[string]asset{}}
		if o.MatchMD5 {
			if e = addMD5Targets(hashIndex, fingerprintSizes, game, doc); e != nil {
				return result, e
			}
		}
		e = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "releases"), func(release string, node *yaml.Node) error {
			t := target{game: game, release: release}
			add(release, t)
			for _, alias := range catalogyaml.Strings(catalogyaml.Value(node, "aliases")) {
				add(alias, t)
			}
			add(catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(node, "authority"), "entry_name")), t)
			return nil
		})
		if e != nil {
			return result, e
		}
	}
	legacyHashes := map[string][]target{}
	if o.NESROMRoot != "" {
		legacyHashes, err = addNESHeaderTargets(o.NESROMRoot, hashIndex, fingerprintSizes)
		if err != nil {
			return result, err
		}
	}
	seen := map[string]bool{}
	for i, entry := range in.entries {
		if o.Progress != nil {
			o.Progress(entry.Path, i+1, len(in.entries))
		}
		ref, e := relativeRef(entry.Path)
		if e != nil {
			return result, e
		}
		if seen[ref] {
			return result, fmt.Errorf("duplicate gamelist launch path %q", ref)
		}
		seen[ref] = true
		candidates, method, hashStatus := matchEntry(entry, ref, index, legacyIndex, hashIndex, legacyHashes, o.MatchMD5, o.MD5ReviewPaths)
		observation := Observation{Path: ref, Status: "unmatched", MatchMethod: method, MD5Status: hashStatus}
		if hashStatus == "name-conflict" || strings.HasSuffix(hashStatus, "needs-review") {
			result.HashReview++
			result.Unmatched++
			observation.Status = hashStatus
			result.Observations = append(result.Observations, observation)
			continue
		}
		if len(candidates) != 1 {
			if len(candidates) > 1 {
				result.Ambiguous++
				observation.Status = "ambiguous"
			} else {
				result.Unmatched++
			}
			result.Observations = append(result.Observations, observation)
			continue
		}
		t := candidates[0]
		c := cards[t.game]
		result.Matched++
		if strings.HasPrefix(method, "reported-md5") {
			result.HashMatched++
		}
		observation.Status = "matched"
		observation.Release = t.release
		relative, _ := filepath.Rel(root, t.game)
		observation.Game = filepath.ToSlash(relative)
		result.Observations = append(result.Observations, observation)
		match := mapping()
		for key, value := range map[string]string{"release": t.release, "importer": "es-de", "collection": o.Collection, "source_file": in.origin, "source_path": ref, "match_method": method, "title": entry.Title, "developer": entry.Developer, "publisher": entry.Publisher, "players": entry.Players, "region": regions.NormalizeDisplay(entry.Region), "reported_md5": entry.MD5, "frontend_id": entry.ID, "rating": entry.Rating, "rom_languages": entry.Languages} {
			if value != "" {
				_ = catalogyaml.Set(match, key, scalar(value))
			}
		}
		if entry.Description != "" {
			_ = catalogyaml.Set(match, "descriptions", encode(map[string]string{o.Language: strings.ReplaceAll(entry.Description, "\r\n", "\n")}))
		}
		if entry.Genre != "" {
			_ = catalogyaml.Set(match, "genres", catalogyaml.StringSequence([]string{entry.Genre}))
		}
		if date := frontendDate(entry.Date); date != "" {
			_ = catalogyaml.Set(match, "release_date", scalar(date))
		}
		media := mapping()
		failed := newFailedMedia()
		refs := []struct{ tag, value, kind, mediaType string }{
			{"image", entry.Image, "screenshot-01", "screenshot"},
			{"thumbnail", entry.Thumbnail, "box-front", "box_front"},
			{"video", entry.Video, "gameplay-video", "video"},
			{"marquee", entry.Marquee, "logo", "logo"},
			{"fanart", entry.Fanart, "fanart", "fanart"},
			{"manual", entry.Manual, "manual", "manual"},
		}
		for _, r := range refs {
			if strings.TrimSpace(r.value) == "" {
				continue
			}
			if role, explicit := o.MediaRoles[r.tag]; explicit {
				if role == "ignore" {
					continue
				}
				r.kind, r.mediaType = mappedRole(r.tag, role)
			}
			relative, e := relativeRef(r.value)
			if e != nil {
				return result, e
			}
			inputPath := path.Join(path.Dir(in.member), relative)
			ext := strings.ToLower(path.Ext(relative))
			supported := ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp"
			if r.mediaType == "video" {
				supported = ext == ".mp4"
			}
			if r.mediaType == "manual" {
				supported = ext == ".pdf"
			}
			if !supported {
				failed.keep(r.tag, relative, r.mediaType)
				result.Warnings = append(result.Warnings, ref+": unsupported "+r.tag+" "+relative)
				continue
			}
			hash, size, e := sourceDigest(in, inputPath)
			if os.IsNotExist(e) {
				failed.keep(r.tag, relative, r.mediaType)
				result.MissingMedia++
				result.Warnings = append(result.Warnings, ref+": missing "+relative)
				continue
			}
			if e != nil {
				return result, e
			}
			if size == 0 {
				failed.keep(r.tag, relative, r.mediaType)
				result.MissingMedia++
				result.Warnings = append(result.Warnings, ref+": empty "+relative)
				continue
			}
			kind, mediaType := r.kind, r.mediaType
			lower := "/" + strings.ToLower(relative) + "/"
			if _, explicit := o.MediaRoles[r.tag]; !explicit && r.tag == "image" && (strings.Contains(lower, "/boxart/") || strings.Contains(lower, "/covers/")) {
				kind, mediaType = "box-front", "box_front"
			}
			// In-memory content key only; normalizeMedia assigns readable paths before staging.
			dest := path.Join("media", t.release, kind+".unspecified.und."+hash[:12]+"."+source+ext)
			if !safeRelative(dest) {
				return result, fmt.Errorf("release key cannot be used as a media folder: %q", t.release)
			}
			targetPath := filepath.Join(t.game, filepath.FromSlash(dest))
			if e = checkParents(root, filepath.Dir(targetPath)); e != nil {
				return result, e
			}
			oldHash, _, e := catalogDigest(targetPath)
			if e != nil && !os.IsNotExist(e) {
				return result, e
			}
			if e == nil && oldHash != hash {
				return result, fmt.Errorf("media collision at %s", targetPath)
			}
			if os.IsNotExist(e) {
				if _, ok := c.media[dest]; !ok {
					c.media[dest] = asset{input: inputPath, digest: hash, size: size}
				}
			}
			candidate := encode(map[string]any{"url": dest, "source_file": relative, "importer": "es-de", "source_tag": r.tag, "sha256": hash, "size": size})
			list := catalogyaml.Value(media, mediaType)
			if list == nil {
				list = &yaml.Node{Kind: yaml.SequenceNode}
				_ = catalogyaml.Set(media, mediaType, list)
			}
			list.Content = append(list.Content, candidate)
		}
		_ = catalogyaml.Set(match, "media", media)
		sources, e := catalogyaml.EnsureMapping(c.doc.Root(), "sources")
		if e != nil {
			return result, e
		}
		stanza, e := catalogyaml.EnsureMapping(sources, source)
		if e != nil {
			return result, e
		}
		matches := catalogyaml.Value(stanza, "matches")
		if matches == nil {
			matches = &yaml.Node{Kind: yaml.SequenceNode}
			_ = catalogyaml.Set(stanza, "matches", matches)
		}
		if matches.Kind != yaml.SequenceNode {
			return result, fmt.Errorf("%s matches must be a sequence", source)
		}
		found := false
		for j, old := range matches.Content {
			if catalogyaml.Scalar(catalogyaml.Value(old, "source_file")) == in.origin && catalogyaml.Scalar(catalogyaml.Value(old, "source_path")) == ref {
				// Preserve fields and languages outside this importer's current observation.
				for k := 0; k+1 < len(match.Content); k += 2 {
					key, value := match.Content[k].Value, match.Content[k+1]
					if key == "media" {
						value, e = reconcileMedia(catalogyaml.Value(old, key), value, failed)
						if e != nil {
							return result, e
						}
					}
					if key == "descriptions" {
						existing := catalogyaml.Value(old, key)
						if existing != nil && existing.Kind == yaml.MappingNode {
							_ = catalogyaml.Mapping(value, func(k string, v *yaml.Node) error { return catalogyaml.Set(existing, k, v) })
							value = existing
						}
					}
					_ = catalogyaml.Set(old, key, value)
				}
				matches.Content[j] = old
				found = true
				break
			}
		}
		if !found {
			matches.Content = append(matches.Content, match)
		}
	}
	// Compare syntax trees: a no-op leaves original formatting and packed bytes alone.
	updates := map[string][]byte{}
	for game, c := range cards {
		added, size, e := normalizeMedia(root, game, source, c)
		if e != nil {
			return result, e
		}
		result.MediaAdded += added
		result.MediaBytes += size
		result.MediaRenamed += len(c.renames)
		previous, e := catalogyaml.Parse(c.original)
		if e != nil {
			return result, e
		}
		if !sameNode(previous.Root(), c.doc.Root()) {
			body, e := c.doc.Marshal()
			if e != nil {
				return result, e
			}
			updates[game] = body
		} else if len(c.media) > 0 {
			updates[game] = c.original
		}
	}
	result.Updated = len(updates)
	policyPath := filepath.Join(root, "source-policies.yaml")
	policyOriginal, e := os.ReadFile(policyPath)
	if os.IsNotExist(e) {
		policyOriginal = []byte("sources: {}\n")
	} else if e != nil {
		return result, e
	}
	policy, e := catalogyaml.Parse(policyOriginal)
	if e != nil {
		return result, e
	}
	policies, e := catalogyaml.EnsureMapping(policy.Root(), "sources")
	if e != nil {
		return result, e
	}
	policyChanged := false
	if result.Matched > 0 && catalogyaml.Value(policies, source) == nil {
		_ = catalogyaml.Set(policies, source, encode(map[string]string{"redistribution": "private", "reason": "Imported local frontend curation; personal use only."}))
		policyChanged = true
	}
	if !o.Apply || len(updates) == 0 && !policyChanged {
		return result, nil
	}
	stage, e := publishCards(root, cards, updates, in, o.Progress, func(stage string) error {
		if policyChanged {
			if e = os.WriteFile(filepath.Join(stage, "source-policies.yaml.before"), policyOriginal, 0600); e != nil {
				return e
			}
			body, e := policy.Marshal()
			if e != nil {
				return e
			}
			if e = atomicfile.WriteFile(policyPath, body, 0644); e != nil {
				return e
			}
		}
		return nil
	})
	result.Backup = stage
	if e != nil {
		return result, e
	}
	body, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return result, e
	}
	if e = os.WriteFile(filepath.Join(stage, "result.json"), body, 0600); e != nil {
		return result, e
	}
	return result, nil
}
func checkParents(root, directory string) error {
	rel, err := filepath.Rel(root, directory)
	if err != nil || !safeRelative(filepath.ToSlash(rel)) {
		return fmt.Errorf("destination escapes catalog")
	}
	cursor := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cursor = filepath.Join(cursor, part)
		info, err := os.Lstat(cursor)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("destination parent is not an ordinary directory: %s", cursor)
		}
	}
	return nil
}
func publishFiles(game string, files map[string]string, remove []string) error {
	packed, err := catalogfs.Packed(filepath.Join(game, "game.yaml"))
	if err != nil {
		return err
	}
	if packed {
		return catalogfs.PutFiles(game, files, remove)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	// Publish media before metadata, so newly referenced files already exist.
	sort.SliceStable(names, func(i, j int) bool { return names[i] != "game.yaml" && names[j] == "game.yaml" })
	for _, name := range names {
		data, err := os.ReadFile(files[name])
		if err != nil {
			return err
		}
		dest := filepath.Join(game, filepath.FromSlash(name))
		if err = atomicfile.WriteFile(dest, data, 0644); err != nil {
			return err
		}
	}
	for _, name := range remove {
		if _, written := files[name]; written {
			continue
		}
		if err := catalogfs.Remove(filepath.Join(game, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	return nil
}
func frontendDate(s string) string {
	if s == "" {
		return ""
	}
	for _, layout := range []string{"20060102T150405", "2006-01-02"} {
		if d, e := time.Parse(layout, s); e == nil {
			return d.Format("2006-01-02")
		}
	}
	return ""
}
