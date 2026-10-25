package releasepolicy

import (
	"sort"
	"strings"
	"unicode"

	"rommeta/internal/catalogrules"
	"rommeta/internal/contentidentity"
	"rommeta/internal/regions"
)

var defaultPriority = []string{"usa", "world", "europe", "japan", "asia", "russia"}

// Choose returns the conventional default from a set of release names.
func Choose(names []string, priority []string, groups map[string][]string) string {
	if len(names) == 0 {
		return ""
	}
	ranks := regionRanks(priority, groups)
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	best := ordered[0]
	bestScore := score(best, ranks)
	for _, name := range ordered[1:] {
		if candidate := score(name, ranks); candidate > bestScore {
			best, bestScore = name, candidate
		}
	}
	return best
}

// Preference returns the conventional release preference, with higher values
// preferred. Equal values intentionally exclude the alphabetical tie-break so
// callers can use bundle metadata while retaining Choose's release policy.
func Preference(name string, priority []string, groups map[string][]string) int {
	return score(name, regionRanks(priority, groups))
}

func score(name string, ranks map[string]int) int {
	lower := strings.ToLower(name)
	value := 0
	for region, rank := range ranks {
		if containsQualifier(name, region) && rank > value {
			value = rank
		}
	}
	for _, marker := range []string{
		"virtual console", "nintendo switch online", "switch online", "playstation network",
		"xbox live arcade", "psn",
	} {
		if strings.Contains(lower, marker) {
			value -= 1000
		}
	}
	if catalogrules.HasUnofficialTag(catalogrules.UnofficialTagsFromName(name)) {
		value -= 1000
	}
	_, content := contentidentity.Analyze(name)
	if content.Kind != "retail" {
		value -= 1000
	}
	// These explicit DAT qualifiers identify a supplement, demo or extracted
	// compilation release. Do not infer them from words in the game title.
	for _, field := range qualifierFields(name) {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "special disc", "bonus disc", "taikenban", "atari anthology":
			value -= 1000
		}
	}
	return value
}

func regionRanks(priority []string, groups map[string][]string) map[string]int {
	if len(priority) == 0 {
		priority = defaultPriority
	}
	result := map[string]int{}
	for index, group := range priority {
		rank := (len(priority) - index) * 10
		canonical := normalize(group)
		if canonical == "" {
			continue
		}
		result[canonical] = rank
		for _, alias := range groups[group] {
			if value := normalize(alias); value != "" {
				result[value] = rank
			}
		}
	}
	return result
}

func containsQualifier(name, wanted string) bool {
	fields := qualifierFields(name)
	if len(fields) == 0 {
		return false
	}
	// DAT release names put the region first. Later qualifiers contain
	// languages and edition labels, even when they resemble region aliases.
	for _, value := range strings.Split(fields[0], ",") {
		if normalize(value) == wanted {
			return true
		}
	}
	return false
}

func qualifierFields(name string) []string {
	var result []string
	start := -1
	for index, r := range name {
		switch r {
		case '(':
			start = index + 1
		case ')':
			if start >= 0 && start <= index {
				result = append(result, name[start:index])
			}
			start = -1
		}
	}
	return result
}

func normalize(value string) string {
	value = strings.ToLower(regions.NormalizeDisplay(strings.TrimSpace(value)))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
