package dat

import (
	"regexp"
	"sort"
	"strings"
)

var arcadeKonamiCodeRE = regexp.MustCompile(`\(([A-Z0-9]{3}):[A-Z]:[A-Z]:[A-Z]:[0-9]{10}\)`)
var arcadeInvaderRevisionRE = regexp.MustCompile(`(?i)\b((?:CV|SV|TV) Version) rev [0-9]+`)
var arcadeEditionVersionRE = regexp.MustCompile(`(?i)(\bEdition) v[0-9][\w.]*`)
var arcadeMeritRevisionRE = regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2} R[0-9]+[A-Z]?\b`)
var arcadeNamcoRevisionRE = regexp.MustCompile(`(?i)^(?:[A-Z]{1,6}[0-9]{1,3})(?:\s*[/ -]*VER\.?\s*[A-Z0-9.]+(?: alt)?)?$`)

// Source-scoped technical strings must not turn into generic title aliases.
func arcadeTechnicalDescription(g datGame) string {
	title := g.Description
	if strings.HasPrefix(arcadeFoldSource(g), "namco/") {
		title = arcadeParentheticalRE.ReplaceAllStringFunc(title, func(q string) string {
			var parts []string
			for _, p := range strings.Split(q[1:len(q)-1], ",") {
				if !arcadeNamcoRevisionRE.MatchString(strings.TrimSpace(p)) {
					parts = append(parts, strings.TrimSpace(p))
				}
			}
			if len(parts) == 0 {
				return ""
			}
			return "(" + strings.Join(parts, ", ") + ")"
		})
	}
	if arcadeFoldSource(g) == "merit/meritm.cpp" {
		title = arcadeMeritRevisionRE.ReplaceAllString(title, "revision 0")
	}
	if arcadeFoldSource(g) == "konami/kpython2.cpp" {
		title = arcadeKonamiCodeRE.ReplaceAllString(title, "")
	}
	if arcadeFoldSource(g) == "midw8080/8080bw.cpp" {
		title = arcadeInvaderRevisionRE.ReplaceAllString(title, "$1")
		title = strings.ReplaceAll(strings.ReplaceAll(title, ", larger roms", ""), ", smaller roms", "")
	}
	return strings.TrimSpace(title)
}

func arcadeVariantBranchTitle(g datGame) string {
	title := arcadeEditionVersionRE.ReplaceAllString(arcadeTechnicalDescription(g), "$1")
	return strings.TrimSpace(arcadeParentheticalRE.ReplaceAllStringFunc(title, func(q string) string {
		var kept []string
		for _, part := range strings.Split(q[1:len(q)-1], ",") {
			part = strings.TrimSpace(part)
			if arcadeTechnicalQualifier(part) {
				continue
			}
			kept = append(kept, part)
		}
		if len(kept) == 0 {
			return ""
		}
		return "(" + strings.Join(kept, ", ") + ")"
	}))
}

// MAME records independent ROM roots for regional HDD/GD-ROM releases and
// cartridge/GD-ROM media of these reviewed systems. Exact cleaned title + known
// hardware is used only here; arbitrary independent roots remain independent.
func foldArcadeTechnicalRoots(groups map[string][]datGame, byName map[string]datGame, hardware map[string]string, r ArcadeRouting, notes map[string]string) map[string]string {
	candidates := map[string][]string{}
	for root := range groups {
		g := byName[root]
		if g.CloneOf != "" || arcadeSpecial(g) || hardware[root] == "" {
			continue
		}
		source := arcadeFoldSource(g)
		if source != "sega/naomi.cpp" && source != "sega/triforce.cpp" && source != "konami/kpython2.cpp" {
			continue
		}
		if arcadeSiblingOfficialQualifiers(arcadeTechnicalDescription(g)) != "" {
			continue
		}
		identity := strings.ToLower(arcadeCardTitle(g))
		if source == "konami/kpython2.cpp" {
			code := arcadeKonamiCodeRE.FindStringSubmatch(g.Description)
			if len(code) != 2 {
				continue
			}
			identity = "program:" + code[1]
		}
		key := arcadeCatalogPlatform(hardware[root], r) + "\x00" + source + "\x00" + identity + "\x00" + arcadeFoldEditionKey(g)
		candidates[key] = append(candidates[key], root)
	}
	titles := map[string]string{}
	for _, roots := range candidates {
		if len(roots) < 2 {
			continue
		}
		sort.Strings(roots)
		root := roots[0]
		for _, other := range roots[1:] {
			groups[root] = append(groups[root], groups[other]...)
			delete(groups, other)
		}
		for _, g := range groups[root] {
			delete(notes, g.Name)
		}
		titles[root] = arcadeCardTitle(byName[root])
	}
	return titles
}
