package exporter

import (
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/medianames"
)

type candidate struct {
	text, source, language string
	rank                   []int
	fromRelease, selection string
}

func resolveFields(g *game, release string, profile Profile) map[string]Value {
	result := map[string]Value{}
	title := g.name
	if title == "" {
		title = release
	}
	result["title"] = Value{Value: title, Source: "catalog"}
	releaseNode := catalogyaml.Value(catalogyaml.Value(g.root, "releases"), release)
	for _, key := range []string{"description", "release_date", "developer", "publisher", "genres", "players"} {
		rule := profile.Selection.Fields[key]
		if key == "description" {
			rule = profile.Selection.Description
		}
		var values []candidate
		// Explicit catalog/release fields override automatic provider candidates.
		for _, node := range []*yaml.Node{releaseNode, g.root} {
			values = append(values, fieldCandidates(node, key, "local", rule)...)
		}
		if len(values) == 0 {
			for _, source := range rule.Sources {
				if key == "description" {
					for _, node := range descriptionNodes(g, source, release, rule.Scope) {
						for _, value := range fieldCandidates(node.node, key, source, rule) {
							value.rank = append(value.rank, node.distance)
							value.fromRelease = node.release
							if node.distance > 0 {
								value.selection = "game"
							}
							values = append(values, value)
						}
					}
				} else {
					for _, node := range sourceNodes(g.root, source, release) {
						values = append(values, fieldCandidates(node, key, source, rule)...)
					}
				}
			}
		}
		sort.SliceStable(values, func(i, j int) bool { return lessRank(values[i].rank, values[j].rank) })
		if len(values) > 0 {
			value := values[0]
			// Provider prose can retain XML/HTML entities after acquisition.
			// Decode one display layer; explicit local overrides stay literal.
			if key == "description" && value.source != "local" {
				value.text = html.UnescapeString(value.text)
			}
			result[key] = Value{Value: value.text, Source: value.source, Language: value.language, Selection: value.selection, FromRelease: value.fromRelease}
		}
	}
	return result
}

func fieldCandidates(node *yaml.Node, key, source string, rule Rule) []candidate {
	var result []candidate
	add := func(text, language string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		rank := make([]int, 0, len(rule.Order))
		for _, dimension := range rule.Order {
			value := 0
			if dimension == "source" {
				value = position(rule.Sources, source)
			}
			if dimension == "language" {
				value = position(rule.Languages, language)
			}
			if source == "local" && dimension == "source" {
				value = -1
			}
			if value < 0 && !(source == "local" && dimension == "source") {
				return
			}
			rank = append(rank, value)
		}
		result = append(result, candidate{text: text, source: source, language: language, rank: rank})
	}
	if key == "description" {
		descriptions := catalogyaml.Value(node, "descriptions")
		for _, language := range rule.Languages {
			add(field(descriptions, language), language)
		}
		if source == "local" && field(node, "description") != "" {
			result = append([]candidate{{text: field(node, "description"), source: source, rank: []int{-1}}}, result...)
		}
	} else {
		text := field(node, key)
		if key == "genres" {
			text = strings.Join(catalogyaml.Strings(catalogyaml.Value(node, key)), " / ")
		}
		if key == "players" && text == "" {
			text = field(node, "max_players")
		}
		add(text, "")
	}
	return result
}

func sourceNodes(root *yaml.Node, source, release string) []*yaml.Node {
	var result []*yaml.Node
	sourceNode := catalogyaml.Value(catalogyaml.Value(root, "sources"), source)
	_ = catalogyaml.Sequence(catalogyaml.Value(sourceNode, "matches"), func(match *yaml.Node) error {
		scopes := catalogyaml.Strings(catalogyaml.Value(match, "releases"))
		if direct := field(match, "release"); direct != "" {
			scopes = append(scopes, direct)
		}
		_ = catalogyaml.Sequence(catalogyaml.Value(match, "releases"), func(child *yaml.Node) error {
			if child.Kind == yaml.MappingNode {
				scopes = append(scopes, field(child, "release"))
				if field(child, "release") == release {
					result = append(result, child)
				}
			}
			return nil
		})
		if len(scopes) == 0 || position(scopes, release) >= 0 {
			result = append(result, match)
		}
		return nil
	})
	return result
}

func position(list []string, value string) int {
	for i, item := range list {
		if strings.EqualFold(item, value) {
			return i
		}
	}
	return -1
}
func lessRank(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func regionKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, " ", "-")))
	switch value {
	case "north-america", "united-states", "us", "u":
		return "usa"
	case "eu", "eur":
		return "europe"
	case "jp", "jpn":
		return "japan"
	case "":
		return "unspecified"
	}
	return value
}

type mediaCandidate struct {
	path, source, region, language, format string
	rank                                   []int
	variant, fromRelease                   string
	style                                  mediaStyle
}

func resolveMedia(root string, g *game, release, rom string, profile Profile, releaseSpecific bool, system string, regionHints ...[]string) (map[string]Media, []string, error) {
	result := map[string]Media{}
	var warnings []string
	mediaRoot := filepath.Join(g.path, "media")
	var paths []string
	err := catalogfs.WalkDir(mediaRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(mediaRoot, path)
		if err != nil {
			return err
		}
		parent := filepath.Dir(rel)
		// Only ordinary registered release folders, never _fold or arbitrary trees.
		if parent == "." || catalogyaml.Value(catalogyaml.Value(g.root, "releases"), parent) != nil {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	releaseNode := catalogyaml.Value(catalogyaml.Value(g.root, "releases"), release)
	regions := catalogyaml.Strings(catalogyaml.Value(releaseNode, "regions"))
	if len(regionHints) > 0 && len(regionHints[0]) > 0 {
		regions = regionHints[0]
	}
	roleNames := make([]string, 0, len(profile.Selection.Media))
	for role := range profile.Selection.Media {
		roleNames = append(roleNames, role)
	}
	sort.Strings(roleNames)
	for _, role := range roleNames {
		rule := profile.Selection.Media[role]
		mapping := roles[role]
		var styles map[string]mediaStyle
		if rule.Styles != nil {
			styles, err = g.styles()
			if err != nil {
				return nil, nil, err
			}
		}
		override, err := pinnedMedia(root, g, release, rom, profile, releaseSpecific, role, system)
		if err != nil {
			return nil, nil, err
		}
		if override != nil {
			if rule.Styles != nil {
				style := assetStyle(styles, filepath.Join(root, filepath.FromSlash(override.SourcePath)))
				override.Style, override.StyleEvidence, override.SourceType = style.style, style.evidence, style.sourceType
			}
			result[role] = *override
			warnings = append(warnings, "explicit reviewed "+role+" override: "+override.SourcePath)
			continue
		}
		var candidates []mediaCandidate
		for _, path := range paths {
			parent := filepath.Dir(path)
			fromRelease := ""
			if parent != mediaRoot {
				fromRelease = filepath.Base(parent)
				if !(releaseSpecific && fromRelease == release) && rule.Scope != "game" {
					continue
				}
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
			if ext == "jpeg" {
				ext = "jpg"
			}
			if !supportedMediaFormat(role, ext) || position(rule.Formats, ext) < 0 {
				continue
			}
			stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			variant, parts := medianames.SplitVariant(strings.Split(stem, "."))
			kind := parts[0]
			ordinal := 0
			if dash := strings.LastIndex(kind, "-"); dash >= 0 {
				if number, err := strconv.Atoi(kind[dash+1:]); err == nil {
					ordinal = number
					kind = kind[:dash]
				}
			}
			if kind != mapping.Kind && !(role == "video" && kind == "video") {
				continue
			}
			source, region, language := "local", "unspecified", ""
			if len(parts) > 1 {
				source = strings.ToLower(parts[len(parts)-1])
			}
			if len(parts) > 2 {
				region = regionKey(parts[1])
			}
			if len(parts) > 3 {
				language = strings.ToLower(parts[2])
			}
			sourceRank := position(rule.Sources, source)
			if source == "local" {
				sourceRank = -1
			}
			if sourceRank < 0 && source != "local" {
				continue
			}
			regionRank := rankRegion(rule.Regions, region, regions)
			if regionRank < 0 {
				continue
			}
			languageRank := 0
			if rule.Languages != nil {
				languageRank = position(rule.Languages, language)
				if languageRank < 0 {
					languageRank = position(rule.Languages, "*")
				}
				if languageRank < 0 {
					continue
				}
			}
			style := assetStyle(styles, path)
			if rule.Styles != nil && position(rule.Styles, style.style) < 0 {
				continue
			}
			if rule.Variants != nil && position(rule.Variants, variant) < 0 {
				continue
			}
			scope := 1
			if fromRelease != "" {
				scope = 2
				if releaseSpecific && fromRelease == release {
					scope = 0
				}
			}
			var rank []int
			if position(rule.Order, "scope") < 0 {
				rank = append(rank, scope)
			}
			for _, dimension := range rule.Order {
				switch dimension {
				case "source":
					rank = append(rank, sourceRank)
				case "region":
					rank = append(rank, regionRank)
				case "region-preference":
					rank = append(rank, rankRegionPreference(rule.Regions, region, regionRank))
				case "format":
					rank = append(rank, position(rule.Formats, ext))
				case "language":
					rank = append(rank, languageRank)
				case "style":
					rank = append(rank, position(rule.Styles, style.style))
				case "variant":
					rank = append(rank, position(rule.Variants, variant))
				case "scope":
					rank = append(rank, scope)
				}
			}
			rank = append(rank, ordinal)
			candidates = append(candidates, mediaCandidate{path: path, source: source, region: region, language: language, format: ext, rank: rank, variant: variant, fromRelease: fromRelease, style: style})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if lessRank(candidates[i].rank, candidates[j].rank) {
				return true
			}
			if lessRank(candidates[j].rank, candidates[i].rank) {
				return false
			}
			return candidates[i].path < candidates[j].path
		})
		for _, candidate := range candidates {
			real, err := realPath(candidate.path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, nil, err
			}
			if !within(root, real) {
				return nil, nil, fmt.Errorf("media path escapes catalog root")
			}
			info, err := catalogfs.Stat(real)
			if err != nil {
				return nil, nil, err
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				continue
			}
			rel, _ := filepath.Rel(root, real)
			stem := strings.TrimSuffix(rom, filepath.Ext(rom))
			output := filepath.ToSlash(filepath.Join("downloaded_media", system, mapping.Dir, filepath.FromSlash(stem)+"."+candidate.format))
			selected := Media{SourcePath: filepath.ToSlash(rel), OutputPath: output, Source: candidate.source, Region: candidate.region, Language: candidate.language, Size: info.Size()}
			if rule.Styles != nil {
				selected.Style, selected.StyleEvidence, selected.SourceType = candidate.style.style, candidate.style.evidence, candidate.style.sourceType
			}
			if rule.Variants != nil {
				selected.Variant, selected.VariantEvidence = candidate.variant, "filename"
				if candidate.variant == "unknown" {
					selected.VariantEvidence = "unspecified-filename-variant"
				}
			}
			if candidate.fromRelease != "" && (!releaseSpecific || candidate.fromRelease != release) {
				selected.Selection, selected.FromRelease = "game", candidate.fromRelease
			}
			result[role] = selected
			break
		}
		if _, ok := result[role]; !ok {
			warnings = append(warnings, "no eligible local "+role)
		}
	}
	return result, warnings, nil
}

// Region affinity retains the original selector semantics: all regions within
// the same $release tier tie unless the profile explicitly opts into a tie-break.
func rankRegion(list []string, region string, releases []string) int {
	for i, item := range list {
		if item == "*" {
			return i
		}
		if item == "$release" {
			for _, release := range releases {
				if regionKey(release) == region {
					return i
				}
			}
		} else if regionKey(item) == region {
			return i
		}
	}
	return -1
}

// Explicit preference applies only inside a matching $release tier. Other
// tiers remain neutral, and a preferred nonrelease region cannot beat affinity.
func rankRegionPreference(list []string, region string, affinity int) int {
	if affinity < 0 || affinity >= len(list) || list[affinity] != "$release" {
		return 0
	}
	for i, item := range list {
		if item != "$release" && (item == "*" || regionKey(item) == region) {
			return i
		}
	}
	return len(list)
}
func supportedMediaFormat(role, format string) bool {
	if role == "manual" {
		return format == "pdf" || format == "md" || format == "txt"
	}
	if role == "video" {
		return position([]string{"mp4", "mkv", "avi", "wmv", "mov", "webm", "m4v"}, format) >= 0
	}
	return format == "png" || format == "jpg" || format == "webp"
}
