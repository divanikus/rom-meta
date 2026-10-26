package exporter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/regions"
)

// TitlePolicy controls display only; it never changes ROM binding or paths.
type TitlePolicy struct {
	Mode     string `yaml:"mode" json:"mode"`
	Labels   string `yaml:"labels" json:"labels"`
	Variants string `yaml:"variants" json:"variants"`
}

// TitleContext keeps unformatted release data so clients can render a complete
// system after collecting multiple API batches. No server paths are included.
type TitleContext struct {
	Regional  string   `json:"regional"`
	Release   string   `json:"release"`
	Regions   []string `json:"regions,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Revision  string   `json:"revision,omitempty"`
	Disc      string   `json:"disc,omitempty"`
	Labels    []string `json:"labels,omitempty"`
}

func normalizeTitlePolicy(p TitlePolicy) (TitlePolicy, error) {
	p.Mode = strings.ToLower(strings.TrimSpace(p.Mode))
	p.Labels = strings.ToLower(strings.TrimSpace(p.Labels))
	p.Variants = strings.ToLower(strings.TrimSpace(p.Variants))
	if p.Mode == "" {
		p.Mode = "regional"
	}
	if p.Labels == "" {
		p.Labels = "keep"
	}
	if p.Variants == "" {
		p.Variants = "auto"
	}
	if p.Mode != "regional" && p.Mode != "release" && p.Mode != "filename" {
		return p, fmt.Errorf("titles.mode must be regional, release or filename")
	}
	if p.Labels != "keep" && p.Labels != "drop" {
		return p, fmt.Errorf("titles.labels must be keep or drop")
	}
	if p.Variants != "none" && p.Variants != "auto" && p.Variants != "release" && p.Variants != "filename" {
		return p, fmt.Errorf("titles.variants must be none, auto, release or filename")
	}
	return p, nil
}

// Bracket blocks are opaque. Keep their bytes, order and repetitions in input;
// unmatched brackets remain ordinary text, and nested blocks are one label.
func splitTitleLabels(name string) (string, []string) {
	var out strings.Builder
	var labels []string
	for i := 0; i < len(name); {
		if name[i] != '[' {
			out.WriteByte(name[i])
			i++
			continue
		}
		depth, end := 1, i+1
		for ; end < len(name) && depth > 0; end++ {
			if name[end] == '[' {
				depth++
			}
			if name[end] == ']' {
				depth--
			}
		}
		if depth != 0 {
			out.WriteString(name[i:])
			break
		}
		labels = append(labels, name[i:end])
		out.WriteByte(' ')
		i = end
	}
	return strings.Join(strings.Fields(out.String()), " "), labels
}

func titleStem(name string) string {
	name = filepath.Base(filepath.FromSlash(name))
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func titleSuffixes(name string) (string, []string) {
	var suffixes []string
	name = strings.TrimSpace(name)
	for strings.HasSuffix(name, ")") {
		depth, start := 0, -1
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == ')' {
				depth++
			}
			if name[i] == '(' {
				depth--
				if depth == 0 {
					start = i
					break
				}
			}
		}
		if start < 0 || start > 0 && name[start-1] != ' ' {
			break
		}
		suffixes = append([]string{name[start+1 : len(name)-1]}, suffixes...)
		name = strings.TrimSpace(name[:start])
	}
	return name, suffixes
}

var titleRevisionRE = regexp.MustCompile(`(?i)^(?:rev(?:ision)?[ .-]+.+|version[ .-]+.+|v[0-9].*)$`)
var titleDiscRE = regexp.MustCompile(`(?i)^(?:disc|disk|side|tape)\s+[0-9a-z]+(?:\s+of\s+[0-9a-z]+)?$`)

func titleLanguage(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ar", "da", "de", "el", "en", "es", "fi", "fr", "it", "ja", "ko", "nl", "no", "pl", "pt", "ru", "sv", "zh":
		return true
	}
	return false
}
func sortedTitleValues(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func releaseTitleContext(root *yaml.Node, release string) *TitleContext {
	node := catalogyaml.Value(catalogyaml.Value(root, "releases"), release)
	authority := catalogyaml.Value(node, "authority")
	raw := field(authority, "entry_name")
	if raw == "" {
		raw = release
	}
	context := &TitleContext{Release: raw, Regions: catalogyaml.Strings(catalogyaml.Value(node, "regions")), Languages: catalogyaml.Strings(catalogyaml.Value(node, "languages"))}
	context.Regional = cleanRegionalTitle(raw, context)
	// Recognize the exact base written by the DAT importer; it is not a manual
	// display override. Other per-release titles include MAME descriptions.
	plain, _ := splitTitleLabels(raw)
	imported, _ := titleSuffixes(plain)
	if declared := field(node, "title"); declared != "" && declared != imported {
		context.Regional = cleanRegionalTitle(declared, context)
	}
	// A MAME entry_name is a machine identifier, not its human description.
	if strings.EqualFold(field(authority, "source"), "mame") {
		if description := field(node, "title"); description != "" {
			context.Release = description
		}
	}
	if context.Revision == "" {
		if version := field(authority, "version"); version != "" {
			context.Revision = "Version " + version
		}
	}
	context.Regions = sortedTitleValues(context.Regions)
	context.Languages = sortedTitleValues(context.Languages)
	return context
}

func cleanRegionalTitle(raw string, context *TitleContext) string {
	plain, labels := splitTitleLabels(raw)
	context.Labels = appendTitleLabels(context.Labels, labels...)
	base, groups := titleSuffixes(plain)
	for _, group := range groups {
		technical := true
		for _, token := range strings.Split(group, ",") {
			token = strings.TrimSpace(token)
			if region, ok := regions.NormalizeDATDisplay(token); ok {
				context.Regions = append(context.Regions, region)
				continue
			}
			language := titleLanguage(token)
			for _, known := range context.Languages {
				if strings.EqualFold(token, known) {
					language = true
				}
			}
			if language {
				context.Languages = append(context.Languages, strings.ToLower(token))
				continue
			}
			if titleRevisionRE.MatchString(token) {
				context.Revision = token
				continue
			}
			if titleDiscRE.MatchString(token) {
				context.Disc = token
				continue
			}
			technical = false
		}
		if !technical {
			base += " (" + group + ")"
		}
	}
	return base
}

func appendTitleLabels(labels []string, extra ...string) []string {
	for _, label := range extra {
		found := false
		for _, old := range labels {
			if old == label {
				found = true
				break
			}
		}
		if !found {
			labels = append(labels, label)
		}
	}
	return labels
}
func withoutTitleDisc(name string) string {
	plain, labels := splitTitleLabels(name)
	base, groups := titleSuffixes(plain)
	for _, g := range groups {
		if !titleDiscRE.MatchString(g) {
			base += " (" + g + ")"
		}
	}
	if len(labels) > 0 {
		base += " " + strings.Join(labels, " ")
	}
	return strings.TrimSpace(base)
}
func titleHasQualifier(name, value string) bool {
	_, groups := titleSuffixes(name)
	for _, group := range groups {
		if strings.EqualFold(group, value) {
			return true
		}
		for _, p := range strings.Split(group, ",") {
			if strings.EqualFold(strings.TrimSpace(p), value) {
				return true
			}
		}
	}
	return false
}
func titleDimension(t *TitleContext, dimension int) string {
	if t == nil {
		return ""
	}
	switch dimension {
	case 0:
		return strings.Join(t.Regions, ", ")
	case 1:
		return t.Revision
	default:
		values := append([]string(nil), t.Languages...)
		for i, v := range values {
			if len(v) > 0 {
				values[i] = strings.ToUpper(v[:1]) + v[1:]
			}
		}
		return strings.Join(values, ", ")
	}
}

func renderExportTitle(r Record, p TitlePolicy, mode string, qualifiers []string, relative bool) Value {
	filename := titleStem(r.ROM)
	if relative {
		filename = strings.TrimSuffix(r.ROM, filepath.Ext(r.ROM))
	}
	source, base := "filename", filename
	var labels []string
	if mode != "filename" && r.TitleContext != nil && r.Status == "matched" {
		source = "release"
		base = r.TitleContext.Regional
		if mode == "release" {
			base = r.TitleContext.Release
		}
		if r.Representation == "playlist-directory" {
			base = withoutTitleDisc(base)
		} else if mode == "regional" && r.TitleContext.Disc != "" && !titleHasQualifier(base, r.TitleContext.Disc) {
			base += " (" + r.TitleContext.Disc + ")"
		}
	}
	// Filename mode is literal unless the user explicitly drops its labels.
	if source == "filename" {
		if p.Labels == "drop" {
			base, _ = splitTitleLabels(base)
		}
		return Value{Value: base, Source: source}
	}
	base, labels = splitTitleLabels(base)
	_, inputLabels := splitTitleLabels(filename)
	_, memberLabels := splitTitleLabels(titleStem(r.Member))
	inputLabels = appendTitleLabels(inputLabels, memberLabels...)
	labels = appendTitleLabels(inputLabels, labels...)
	labels = appendTitleLabels(labels, r.TitleContext.Labels...)
	var add []string
	for _, q := range qualifiers {
		if q != "" && !titleHasQualifier(base, q) {
			add = append(add, q)
		}
	}
	if len(add) > 0 {
		base += " (" + strings.Join(add, ", ") + ")"
	}
	if p.Labels == "keep" && len(labels) > 0 {
		base = strings.TrimSpace(base) + " " + strings.Join(labels, " ")
	}
	basis := r.Release
	if basis == "" {
		basis = r.MetadataRelease
	}
	return Value{Value: strings.TrimSpace(base), Source: source, FromRelease: basis, Selection: mode}
}

func applyExportTitles(plan *Plan) error {
	p, err := normalizeTitlePolicy(plan.Profile.Titles)
	if err != nil {
		return err
	}
	plan.Profile.Titles = p
	groups := map[string][]int{}
	for i := range plan.Records {
		r := &plan.Records[i]
		if r.Fields == nil {
			r.Fields = map[string]Value{}
		}
		r.Fields["title"] = renderExportTitle(*r, p, p.Mode, nil, false)
		if !r.Hidden && r.Status == "matched" && r.Game != "" {
			groups[r.Game] = append(groups[r.Game], i)
		}
	}
	for _, ids := range groups {
		if len(ids) < 2 || p.Variants == "none" {
			continue
		}
		if p.Variants != "auto" {
			for _, i := range ids {
				r := &plan.Records[i]
				r.Fields["title"] = renderExportTitle(*r, p, p.Variants, nil, false)
			}
			continue
		}
		if p.Mode != "regional" {
			continue
		}
		qualifiers := make(map[int][]string, len(ids))
		for dim := 0; dim < 3; dim++ {
			// Region identifies the selected editions even when their local names
			// differ. Add revision/language only within still indistinguishable names.
			partitions := map[string][]int{"": ids}
			if dim > 0 {
				partitions = map[string][]int{}
				for _, i := range ids {
					key := strings.ToLower(plan.Records[i].Fields["title"].Value)
					partitions[key] = append(partitions[key], i)
				}
			}
			for _, part := range partitions {
				if len(part) < 2 {
					continue
				}
				first, differs := titleDimension(plan.Records[part[0]].TitleContext, dim), false
				for _, i := range part[1:] {
					if first != titleDimension(plan.Records[i].TitleContext, dim) {
						differs = true
					}
				}
				if !differs {
					continue
				}
				for _, i := range part {
					r := &plan.Records[i]
					qualifiers[i] = append(qualifiers[i], titleDimension(r.TitleContext, dim))
					r.Fields["title"] = renderExportTitle(*r, p, p.Mode, qualifiers[i], false)
				}
			}
		}
	}
	if p.Variants == "auto" {
		// Also handle homonyms and duplicate input basenames, without grouping games
		// by display title or inventing a region/revision for unidentified inputs.
		for _, relative := range []bool{false, true} {
			collisions := map[string][]int{}
			for i, r := range plan.Records {
				if !r.Hidden {
					key := strings.ToLower(r.Fields["title"].Value)
					collisions[key] = append(collisions[key], i)
				}
			}
			for _, ids := range collisions {
				if len(ids) > 1 {
					for _, i := range ids {
						r := &plan.Records[i]
						r.Fields["title"] = renderExportTitle(*r, p, "filename", nil, relative)
					}
				}
			}
		}
	}
	for _, r := range plan.Records {
		if strings.TrimSpace(r.Fields["title"].Value) == "" {
			return fmt.Errorf("display title is empty for %q; keep bracket labels or use a nonempty input name", r.ROM)
		}
	}
	return nil
}
