package exporter

// Export-only game association tolerates known optical filename qualifiers. It
// never identifies a dump/release or a registered modification by its title.
import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/regions"
	"rommeta/internal/titlematch"
)

var opticalQualifierRE = regexp.MustCompile(`(?i)^(?:(?:beta|proto(?:type)?|demo|sample|preview|promo)(?:[ ._-].*)?|unl|unlicensed|[0-9]{4}[-.][0-9x?]{2}[-.][0-9x?]{2}|[0-9]+[a-z][0-9a-z]*(?:[- ][0-9a-z]+)*|made in .+|hybrid version)$`)

var opticalLanguageVersionRE = regexp.MustCompile(`(?i)^([a-z]{2}) (?:v[0-9][0-9a-z._-]*|beta)$`)
var opticalShortTitleRE = regexp.MustCompile(`(?::\s+|\s+-\s+)`)

func opticalTitle(name string) string {
	name = filepath.Base(name)
	if nameExtension(strings.ToLower(filepath.Ext(name))) {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	plain, _ := splitTitleLabels(name)
	base, groups := titleSuffixes(plain)
	for _, group := range groups {
		technical := true
		for _, part := range strings.Split(group, ",") {
			part = strings.TrimSpace(part)
			_, region := regions.NormalizeDATDisplay(part)
			if !region && !titleLanguage(part) && !titleRevisionRE.MatchString(part) && !titleDiscRE.MatchString(part) && !opticalQualifierRE.MatchString(part) && !opticalLanguageVersion(part) {
				technical = false
			}
		}
		if !technical {
			base += " (" + group + ")"
		}
	}
	return titlematch.Key(base)
}

func addOpticalName(index *map[string][]*game, key string, g *game) {
	if key == "" {
		return
	}
	if *index == nil {
		*index = map[string][]*game{}
	}
	for _, old := range (*index)[key] {
		if old.path == g.path {
			return
		}
	}
	(*index)[key] = append((*index)[key], g)
}
func (index *catalogIndex) addExportName(name string, g *game, canonical bool) {
	key := opticalTitle(name)
	addOpticalName(&index.ExportNames, key, g)
	if canonical {
		addOpticalName(&index.ExportCanonical, key, g)
	}
	// A shortened title is weaker still and is considered only for an explicitly
	// modified optical filename. Shared series prefixes remain ambiguous.
	for _, at := range opticalShortTitleRE.FindAllStringIndex(name, -1) {
		short := opticalTitle(name[:at[0]])
		if len(strings.Fields(short)) >= 2 {
			addOpticalName(&index.ExportShortNames, short, g)
		}
	}
}
func indexExportNames(index *catalogIndex, g *game) {
	index.addExportName(g.name, g, true)
	var aliases func(*yaml.Node, bool)
	aliases = func(node *yaml.Node, canonical bool) {
		if node == nil {
			return
		}
		if node.Kind == yaml.ScalarNode {
			index.addExportName(node.Value, g, canonical)
			return
		}
		if node.Kind == yaml.SequenceNode {
			for _, child := range node.Content {
				aliases(child, canonical)
			}
			return
		}
		for _, key := range []string{"title", "name", "aliases", "alternate_titles", "matches"} {
			aliases(catalogyaml.Value(node, key), canonical)
		}
	}
	for _, key := range []string{"aliases", "alternate_titles"} {
		aliases(catalogyaml.Value(g.root, key), true)
	}
	_ = catalogyaml.Mapping(catalogyaml.Value(g.root, "sources"), func(_ string, node *yaml.Node) error { aliases(node, false); return nil })
	_ = catalogyaml.Mapping(catalogyaml.Value(g.root, "variants"), func(_ string, node *yaml.Node) error { aliases(catalogyaml.Value(node, "title"), false); return nil })
	_ = catalogyaml.Mapping(catalogyaml.Value(g.root, "releases"), func(name string, node *yaml.Node) error {
		index.addExportName(name, g, true)
		index.addExportName(field(catalogyaml.Value(node, "authority"), "entry_name"), g, true)
		aliases(catalogyaml.Value(node, "aliases"), true)
		return nil
	})
}
func opticalLanguageVersion(token string) bool {
	m := opticalLanguageVersionRE.FindStringSubmatch(token)
	return len(m) > 1 && titleLanguage(m[1])
}
func opticalModified(r Record) bool {
	if translationInput(&r) {
		return true
	}
	name := strings.TrimSuffix(filepath.Base(r.ROM), filepath.Ext(r.ROM))
	plain, labels := splitTitleLabels(name)
	if len(labels) > 0 {
		return true
	}
	_, groups := titleSuffixes(plain)
	for _, group := range groups {
		if opticalLanguageVersion(group) {
			return true
		}
	}
	return false
}

func observedTitleContext(name string) *TitleContext {
	name = filepath.Base(name)
	if nameExtension(strings.ToLower(filepath.Ext(name))) {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	c := &TitleContext{Release: name}
	c.Regional = cleanRegionalTitle(name, c)
	c.Regions, c.Languages = sortedTitleValues(c.Regions), sortedTitleValues(c.Languages)
	return c
}

func opticalStage(name string) string {
	_, groups := titleSuffixes(name)
	for _, group := range groups {
		lower := strings.ToLower(group)
		for _, stage := range []string{"beta", "proto", "demo", "sample", "preview", "promo"} {
			if strings.HasPrefix(lower, stage) {
				return stage
			}
		}
	}
	return "original"
}

func opticalMetadataBasis(g *game, input *TitleContext) string {
	var choices []string
	_ = catalogyaml.Mapping(catalogyaml.Value(g.root, "releases"), func(release string, _ *yaml.Node) error {
		c := releaseTitleContext(g.root, release)
		if opticalStage(c.Release) != opticalStage(input.Release) {
			return nil
		}
		if len(input.Regions) > 0 && strings.Join(c.Regions, ",") != strings.Join(input.Regions, ",") {
			return nil
		}
		if input.Disc != "" && opticalDiscNumber(c.Disc) != opticalDiscNumber(input.Disc) {
			return nil
		}
		choices = append(choices, release)
		return nil
	})
	if len(choices) == 1 {
		return choices[0]
	}
	return catalogyaml.DefaultRelease(g.root)
}

func qualifiedNameFallback(root string, index *catalogIndex, record *Record, profile Profile, system string) error {
	// Consumer lookup retains its strict identity rules. This is called only by
	// export name fallback after fingerprint and complete-name matching failed.
	if record.Disc == nil {
		return nil
	}
	key := opticalTitle(record.ROM)
	candidates := index.ExportCanonical[key]
	if len(candidates) == 0 {
		candidates = index.ExportNames[key]
	}
	shortened := false
	if len(candidates) == 0 && opticalModified(*record) {
		candidates = index.ExportShortNames[key]
		shortened = true
	}
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) > 1 {
		record.Status, record.Reason = "ambiguous", "qualified optical title identifies multiple catalog games"
		for _, g := range candidates {
			record.Candidates = append(record.Candidates, g.name)
		}
		sort.Strings(record.Candidates)
		return nil
	}
	g := candidates[0]
	if err := loadGameDocument(root, g); err != nil {
		return err
	}
	context := observedTitleContext(record.ROM)
	if err := attachMetadataRecord(root, g, "", opticalMetadataBasis(g, context), record, profile, system); err != nil {
		return err
	}
	record.TitleContext = context
	record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "unique qualified catalog/provider title; exact release and dump remain unverified"
	if shortened {
		record.Reason = "unique shortened modified optical title; exact release and modification remain unverified"
	}
	record.Warnings = append(record.Warnings, "Game association by optical filename; the observed version name is preserved and no exact release or modification is claimed")
	return nil
}

// Roman numerals are accepted only in an explicit Disc/Disk token, never in
// the game title. Canonical forms prevent invalid numerals from forming sets.
func opticalDiscNumber(token string) int {
	m := launchBoxDiscRE.FindStringSubmatch("(" + token + ")")
	if len(m) == 0 {
		return 0
	}
	return opticalNumber(m[1])
}
func opticalNumber(s string) int {
	for n := 1; n <= 32; n++ {
		if s == strconv.Itoa(n) || strings.EqualFold(s, romanDisc(n)) {
			return n
		}
	}
	return 0
}
func romanDisc(n int) string {
	out := ""
	for i, v := range []int{10, 9, 5, 4, 1} {
		for n >= v {
			out += []string{"X", "IX", "V", "IV", "I"}[i]
			n -= v
		}
	}
	return out
}
