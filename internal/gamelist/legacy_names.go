package gamelist

import (
	"regexp"
	"sort"
	"strings"

	"rommeta/internal/regions"
)

var releaseQualifier = regexp.MustCompile(`^(.+?)\s+\(([^()]*)\)(.*)$`)
var nextQualifier = regexp.MustCompile(`^\(([^()]*)\)(.*)$`)

func legacyRegions(value string) (string, bool) {
	value = strings.TrimSpace(value)
	short := map[rune]string{'U': "USA", 'E': "Europe", 'J': "Japan", 'W': "World"}
	var values []string
	if len(value) <= 3 && value == strings.ToUpper(value) && !strings.Contains(value, ",") {
		seen := map[rune]bool{}
		for _, code := range value {
			region, ok := short[code]
			if !ok || seen[code] || code == 'W' && len(value) != 1 {
				values = nil
				break
			}
			seen[code] = true
			values = append(values, region)
		}
	}
	if len(values) == 0 {
		for _, part := range strings.Split(value, ",") {
			region, ok := regions.NormalizeDATDisplay(part)
			if !ok || region == "Unknown" {
				return "", false
			}
			values = append(values, region)
		}
	}
	sort.Strings(values)
	for i := 1; i < len(values); i++ {
		if values[i] == values[i-1] {
			return "", false
		}
	}
	return strings.Join(values, ","), len(values) > 0
}

func languageQualifier(value string) bool {
	known := map[string]bool{
		"en": true, "fr": true, "de": true, "es": true, "it": true, "nl": true,
		"pt": true, "ja": true, "zh": true, "ko": true, "ru": true, "sv": true,
		"no": true, "da": true, "fi": true, "pl": true, "cs": true, "hu": true,
		"sk": true, "tr": true, "ar": true,
	}
	for _, part := range strings.Split(strings.ToLower(value), ",") {
		if !known[strings.TrimSpace(part)] {
			return false
		}
	}
	return true
}

// legacyNameKeys preserves the complete title, region set and content qualifiers.
// Only the dump-good marker is discarded. Catalog language/GB compatibility
// annotations may be absent in a legacy list; all possible targets remain in
// the index, so an omitted annotation cannot silently choose between releases.
func legacyNameKeys(name string, catalog bool) []string {
	m := releaseQualifier.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return nil
	}
	region, ok := legacyRegions(m[2])
	if !ok || strings.ContainsAny(m[1], "[]") {
		return nil
	}
	suffix := strings.TrimSpace(m[3])
	var qualifiers []string
	var optional []bool
	for suffix != "" {
		if strings.HasPrefix(suffix, "[!]") {
			suffix = strings.TrimSpace(strings.TrimPrefix(suffix, "[!]"))
			continue
		}
		q := nextQualifier.FindStringSubmatch(suffix)
		if q == nil || strings.ContainsAny(q[1], "[]") {
			return nil
		}
		qualifiers = append(qualifiers, "("+strings.TrimSpace(q[1])+")")
		optional = append(optional, catalog && (languageQualifier(q[1]) || strings.EqualFold(strings.TrimSpace(q[1]), "GB Compatible")))
		suffix = strings.TrimSpace(q[2])
	}
	if len(qualifiers) > 16 {
		return nil
	}
	parts := []string{""}
	for i, qualifier := range qualifiers {
		var next []string
		for _, prefix := range parts {
			next = append(next, prefix+" "+qualifier)
			if optional[i] {
				next = append(next, prefix)
			}
		}
		if len(next) > 16 {
			return nil
		}
		parts = next
	}
	var keys []string
	for _, suffix := range parts {
		keys = append(keys, nameKey(m[1])+"\x00"+nameKey(region)+"\x00"+nameKey(suffix))
	}
	return keys
}

func pathNeedsReview(ref string, prefixes []string) bool {
	for _, prefix := range prefixes {
		current, review := strings.ToLower(ref), strings.ToLower(prefix)
		if current == review || strings.HasPrefix(current, review+"/") {
			return true
		}
	}
	return false
}
