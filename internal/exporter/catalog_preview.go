package exporter

import (
	"context"
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
)

// CatalogPreview is a metadata projection, never a ROM match or native merge plan.
// Media descriptors are selected without opening their payloads.
type CatalogPreview struct {
	Release  string           `json:"release,omitempty"`
	Variant  string           `json:"variant,omitempty"`
	Fields   map[string]Value `json:"fields"`
	Media    map[string]Media `json:"media"`
	Warnings []string         `json:"warnings,omitempty"`
	record   Record
}

// PreviewCatalog reuses export selection for one already-read catalog document.
// An absent edition uses the declared default or the sole release. Multiple
// releases without a default remain game-scoped instead of guessing an edition.
func PreviewCatalog(ctx context.Context, root, gamePath string, node *yaml.Node, profile Profile, release, variant string) (CatalogPreview, error) {
	if err := ctx.Err(); err != nil {
		return CatalogPreview{}, err
	}
	if release != "" && variant != "" {
		return CatalogPreview{}, fmt.Errorf("choose a release or a variant")
	}
	var err error
	root, err = realPath(root)
	if err != nil {
		return CatalogPreview{}, err
	}
	gamePath, err = realPath(gamePath)
	if err != nil {
		return CatalogPreview{}, err
	}
	if !within(root, gamePath) {
		return CatalogPreview{}, fmt.Errorf("game path escapes catalog root")
	}
	releases := catalogyaml.Value(node, "releases")
	if variant == "" && release == "" {
		release = field(node, "default_release")
		if release == "" && releases != nil && len(releases.Content) == 2 {
			release = releases.Content[0].Value
		}
	}
	if release != "" && catalogyaml.Value(releases, release) == nil {
		return CatalogPreview{}, fmt.Errorf("unknown release: %s", release)
	}
	if variant != "" && catalogyaml.Value(catalogyaml.Value(node, "variants"), variant) == nil {
		return CatalogPreview{}, fmt.Errorf("unknown variant: %s", variant)
	}
	g := &game{path: gamePath, name: field(node, "title"), root: node, context: ctx}
	if g.name == "" {
		g.name = filepath.Base(gamePath)
	}
	record := Record{}
	if release == "" && variant == "" {
		err = attachMetadataRecord(root, g, "", "", &record, profile, "")
	} else {
		err = attachFingerprintRecord(root, fingerprint{game: g, release: release, variant: variant}, &record, profile, "")
	}
	if err != nil {
		return CatalogPreview{}, err
	}
	if record.TitleContext != nil {
		// Only the catalog edition is known. Input labels and cross-input duplicate
		// disambiguation are deliberately left to real export with supplied ROMs.
		record.Status = "matched"
		record.Fields["title"] = renderExportTitle(record, profile.Titles, profile.Titles.Mode, nil, false)
	}
	for role, asset := range record.Media {
		asset.OutputPath = ""
		record.Media[role] = asset
	}
	if exportExcluded(filepath.Base(filepath.Dir(gamePath))) {
		record.Warnings = append(record.Warnings, "This platform is excluded from frontend export; metadata inspection remains available.")
	}
	if release == "" && variant == "" {
		if releases == nil || len(releases.Content) == 0 {
			record.Warnings = append(record.Warnings, "No releases registered: showing game-level selection.")
		} else {
			record.Warnings = append(record.Warnings, "No default release: choose an edition to inspect release-specific selection.")
		}
	}
	if err := ctx.Err(); err != nil {
		return CatalogPreview{}, err
	}
	return CatalogPreview{Release: record.MetadataRelease, Variant: record.Variant, Fields: record.Fields, Media: record.Media, Warnings: record.Warnings, record: record}, nil
}
