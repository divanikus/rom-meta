package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/medianames"
)

// MediaOverride records a reviewed file choice for one exact catalog release.
// It can intentionally select another release's media, without changing identity.
type MediaOverride struct {
	Game    string `yaml:"game" json:"game"`
	Release string `yaml:"release" json:"release"`
	Role    string `yaml:"role" json:"role"`
	Path    string `yaml:"path" json:"path"`
}

func safeMediaRelative(path string) bool {
	return path != "" && !strings.ContainsAny(path, "\\:\x00") &&
		!filepath.IsAbs(path) && !strings.HasPrefix(path, "/") &&
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) == path &&
		path != ".." && !strings.HasPrefix(path, "../")
}

func validateMediaOverrides(p Profile) error {
	seen := map[string]bool{}
	for _, pin := range p.MediaOverrides {
		if !safeMediaRelative(pin.Game) || !strings.HasPrefix(pin.Game, "platforms/") || strings.Count(pin.Game, "/") < 2 ||
			strings.TrimSpace(pin.Release) == "" || !safeMediaRelative(pin.Path) || !strings.HasPrefix(pin.Path, "media/") {
			return fmt.Errorf("media override needs a catalog game, exact release and safe game-relative media path")
		}
		if _, ok := p.Selection.Media[pin.Role]; !ok {
			return fmt.Errorf("media override role is not enabled: %s", pin.Role)
		}
		key := pin.Game + "\x00" + pin.Release + "\x00" + pin.Role
		if seen[key] {
			return fmt.Errorf("duplicate media override for %s / %s / %s", pin.Game, pin.Release, pin.Role)
		}
		seen[key] = true
	}
	return nil
}

func pinnedMedia(root string, g *game, release, rom string, profile Profile, releaseSpecific bool, role, system string) (*Media, error) {
	if !releaseSpecific {
		return nil, nil
	}
	gameRel, err := filepath.Rel(root, g.path)
	if err != nil {
		return nil, err
	}
	for _, pin := range profile.MediaOverrides {
		if pin.Game != filepath.ToSlash(gameRel) || pin.Release != release || pin.Role != role {
			continue
		}
		path := g.path
		for _, part := range strings.Split(pin.Path, "/") {
			path = filepath.Join(path, part)
			info, err := catalogfs.Lstat(path)
			if err != nil {
				return nil, fmt.Errorf("media override: %w", err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("media override contains a symlink")
			}
		}
		real, err := realPath(path)
		if err != nil {
			return nil, err
		}
		if !within(g.path, real) || !within(root, real) {
			return nil, fmt.Errorf("media override escapes catalog game")
		}
		info, err := catalogfs.Stat(real)
		if err != nil {
			return nil, err
		}
		format := strings.ToLower(strings.TrimPrefix(filepath.Ext(real), "."))
		if format == "jpeg" {
			format = "jpg"
		}
		if !info.Mode().IsRegular() || info.Size() == 0 || !supportedMediaFormat(role, format) {
			return nil, fmt.Errorf("media override is empty, not a file or unsupported for %s", role)
		}
		_, parts := medianames.SplitVariant(strings.Split(strings.TrimSuffix(filepath.Base(real), filepath.Ext(real)), "."))
		source, region, language := "local", "unspecified", ""
		if len(parts) > 1 {
			source = strings.ToLower(parts[len(parts)-1])
		}
		if len(parts) > 2 {
			region = regionKey(parts[1])
		}
		if len(parts) > 3 {
			language = strings.ToLower(parts[2])
		}
		rel, _ := filepath.Rel(root, real)
		stem := strings.TrimSuffix(rom, filepath.Ext(rom))
		output := filepath.ToSlash(filepath.Join("downloaded_media", system, roles[role].Dir, filepath.FromSlash(stem)+"."+format))
		return &Media{SourcePath: filepath.ToSlash(rel), OutputPath: output, Source: source, Region: region, Language: language, Size: info.Size(), Selection: "override"}, nil
	}
	return nil, nil
}
