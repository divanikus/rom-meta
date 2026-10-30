package frontend

import (
	"os"
	"path/filepath"
	"strings"
)

// Playlist equivalence uses ordered absolute member paths, never filenames or
// game titles. It preserves an established native M3U launch/save identity.
func (p *Plan) launchIdentity(raw string, fromPackage bool) string {
	key := keyPath(p.Root, raw)
	cacheKey := key
	if fromPackage {
		cacheKey = "package:" + key
	}
	if cached, ok := p.identities[cacheKey]; ok {
		return cached
	}
	result := key
	defer func() { p.identities[cacheKey] = result }()
	if !strings.EqualFold(filepath.Ext(raw), ".m3u") {
		return result
	}
	relative := strings.ReplaceAll(raw, "\\", "/")
	var body []byte
	var name string
	var err error
	if fromPackage && strings.HasPrefix(relative, "Playlists/") {
		name, err = safeJoin(p.Package, relative)
		if err != nil {
			return result
		}
		body, err = p.read(p.Package, relative, false)
		if err != nil {
			return result
		}
	} else {
		name = filepath.FromSlash(relative)
		if !filepath.IsAbs(name) {
			name = filepath.Join(p.Root, name)
		}
		info, err := os.Stat(name)
		if err != nil || info.Size() > 1024*1024 || !info.Mode().IsRegular() {
			return result
		}
		body, err = os.ReadFile(name)
		if err != nil {
			return result
		}
		proof := fingerprint{true, hashBytes(body)}
		if inside(p.Root, name) {
			p.guards[name] = proof
		} else {
			p.external[name] = proof
		}
	}
	var paths []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "://") || strings.ContainsAny(line, "\r\x00") {
			return result
		}
		member := keyPath(filepath.Dir(name), line)
		if seen[member] {
			return result
		}
		seen[member] = true
		paths = append(paths, member)
	}
	if len(paths) >= 2 && len(paths) <= 32 {
		result = "m3u:" + hashBytes([]byte(strings.Join(paths, "\n")))
	}
	return result
}

func (p *Plan) sameLaunch(existing, incoming string) bool {
	return keyPath(p.Root, existing) == keyPath(p.Root, incoming) || p.launchIdentity(existing, false) == p.launchIdentity(incoming, true)
}
