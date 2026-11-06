package exporter

import (
	"path/filepath"
	"sort"
	"strings"
)

// NameSuggestion proposes metadata, without binding a release, variant or hash.
type NameSuggestion struct {
	Game  string `json:"game"`
	Title string `json:"title"`
}

// Only full normalized titles/aliases qualify. Never expand short series names,
// and retain competing archive/member titles as separate choices for review.
func suggestTranslationNames(root string, index *catalogIndex, record *Record) bool {
	if index == nil || !translationInput(record) {
		return false
	}
	names := []string{record.ROM}
	if record.Member != "" {
		names = append(names, record.Member)
	}
	found := map[string]NameSuggestion{}
	for _, name := range names {
		key := opticalTitle(name)
		candidates := index.ExportCanonical[key]
		if len(candidates) == 0 {
			candidates = index.ExportNames[key]
		}
		for _, game := range candidates {
			relative, err := filepath.Rel(root, game.path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				continue
			}
			name := filepath.ToSlash(relative)
			found[name] = NameSuggestion{Game: name, Title: game.name}
		}
	}
	if len(found) == 0 {
		return false
	}
	record.Suggestions = nil
	record.Candidates = nil
	for _, suggestion := range found {
		record.Suggestions = append(record.Suggestions, suggestion)
	}
	sort.Slice(record.Suggestions, func(i, j int) bool {
		if record.Suggestions[i].Title != record.Suggestions[j].Title {
			return record.Suggestions[i].Title < record.Suggestions[j].Title
		}
		return record.Suggestions[i].Game < record.Suggestions[j].Game
	})
	for _, suggestion := range record.Suggestions {
		record.Candidates = append(record.Candidates, suggestion.Title)
	}
	record.Status = "ambiguous"
	record.Reason = "Filename suggests a library game; confirm the metadata association. Translation and release are unverified."
	if len(found) > 1 {
		record.Reason = "Filename or archive member suggests several library games; choose the metadata association."
	}
	return true
}
