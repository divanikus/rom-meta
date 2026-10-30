package exporter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogyaml"
)

func (fp fingerprint) targetName() string {
	if fp.variant != "" {
		return fp.variant
	}
	return fp.release
}
func (fp fingerprint) targetKey() string {
	return fp.game.path + "\x00" + fp.release + "\x00" + fp.variant
}

func translationInput(record *Record) bool {
	name := strings.ToLower(record.ROM + "/" + record.Member)
	return strings.Contains("/"+name, "/translations/") || strings.Contains(name, "[t-") || strings.Contains(name, "[t+")
}

func attachFingerprintRecord(root string, fp fingerprint, record *Record, profile Profile, system string) error {
	if fp.variant == "" {
		return attachRecord(root, fp.game, fp.release, record, profile, system)
	}
	g := fp.game
	if g.root == nil {
		doc, digest, err := lookupDocument(root, filepath.Join(g.path, "game.yaml"))
		if err != nil {
			return err
		}
		if digest != g.documentDigest {
			return fmt.Errorf("catalog changed behind cached lookup index; review and restart API")
		}
		g.root = doc
	}
	chain, base, err := catalogyaml.VariantChain(g.root, fp.variant)
	if err != nil {
		return err
	}
	if err := attachRecord(root, g, base, record, profile, system); err != nil {
		return err
	}
	record.Release, record.Variant = "", fp.variant
	node := catalogyaml.Value(catalogyaml.Value(g.root, "variants"), fp.variant)
	info := &VariantInfo{Title: field(node, "title"), Kind: field(node, "kind"), BasedOn: field(node, "based_on"), Chain: chain, Group: field(node, "group"), Version: field(node, "version")}
	allVariants, err := catalogyaml.Variants(g.root)
	if err != nil {
		return err
	}
	for _, owner := range chain[1:] {
		for _, variant := range allVariants {
			if variant.Name == owner {
				info.Ancestors = append(info.Ancestors, variant)
			}
		}
	}
	record.VariantInfo = info
	selection := "base"
	if base == "" {
		selection = "game"
	}
	for key, value := range record.Fields {
		if key != "title" {
			if value.FromRelease == "" {
				value.FromRelease = record.MetadataRelease
			}
			if value.Selection == "" {
				value.Selection = selection
			}
			record.Fields[key] = value
		}
	}
	for role, media := range record.Media {
		if media.FromRelease == "" {
			media.FromRelease = record.MetadataRelease
		}
		if media.Selection == "" {
			media.Selection = selection
		}
		record.Media[role] = media
	}
	// Apply parent overrides first, then the closest child's explicit values.
	for i := len(chain) - 1; i >= 0; i-- {
		owner := chain[i]
		variant := catalogyaml.Value(catalogyaml.Value(g.root, "variants"), owner)
		for _, key := range []string{"description", "release_date", "developer", "publisher", "genres", "players"} {
			rule := profile.Selection.Fields[key]
			if key == "description" {
				rule = profile.Selection.Description
			}
			values := fieldCandidates(variant, key, "local", rule)
			sort.SliceStable(values, func(i, j int) bool { return lessRank(values[i].rank, values[j].rank) })
			if len(values) > 0 {
				value := values[0]
				record.Fields[key] = Value{Value: value.text, Language: value.language, Source: "local", Selection: "variant", FromVariant: owner}
			}
		}
		baseRegions := catalogyaml.Strings(catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(g.root, "releases"), record.MetadataRelease), "regions"))
		media, warnings, err := resolveMedia(root, g, owner, record.ROM, profile, true, system, baseRegions, []string{owner})
		if err != nil {
			return err
		}
		for role, asset := range media {
			asset.FromRelease, asset.FromVariant, asset.Selection = "", owner, "variant"
			record.Media[role] = asset
		}
		for _, warning := range warnings {
			if !strings.HasPrefix(warning, "no eligible local ") {
				record.Warnings = append(record.Warnings, warning)
			}
		}
	}
	var title string
	for _, owner := range chain {
		variant := catalogyaml.Value(catalogyaml.Value(g.root, "variants"), owner)
		if title == "" {
			title = field(variant, "title")
		}
		if len(info.Languages) == 0 {
			info.Languages = catalogyaml.Strings(catalogyaml.Value(variant, "languages"))
		}
	}
	if title == "" {
		title = fp.variant
	}
	context := &TitleContext{Release: fp.variant, Languages: info.Languages}
	if record.TitleContext != nil {
		context.Regions = record.TitleContext.Regions
	}
	context.Regional = cleanRegionalTitle(title, context)
	// Input language belongs to the derivative, not to its untranslated base.
	context.Languages = sortedTitleValues(info.Languages)
	if info.Version != "" {
		context.Revision = "Version " + info.Version
	}
	record.TitleContext = context
	record.Fields["title"] = Value{Value: context.Regional, Source: "variant", FromVariant: fp.variant}
	filtered := record.Warnings[:0]
	for _, warning := range record.Warnings {
		if strings.HasPrefix(warning, "no eligible local ") {
			role := strings.TrimPrefix(warning, "no eligible local ")
			if _, exists := record.Media[role]; exists {
				continue
			}
		}
		filtered = append(filtered, warning)
	}
	record.Warnings = filtered
	return nil
}

func sameVariantEdition(a, b Record) bool {
	if a.Variant == "" && b.Variant == "" {
		return true
	}
	if a.Variant == b.Variant {
		return true
	}
	familyA, nA, _ := discEdition(a.Variant)
	familyB, nB, _ := discEdition(b.Variant)
	if nA <= 0 || nB <= 0 || familyA != familyB || a.VariantInfo == nil || b.VariantInfo == nil {
		return false
	}
	x, y := a.VariantInfo, b.VariantInfo
	return x.Kind == y.Kind && x.Group == y.Group && x.Version == y.Version && strings.Join(x.Languages, "\x00") == strings.Join(y.Languages, "\x00")
}
