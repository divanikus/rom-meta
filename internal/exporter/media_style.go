package exporter

import (
	"path/filepath"
	"rommeta/internal/sourceassets"
	"strings"
)

type mediaStyle struct{ style, evidence, sourceType string }

// savedMediaStyles binds categories to exact saved downloader paths, never source
// priority, image dimensions or a guess from the title/region. Unknown is honest.
func savedMediaStyles(gamePath string) (map[string]mediaStyle, error) {
	result := map[string]mediaStyle{}
	for _, source := range []string{"launchbox", "screenscraper"} {
		categories, err := sourceassets.LocalMediaCategories(gamePath, source)
		if err != nil {
			return nil, err
		}
		for _, category := range categories {
			style := "unknown"
			switch source + ":" + strings.ToLower(strings.TrimSpace(category.SourceType)) {
			case "launchbox:box - front", "launchbox:box - front - reconstructed", "launchbox:fanart - box - front", "screenscraper:box-2d":
				style = "flat-front"
			}
			value := mediaStyle{style, "provider-category:" + source + ":" + category.SourceType, category.SourceType}
			path := filepath.Clean(category.Path)
			if old, ok := result[path]; ok && old.style != value.style {
				value = mediaStyle{"unknown", "conflicting-saved-provider-category", ""}
			}
			result[path] = value
		}
	}
	return result, nil
}
func assetStyle(styles map[string]mediaStyle, path string) mediaStyle {
	if value, ok := styles[filepath.Clean(path)]; ok {
		return value
	}
	return mediaStyle{"unknown", "no-bound-provider-category", ""}
}
func (g *game) styles() (map[string]mediaStyle, error) {
	if g.mediaStyles == nil {
		styles, err := savedMediaStyles(g.path)
		if err != nil {
			return nil, err
		}
		g.mediaStyles = styles
	}
	return g.mediaStyles, nil
}
