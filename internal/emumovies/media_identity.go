package emumovies

import (
	"regexp"
	"strings"

	"rommeta/internal/platformnames"
	"rommeta/internal/regions"
)

// mediaTitle removes transport/release attributes, retaining edition, hardware
// and unknown qualifiers. Unknown tags must not silently become base-game keys.
func mediaTitle(value string) string {
	value = tagPattern.ReplaceAllStringFunc(value, func(group string) string {
		var identity []string
		for _, tag := range splitQualifier(tagValuePattern.FindStringSubmatch(group)[1]) {
			if !neutralMediaTag(tag) {
				identity = append(identity, strings.TrimSpace(tag))
			}
		}
		if len(identity) == 0 {
			return " "
		}
		return " (" + strings.Join(identity, ", ") + ") "
	})
	return strings.TrimSpace(repeatedSpace.ReplaceAllString(value, " "))
}

func neutralMediaTag(tag string) bool {
	if _, ok := regions.NormalizeDATDisplay(tag); ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "ar", "da", "de", "el", "en", "es", "fi", "fr", "it", "ja", "ko", "nl", "no", "pl", "pt", "ru", "sv", "zh":
		return true
	}
	return mediaAttributePattern.MatchString(strings.TrimSpace(tag))
}

// Explicit aliases may express regional names or reordered words, but cannot
// erase an edition/hardware qualifier or extend a title into a different game.
func mediaIdentityCompatible(stem, release string) bool {
	sourceKey, releaseKey := mediaIdentityKey(stem), mediaIdentityKey(release)
	if sourceKey == "" || releaseKey == "" {
		return false
	}
	if sourceKey == releaseKey {
		return true
	}
	if strings.Contains(sourceKey, releaseKey) || strings.Contains(releaseKey, sourceKey) {
		return false
	}
	for _, pair := range [][2]string{{stem, releaseKey}, {release, sourceKey}} {
		for _, group := range tagValuePattern.FindAllStringSubmatch(pair[0], -1) {
			for _, tag := range splitQualifier(group[1]) {
				if !neutralMediaTag(tag) && !strings.Contains(pair[1], compactTitleKey(tag)) {
					return false
				}
			}
		}
	}
	return true
}

func mediaIdentityKey(value string) string {
	key := looseTitleKey(titleShape(mediaTitle(value)))
	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(key, article) {
			key = strings.TrimPrefix(key, article)
			break
		}
	}
	return strings.ReplaceAll(key, " ", "")
}

var mediaAttributePattern = regexp.MustCompile(`(?i)^(?:(?:disc|disk|cd|side|track)\s*(?:[0-9]+|[a-z]|[ivxlcdm]+)(?:\s+of\s+[0-9]+)?|rev(?:ision)?\s+(?:[a-z]|[0-9]+(?:\.[0-9a-z]+)*)|v(?:ersion)?\s*[0-9]+(?:\.[0-9a-z]+)*|[0-9]{4}-[0-9]{2}-[0-9]{2})$`)

var pspMinisLabel = regexp.MustCompile(`(?i)\(\s*minis\s*\)`)
var wiiWareLabel = regexp.MustCompile(`(?i)\(\s*wiiware\s*\)`)

// Collection-label fallbacks are catalog-side only. WiiWare additionally needs
// its explicit provider video directory; ordinary Wii disc media cannot qualify.
func catalogMediaName(platform, sourceName, catalogName string, providerSystem ...string) string {
	var label *regexp.Regexp
	switch platformnames.ForSource(platform, platformnames.SourceEmuMovies) {
	case "Sony PSP":
		label = pspMinisLabel
	case "Nintendo Wii":
		if len(providerSystem) == 0 || !strings.EqualFold(strings.TrimSpace(providerSystem[0]), "Nintendo WiiWare") {
			return catalogName
		}
		label = wiiWareLabel
	default:
		return catalogName
	}
	if !label.MatchString(catalogName) || label.MatchString(sourceName) {
		return catalogName
	}
	return strings.TrimSpace(label.ReplaceAllString(catalogName, " "))
}
