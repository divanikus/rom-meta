package dat

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"rommeta/internal/catalogfs"
)

type ArcadePlatform struct {
	Kind           string `json:"kind" yaml:"kind"`
	NamingEvidence string `json:"naming_evidence" yaml:"naming_evidence"`
}
type ArcadeRouting struct {
	CatalogPlatform string                       `json:"catalog_platform"`
	Version         int                          `json:"version"`
	Platforms       map[string]ArcadePlatform    `json:"platforms"`
	Sources         map[string]string            `json:"sources"`
	BIOS            map[string]string            `json:"bios"`
	Sets            map[string]string            `json:"sets"`
	SourceSets      map[string]map[string]string `json:"source_sets,omitempty"`
	Families        map[string]string            `json:"families,omitempty"`
}

func defaultArcadeRouting() ArcadeRouting {
	r := ArcadeRouting{Version: 1, CatalogPlatform: "Arcade", Platforms: map[string]ArcadePlatform{}, Sources: map[string]string{}, BIOS: map[string]string{}, Sets: map[string]string{}}
	add := func(name, kind, evidence string, sources, bios []string) {
		name = "Arcade - " + name
		r.Platforms[name] = ArcadePlatform{kind, evidence}
		for _, s := range sources {
			r.Sources[s] = name
		}
		for _, s := range bios {
			r.BIOS[s] = name
		}
	}
	add("Other", "collection", "RomMeta fallback for positively classified arcade video games", nil, nil)
	add("SNK - Neo Geo", "hardware", "MAME Neo Geo driver; RomMeta formatted hardware name", []string{"snk/neogeo.cpp", "neogeo/neogeo.cpp"}, []string{"neogeo", "ng_mv4f", "ng_mv2f", "ng_mv1", "ng_mv1f", "ng_mv1fz"})
	add("Capcom - CPS-1", "hardware", "MAME capcom/cps1.cpp; RomMeta formatted hardware name", []string{"capcom/cps1.cpp"}, nil)
	add("Capcom - CPS-2", "hardware", "MAME capcom/cps2.cpp; RomMeta formatted hardware name", []string{"capcom/cps2.cpp"}, nil)
	add("Capcom - CPS-3", "hardware", "MAME capcom/cps3.cpp; RomMeta formatted hardware name", []string{"capcom/cps3.cpp"}, nil)
	add("Sega - Model 2", "hardware", "MAME sega/model2.cpp; RomMeta formatted hardware name", []string{"sega/model2.cpp"}, nil)
	add("Sega - Model 3", "hardware", "MAME sega/model3.cpp; RomMeta formatted hardware name", []string{"sega/model3.cpp"}, nil)
	add("Sega - Naomi", "hardware", "Redump system name; MAME naomi BIOS family", nil, []string{"naomi", "naomigd"})
	add("Sega - Naomi 2", "hardware", "Redump system name; MAME naomi2 BIOS family", nil, []string{"naomi2", "naomi2gd"})
	add("Sammy - Atomiswave", "hardware", "MAME awbios family; RomMeta formatted hardware name", []string{"sega/dc_atomiswave.cpp"}, []string{"awbios"})
	add("Namco - Classics", "collection", "RomMeta mixed collection; reviewed Pac-Man driver membership", []string{"pacman/pacman.cpp"}, nil)
	add("Nintendo - PlayChoice-10", "hardware", "MAME nintendo/playch10.cpp; RomMeta formatted hardware name", []string{"nintendo/playch10.cpp"}, []string{"playch10"})
	add("Nintendo - VS. System", "hardware", "MAME nintendo/vsnes.cpp; RomMeta formatted hardware name", []string{"nintendo/vsnes.cpp"}, nil)
	add("Sega - Mega Play", "hardware", "MAME sega/megaplay.cpp; RomMeta formatted hardware name", []string{"sega/megaplay.cpp"}, []string{"megaplay"})
	add("Sega - Mega-Tech", "hardware", "MAME sega/megatech.cpp; RomMeta formatted hardware name", []string{"sega/megatech.cpp"}, []string{"megatech"})
	extendArcadeHardware(&r)
	groupArcadeFamilies(&r)
	return r
}

func loadArcadeRouting(path string) (ArcadeRouting, error) {
	r := defaultArcadeRouting()
	if path != "" {
		f, err := catalogfs.Open(path)
		if err != nil {
			return r, err
		}
		defer f.Close()
		d := json.NewDecoder(io.LimitReader(f, 4<<20))
		d.DisallowUnknownFields()
		var overrides ArcadeRouting
		if err = d.Decode(&overrides); err != nil {
			return r, err
		}
		if err = d.Decode(new(any)); err != io.EOF {
			return r, fmt.Errorf("routing must contain exactly one JSON document")
		}
		if overrides.Version != 1 {
			return r, fmt.Errorf("unsupported routing version %d", overrides.Version)
		}
		if overrides.CatalogPlatform != "" && overrides.CatalogPlatform != r.CatalogPlatform {
			return r, fmt.Errorf("arcade catalog_platform must be Arcade")
		}
		for k, v := range overrides.Platforms {
			r.Platforms[k] = v
		}
		for k, v := range overrides.Families {
			r.Families[k] = v
		}
		for k, v := range overrides.Sources {
			r.Sources[k] = v
		}
		for k, v := range overrides.BIOS {
			r.BIOS[k] = v
		}
		for k, v := range overrides.Sets {
			r.Sets[k] = v
		}
		for source, sets := range overrides.SourceSets {
			if r.SourceSets[source] == nil {
				r.SourceSets[source] = map[string]string{}
			}
			for name, platform := range sets {
				r.SourceSets[source][name] = platform
			}
		}
	}
	seen := map[string]bool{}
	for name, p := range r.Platforms {
		if !strings.HasPrefix(name, "Arcade - ") || safePathName(name) != name || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
			return r, fmt.Errorf("unsafe arcade platform name %q", name)
		}
		if p.Kind != "hardware" && p.Kind != "collection" {
			return r, fmt.Errorf("invalid platform kind for %s", name)
		}
		if strings.TrimSpace(p.NamingEvidence) == "" {
			return r, fmt.Errorf("missing naming evidence for %s", name)
		}
		if seen[strings.ToLower(name)] {
			return r, fmt.Errorf("case-insensitive platform collision: %s", name)
		}
		seen[strings.ToLower(name)] = true
	}
	for _, m := range []map[string]string{r.Sources, r.BIOS, r.Sets} {
		for _, platform := range m {
			if _, ok := r.Platforms[platform]; !ok {
				return r, fmt.Errorf("routing refers to undefined platform %s", platform)
			}
		}
	}

	for source, sets := range r.SourceSets {
		if strings.TrimSpace(source) == "" || strings.Contains(source, "\\") {
			return r, fmt.Errorf("invalid source_sets driver %q", source)
		}
		for name, platform := range sets {
			if !arcadeShortNameRE.MatchString(name) {
				return r, fmt.Errorf("invalid source_sets name %q", name)
			}
			if _, ok := r.Platforms[platform]; !ok {
				return r, fmt.Errorf("routing refers to undefined platform %s", platform)
			}
		}
	}
	if err := validateArcadeFamilies(r); err != nil {
		return r, err
	}
	return r, nil
}

func routeArcade(g datGame, index map[string]datGame, r ArcadeRouting) (string, string) {
	if p := r.Sets[g.Name]; p != "" {
		return p, "explicit set rule"
	}
	source := strings.ReplaceAll(g.SourceFile, "\\", "/")
	p := ""
	visited := map[string]bool{g.Name: true}
	for parent := g.ROMOf; parent != ""; {
		if visited[parent] {
			return "", "ROM dependency cycle"
		}
		visited[parent] = true
		if v := r.BIOS[parent]; v != "" {
			if p != "" && p != v {
				return "", "conflicting BIOS hardware rules"
			}
			p = v
		}
		ancestor, ok := index[parent]
		if !ok {
			return "", "missing ROM dependency " + parent
		}
		parent = ancestor.ROMOf
	}
	// A reviewed board exception can override an inherited BIOS family, but
	// missing/cyclic ROM dependencies above must still be reported.
	if scoped := r.SourceSets[source][g.Name]; scoped != "" {
		return scoped, "reviewed set in source file " + source
	}
	if v := r.Sources[source]; v != "" {
		if p != "" && p != v {
			return "", "BIOS/source hardware conflict"
		}
		if p == "" {
			return v, "source file " + source
		}
	}
	if p != "" {
		return p, "BIOS family"
	}
	return "Arcade - Other", "unmapped hardware; explicit fallback collection"
}

func arcadeBundleGames(games []datGame, fold bool) ([]gameBundle, map[string]string) {
	return arcadeBundleGamesWithHardware(games, fold, nil, ArcadeRouting{})
}

func arcadeBundleGamesWithHardware(games []datGame, fold bool, hardware map[string]string, routing ArcadeRouting) ([]gameBundle, map[string]string) {
	return arcadeBundleGamesWithReference(games, fold, hardware, routing, nil)
}

// Membership stays local to a hardware family; ancestry evidence does not.
// MAME often makes the other-board version a clone of a parent in another family.
func arcadeBundleGamesWithReference(games []datGame, fold bool, hardware map[string]string, routing ArcadeRouting, reference map[string]datGame) ([]gameBundle, map[string]string) {
	byName := map[string]datGame{}
	for _, g := range games {
		byName[g.Name] = g
	}
	if reference == nil {
		reference = byName
	}
	aliases := arcadeOfficialCloneAliases(games, hardware, routing)
	notes := map[string]string{}
	groups := map[string][]datGame{}
	for _, g := range games {
		root := g.Name
		if fold {
			seen := map[string]bool{g.Name: true}
			current := g
			for arcadeCatalogParent(current) != "" {
				parent, ok := byName[arcadeCatalogParent(current)]
				if !ok {
					notes[g.Name] = "clone parent not selected on the same platform"
					break
				}
				if seen[parent.Name] {
					root = g.Name
					notes[g.Name] = "clone cycle"
					break
				}
				seen[parent.Name] = true
				if !arcadeFoldHardwareCompatible(current, parent, hardware, routing) {
					notes[g.Name] = "different or unknown driver hardware requires fold review"
					break
				}
				if arcadeFoldSpecial(current) || arcadeFoldSpecial(parent) {
					notes[g.Name] = "content hack or distinct unofficial content kept separate"
					break
				}
				if arcadeReviewedContentVariant(current) || arcadeReviewedContentVariant(parent) {
					notes[g.Name] = "reviewed content variant kept separate"
					break
				}
				if arcadeBootleg(current) && !arcadeBootlegMatchesAliases(current, parent, aliases) {
					notes[g.Name] = "renamed or modified bootleg requires content review"
					break
				}
				if arcadeFoldEditionKey(current) != arcadeFoldEditionKey(parent) {
					notes[g.Name] = "different content editions require fold review"
					break
				}
				root = parent.Name
				current = parent
			}
		}
		groups[root] = append(groups[root], g)
	}
	siblingTitles := map[string]string{}
	if fold {
		siblingTitles = foldArcadeSiblingGroups(groups, reference, hardware, routing, notes)
		for root, title := range foldArcadeTechnicalRoots(groups, reference, hardware, routing, notes) {
			siblingTitles[root] = title
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := arcadeBaseTitle(byName[keys[i]].Description), arcadeBaseTitle(byName[keys[j]].Description)
		if a != b {
			return a < b
		}
		return keys[i] < keys[j]
	})
	used := map[string]bool{}
	var bundles []gameBundle
	for _, k := range keys {
		releases := groups[k]
		sort.Slice(releases, func(i, j int) bool { return releases[i].Name < releases[j].Name })
		title := arcadeCardTitle(byName[k])
		if arcadeBootleg(byName[k]) || arcadeFoldSpecial(byName[k]) {
			title = byName[k].Description
		}
		title = firstNonEmpty(siblingTitles[k], title, k)
		folder := safePathName(title)
		runes := []rune(folder)
		if len(utf16.Encode(runes)) > 180 {
			for len(utf16.Encode(runes)) > 160 {
				runes = runes[:len(runes)-1]
			}
			folder = strings.TrimSpace(string(runes)) + " [" + k + "]"
		}
		base := strings.ToUpper(strings.SplitN(folder, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			folder = "Game - " + folder
		}
		if folder == "" || folder == "." || strings.HasPrefix(folder, ".") || strings.HasPrefix(folder, "_") {
			folder = "Game - " + k
		}
		if used[strings.ToLower(folder)] {
			folder += " [" + k + "]"
		}
		for used[strings.ToLower(folder)] {
			folder += "-"
		}
		used[strings.ToLower(folder)] = true
		bundles = append(bundles, gameBundle{title, folder, releases})
	}
	return bundles, notes
}

func arcadeSpecial(g datGame) bool {
	// Preferred-release policy is separate from game identity. Development and
	// protection variants can share a card without becoming the initial default.
	if arcadeBootleg(g) || g.Unofficial || g.Incomplete || arcadeFoldSpecial(g) || arcadeProtectionHackRE.MatchString(g.Description) {
		return true
	}
	for _, q := range arcadeParentheticalRE.FindAllString(g.Description, -1) {
		if arcadeSpecialQualifierRE.MatchString(q) {
			return true
		}
	}
	return false
}

var arcadeProtectionHackRE = regexp.MustCompile(`(?i)\b(?:no protection|protection(?: removal| bypass)?|decryption|decrypted) hack\b`)

func arcadeFoldSpecial(g datGame) bool {
	if (g.Unofficial && !arcadeBootleg(g)) || g.Incomplete {
		return true
	}
	lower := strings.ToLower(g.Description + " " + g.Manufacturer)
	lower = arcadeProtectionHackRE.ReplaceAllString(lower, "")
	if arcadeReviewedTechnicalClone(g) {
		lower = strings.ReplaceAll(lower, "hack of japan set", "")
	}
	return strings.Contains(lower, "hack")
}

func arcadeRegion(title string) string {
	start := strings.Index(title, "(")
	if start < 0 {
		return ""
	}
	value := strings.ToLower(title[start+1:])
	for _, pair := range [][2]string{{"u.s.a.", "USA"}, {"usa", "USA"}, {"us", "USA"}, {"world", "World"}, {"europe", "Europe"}, {"euro", "Europe"}, {"japan", "Japan"}, {"asia", "Asia"}, {"korea", "Korea"}, {"taiwan", "Taiwan"}, {"hong kong", "Hong Kong"}, {"australia", "Australia"}, {"brazil", "Brazil"}} {
		if strings.HasPrefix(value, pair[0]) && len(value) > len(pair[0]) && strings.ContainsRune(" ,)", rune(value[len(pair[0])])) {
			return pair[1]
		}
	}
	return ""
}
