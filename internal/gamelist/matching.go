package gamelist

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogyaml"
)

var md5Pattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func addTarget(index map[string][]target, key string, t target) {
	if key == "" {
		return
	}
	for _, old := range index[key] {
		if old == t {
			return
		}
	}
	index[key] = append(index[key], t)
}
func addMD5Targets(index map[string][]target, game string, doc *catalogyaml.Document) error {
	fingerprints, err := catalogcache.ReadFingerprints(game + "/fingerprints.yaml")
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, fp := range fingerprints {
		counts[fp.Release+"\x00"+fp.Variant]++
	}
	for _, fp := range fingerprints {
		md5 := strings.ToLower(strings.TrimSpace(fp.MD5))
		if !md5Pattern.MatchString(md5) || fp.Size <= 0 || fp.PayloadKind != "" || counts[fp.Release+"\x00"+fp.Variant] != 1 {
			continue
		}
		if fp.Variant != "" {
			if catalogyaml.Value(catalogyaml.Value(doc.Root(), "variants"), fp.Variant) == nil {
				continue
			}
		} else if fp.Release == "" || catalogyaml.Value(catalogyaml.Value(doc.Root(), "releases"), fp.Release) == nil {
			continue
		}
		addTarget(index, md5, target{game: game, release: fp.Release, variant: fp.Variant})
	}
	return nil
}
func derivativeLabel(name string) bool {
	n := "/" + strings.ToLower(name)
	for _, label := range []string{"/translations/", "/variants/", "/variant/", "/mods/", "/hack/", "/hacks/", "[t-", "[t+", "[hack", "[rus", "[ru ", "(hack)"} {
		if strings.Contains(n, label) {
			return true
		}
	}
	return false
}
func matchEntry(entry Entry, ref string, names, hashes map[string][]target, useMD5 bool) ([]target, string, string) {
	name := path.Base(ref)
	stem := name
	if romExtensions[strings.ToLower(path.Ext(name))] {
		stem = strings.TrimSuffix(name, path.Ext(name))
	}
	byName := names[nameKey(name)]
	if len(byName) == 0 {
		byName = names[nameKey(stem)]
	}
	if !useMD5 {
		return byName, "exact-release-name", ""
	}
	hash := strings.ToLower(strings.TrimSpace(entry.MD5))
	if hash == "" {
		return byName, "exact-release-name", "absent"
	}
	if !md5Pattern.MatchString(hash) {
		return byName, "exact-release-name", "invalid"
	}
	byHash := hashes[hash]
	if len(byHash) == 0 {
		return byName, "exact-release-name", "not-found"
	}
	// A source-reported hash is a metadata association, not verification of the
	// selected ROM. Never silently move an existing exact name to another target.
	if len(byName) == 1 {
		for _, t := range byHash {
			if t == byName[0] {
				return byName, "reported-md5-and-name", "matched"
			}
		}
		return nil, "", "name-conflict"
	}
	if len(byHash) != 1 {
		return byHash, "reported-md5", "ambiguous"
	}
	if byHash[0].variant != "" {
		return nil, "", "variant-needs-review"
	}
	if derivativeLabel(ref) {
		return nil, "", "derivative-needs-review"
	}
	return byHash, "reported-md5", "matched"
}
func validateMediaRoles(roles map[string]string) error {
	tags := map[string]bool{"image": true, "thumbnail": true, "marquee": true, "video": true, "fanart": true, "manual": true}
	kinds := map[string]bool{"screenshot": true, "box-front": true, "logo": true, "gameplay-video": true, "fanart": true, "manual": true, "ignore": true}
	for tag, role := range roles {
		if !tags[tag] || !kinds[role] {
			return fmt.Errorf("unsupported media role mapping %s=%s", tag, role)
		}
	}
	return nil
}
func mappedRole(tag, role string) (string, string) {
	switch role {
	case "screenshot":
		if tag == "thumbnail" {
			return "screenshot-02", "screenshot"
		}
		return "screenshot-01", "screenshot"
	case "box-front":
		return "box-front", "box_front"
	case "gameplay-video":
		return "gameplay-video", "video"
	default:
		return role, role
	}
}
