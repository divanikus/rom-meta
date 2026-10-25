package sourceyaml

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"rommeta/internal/regions"
	"rommeta/internal/sourceurl"
)

// MediaCandidate is one source-provided downloadable media candidate.
type MediaCandidate struct {
	URL        string `json:"url"`
	Region     string `json:"region,omitempty"`
	Language   string `json:"language,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Format     string `json:"format,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	CRC32      string `json:"crc32,omitempty"`
	MD5        string `json:"md5,omitempty"`
	SHA1       string `json:"sha1,omitempty"`
	Size       int64  `json:"size,omitempty"`
}

// TitleCandidate is one source-provided title variant with optional provenance.
type TitleCandidate struct {
	Title      string `json:"title"`
	Region     string `json:"region,omitempty"`
	Language   string `json:"language,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	SourceType string `json:"source_type,omitempty"`
}

// RenderTitles writes the conventional source title-variant block:
//
//	titles:
//	  - title: "Lylat Wars"
//	    region: "Europe"
func RenderTitles(w io.Writer, indent string, titles []TitleCandidate) {
	normalized := normalizeTitleCandidates(titles)
	if len(normalized) == 0 {
		return
	}
	fmt.Fprintf(w, "%stitles:\n", indent)
	for _, candidate := range normalized {
		fmt.Fprintf(w, "%s  - title: %s\n", indent, YAMLString(candidate.Title))
		writeTitleField(w, indent, "region", regions.NormalizeDisplay(candidate.Region))
		writeTitleField(w, indent, "language", candidate.Language)
		writeTitleField(w, indent, "source_id", candidate.SourceID)
		writeTitleField(w, indent, "source_type", candidate.SourceType)
	}
}

func normalizeTitleCandidates(titles []TitleCandidate) []TitleCandidate {
	var result []TitleCandidate
	seen := map[string]bool{}
	for _, candidate := range titles {
		candidate.Title = strings.TrimSpace(candidate.Title)
		candidate.Region = regions.NormalizeDisplay(candidate.Region)
		candidate.Language = strings.TrimSpace(candidate.Language)
		candidate.SourceID = strings.TrimSpace(candidate.SourceID)
		candidate.SourceType = strings.TrimSpace(candidate.SourceType)
		if candidate.Title == "" {
			continue
		}
		key := strings.ToLower(candidate.Title) + "\x00" +
			strings.ToLower(candidate.Region) + "\x00" +
			strings.ToLower(candidate.Language) + "\x00" +
			strings.ToLower(candidate.SourceID) + "\x00" +
			strings.ToLower(candidate.SourceType)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, candidate)
	}
	return result
}

// RenderMedia writes the conventional source media block:
//
//	media:
//	  box_front:
//	    - url: "https://..."
//	      region: "Japan"
func RenderMedia(w io.Writer, indent string, media map[string][]MediaCandidate) {
	if len(media) == 0 {
		return
	}
	keys := make([]string, 0, len(media))
	for key, candidates := range media {
		if key != "" && len(candidates) > 0 {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "%smedia:\n", indent)
	for _, key := range keys {
		fmt.Fprintf(w, "%s  %s:\n", indent, key)
		for _, candidate := range media[key] {
			mediaURL := sourceurl.StripSecrets(candidate.URL)
			if mediaURL == "" {
				continue
			}
			fmt.Fprintf(w, "%s    - url: %s\n", indent, YAMLString(mediaURL))
			writeStringField(w, indent, "region", regions.NormalizeDisplay(candidate.Region))
			writeStringField(w, indent, "language", candidate.Language)
			writeStringField(w, indent, "quality", candidate.Quality)
			writeStringField(w, indent, "format", candidate.Format)
			writeStringField(w, indent, "source_id", candidate.SourceID)
			writeStringField(w, indent, "source_file", candidate.SourceFile)
			writeStringField(w, indent, "source_type", candidate.SourceType)
			writeStringField(w, indent, "crc32", candidate.CRC32)
			writeStringField(w, indent, "md5", candidate.MD5)
			writeStringField(w, indent, "sha1", candidate.SHA1)
			if candidate.Size > 0 {
				fmt.Fprintf(w, "%s      size: %d\n", indent, candidate.Size)
			}
		}
	}
}

// RenderEnglishDescription writes a conventional English-only description block.
func RenderEnglishDescription(w io.Writer, indent, description string) {
	RenderDescriptions(w, indent, map[string]string{"en": description})
}

// RenderDescriptions writes the conventional localized source description block:
//
//	descriptions:
//	  en: |-
//	    English text.
//	  ja: |-
//	    Japanese text.
func RenderDescriptions(w io.Writer, indent string, descriptions map[string]string) {
	if len(descriptions) == 0 {
		return
	}
	normalized := map[string]string{}
	for language, description := range descriptions {
		language = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
		description = strings.TrimSpace(normalizeDescriptionText(description))
		if language == "" || description == "" {
			continue
		}
		if _, exists := normalized[language]; !exists {
			normalized[language] = description
		}
	}
	keys := make([]string, 0, len(normalized))
	for language := range normalized {
		keys = append(keys, language)
	}
	if len(keys) == 0 {
		return
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "en" {
			return true
		}
		if keys[j] == "en" {
			return false
		}
		return keys[i] < keys[j]
	})
	fmt.Fprintf(w, "%sdescriptions:\n", indent)
	for _, language := range keys {
		fmt.Fprintf(w, "%s  %s: |-\n", indent, yamlKey(language))
		for _, line := range strings.Split(normalized[language], "\n") {
			fmt.Fprintf(w, "%s    %s\n", indent, line)
		}
	}
}

func normalizeDescriptionText(value string) string {
	// YAML recognizes these as line breaks too. Normalize before splitting
	// and indenting the block, or later lines can escape the description.
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\u0085', '\u2028', '\u2029':
			return '\n'
		}
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, value)
}

// YAMLString renders a string as a simple quoted YAML scalar.
func YAMLString(value string) string {
	return strconv.Quote(value)
}

func yamlKey(value string) string {
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return YAMLString(value)
	}
	return value
}

func writeStringField(w io.Writer, indent, name, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(w, "%s      %s: %s\n", indent, name, YAMLString(value))
}

func writeTitleField(w io.Writer, indent, name, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(w, "%s    %s: %s\n", indent, name, YAMLString(value))
}
