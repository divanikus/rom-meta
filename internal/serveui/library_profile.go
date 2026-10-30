package serveui

import (
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
)

// The ordinary view retains source observations and overlays explicit game
// fields only. It does not silently pick a release or an export language policy.
func libraryProfile(root *yaml.Node, title, platform string, blocks []sourceBlock) gameProfile {
	p := buildGameProfile(title, platform, blocks)
	scalar := func(key string) string { return strings.TrimSpace(catalogyaml.Scalar(catalogyaml.Value(root, key))) }
	for key, destination := range map[string]*string{"title": &p.Title, "description": &p.Description, "developer": &p.Developer, "publisher": &p.Publisher, "release_date": &p.ReleaseDate, "players": &p.Players, "rating": &p.Rating} {
		if value := scalar(key); value != "" {
			*destination = value
			if key == "description" {
				p.DescriptionLanguage = ""
				p.Source = "local"
			}
		}
	}
	for key, destination := range map[string]*[]string{"genres": &p.Genres, "themes": &p.Themes, "collections": &p.Collections, "franchises": &p.Franchises} {
		if values := catalogyaml.Strings(catalogyaml.Value(root, key)); len(values) > 0 {
			*destination = values
		}
	}
	var local []description
	if text := scalar("description"); text != "" {
		local = append(local, description{Source: "local", Text: text})
	}
	_ = catalogyaml.Mapping(catalogyaml.Value(root, "descriptions"), func(language string, node *yaml.Node) error {
		if text := strings.TrimSpace(catalogyaml.Scalar(node)); text != "" {
			local = append(local, description{Language: language, Source: "local", Text: text})
		}
		return nil
	})
	if len(local) > 0 {
		p.Descriptions = append(local, p.Descriptions...)
		if scalar("description") == "" {
			p.Description = local[0].Text
			p.DescriptionLanguage = local[0].Language
			p.Source = "local"
		}
	}
	return p
}

func renderCatalog(root *yaml.Node) string { content, _ := yaml.Marshal(root); return string(content) }
