package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/platformnames"
)

type PlatformOption struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Aliases       []string `json:"aliases"`
	ExportSystems []string `json:"export_systems"`
	ExportTargets []string `json:"export_targets"`
	Extensions    []string `json:"extensions,omitempty"`
	Catalogs      []string `json:"catalogs"`
	CatalogStatus string   `json:"catalog_status"`
	Reason        string   `json:"reason,omitempty"`
}
type PlatformChoice struct {
	PlatformOption
	System string
}
type PlatformSelection struct {
	Requested string `json:"requested"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	System    string `json:"system"`
	Folder    string `json:"folder"`
	MatchedBy string `json:"matched_by"`
}
type PlatformSelector struct {
	Names        platformnames.Registry
	Catalogs     []LookupPlatform
	CatalogKnown bool
}

func LoadSelectorNames(root string) (platformnames.Registry, error) {
	names, _, err := platformnames.LoadRegistry(root)
	if err != nil {
		return names, err
	}
	body, err := catalogfs.ReadFile(filepath.Join(root, "config", "sources.yaml"))
	if os.IsNotExist(err) {
		return names, nil
	}
	if err != nil {
		return names, err
	}
	var config struct {
		Aliases map[string]string `yaml:"platform_aliases"`
	}
	if err = yaml.Unmarshal(body, &config); err != nil {
		return names, fmt.Errorf("platform aliases in config/sources.yaml: %w", err)
	}
	names.MergeAliases(config.Aliases)
	return names, nil
}
func selectorNames(entry platformnames.Platform) []string {
	names := append([]string{}, entry.Aliases...)
	for _, ref := range entry.Sources {
		names = append(names, ref.Name)
		names = append(names, ref.Names...)
	}
	return uniqueNames(names)
}
func uniqueNames(values []string) []string {
	values = append([]string{}, values...)
	sort.Strings(values)
	seen := map[string]bool{}
	var result []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		key := platformnames.Normalize(v)
		if key != "" && !seen[key] {
			seen[key] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}
func NewPlatformSelector(names platformnames.Registry, catalogs []LookupPlatform) *PlatformSelector {
	p := &PlatformSelector{Names: names.Clone(), Catalogs: append([]LookupPlatform(nil), catalogs...), CatalogKnown: catalogs != nil}
	for _, system := range SupportedSystems() {
		p.Names.AddAliases(systems[system].Platform, system)
	}
	for i := range p.Catalogs {
		c := &p.Catalogs[i]
		name := c.Name
		if name == "" {
			if entry, ok := p.Names.Entry(c.Platform); ok {
				name = entry.Name
			} else {
				for _, alias := range c.Aliases {
					if entry, ok := p.Names.Entry(alias); ok {
						name = entry.Name
						break
					}
				}
			}
		}
		if name == "" {
			for _, id := range c.ExportSystems {
				if spec, ok := systems[id]; ok {
					name = spec.Platform
					break
				}
			}
			if name == "" {
				name = c.Platform
			}
		}
		c.Name = name
		aliases := append([]string{c.Platform}, c.SelectorAliases...)
		// Preserve ambiguous provider spellings rather than promoting them to aliases.
		for _, alias := range c.Aliases {
			if len(p.Names.Candidates(alias)) <= 1 {
				aliases = append(aliases, alias)
			}
		}
		p.Names.AddAliases(name, aliases...)
	}
	return p
}
func LoadPlatformSelector(root string) (*PlatformSelector, error) {
	names, err := LoadSelectorNames(root)
	if err != nil {
		return nil, err
	}
	catalog := &LookupCatalog{Root: root, Platforms: []LookupPlatform{}}
	if err = catalog.discoverPlatforms(); err != nil {
		return nil, err
	}
	return NewPlatformSelector(names, catalog.Platforms), nil
}
func (p *PlatformSelector) effectiveSystems(c LookupPlatform) []string {
	ids := c.ExportSystems
	for _, parent := range p.Catalogs {
		for _, member := range parent.Catalogs {
			if member == c.Platform {
				ids = append(append([]string{}, ids...), parent.ExportSystems...)
			}
		}
	}
	var known []string
	for _, id := range ids {
		if _, ok := systems[id]; ok {
			known = append(known, id)
		}
	}
	return uniqueNames(known)
}
func (p *PlatformSelector) option(entry platformnames.Platform) PlatformOption {
	row := PlatformOption{Code: p.Names.Code(entry.Name), Name: entry.Name, Aliases: []string{}, ExportSystems: []string{}, ExportTargets: []string{}, Catalogs: []string{}}
	for _, alias := range selectorNames(entry) {
		matches := p.Names.Candidates(alias)
		if len(matches) == 1 && matches[0].Name == entry.Name && platformnames.Normalize(alias) != platformnames.Normalize(entry.Name) && !strings.EqualFold(alias, row.Code) {
			row.Aliases = append(row.Aliases, alias)
		}
	}
	for _, id := range SupportedSystems() {
		if systems[id].Platform == catalogFamily(entry.Name) {
			row.ExportSystems = append(row.ExportSystems, id)
		}
	}
	for _, c := range p.Catalogs {
		if catalogFamily(c.Name) == catalogFamily(entry.Name) {
			row.Catalogs = append(row.Catalogs, c.Platform)
			row.ExportSystems = append(row.ExportSystems, p.effectiveSystems(c)...)
		}
	}
	row.ExportSystems = uniqueNames(row.ExportSystems)
	sort.Strings(row.Catalogs)
	for _, id := range row.ExportSystems {
		row.Extensions = append(row.Extensions, systems[id].Extensions...)
	}
	row.Extensions = uniqueNames(row.Extensions)
	if len(row.ExportSystems) > 0 {
		row.ExportTargets = []string{"es-de", "launchbox"}
	} else {
		row.Reason = "export reader not implemented"
	}
	row.CatalogStatus = "not checked"
	if p.CatalogKnown {
		choice := PlatformChoice{PlatformOption: row}
		if len(row.ExportSystems) == 0 {
			row.CatalogStatus = "export unsupported"
		} else if _, err := p.CatalogFor(choice, "", p.system(choice, PlatformChoice{})); err != nil {
			row.CatalogStatus = err.Error()
		} else {
			row.CatalogStatus = "available"
		}
	}
	return row
}
func (p *PlatformSelector) Options() []PlatformOption {
	var rows []PlatformOption
	for _, entry := range p.Names.Platforms() {
		rows = append(rows, p.option(entry))
	}
	return rows
}
func validSelector(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, part := range strings.Split(strings.ReplaceAll(value, "\\", "/"), "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
func (p *PlatformSelector) Resolve(value string) (PlatformChoice, error) {
	if !validSelector(value) {
		return PlatformChoice{}, fmt.Errorf("invalid platform selector %q", value)
	}
	matches := p.Names.Candidates(value)
	if len(matches) == 0 {
		return PlatformChoice{}, fmt.Errorf("unknown platform %q; use platforms list or export --list-platforms", value)
	}
	if len(matches) > 1 {
		var names []string
		for _, m := range matches {
			names = append(names, m.Name)
		}
		return PlatformChoice{}, fmt.Errorf("ambiguous platform %q: %s", value, strings.Join(names, ", "))
	}
	row := p.option(matches[0])
	system := ""
	if spec, ok := systems[strings.ToLower(strings.TrimSpace(value))]; ok && spec.Platform == catalogFamily(row.Name) {
		system = strings.ToLower(strings.TrimSpace(value))
	}
	return PlatformChoice{row, system}, nil
}
func (p *PlatformSelector) system(choice, folder PlatformChoice) string {
	if choice.System != "" {
		return choice.System
	}
	if folder.System != "" {
		return folder.System
	}
	for _, id := range choice.ExportSystems {
		if id == choice.Code {
			return id
		}
	}
	if len(choice.ExportSystems) > 0 {
		return choice.ExportSystems[0]
	}
	return ""
}
func (p *PlatformSelector) covered(c LookupPlatform) bool {
	for _, parent := range p.Catalogs {
		if parent.Platform == c.Platform {
			continue
		}
		for _, member := range parent.Catalogs {
			if member == c.Platform {
				return true
			}
		}
	}
	return false
}
func (p *PlatformSelector) CatalogFor(choice PlatformChoice, requested, system string) (string, error) {
	if !p.CatalogKnown {
		return "", nil
	}
	var exact, eligible []string
	for _, c := range p.Catalogs {
		if catalogFamily(c.Name) != catalogFamily(choice.Name) || position(p.effectiveSystems(c), system) < 0 {
			continue
		}
		if platformnames.Normalize(requested) == platformnames.Normalize(c.Platform) {
			exact = append(exact, c.Platform)
		}
		if !p.covered(c) {
			eligible = append(eligible, c.Platform)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return "", fmt.Errorf("ambiguous catalog folder spelling %q: %s", requested, strings.Join(exact, ", "))
	}
	sort.Strings(eligible)
	if len(eligible) == 1 {
		return eligible[0], nil
	}
	if len(eligible) > 1 {
		return "", fmt.Errorf("multiple catalog folders claim %s: %s", choice.Name, strings.Join(eligible, ", "))
	}
	return "", fmt.Errorf("export catalog missing or unavailable for %s", choice.Name)
}
func (p *PlatformSelector) Scope(platform string) []string {
	for _, c := range p.Catalogs {
		if c.Platform == platform && len(c.Catalogs) > 0 {
			return c.Catalogs
		}
	}
	return []string{platform}
}
func samePlatform(a, b PlatformChoice) bool { return catalogFamily(a.Name) == catalogFamily(b.Name) }
func folderReason(label string, choice PlatformChoice) string {
	if platformnames.Normalize(label) == platformnames.Normalize(choice.Name) {
		return "canonical folder name"
	}
	if strings.EqualFold(label, choice.Code) || choice.System != "" {
		return "platform code in folder name"
	}
	return "registered folder alias"
}

// Single accepts a generic ROM directory only through the explicit selector.
// Names select a reader/catalog; payload checks happen afterwards and are separate.
func (p *PlatformSelector) Single(value, folder string) (*PlatformSelection, string, error) {
	choice, err := p.Resolve(value)
	if err != nil {
		return nil, "", err
	}
	if len(choice.ExportSystems) == 0 {
		return nil, "", fmt.Errorf("export unsupported for %s (%s): %s", choice.Code, choice.Name, choice.Reason)
	}
	current := folder
	var detected PlatformChoice
	reason := "explicit --platform; folder name unrecognized"
	for {
		label := filepath.Base(current)
		matches := p.Names.Candidates(label)
		if len(matches) == 1 {
			detected, err = p.Resolve(label)
			if err != nil {
				return nil, "", err
			}
			if !samePlatform(choice, detected) {
				return nil, "", fmt.Errorf("selected %s but folder %q identifies %s", choice.Name, label, detected.Name)
			}
			reason = folderReason(label, detected)
			if current != folder {
				reason = "ancestor " + reason + ": " + label
			}
			break
		}
		if len(matches) > 1 {
			compatible := false
			for _, match := range matches {
				if catalogFamily(match.Name) == catalogFamily(choice.Name) {
					compatible = true
				}
			}
			if !compatible {
				return nil, "", fmt.Errorf("folder %q has ambiguous platform names incompatible with %s", label, choice.Name)
			}
			reason = "explicit --platform disambiguates folder alias"
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if detected.Name == "" {
		entries, err := catalogfs.ReadDir(folder)
		if err != nil {
			return nil, "", err
		}
		families := map[string]bool{}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if candidate, e := p.Resolve(entry.Name()); e == nil {
				families[catalogFamily(candidate.Name)] = true
			}
		}
		if len(families) > 1 {
			return nil, "", fmt.Errorf("ROM root contains several platform folders; use --platforms or --all instead of --platform")
		}
	}
	system := p.system(choice, detected)
	catalog, err := p.CatalogFor(choice, value, system)
	if err != nil {
		return nil, "", err
	}
	return &PlatformSelection{Requested: value, Code: choice.Code, Name: choice.Name, System: system, Folder: folder, MatchedBy: reason}, catalog, nil
}

type FolderSelection struct {
	Selection *PlatformSelection `json:"selection"`
	Catalog   string             `json:"catalog,omitempty"`
}

func (p *PlatformSelector) Folders(root string, requested []string, all bool) ([]FolderSelection, []SkippedSystem, error) {
	entries, err := catalogfs.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	var folders []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(entry.Name(), "_") {
			folders = append(folders, entry.Name())
		}
	}
	var selected []FolderSelection
	var skipped []SkippedSystem
	add := func(value, label string, choice, folder PlatformChoice) error {
		system := p.system(choice, folder)
		name := filepath.Join(root, label)
		real, err := realPath(name)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			skipped = append(skipped, SkippedSystem{System: label, Reason: err.Error()})
			return nil
		}
		if !within(root, real) {
			skipped = append(skipped, SkippedSystem{System: label, Reason: "ROM directory link escapes selected root"})
			return nil
		}
		catalog, err := p.CatalogFor(choice, value, system)
		if err != nil {
			skipped = append(skipped, SkippedSystem{System: label, Reason: err.Error()})
			return nil
		}
		reason := folderReason(label, folder)
		if len(p.Names.Candidates(label)) > 1 {
			reason = "explicit selector disambiguates folder alias"
		}
		selected = append(selected, FolderSelection{&PlatformSelection{Requested: value, Code: choice.Code, Name: choice.Name, System: system, Folder: real, MatchedBy: reason}, catalog})
		return nil
	}
	if all {
		for _, label := range folders {
			choice, err := p.Resolve(label)
			if err != nil {
				skipped = append(skipped, SkippedSystem{System: label, Reason: err.Error()})
				continue
			}
			if len(choice.ExportSystems) == 0 {
				skipped = append(skipped, SkippedSystem{System: label, Reason: choice.Reason})
				continue
			}
			if err := add(label, label, choice, choice); err != nil {
				return nil, nil, err
			}
		}
	} else {
		seen := map[string]bool{}
		for _, value := range requested {
			choice, err := p.Resolve(value)
			if err != nil {
				return nil, nil, err
			}
			identity := catalogFamily(choice.Name)
			if seen[identity] {
				return nil, nil, fmt.Errorf("duplicate selected platform %s", choice.Name)
			}
			seen[identity] = true
			if len(choice.ExportSystems) == 0 {
				return nil, nil, fmt.Errorf("export unsupported for %s: %s", choice.Name, choice.Reason)
			}
			var matches []struct {
				label  string
				choice PlatformChoice
			}
			for _, label := range folders {
				candidate, err := p.Resolve(label)
				if err != nil {
					for _, entry := range p.Names.Candidates(label) {
						if catalogFamily(entry.Name) == identity {
							candidate = choice
							candidate.System = ""
							err = nil
							break
						}
					}
				}
				if err == nil && samePlatform(choice, candidate) {
					matches = append(matches, struct {
						label  string
						choice PlatformChoice
					}{label, candidate})
				}
			}
			if len(matches) == 0 {
				skipped = append(skipped, SkippedSystem{System: value, Reason: "ROM system directory is missing"})
				continue
			}
			if len(matches) > 1 {
				var labels []string
				for _, m := range matches {
					labels = append(labels, m.label)
				}
				return nil, nil, fmt.Errorf("ambiguous ROM folders for %s: %s; use --platform with the exact --rom-root", choice.Name, strings.Join(labels, ", "))
			}
			if err := add(value, matches[0].label, choice, matches[0].choice); err != nil {
				return nil, nil, err
			}
		}
	}
	// Never pick one of several folders that would overwrite the same native system.
	counts := map[string]int{}
	for _, s := range selected {
		counts[systems[s.Selection.System].Platform]++
	}
	var safe []FolderSelection
	for _, s := range selected {
		if counts[systems[s.Selection.System].Platform] > 1 {
			skipped = append(skipped, SkippedSystem{System: filepath.Base(s.Selection.Folder), Reason: "multiple ROM folders map to the same export platform; select an exact root with --platform"})
		} else {
			safe = append(safe, s)
		}
	}
	return safe, skipped, nil
}
