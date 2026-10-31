// Package exporter projects a local catalog onto an observed ROM selection.
// It never fetches providers, reads credentials, or modifies source data.
package exporter

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

type Options struct {
	HashROMs                                             bool
	LegacyOnly                                           bool
	selector                                             *PlatformSelector
	selection                                            *PlatformSelection
	selectedCatalog                                      string
	Context                                              context.Context
	DataRoot, ROMRoot, Platform, Out, ProfilePath, Match string
	Target, System, ArcadeSnapshot                       string
	Limit                                                int
	Apply                                                bool
	Progress                                             func(stage, name string, current, total int)
}

type Rule struct {
	Styles    []string `yaml:"styles,omitempty" json:"styles,omitempty"`
	Scope     string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	Variants  []string `yaml:"variants,omitempty" json:"variants,omitempty"`
	Order     []string `yaml:"order" json:"order"`
	Languages []string `yaml:"languages" json:"languages,omitempty"`
	Sources   []string `yaml:"sources" json:"sources"`
	Regions   []string `yaml:"regions" json:"regions,omitempty"`
	Formats   []string `yaml:"formats" json:"formats,omitempty"`
}

type Selection struct {
	Description Rule            `yaml:"description" json:"description"`
	Fields      map[string]Rule `yaml:"fields" json:"fields,omitempty"`
	Media       map[string]Rule `yaml:"media" json:"media"`
}

type Profile struct {
	LaunchBox            LaunchBoxPolicy       `yaml:"launchbox,omitempty" json:"launchbox"`
	SchemaVersion        int                   `yaml:"schema_version" json:"schema_version"`
	Target               string                `yaml:"target" json:"target"`
	Titles               TitlePolicy           `yaml:"titles" json:"titles"`
	Selection            Selection             `yaml:"selection" json:"selection"`
	MediaOverrides       []MediaOverride       `yaml:"media_overrides,omitempty" json:"media_overrides,omitempty"`
	DescriptionFallbacks []DescriptionFallback `yaml:"description_fallbacks,omitempty" json:"description_fallbacks,omitempty"`
}

type Value struct {
	Selection   string `json:"selection,omitempty"`
	FromVariant string `json:"from_variant,omitempty"`
	FromRelease string `json:"from_release,omitempty"`
	Value       string `json:"value"`
	Source      string `json:"source"`
	Language    string `json:"language,omitempty"`
}

type Media struct {
	Style           string `json:"style,omitempty"`
	StyleEvidence   string `json:"style_evidence,omitempty"`
	Variant         string `json:"variant,omitempty"`
	SourceType      string `json:"source_type,omitempty"`
	VariantEvidence string `json:"variant_evidence,omitempty"`
	FromVariant     string `json:"from_variant,omitempty"`
	FromRelease     string `json:"from_release,omitempty"`
	Selection       string `json:"selection,omitempty"`
	SourcePath      string `json:"source_path"`
	OutputPath      string `json:"output_path"`
	Source          string `json:"source"`
	Region          string `json:"region,omitempty"`
	Language        string `json:"language,omitempty"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256,omitempty"`
}

type PlaylistInput struct {
	Disc      *DiscObservation `json:"disc,omitempty"`
	ROM       string           `json:"rom"`
	Size      int64            `json:"size"`
	SHA1      string           `json:"sha1"`
	Variant   string           `json:"variant,omitempty"`
	Release   string           `json:"release,omitempty"`
	MatchedBy string           `json:"matched_by"`
}

// VariantInfo retains modification identity separately from inherited metadata.
type VariantInfo struct {
	Title     string                `json:"title,omitempty"`
	Kind      string                `json:"kind"`
	BasedOn   string                `json:"based_on,omitempty"`
	Chain     []string              `json:"chain"`
	Ancestors []catalogyaml.Variant `json:"ancestors,omitempty"`
	Languages []string              `json:"languages,omitempty"`
	Group     string                `json:"group,omitempty"`
	Version   string                `json:"version,omitempty"`
}

// DiscVariant retains each observed disc's prepared identity in a metadata group.
// The first member supplies group metadata; this does not invent a patch edition.
type DiscVariant struct {
	Name            string       `json:"name"`
	Variant         string       `json:"variant"`
	MetadataRelease string       `json:"metadata_release"`
	Info            *VariantInfo `json:"variant_info"`
}

type Record struct {
	HashRequired    []string            `json:"hash_required,omitempty"`
	SourceOnly      bool                `json:"source_only,omitempty"`
	GameTitle       string              `json:"game_title,omitempty"`
	DiscVariants    []DiscVariant       `json:"disc_variants,omitempty"`
	Variant         string              `json:"variant,omitempty"`
	VariantInfo     *VariantInfo        `json:"variant_info,omitempty"`
	TitleContext    *TitleContext       `json:"title_context,omitempty"`
	Disc            *DiscObservation    `json:"disc,omitempty"`
	Catalog         *CatalogObservation `json:"catalog,omitempty"`
	Payload         *PayloadProof       `json:"payload,omitempty"`
	LaunchFile      string              `json:"launch_file,omitempty"`
	Playlist        []PlaylistInput     `json:"playlist,omitempty"`
	Hidden          bool                `json:"hidden,omitempty"`
	Representation  string              `json:"representation,omitempty"`
	ArcadeSnapshot  string              `json:"catalog_snapshot,omitempty"`
	MatchedBy       string              `json:"matched_by,omitempty"`
	MetadataRelease string              `json:"metadata_release,omitempty"`
	ROM             string              `json:"rom"`
	Member          string              `json:"member,omitempty"`
	Size            int64               `json:"size,omitempty"`
	SHA1            string              `json:"sha1,omitempty"`
	Status          string              `json:"status"`
	Reason          string              `json:"reason,omitempty"`
	Game            string              `json:"game,omitempty"`
	Release         string              `json:"release,omitempty"`
	Fields          map[string]Value    `json:"fields,omitempty"`
	Media           map[string]Media    `json:"media,omitempty"`
	Warnings        []string            `json:"warnings,omitempty"`
	Candidates      []string            `json:"candidates,omitempty"`
}

type Plan struct {
	PlatformName      string               `json:"platform_name,omitempty"`
	PlatformSelection *PlatformSelection   `json:"platform_selection,omitempty"`
	LaunchBox         *LaunchBoxProjection `json:"launchbox,omitempty"`
	HashMatches       int                  `json:"hash_matches"`
	NameMatches       int                  `json:"name_matches"`
	GameOnly          int                  `json:"game_only"`
	SchemaVersion     int                  `json:"schema_version"`
	Target            string               `json:"target"`
	Platform          string               `json:"platform"`
	System            string               `json:"system"`
	DataRoot          string               `json:"data_root"`
	ROMRoot           string               `json:"rom_root"`
	Out               string               `json:"-"`
	SourceAPI         string               `json:"source_api,omitempty"`
	MediaRoot         string               `json:"-"`
	MediaPaths        map[string]string    `json:"-"`
	Context           context.Context      `json:"-"`
	Profile           Profile              `json:"profile"`
	Records           []Record             `json:"records"`
	Matched           int                  `json:"matched"`
	Ambiguous         int                  `json:"ambiguous"`
	Unmatched         int                  `json:"unmatched"`
	Unsupported       int                  `json:"unsupported"`
	MediaFiles        int                  `json:"media_files"`
	MediaBytes        int64                `json:"media_bytes"`
}

var roles = map[string]struct{ Kind, Dir string }{
	"cover": {"box-front", "covers"}, "screenshot": {"screenshot", "screenshots"},
	"logo": {"logo", "marquees"}, "video": {"gameplay-video", "videos"},
	"title-screen": {"title-screen", "titlescreens"}, "box-3d": {"box-3d", "3dboxes"},
	"back-cover": {"box-back", "backcovers"}, "fanart": {"fanart", "fanart"},
	"manual":     {"manual", "manuals"},
	"cart-front": {"cart-front", "cartfronts"}, "cart-back": {"cart-back", "cartbacks"},
	"cart-3d": {"cart-3d", "3dcarts"}, "disc": {"disc", "discs"},
	"box-spine": {"box-spine", "boxspines"}, "banner": {"banner", "banners"}, "icon": {"icon", "icons"},
}

func loadProfile(root, name string) (Profile, error) {
	var input io.Reader
	if name != "" {
		f, err := catalogfs.Open(name)
		if err != nil {
			return Profile{}, err
		}
		defer f.Close()
		input = f
	}
	return loadProfileReader(root, input)
}

// ExportProfile parses caller-supplied policy bytes using server catalog defaults.
// It never treats a client filename as a server filesystem path.
func ExportProfile(root, content string) (Profile, error) {
	return loadProfileReader(root, strings.NewReader(content))
}

// ValidateExportProfile checks policy syntax without reading a client catalog.
func ValidateExportProfile(content string) error {
	_, err := loadProfileReader("", strings.NewReader(content))
	return err
}

func loadProfileReader(root string, input io.Reader) (Profile, error) {
	priorities := map[string][]string{
		"metadata": {"launchbox", "screenscraper", "openvgdb", "igdb"},
		"artwork":  {"emumovies", "launchbox", "screenscraper", "igdb", "openvgdb"},
		"video":    {"emumovies", "screenscraper"},
	}
	if root != "" {
		if contents, err := catalogfs.ReadFile(filepath.Join(root, "config", "sources.yaml")); err == nil {
			doc, err := catalogyaml.Parse(contents)
			if err != nil {
				return Profile{}, fmt.Errorf("source priorities: %w", err)
			}
			for level := range priorities {
				node := catalogyaml.Value(catalogyaml.Value(doc.Root(), "priority"), level)
				if node != nil {
					priorities[level] = catalogyaml.Strings(node)
				}
			}
		} else if !os.IsNotExist(err) {
			return Profile{}, err
		}
	}
	p := Profile{SchemaVersion: 1, Target: "es-de"}
	if input != nil {
		decoder := yaml.NewDecoder(input)
		decoder.KnownFields(true)
		if err := decoder.Decode(&p); err != nil {
			return p, fmt.Errorf("export profile: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return p, fmt.Errorf("export profile must contain one YAML document")
		}
	}
	if p.SchemaVersion != 1 {
		return p, fmt.Errorf("profile requires schema_version: 1")
	}
	if _, err := NormalizeTarget(p.Target); err != nil {
		return p, fmt.Errorf("profile target: %w", err)
	}
	// Selection is shared. Retain the version-1 policy marker internally; only
	// Options.Target selects the native projection.
	p.Target = "es-de"
	var err error
	p.LaunchBox, err = normalizeLaunchBoxPolicy(p.LaunchBox)
	if err != nil {
		return p, err
	}
	p.Titles, err = normalizeTitlePolicy(p.Titles)
	if err != nil {
		return p, err
	}
	p.Selection.Description = defaultRule(p.Selection.Description, Rule{Scope: "game", Order: []string{"language", "source"}, Languages: []string{"en"}, Sources: priorities["metadata"]})
	if p.Selection.Fields == nil {
		p.Selection.Fields = map[string]Rule{}
	}
	for _, field := range []string{"release_date", "developer", "publisher", "genres", "players"} {
		p.Selection.Fields[field] = defaultRule(p.Selection.Fields[field], Rule{Scope: "release", Order: []string{"source"}, Sources: priorities["metadata"]})
	}
	for field, rule := range p.Selection.Fields {
		if field != "release_date" && field != "developer" && field != "publisher" && field != "genres" && field != "players" {
			return p, fmt.Errorf("unsupported export field: %s", field)
		}
		if err := validateRule(rule, false, false); err != nil {
			return p, fmt.Errorf("field %s: %w", field, err)
		}
	}
	if err := validateRule(p.Selection.Description, false, true); err != nil {
		return p, fmt.Errorf("description: %w", err)
	}
	if p.Selection.Media == nil {
		p.Selection.Media = map[string]Rule{"cover": {}, "screenshot": {}, "logo": {}, "video": {}}
	}
	for role, rule := range p.Selection.Media {
		if _, ok := roles[role]; !ok {
			return p, fmt.Errorf("unsupported media role: %s", role)
		}
		sources, formats := priorities["artwork"], []string{"png", "jpg", "webp"}
		if role == "video" {
			sources, formats = priorities["video"], []string{"mp4", "webm", "mkv", "avi", "mov", "wmv", "m4v"}
		}
		if role == "manual" {
			formats = []string{"pdf"}
		}
		defaults := Rule{Scope: "release", Order: []string{"region", "source", "format"}, Regions: []string{"$release", "world", "unspecified"}, Sources: sources, Formats: formats}
		p.Selection.Media[role] = defaultRule(rule, defaults)
		if err := validateRule(p.Selection.Media[role], true, true); err != nil {
			return p, fmt.Errorf("media %s: %w", role, err)
		}
	}
	if err := validateMediaOverrides(p); err != nil {
		return p, err
	}
	if err := validateDescriptionFallbacks(p); err != nil {
		return p, err
	}
	return p, nil
}

func defaultRule(r, d Rule) Rule {
	if r.Scope == "" {
		r.Scope = d.Scope
	}
	r.Scope = strings.ToLower(strings.TrimSpace(r.Scope))
	if r.Order == nil {
		r.Order = d.Order
	}
	if r.Languages == nil {
		r.Languages = d.Languages
	}
	if r.Sources == nil {
		r.Sources = d.Sources
	}
	if r.Regions == nil {
		r.Regions = d.Regions
	}
	if r.Formats == nil {
		r.Formats = d.Formats
	}
	for _, list := range [][]string{r.Order, r.Sources, r.Languages, r.Regions, r.Formats, r.Variants, r.Styles} {
		for i := range list {
			list[i] = strings.ToLower(strings.TrimSpace(list[i]))
		}
	}
	return r
}

func validateRule(r Rule, media, localized bool) error {
	if r.Scope != "release" && !(r.Scope == "game" && (media || localized)) {
		return fmt.Errorf("scope must be release, or game for descriptions/media")
	}
	if r.Styles != nil && !media {
		return fmt.Errorf("styles apply only to media")
	}
	for _, style := range r.Styles {
		if style != "flat-front" && style != "front-spine" && style != "unknown" {
			return fmt.Errorf("unsupported artwork style: %s", style)
		}
	}
	if r.Variants != nil && !media {
		return fmt.Errorf("variants apply only to media")
	}
	for _, variant := range r.Variants {
		if variant != "original" && variant != "reconstructed" && variant != "fanart" && variant != "unknown" {
			return fmt.Errorf("unsupported artwork variant: %s", variant)
		}
	}
	if !media && (r.Regions != nil || r.Formats != nil) {
		return fmt.Errorf("regions and formats apply only to media")
	}
	if !localized && r.Languages != nil {
		return fmt.Errorf("languages apply only to descriptions or media")
	}
	if len(r.Order) == 0 {
		return fmt.Errorf("order must not be empty")
	}
	seen := map[string]bool{}
	for _, dimension := range r.Order {
		if seen[dimension] {
			return fmt.Errorf("duplicate order criterion: %s", dimension)
		}
		seen[dimension] = true
		if dimension == "language" && r.Languages == nil {
			return fmt.Errorf("language order requires a languages list")
		}
		if dimension == "region-preference" && !seen["region"] {
			return fmt.Errorf("region-preference order requires an earlier region criterion")
		}
		if dimension == "style" && len(r.Styles) == 0 {
			return fmt.Errorf("style order requires a styles list")
		}
		if dimension == "variant" && len(r.Variants) == 0 {
			return fmt.Errorf("variant order requires a variants list")
		}
		if dimension != "source" && !(localized && dimension == "language") && !(media && (dimension == "region" || dimension == "region-preference" || dimension == "format" || dimension == "variant" || dimension == "style" || dimension == "scope")) {
			return fmt.Errorf("unsupported criterion: %s", dimension)
		}
	}
	return nil
}

func within(root, name string) bool {
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func realPath(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	return catalogfs.EvalSymlinks(abs)
}
