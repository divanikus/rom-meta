package catalogrules

import (
	"regexp"
	"strings"
)

// IsIgnoredGameFolder reports whether a catalog game folder is service data,
// not a playable game bundle that metadata/fold operations should process.
func IsIgnoredGameFolder(name string) bool {
	return IsBIOSName(name)
}

// IsBIOSName reports whether a folder or release name uses the conventional
// No-Intro/Redump BIOS marker.
func IsBIOSName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "[bios]")
}

// HasUnofficialTag reports whether a release tag marks non-official or
// non-retail software that should not borrow official release metadata by title.
func HasUnofficialTag(tags []string) bool {
	for _, tag := range tags {
		if IsUnofficialTag(tag) {
			return true
		}
	}
	return false
}

// IsUnofficialTag reports whether a normalized release tag should isolate
// matching from ordinary retail releases.
func IsUnofficialTag(tag string) bool {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "pirate", "unlicensed", "aftermarket", "homebrew":
		return true
	default:
		return false
	}
}

// UnofficialTagsFromName extracts convention-level unofficial markers from a
// release or source title.
func UnofficialTagsFromName(name string) []string {
	var tags []string
	for _, token := range tagTokens(name) {
		for _, part := range strings.FieldsFunc(token, func(r rune) bool {
			return r == ',' || r == '+'
		}) {
			lower := strings.ToLower(strings.TrimSpace(part))
			switch {
			case strings.Contains(lower, "pirate"):
				tags = appendUnique(tags, "pirate")
			case lower == "unl" || strings.Contains(lower, "unlicensed"):
				tags = appendUnique(tags, "unlicensed")
			case strings.Contains(lower, "aftermarket"):
				tags = appendUnique(tags, "aftermarket")
			case strings.Contains(lower, "homebrew"):
				tags = appendUnique(tags, "homebrew")
			}
		}
	}
	return tags
}

func tagTokens(name string) []string {
	var tokens []string
	for _, match := range tagTokenPattern.FindAllStringSubmatch(name, -1) {
		if len(match) >= 2 {
			tokens = append(tokens, strings.TrimSpace(match[1]))
		}
	}
	return tokens
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

var tagTokenPattern = regexp.MustCompile(`[\(\[]([^\)\]]+)[\)\]]`)
