package serveui

import (
	"sort"
	"strconv"
	"strings"
)

type gameProfile struct {
	Title               string         `json:"title,omitempty"`
	Description         string         `json:"description,omitempty"`
	DescriptionLanguage string         `json:"description_language,omitempty"`
	Descriptions        []description  `json:"descriptions,omitempty"`
	Source              string         `json:"source,omitempty"`
	ReleaseDate         string         `json:"release_date,omitempty"`
	Developer           string         `json:"developer,omitempty"`
	Publisher           string         `json:"publisher,omitempty"`
	Genres              []string       `json:"genres,omitempty"`
	Themes              []string       `json:"themes,omitempty"`
	Collections         []string       `json:"collections,omitempty"`
	Franchises          []string       `json:"franchises,omitempty"`
	Players             string         `json:"players,omitempty"`
	Rating              string         `json:"rating,omitempty"`
	RatingCount         string         `json:"rating_count,omitempty"`
	Platform            string         `json:"platform,omitempty"`
	AlternateTitles     []titleVariant `json:"alternate_titles,omitempty"`
}

type sourceProfile struct {
	Title               string         `json:"title,omitempty"`
	Description         string         `json:"description,omitempty"`
	DescriptionLanguage string         `json:"description_language,omitempty"`
	Descriptions        []description  `json:"descriptions,omitempty"`
	ReleaseDate         string         `json:"release_date,omitempty"`
	Developer           string         `json:"developer,omitempty"`
	Publisher           string         `json:"publisher,omitempty"`
	Genres              []string       `json:"genres,omitempty"`
	Themes              []string       `json:"themes,omitempty"`
	Collections         []string       `json:"collections,omitempty"`
	Franchises          []string       `json:"franchises,omitempty"`
	Players             string         `json:"players,omitempty"`
	Rating              string         `json:"rating,omitempty"`
	RatingCount         string         `json:"rating_count,omitempty"`
	Platform            string         `json:"platform,omitempty"`
	AlternateTitles     []titleVariant `json:"alternate_titles,omitempty"`
}

type sourceMatch struct {
	Release         string            `json:"release,omitempty"`
	Releases        []string          `json:"releases,omitempty"`
	Title           string            `json:"title,omitempty"`
	Region          string            `json:"region,omitempty"`
	Platform        string            `json:"platform,omitempty"`
	ReleaseDate     string            `json:"release_date,omitempty"`
	Developer       string            `json:"developer,omitempty"`
	Publisher       string            `json:"publisher,omitempty"`
	Genres          []string          `json:"genres,omitempty"`
	Themes          []string          `json:"themes,omitempty"`
	Collections     []string          `json:"collections,omitempty"`
	Franchises      []string          `json:"franchises,omitempty"`
	Platforms       []string          `json:"platforms,omitempty"`
	MaxPlayers      string            `json:"max_players,omitempty"`
	Players         string            `json:"players,omitempty"`
	Rating          string            `json:"rating,omitempty"`
	CommunityRating string            `json:"community_rating,omitempty"`
	RatingCount     string            `json:"rating_count,omitempty"`
	AlternateTitles []string          `json:"alternate_titles,omitempty"`
	Titles          []titleVariant    `json:"titles,omitempty"`
	Descriptions    map[string]string `json:"descriptions,omitempty"`
	References      map[string]string `json:"references,omitempty"`
	MediaCounts     map[string]int    `json:"media_counts,omitempty"`
	MatchedQuery    string            `json:"matched_query,omitempty"`
	MatchScore      string            `json:"match_score,omitempty"`
}

type titleVariant struct {
	Title    string `json:"title"`
	Region   string `json:"region,omitempty"`
	Language string `json:"language,omitempty"`
}

type description struct {
	Language string `json:"language,omitempty"`
	Source   string `json:"source,omitempty"`
	Text     string `json:"text"`
}

func setMatchField(match *sourceMatch, value string) {
	key, rawValue, ok := splitKeyValue(value)
	if !ok {
		return
	}
	parsed := parseSimpleScalar(rawValue)
	switch key {
	case "release":
		match.Release = parsed
	case "releases":
		match.Releases = parseInlineList(rawValue)
	case "title":
		match.Title = parsed
	case "region":
		match.Region = parsed
	case "platform":
		match.Platform = parsed
	case "release_date":
		match.ReleaseDate = parsed
	case "developer":
		match.Developer = parsed
	case "publisher":
		match.Publisher = parsed
	case "genres":
		match.Genres = parseInlineList(rawValue)
	case "themes":
		match.Themes = parseInlineList(rawValue)
	case "collections":
		match.Collections = parseInlineList(rawValue)
	case "franchises":
		match.Franchises = parseInlineList(rawValue)
	case "platforms":
		match.Platforms = parseInlineList(rawValue)
	case "max_players":
		match.MaxPlayers = parsed
	case "players":
		match.Players = parsed
	case "rating":
		match.Rating = parsed
	case "community_rating":
		match.CommunityRating = parsed
	case "rating_count":
		match.RatingCount = parsed
	case "alternate_titles":
		match.AlternateTitles = parseInlineList(rawValue)
	case "matched_query":
		match.MatchedQuery = parsed
	case "match_score":
		match.MatchScore = parsed
	}
}

func setTitleField(title *titleVariant, value string) {
	key, rawValue, ok := splitKeyValue(value)
	if !ok {
		return
	}
	parsed := parseSimpleScalar(rawValue)
	switch key {
	case "title":
		title.Title = parsed
	case "region":
		title.Region = parsed
	case "language":
		title.Language = parsed
	}
}

func compactMatches(matches []sourceMatch) []sourceMatch {
	var result []sourceMatch
	for _, match := range matches {
		match.AlternateTitles = uniqueStrings(match.AlternateTitles)
		match.Genres = uniqueStrings(match.Genres)
		match.Themes = uniqueStrings(match.Themes)
		match.Collections = uniqueStrings(match.Collections)
		match.Franchises = uniqueStrings(match.Franchises)
		match.Platforms = uniqueStrings(match.Platforms)
		match.Titles = uniqueTitleVariants(match.Titles)
		if match.Descriptions != nil {
			for language, description := range match.Descriptions {
				description = strings.TrimSpace(description)
				if description == "" {
					delete(match.Descriptions, language)
				} else {
					match.Descriptions[language] = description
				}
			}
			if len(match.Descriptions) == 0 {
				match.Descriptions = nil
			}
		}
		if !isEmptyMatch(match) {
			result = append(result, match)
		}
	}
	return result
}

func isEmptyMatch(match sourceMatch) bool {
	return match.Release == "" &&
		len(match.Releases) == 0 &&
		match.Title == "" &&
		match.Region == "" &&
		match.Platform == "" &&
		match.ReleaseDate == "" &&
		match.Developer == "" &&
		match.Publisher == "" &&
		len(match.Genres) == 0 &&
		len(match.Themes) == 0 &&
		len(match.Collections) == 0 &&
		len(match.Franchises) == 0 &&
		match.Players == "" &&
		match.MaxPlayers == "" &&
		match.Rating == "" &&
		match.CommunityRating == "" &&
		match.RatingCount == "" &&
		len(match.AlternateTitles) == 0 &&
		len(match.Titles) == 0 &&
		len(match.Descriptions) == 0 &&
		len(match.MediaCounts) == 0
}

func sourceProfileFromMatches(matches []sourceMatch) sourceProfile {
	var profile sourceProfile
	for _, match := range matches {
		profile.Title = firstNonEmpty(profile.Title, match.Title, match.Release, firstString(match.Releases))
		if profile.Description == "" {
			profile.Description, profile.DescriptionLanguage = bestDescription(match.Descriptions)
		}
		profile.Descriptions = appendDescriptionMap(profile.Descriptions, "", match.Descriptions)
		profile.ReleaseDate = firstNonEmpty(profile.ReleaseDate, match.ReleaseDate)
		profile.Developer = firstNonEmpty(profile.Developer, match.Developer)
		profile.Publisher = firstNonEmpty(profile.Publisher, match.Publisher)
		profile.Genres = appendUniqueStrings(profile.Genres, match.Genres...)
		profile.Themes = appendUniqueStrings(profile.Themes, match.Themes...)
		profile.Collections = appendUniqueStrings(profile.Collections, match.Collections...)
		profile.Franchises = appendUniqueStrings(profile.Franchises, match.Franchises...)
		profile.Players = firstNonEmpty(profile.Players, match.Players, match.MaxPlayers)
		profile.Rating = firstNonEmpty(profile.Rating, match.Rating, match.CommunityRating)
		profile.RatingCount = firstNonEmpty(profile.RatingCount, match.RatingCount)
		profile.Platform = firstNonEmpty(profile.Platform, match.Platform, firstString(match.Platforms))
		profile.AlternateTitles = appendTitleVariants(profile.AlternateTitles, titleVariantsFromMatch(match)...)
	}
	return profile
}

func buildGameProfile(title, platform string, blocks []sourceBlock) gameProfile {
	profile := gameProfile{Title: title, Platform: platform}
	for _, source := range preferredSources(blocks) {
		sourceProfile := source.Profile
		profile.Title = firstNonEmpty(profile.Title, sourceProfile.Title)
		if profile.Description == "" && sourceProfile.Description != "" {
			profile.Description = sourceProfile.Description
			profile.DescriptionLanguage = sourceProfile.DescriptionLanguage
			profile.Source = source.Source
		}
		profile.Descriptions = appendPreferredDescriptionsByLanguage(profile.Descriptions, source.Source, sourceProfile.Descriptions...)
		profile.ReleaseDate = firstNonEmpty(profile.ReleaseDate, sourceProfile.ReleaseDate)
		profile.Developer = firstNonEmpty(profile.Developer, sourceProfile.Developer)
		profile.Publisher = firstNonEmpty(profile.Publisher, sourceProfile.Publisher)
		profile.Genres = appendUniqueStrings(profile.Genres, sourceProfile.Genres...)
		profile.Themes = appendUniqueStrings(profile.Themes, sourceProfile.Themes...)
		profile.Collections = appendUniqueStrings(profile.Collections, sourceProfile.Collections...)
		profile.Franchises = appendUniqueStrings(profile.Franchises, sourceProfile.Franchises...)
		profile.Players = firstNonEmpty(profile.Players, sourceProfile.Players)
		profile.Rating = firstNonEmpty(profile.Rating, sourceProfile.Rating)
		profile.RatingCount = firstNonEmpty(profile.RatingCount, sourceProfile.RatingCount)
		profile.Platform = firstNonEmpty(profile.Platform, sourceProfile.Platform, platform)
		profile.AlternateTitles = appendTitleVariants(profile.AlternateTitles, sourceProfile.AlternateTitles...)
	}
	return profile
}

func preferredSources(blocks []sourceBlock) []sourceBlock {
	order := map[string]int{
		"launchbox":     0,
		"screenscraper": 1,
		"igdb":          2,
		"openvgdb":      3,
		"emumovies":     4,
	}
	result := append([]sourceBlock(nil), blocks...)
	sort.SliceStable(result, func(i, j int) bool {
		left, leftOK := order[result[i].Source]
		right, rightOK := order[result[j].Source]
		if leftOK && rightOK {
			return left < right
		}
		if leftOK {
			return true
		}
		if rightOK {
			return false
		}
		return result[i].Source < result[j].Source
	})
	return result
}

func titleVariantsFromMatch(match sourceMatch) []titleVariant {
	var titles []titleVariant
	for _, value := range match.AlternateTitles {
		titles = appendTitleVariants(titles, titleVariant{Title: value, Region: match.Region})
	}
	titles = appendTitleVariants(titles, match.Titles...)
	return titles
}

func bestDescription(descriptions map[string]string) (string, string) {
	if len(descriptions) == 0 {
		return "", ""
	}
	if description := strings.TrimSpace(descriptions["en"]); description != "" {
		return description, "en"
	}
	keys := make([]string, 0, len(descriptions))
	for language := range descriptions {
		keys = append(keys, language)
	}
	sort.Strings(keys)
	for _, language := range keys {
		if description := strings.TrimSpace(descriptions[language]); description != "" {
			return description, language
		}
	}
	return "", ""
}

func appendDescriptionMap(values []description, source string, descriptions map[string]string) []description {
	if len(descriptions) == 0 {
		return values
	}
	keys := make([]string, 0, len(descriptions))
	for language := range descriptions {
		keys = append(keys, language)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i] == "en" {
			return true
		}
		if keys[j] == "en" {
			return false
		}
		return keys[i] < keys[j]
	})
	for _, language := range keys {
		values = appendDescriptions(values, source, description{
			Language: language,
			Text:     descriptions[language],
		})
	}
	return values
}

func appendDescriptions(values []description, source string, additions ...description) []description {
	for _, addition := range additions {
		addition.Language = strings.TrimSpace(addition.Language)
		addition.Source = firstNonEmpty(addition.Source, source)
		addition.Text = strings.TrimSpace(addition.Text)
		if addition.Text == "" {
			continue
		}
		exists := false
		for _, existing := range values {
			if strings.EqualFold(existing.Language, addition.Language) &&
				strings.EqualFold(existing.Source, addition.Source) &&
				existing.Text == addition.Text {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, addition)
		}
	}
	return values
}

func appendPreferredDescriptionsByLanguage(values []description, source string, additions ...description) []description {
	for _, addition := range additions {
		addition.Language = strings.TrimSpace(addition.Language)
		addition.Source = firstNonEmpty(addition.Source, source)
		addition.Text = strings.TrimSpace(addition.Text)
		if addition.Text == "" {
			continue
		}
		exists := false
		for _, existing := range values {
			if strings.EqualFold(existing.Language, addition.Language) {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, addition)
		}
	}
	return values
}

func parseInlineList(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil
	}
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if value == "" {
		return nil
	}
	var result []string
	for _, part := range splitInlineList(value) {
		parsed := parseSimpleScalar(part)
		if parsed != "" {
			result = append(result, parsed)
		}
	}
	return uniqueStrings(result)
}

func splitKeyValue(value string) (string, string, bool) {
	key, raw, ok := strings.Cut(value, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(raw), true
}

func splitInlineList(value string) []string {
	var result []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\' && inDouble:
			b.WriteRune(r)
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteRune(r)
		case r == ',' && !inSingle && !inDouble:
			result = append(result, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	result = append(result, strings.TrimSpace(b.String()))
	return result
}

func parseSimpleScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func uniqueStrings(values []string) []string {
	var result []string
	return appendUniqueStrings(result, values...)
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		exists := false
		for _, existing := range values {
			if strings.EqualFold(existing, value) {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, value)
		}
	}
	return values
}

func uniqueTitleVariants(values []titleVariant) []titleVariant {
	var result []titleVariant
	return appendTitleVariants(result, values...)
}

func appendTitleVariants(values []titleVariant, additions ...titleVariant) []titleVariant {
	for _, addition := range additions {
		addition.Title = strings.TrimSpace(addition.Title)
		addition.Region = strings.TrimSpace(addition.Region)
		addition.Language = strings.TrimSpace(addition.Language)
		if addition.Title == "" {
			continue
		}
		exists := false
		for _, existing := range values {
			if strings.EqualFold(existing.Title, addition.Title) &&
				strings.EqualFold(existing.Region, addition.Region) &&
				strings.EqualFold(existing.Language, addition.Language) {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, addition)
		}
	}
	return values
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
