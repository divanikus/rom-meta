package serveui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogrules"
	"rommeta/internal/exporter"
)

type exportSettings struct {
	Target            string   `json:"target,omitempty"`
	Language          string   `json:"language"`
	Media             string   `json:"media"`
	MediaRoles        []string `json:"media_roles,omitempty"`
	ArtworkPreference string   `json:"artwork_preference,omitempty"`
	MetadataSources   []string `json:"metadata_sources,omitempty"`
	ArtworkSources    []string `json:"artwork_sources,omitempty"`
	VideoSources      []string `json:"video_sources,omitempty"`
	DescriptionScope  string   `json:"description_scope,omitempty"`
	MediaScope        string   `json:"media_scope,omitempty"`
	TitleMode         string   `json:"title_mode,omitempty"`
}

type previewGameRequest struct {
	Folder  string `json:"folder"`
	Release string `json:"release,omitempty"`
	Variant string `json:"variant,omitempty"`
}

type exportPreviewRequest struct {
	Platform   string               `json:"platform,omitempty"`
	Settings   exportSettings       `json:"settings"`
	Games      []previewGameRequest `json:"games,omitempty"`
	IncludeRaw bool                 `json:"include_raw,omitempty"`
}

type previewGame struct {
	Folder  string                      `json:"folder"`
	Preview *exporter.CatalogPreview    `json:"preview,omitempty"`
	Error   string                      `json:"error,omitempty"`
	Raw     *exporter.CatalogPreviewXML `json:"raw,omitempty"`
}

type exportPreviewResponse struct {
	Target      string        `json:"target"`
	ProfileYAML string        `json:"profile_yaml"`
	Games       []previewGame `json:"games"`
}

// The compact controls create a new, neutral shared selection profile. Advanced
// source preferences are optional; omission retains catalog-configured defaults.
func (s *server) exportProfile(settings exportSettings) (exporter.Profile, string, string, error) {
	target, err := exporter.NormalizeTarget(settings.Target)
	if err != nil {
		return exporter.Profile{}, "", "", err
	}
	p, err := exporter.ExportProfile(s.root, "schema_version: 1\n")
	if err != nil {
		return p, "", "", err
	}
	language := strings.ToLower(strings.TrimSpace(settings.Language))
	switch language {
	case "en", "ru", "ja", "fr", "de", "es", "it", "pt", "zh", "ko":
	default:
		return p, "", "", fmt.Errorf("unsupported description language")
	}
	p.Selection.Description.Languages = []string{language}
	if language != "en" {
		p.Selection.Description.Languages = append(p.Selection.Description.Languages, "en")
	}
	if settings.DescriptionScope != "" {
		p.Selection.Description.Scope = settings.DescriptionScope
	}
	if settings.TitleMode != "" {
		if settings.TitleMode != "regional" && settings.TitleMode != "release" {
			return p, "", "", fmt.Errorf("title mode must be regional or release")
		}
		p.Titles.Mode = settings.TitleMode
	}
	if settings.MetadataSources != nil {
		p.Selection.Description.Sources = settings.MetadataSources
		for key, rule := range p.Selection.Fields {
			rule.Sources = settings.MetadataSources
			p.Selection.Fields[key] = rule
		}
	}
	artworkRule, videoRule := p.Selection.Media["cover"], p.Selection.Media["video"]
	switch settings.Media {
	case "all":
	case "expanded":
		if settings.MediaRoles == nil {
			settings.MediaRoles = []string{}
			for _, role := range exporter.ExportMediaRoles(target) {
				settings.MediaRoles = append(settings.MediaRoles, role.ID)
			}
		}
	case "custom":
		if settings.MediaRoles == nil {
			return p, "", "", fmt.Errorf("custom media selection requires media_roles")
		}
	case "images":
		delete(p.Selection.Media, "video")
	case "cover":
		for role := range p.Selection.Media {
			if role != "cover" {
				delete(p.Selection.Media, role)
			}
		}
	case "none":
		p.Selection.Media = map[string]exporter.Rule{}
	default:
		return p, "", "", fmt.Errorf("unsupported media preset")
	}
	if settings.MediaRoles != nil {
		p.Selection.Media = map[string]exporter.Rule{}
		for _, role := range settings.MediaRoles {
			if _, exists := p.Selection.Media[role]; exists {
				return p, "", "", fmt.Errorf("duplicate media role: %s", role)
			}
			rule := artworkRule
			if role == "video" {
				rule = videoRule
			}
			if role == "manual" {
				rule.Formats = []string{"pdf"}
			}
			p.Selection.Media[role] = rule
		}
	}
	if err := exporter.ValidateTargetMedia(p, target); err != nil {
		return p, "", "", err
	}
	var variants []string
	switch settings.ArtworkPreference {
	case "", "auto":
	case "original":
		variants = []string{"original", "unknown", "reconstructed", "fanart"}
	case "reconstructed":
		variants = []string{"reconstructed", "original", "unknown", "fanart"}
	default:
		return p, "", "", fmt.Errorf("artwork preference must be auto, original or reconstructed")
	}
	for role, rule := range p.Selection.Media {
		if variants != nil && role != "video" && role != "manual" {
			rule.Variants = variants
			rule.Order = []string{"region", "variant", "source", "format"}
		}
		if settings.MediaScope != "" {
			rule.Scope = settings.MediaScope
		}
		sources := settings.ArtworkSources
		if role == "video" {
			sources = settings.VideoSources
		}
		if sources != nil {
			rule.Sources = sources
		}
		p.Selection.Media[role] = rule
	}
	// Marshal only portable selection. Frontend and workstation paths remain CLI
	// inputs; a saved profile can therefore be reused by both frontend adapters.
	fields := map[string]any{}
	for key, rule := range p.Selection.Fields {
		fields[key] = portableRule(rule)
	}
	media := map[string]any{}
	for key, rule := range p.Selection.Media {
		media[key] = portableRule(rule)
	}
	wire := map[string]any{"schema_version": p.SchemaVersion, "titles": p.Titles, "selection": map[string]any{"description": portableRule(p.Selection.Description), "fields": fields, "media": media}}
	content, err := yaml.Marshal(wire)
	if err != nil {
		return p, "", "", err
	}
	p, err = exporter.ExportProfile(s.root, string(content))
	return p, string(content), target, err
}

// Preserve nil versus explicit empty lists: the profile reader assigns defaults
// only to nil slices. Generic Rule YAML serialization emits nil lists as [].
func portableRule(rule exporter.Rule) map[string]any {
	result := map[string]any{"scope": rule.Scope, "order": rule.Order, "sources": rule.Sources}
	for key, list := range map[string][]string{"languages": rule.Languages, "regions": rule.Regions, "formats": rule.Formats, "styles": rule.Styles, "variants": rule.Variants} {
		if list != nil {
			result[key] = list
		}
	}
	return result
}

// POST computes bounded projections only. It never saves policy/catalog files,
// matches ROMs, opens media payloads or applies a frontend export.
func (s *server) exportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("POST required"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request exportPreviewRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, fmt.Errorf("one JSON document required"))
		return
	}
	if len(request.Games) > 32 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("at most 32 games per preview"))
		return
	}
	if len(request.Games) > 0 && !safeCatalogName(request.Platform) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid platform"))
		return
	}
	for _, game := range request.Games {
		if !safeCatalogName(game.Folder) || catalogrules.IsIgnoredGameFolder(game.Folder) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid game folder"))
			return
		}
	}
	p, content, target, err := s.exportProfile(request.Settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response := exportPreviewResponse{Target: target, ProfileYAML: content, Games: []previewGame{}}
	platform := s.resolvePlatform(request.Platform)
	for _, input := range request.Games {
		if err := r.Context().Err(); err != nil {
			return
		}
		item := previewGame{Folder: input.Folder}
		found, document, err := catalogcache.ReadGameDocument(s.root, platform, input.Folder)
		if err == nil {
			var preview exporter.CatalogPreview
			preview, err = exporter.PreviewCatalog(r.Context(), s.root, found.Path, document.Root(), p, input.Release, input.Variant)
			if err == nil {
				item.Preview = &preview
				if request.IncludeRaw {
					var raw exporter.CatalogPreviewXML
					raw, err = exporter.RenderCatalogPreviewXML(preview, p, target, request.Platform, input.Folder)
					if err == nil {
						item.Raw = &raw
					}
				}
			}
		}
		if err != nil {
			item.Error = err.Error()
		}
		response.Games = append(response.Games, item)
	}
	if r.Context().Err() == nil {
		writeJSON(w, response)
	}
}

// exportOptions returns actual catalog defaults without enumerating games/media.
// Registered source namespaces augment defaults; platform summaries supply more
// observations in the browser. Discovery never enables a provider automatically.
func (s *server) exportOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	p, err := exporter.ExportProfile(s.root, "schema_version: 1\n")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	options := exportSourceOptions{Metadata: p.Selection.Description.Sources, Artwork: p.Selection.Media["cover"].Sources, Video: p.Selection.Media["video"].Sources, Available: []string{}, MediaRoles: map[string][]exporter.MediaRole{"es-de": exporter.ExportMediaRoles("es-de"), "launchbox": exporter.ExportMediaRoles("launchbox")}}
	content, err := catalogfs.ReadFile(filepath.Join(s.root, "source-policies.yaml"))
	if err == nil {
		var document *catalogyaml.Document
		document, err = catalogyaml.Parse(content)
		if err == nil {
			err = catalogyaml.Mapping(catalogyaml.Value(document.Root(), "sources"), func(key string, _ *yaml.Node) error { options.Available = append(options.Available, key); return nil })
		}
	}
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("read source registry: %w", err))
		return
	}
	sort.Strings(options.Available)
	writeJSON(w, options)
}

type exportSourceOptions struct {
	Metadata   []string                        `json:"metadata_sources"`
	Artwork    []string                        `json:"artwork_sources"`
	Video      []string                        `json:"video_sources"`
	Available  []string                        `json:"available_sources"`
	MediaRoles map[string][]exporter.MediaRole `json:"media_roles"`
}
