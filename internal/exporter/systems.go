package exporter

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"rommeta/internal/platformnames"
)

type systemSpec struct {
	Platform     string
	Extensions   []string
	Disc, Arcade bool
}

// Compatibility profiles preserve established system IDs and exact readers.
// They do not restrict platform coverage: other catalog platforms and formats
// use the generic ROM-set observer. Native emulator readiness remains separate.
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

// Typed metadata readers retain their format contracts independently of the
// generic export observer. Export coverage is derived from catalog headers.
var metadataCartridges = map[string]systemSpec{
	"satellaview":     {"Nintendo - Satellaview", []string{".bs"}, false, false},
	"atari2600":       {"Atari - Atari 2600", []string{".a26"}, false, false},
	"atari5200":       {"Atari - Atari 5200", []string{".a52"}, false, false},
	"atarijaguar":     {"Atari - Atari Jaguar (J64)", []string{".j64"}, false, false},
	"atarilynx":       {"Atari - Atari Lynx (LYX)", []string{".lyx"}, false, false},
	"wonderswancolor": {"Bandai - WonderSwan Color", []string{".wsc"}, false, false},
	"msx":             {"Microsoft - MSX", []string{".rom"}, false, false},
	"msx2":            {"Microsoft - MSX2", []string{".rom"}, false, false},
	"pcengine":        {"NEC - PC Engine - TurboGrafx-16", []string{".pce"}, false, false},
	"nds":             {"Nintendo - Nintendo DS", []string{".nds"}, false, false},
	"virtualboy":      {"Nintendo - Virtual Boy", []string{".vb"}, false, false},
	"beena":           {"Sega - Beena", []string{".bin"}, false, false},
	"sega32x":         {"Sega - 32X", []string{".32x", ".bin", ".md", ".gen"}, false, false},
}

// specialized systems retain their historical reader IDs; ordinary platforms
// use the registry code and the common ROM-set observer, including local entries.
func exportExcluded(name string) bool {
	normalized := platformnames.Normalize(name)
	return normalized == platformnames.Normalize("IBM - PC and Compatibles") || normalized == "ibmpc" || normalized == "scummvm" || normalized == "scumm vm"
}
func validSystemID(id string) bool {
	if id == "" || exportExcluded(id) {
		return false
	}
	for _, r := range id {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
func genericSystem(id string) bool { _, special := systems[id]; return !special && validSystemID(id) }
func exportSystemsFor(name string, names platformnames.Registry) []string {
	name = catalogFamily(names.ResolveCLI(name))
	if exportExcluded(name) {
		return nil
	}
	var ids []string
	for id, spec := range systems {
		if spec.Platform == name {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		code := names.Code(name)
		if code == "" {
			code = strings.ReplaceAll(platformnames.Normalize(name), " ", "-")
		}
		if validSystemID(code) {
			ids = append(ids, code)
		}
	}
	sort.Strings(ids)
	return ids
}
func SupportedSystems() []string {
	names := platformnames.DefaultRegistry()
	var ids []string
	for _, entry := range names.Platforms() {
		ids = append(ids, exportSystemsFor(entry.Name, names)...)
	}
	return uniqueNames(ids)
}
func systemFor(platform, requested string) (string, error) {
	names := platformnames.DefaultRegistry()
	platform = catalogFamily(names.ResolveCLI(platform))
	if exportExcluded(platform) {
		return "", fmt.Errorf("platform %q is excluded from ROM-set export", platform)
	}
	if requested != "" {
		if spec, ok := systems[requested]; ok {
			if spec.Platform == platform {
				return requested, nil
			}
			return "", fmt.Errorf("system %q does not map to catalog platform %q", requested, platform)
		}
		if entry, ok := names.Entry(requested); ok && catalogFamily(entry.Name) != platform {
			return "", fmt.Errorf("system %q does not map to catalog platform %q", requested, platform)
		}
		if validSystemID(requested) {
			return requested, nil
		}
		return "", fmt.Errorf("invalid export system %q", requested)
	}
	ids := exportSystemsFor(platform, names)
	if len(ids) > 0 {
		return ids[0], nil
	}
	return "", fmt.Errorf("no export identity for %q", platform)
}
func payloadExtension(system, ext string) bool {
	spec, ok := systems[system]
	if !ok {
		if metadata, exists := metadataCartridges[system]; exists {
			return position(metadata.Extensions, ext) >= 0
		}
		if genericSystem(system) {
			return genericPayloadExtension(ext)
		}
		spec = metadataCartridges[system]
	}
	return position(spec.Extensions, ext) >= 0
}
func inputExtension(system, ext string) bool {
	if genericCandidateExtension(ext) {
		return true
	}
	if payloadExtension(system, ext) || ext == ".zip" || ext == ".7z" || ext == ".rar" {
		return true
	}
	return systems[system].Disc && position([]string{".cue", ".gdi", ".m3u", ".cso", ".pbp"}, ext) >= 0
}
func nameExtension(ext string) bool {
	if genericCandidateExtension(ext) {
		for _, r := range ext {
			if r >= 'a' && r <= 'z' {
				return true
			}
		}
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
