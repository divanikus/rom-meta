package exporter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LaunchBoxPolicy ranks launchable versions. Unlisted values follow listed ones;
// ties use the complete version label and original relative path.
type LaunchBoxPolicy struct {
	Kinds     []string `yaml:"kinds,omitempty" json:"kinds"`
	Regions   []string `yaml:"regions,omitempty" json:"regions,omitempty"`
	Languages []string `yaml:"languages,omitempty" json:"languages,omitempty"`
}

func normalizeLaunchBoxPolicy(p LaunchBoxPolicy) (LaunchBoxPolicy, error) {
	if p.Kinds == nil {
		p.Kinds = []string{"original"}
	}
	for _, list := range []*[]string{&p.Kinds, &p.Regions, &p.Languages} {
		if len(*list) > 32 {
			return p, fmt.Errorf("launchbox preference lists allow at most 32 values")
		}
		values := append([]string{}, (*list)...)
		seen := map[string]bool{}
		for i, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" || len(value) > 64 || strings.ContainsAny(value, "\r\n\t") || seen[value] {
				return p, fmt.Errorf("invalid or duplicate launchbox preference: %q", value)
			}
			seen[value] = true
			values[i] = value
		}
		*list = values
	}
	return p, nil
}

// ClientExportProfile removes the client-only rendering section before calling
// an older catalog API. Validation still covers the full caller profile.
func ClientExportProfile(content string) (string, LaunchBoxPolicy, error) {
	if content == "" {
		p, err := normalizeLaunchBoxPolicy(LaunchBoxPolicy{})
		return "", p, err
	}
	p, err := loadProfileReader("", strings.NewReader(content))
	if err != nil {
		return "", LaunchBoxPolicy{}, err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal([]byte(content), &doc); err != nil {
		return "", p.LaunchBox, err
	}
	node := doc.Content[0]
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "launchbox" {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			data, err := yaml.Marshal(&doc)
			return string(data), p.LaunchBox, err
		}
	}
	return content, p.LaunchBox, nil
}

type LaunchBoxVersion struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	Records           []int  `json:"records"`
	GeneratedPlaylist bool   `json:"generated_playlist,omitempty"`
}
type LaunchBoxGroup struct {
	ID       string             `json:"id"`
	Game     string             `json:"game,omitempty"`
	Title    string             `json:"title"`
	Primary  string             `json:"primary"`
	Versions []LaunchBoxVersion `json:"versions"`
}
type LaunchBoxProjection struct {
	Groups       []LaunchBoxGroup `json:"groups"`
	VersionCount int              `json:"version_count"`
	Warnings     []string         `json:"warnings,omitempty"`
}

type launchBoxApplication struct {
	ID              string `xml:"Id"`
	GameID          string `xml:"GameID"`
	Name            string `xml:"Name"`
	ApplicationPath string `xml:"ApplicationPath"`
	Section         string `xml:"Section"`
	Version         string `xml:"Version"`
	UseEmulator     bool   `xml:"UseEmulator"`
	EmulatorID      string `xml:"EmulatorId"`
	Installed       bool   `xml:"Installed"`
	Priority        int    `xml:"Priority"`
	Region          string `xml:"Region,omitempty"`
	ReleaseDate     string `xml:"ReleaseDate,omitempty"`
	Developer       string `xml:"Developer,omitempty"`
	Publisher       string `xml:"Publisher,omitempty"`
	// Disc is intentionally absent: auto-M3U generation considers all numbered
	// attached versions and could mix regional/revision sets on this game card.
}

func launchBoxAbsolute(plan *Plan, r Record) string {
	name := filepath.ToSlash(filepath.Clean(filepath.Join(plan.ROMRoot, filepath.FromSlash(launchBoxFile(r)))))
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	return name
}
func launchBoxVersionID(plan *Plan, r Record) string {
	return launchBoxUUID(launchBoxPlatform(plan.System) + "\nversion:" + launchBoxAbsolute(plan, r))
}
func launchBoxPlayable(plan *Plan, r Record) bool {
	return !r.Hidden && r.Status != "unsupported" && (!systems[plan.System].Arcade || r.Status == "matched")
}
func launchBoxKind(r Record) string {
	if r.VariantInfo != nil && r.VariantInfo.Kind != "" {
		return strings.ToLower(r.VariantInfo.Kind)
	}
	if r.Variant != "" {
		return "variant"
	}
	return "original"
}
func preference(values, preferred []string) int {
	best := len(preferred)
	for _, value := range values {
		for i, wanted := range preferred {
			if strings.EqualFold(value, wanted) && i < best {
				best = i
			}
		}
	}
	return best
}
func versionRank(r Record, p LaunchBoxPolicy) []int {
	var regions, languages []string
	if r.TitleContext != nil {
		regions, languages = r.TitleContext.Regions, r.TitleContext.Languages
	}
	if r.VariantInfo != nil && len(r.VariantInfo.Languages) > 0 {
		languages = r.VariantInfo.Languages
	}
	return []int{preference([]string{launchBoxKind(r)}, p.Kinds), preference(regions, p.Regions), preference(languages, p.Languages)}
}
func launchBoxVersionName(r Record) string {
	if r.Variant != "" {
		variant := r.Variant
		if len(r.Playlist) > 0 {
			variant = withoutTitleDisc(variant)
		}
		name := launchBoxKind(r) + ": " + variant
		if r.VariantInfo != nil {
			if len(r.VariantInfo.Languages) > 0 {
				name += " [" + strings.Join(r.VariantInfo.Languages, ", ") + "]"
			}
			if r.VariantInfo.Group != "" {
				name += " / " + r.VariantInfo.Group
			}
			if r.VariantInfo.Version != "" {
				name += " / v" + r.VariantInfo.Version
			}
		}
		if len(r.Playlist) > 0 {
			name += fmt.Sprintf(" [%d discs]", len(r.Playlist))
		}
		return name
	}
	name := r.Release
	if name == "" {
		name = r.MetadataRelease
	}
	if name == "" {
		name = r.Fields["title"].Value
	}
	if len(r.Playlist) > 0 {
		name = withoutTitleDisc(name) + fmt.Sprintf(" [%d discs]", len(r.Playlist))
	}
	if name == "" {
		name = r.ROM
	}
	return name
}

// Only an exact catalog release spelling with its Disc token removed can form
// an observed optical set. Other qualifiers remain significant.
var launchBoxDiscRE = regexp.MustCompile(`(?i)\((?:disc|disk) ([1-9][0-9]*)(?: of ([1-9][0-9]*))?\)`)

func launchBoxDiscKey(r Record) (string, int, int) {
	if r.Status != "matched" || r.Game == "" || r.Release == "" || r.Variant != "" || len(r.Playlist) > 0 || r.TitleContext == nil || r.TitleContext.Disc == "" {
		return "", 0, 0
	}
	ext := strings.ToLower(filepath.Ext(r.ROM))
	if ext != ".chd" && ext != ".iso" {
		return "", 0, 0
	}
	matches := launchBoxDiscRE.FindAllStringSubmatchIndex(r.Release, -1)
	if len(matches) != 1 {
		return "", 0, 0
	}
	m := matches[0]
	number, _ := strconv.Atoi(r.Release[m[2]:m[3]])
	total := 0
	if m[4] >= 0 {
		total, _ = strconv.Atoi(r.Release[m[4]:m[5]])
	}
	name := strings.TrimSpace(r.Release[:m[0]] + r.Release[m[1]:])
	key := r.Game + "\n" + name + "\n" + strings.Join(r.TitleContext.Regions, ",") + "\n" + strings.Join(r.TitleContext.Languages, ",") + "\n" + r.TitleContext.Revision
	return key, number, total
}

func buildLaunchBoxProjection(plan *Plan) (*LaunchBoxProjection, error) {
	policy, err := normalizeLaunchBoxPolicy(plan.Profile.LaunchBox)
	if err != nil {
		return nil, err
	}
	result := &LaunchBoxProjection{Groups: []LaunchBoxGroup{}}
	byGame := map[string][]int{}
	for i, r := range plan.Records {
		if !launchBoxPlayable(plan, r) {
			continue
		}
		if !ValidInputPath(launchBoxFile(r)) {
			return nil, fmt.Errorf("invalid launch path")
		}
		byGame[launchBoxID(plan, r)] = append(byGame[launchBoxID(plan, r)], i)
	}
	keys := make([]string, 0, len(byGame))
	for key := range byGame {
		keys = append(keys, key)
	}
	// Keep the visible group order deterministic in source path order.
	sort.Slice(keys, func(i, j int) bool {
		return plan.Records[byGame[keys[i]][0]].ROM < plan.Records[byGame[keys[j]][0]].ROM
	})
	for _, id := range keys {
		indices := byGame[id]
		g := LaunchBoxGroup{ID: id, Versions: []LaunchBoxVersion{}}
		if plan.Records[indices[0]].Status == "matched" {
			g.Game = plan.Records[indices[0]].Game
		}
		used := map[int]bool{}
		discs := map[string][]int{}
		for _, i := range indices {
			key, _, _ := launchBoxDiscKey(plan.Records[i])
			if key != "" {
				discs[key] = append(discs[key], i)
			}
		}
		for _, i := range indices {
			if used[i] {
				continue
			}
			r := plan.Records[i]
			v := LaunchBoxVersion{ID: launchBoxVersionID(plan, r), Name: launchBoxVersionName(r), Path: filepath.Join(plan.ROMRoot, filepath.FromSlash(launchBoxFile(r))), Records: []int{i}}
			key, _, _ := launchBoxDiscKey(r)
			members := append([]int(nil), discs[key]...)
			if key != "" && len(members) > 1 {
				sort.Slice(members, func(a, b int) bool {
					_, x, _ := launchBoxDiscKey(plan.Records[members[a]])
					_, y, _ := launchBoxDiscKey(plan.Records[members[b]])
					return x < y
				})
				complete := len(members) <= 32
				var identity strings.Builder
				identity.WriteString(launchBoxPlatform(plan.System) + "\ndiscset:" + key)
				for n, index := range members {
					_, number, total := launchBoxDiscKey(plan.Records[index])
					if number != n+1 || total > 0 && total != len(members) {
						complete = false
					}
					name := launchBoxAbsolute(plan, plan.Records[index])
					if strings.ContainsAny(name, "\r\n") {
						return nil, fmt.Errorf("disc path cannot contain a newline")
					}
					identity.WriteString("\n" + name)
				}
				if complete {
					v.ID = launchBoxUUID(identity.String())
					v.Records = members
					v.Name = launchBoxDiscRE.ReplaceAllString(launchBoxVersionName(plan.Records[members[0]]), "")
					v.Name = strings.TrimSpace(v.Name) + fmt.Sprintf(" [%d discs]", len(members))
					v.Path = filepath.ToSlash(filepath.Join("Playlists", launchBoxPlatform(plan.System), v.ID+".m3u"))
					v.GeneratedPlaylist = true
					result.Warnings = append(result.Warnings, "Generated M3U for observed consecutive catalog discs: "+v.Name+"; full catalog/set completeness is not verified")
				}
			}
			if key != "" && !v.GeneratedPlaylist {
				result.Warnings = append(result.Warnings, "Numbered disc remains an individual version; no safe observed consecutive set: "+r.ROM)
			}
			for _, index := range v.Records {
				used[index] = true
			}
			g.Versions = append(g.Versions, v)
		}
		sort.SliceStable(g.Versions, func(i, j int) bool {
			a, b := g.Versions[i], g.Versions[j]
			x, y := versionRank(plan.Records[a.Records[0]], policy), versionRank(plan.Records[b.Records[0]], policy)
			if lessRank(x, y) {
				return true
			}
			if lessRank(y, x) {
				return false
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.Path < b.Path
		})
		g.Primary = g.Versions[0].ID
		main := plan.Records[g.Versions[0].Records[0]]
		if main.Status == "matched" {
			g.Title = main.GameTitle
		}
		if g.Title == "" && main.Status == "matched" && main.TitleContext != nil {
			g.Title = main.TitleContext.Regional
		}
		if g.Title == "" {
			g.Title = main.Fields["title"].Value
		}
		counts := map[string]int{}
		for _, v := range g.Versions {
			counts[strings.ToLower(v.Name)]++
		}
		for i := range g.Versions {
			v := &g.Versions[i]
			if counts[strings.ToLower(v.Name)] > 1 {
				v.Name += " / " + plan.Records[v.Records[0]].ROM
			}
		}
		// Duplicate launches would produce duplicate native version IDs.
		seen := map[string]bool{}
		for _, v := range g.Versions {
			if seen[v.ID] {
				return nil, fmt.Errorf("duplicate LaunchBox launch identity")
			}
			seen[v.ID] = true
		}
		result.VersionCount += len(g.Versions)
		result.Groups = append(result.Groups, g)
	}
	return result, nil
}

func writeLaunchBoxPlaylists(plan *Plan, staging string) error {
	if plan.LaunchBox == nil {
		return nil
	}
	for _, g := range plan.LaunchBox.Groups {
		for _, v := range g.Versions {
			if !v.GeneratedPlaylist {
				continue
			}
			var body strings.Builder
			for _, i := range v.Records {
				name := filepath.Join(plan.ROMRoot, filepath.FromSlash(launchBoxFile(plan.Records[i])))
				if strings.ContainsAny(name, "\r\n") {
					return fmt.Errorf("invalid playlist input")
				}
				body.WriteString(name + "\n")
			}
			if !ValidInputPath(v.Path) {
				return fmt.Errorf("invalid generated playlist path")
			}
			if err := writeOutput(filepath.Join(staging, filepath.FromSlash(v.Path)), []byte(body.String())); err != nil {
				return err
			}
		}
	}
	return nil
}
