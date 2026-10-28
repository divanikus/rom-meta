package exporter

import (
	"fmt"
	"sort"
)

type systemSpec struct {
	Platform     string
	Extensions   []string
	Disc, Arcade bool
}

// These are launch readers, not a promise of every emulator's format support.
// ES-DE system IDs/extensions were checked against its Windows configuration.
var systems = map[string]systemSpec{
	"n64":          {"Nintendo - Nintendo 64", []string{".z64", ".v64", ".n64"}, false, false},
	"nes":          {"Nintendo - Nintendo Entertainment System", []string{".nes", ".unf", ".unif"}, false, false},
	"snes":         {"Nintendo - Super Nintendo Entertainment System", []string{".sfc", ".smc", ".bin"}, false, false},
	"gb":           {"Nintendo - Game Boy", []string{".gb", ".dmg", ".sgb", ".gbx"}, false, false},
	"gbc":          {"Nintendo - Game Boy Color", []string{".gbc", ".cgb", ".gb", ".gbx"}, false, false},
	"gba":          {"Nintendo - Game Boy Advance", []string{".gba", ".agb", ".bin"}, false, false},
	"gamegear":     {"Sega - Game Gear", []string{".gg", ".sms", ".bin", ".rom"}, false, false},
	"mastersystem": {"Sega - Master System - Mark III", []string{".sms", ".bin", ".rom"}, false, false},
	"megadrive":    {"Sega - Mega Drive - Genesis", []string{".md", ".gen", ".bin", ".smd"}, false, false},
	"genesis":      {"Sega - Mega Drive - Genesis", []string{".md", ".gen", ".bin", ".smd"}, false, false},
	"3do":          {"3DO Interactive Multiplayer", []string{".chd", ".iso"}, true, false},
	"psx":          {"Sony - PlayStation", []string{".chd", ".iso"}, true, false},
	"ps2":          {"Sony - PlayStation 2", []string{".chd", ".iso"}, true, false},
	"ps3":          {"Sony - PlayStation 3", []string{".iso", ".ps3"}, true, false},
	"xbox":         {"Microsoft - Xbox", []string{".iso", ".xiso"}, true, false},
	"xbox360":      {"Microsoft - Xbox 360", []string{".iso"}, true, false},
	"dreamcast":    {"Sega - Dreamcast", []string{".chd", ".iso"}, true, false},
	"saturn":       {"Sega - Saturn", []string{".chd", ".iso"}, true, false},
	"segacd":       {"Sega - Mega CD & Sega CD", []string{".chd", ".iso"}, true, false},
	"pcenginecd":   {"NEC - PC Engine CD & TurboGrafx CD", []string{".chd", ".iso"}, true, false},
	"arcade":       {"Arcade", []string{".zip", ".7z"}, false, true},
	"mame":         {"Arcade", []string{".zip", ".7z"}, false, true},
}

// Metadata-only payload readers share the bounded raw/ZIP proof path without
// advertising a new ES-DE launch/export system.
var metadataCartridges = map[string]systemSpec{
	"sega32x": {"Sega - 32X", []string{".32x", ".bin", ".md", ".gen"}, false, false},
}

func SupportedSystems() []string {
	var names []string
	for name := range systems {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func systemFor(platform, requested string) (string, error) {
	platform = catalogFamily(platform)
	if requested != "" {
		spec, ok := systems[requested]
		if !ok || spec.Platform != platform {
			return "", fmt.Errorf("system %q does not map to catalog platform %q", requested, platform)
		}
		return requested, nil
	}
	for _, name := range SupportedSystems() {
		if systems[name].Platform == platform {
			return name, nil
		}
	}
	return "", fmt.Errorf("no ES-DE launch reader for catalog platform %q", platform)
}

func payloadExtension(system, ext string) bool {
	spec, ok := systems[system]
	if !ok {
		spec = metadataCartridges[system]
	}
	return position(spec.Extensions, ext) >= 0
}
func inputExtension(system, ext string) bool {
	if payloadExtension(system, ext) || ext == ".zip" || ext == ".7z" || ext == ".rar" {
		return true
	}
	return systems[system].Disc && position([]string{".cue", ".gdi", ".m3u", ".cso", ".pbp"}, ext) >= 0
}
func nameExtension(ext string) bool {
	if position([]string{".zip", ".7z", ".chd", ".iso"}, ext) >= 0 {
		return true
	}
	for system := range metadataCartridges {
		if payloadExtension(system, ext) {
			return true
		}
	}
	for system := range systems {
		if payloadExtension(system, ext) {
			return true
		}
	}
	return false
}

// Find actual stored folders by their platform header, rather than assuming an
// alias, display name or representation suffix is the physical catalog path.
func storedPlatforms(root string) (map[string][]string, error) {
	result := map[string][]string{}
	catalog, err := NewLookupCatalog(root, "")
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for _, p := range catalog.Platforms {
		for _, member := range p.Catalogs {
			if member != p.Platform {
				covered[member] = true
			}
		}
	}
	for _, p := range catalog.Platforms {
		if covered[p.Platform] {
			continue
		}
		for _, id := range p.ExportSystems {
			canonical := systems[id].Platform
			if position(result[canonical], p.Platform) < 0 {
				result[canonical] = append(result[canonical], p.Platform)
			}
		}
	}
	return result, nil
}
