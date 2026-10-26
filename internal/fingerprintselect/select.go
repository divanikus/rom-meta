package fingerprintselect

import (
	"path/filepath"
	"regexp"
	"strings"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogrules"
)

var trackNumberPattern = regexp.MustCompile(`(?i)\btrack\s*0*(\d+)\b`)

// UsableForHashLookup reports whether a DAT file fingerprint is safe enough
// to use as a game identity lookup key in online sources.
func UsableForHashLookup(fingerprint catalogcache.Fingerprint, releaseFileCount int) bool {
	if fingerprint.Variant != "" || fingerprint.PayloadKind != "" {
		return false
	} // Arcade chip/disk identities are not whole-game archive hashes.
	if releaseFileCount > 1 && looksLikeDiscFile(fingerprint) {
		return false
	}
	return true
}

// Priority ranks fingerprints when a source can try only a bounded number of
// hash lookups. Larger values are better.
func Priority(fingerprint catalogcache.Fingerprint, defaultRelease string, releaseFileCount int) int {
	priority := 0
	// A bounded lookup must reach the original even when a legacy default points
	// at a regional pirate/aftermarket clone. All-unofficial cards still rank
	// their own default normally relative to their other unofficial releases.
	if catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(fingerprint.Release)) {
		priority -= 1000
	}
	if strings.EqualFold(strings.TrimSpace(fingerprint.Release), strings.TrimSpace(defaultRelease)) {
		priority += 100
	}
	if releaseFileCount == 1 {
		priority += 50
	}
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(fingerprint.Name), "."))
	if isCartridgeFormat(extension) || isCartridgeFormat(fingerprint.Format) {
		priority += 40
	}
	if track := TrackNumber(fingerprint.Name); track == 1 {
		priority += 20
	}
	return priority
}

// TrackNumber extracts a Redump-style track number from a file name.
func TrackNumber(name string) int {
	matches := trackNumberPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return 0
	}
	value := 0
	for _, char := range matches[1] {
		value = value*10 + int(char-'0')
	}
	return value
}

func looksLikeDiscFile(fingerprint catalogcache.Fingerprint) bool {
	format := strings.ToLower(strings.TrimSpace(fingerprint.Format))
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(fingerprint.Name), "."))
	if TrackNumber(fingerprint.Name) > 0 {
		return true
	}
	switch format {
	case "cue", "toc", "gdi", "ccd", "sub", "img", "iso", "chd", "bin", "wav", "flac", "ape":
		return true
	}
	switch extension {
	case "cue", "toc", "gdi", "ccd", "sub", "img", "iso", "chd", "bin", "wav", "flac", "ape":
		return true
	default:
		return false
	}
}

func isCartridgeFormat(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "a26", "a52", "a78", "col", "gb", "gba", "gbc", "gg", "j64", "lnx", "md", "n64", "nds", "nes", "ngc", "ngp", "pce", "sfc", "sg", "smc", "sms", "v64", "vb", "ws", "wsc", "z64":
		return true
	default:
		return false
	}
}
