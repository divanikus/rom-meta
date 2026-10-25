package dat

import (
	"regexp"
	"sort"
	"strings"
)

// Titles label a parent/clone group; they are not its identity. Only explicit
// content-edition differences veto an otherwise eligible official clone link.
var arcadeEditionRE = regexp.MustCompile(`(?i)\b(?:[a-z]+ edition|hard kit|high score kit|arrange(?:d)?|remix|tournament|enhanced|master boy (?:olympic|version ii)|speed[ -]?up|turbo|questions? series \d+|(?:alt(?:ernate)?|sex|adult) questions?)\b`)

var arcadeNamedDeluxeRE = regexp.MustCompile(`(?i)\bdeluxe\b`)

func arcadeEditionKey(title string) string {
	matches := arcadeEditionRE.FindAllString(strings.ToLower(title), -1)
	// Bare Deluxe in a qualifier identifies a cabinet, not a content edition.
	if arcadeNamedDeluxeRE.MatchString(strings.SplitN(title, " (", 2)[0]) {
		matches = append(matches, "deluxe")
	}
	sort.Strings(matches)
	return strings.Join(matches, "|")
}

var arcadeQualifierRE = regexp.MustCompile(`(?i)^(?:v\s*\d[\w. /-]*|(?:rev(?:ision)?|ver(?:sion)?)\b\.?\s*\w[\w. /-]*|set\s+\d[\w. /-]*|fd(?:1089[ab]|1094)(?:\s+317-(?:\d+|unknown))?|(?:317|315)-\d+|8751(?:\s+(?:317|315)-\d+)?|system\s*16[abc]?|mpu4 video|export|analog controls|digital controls|unprotected|encrypted|not encrypted|decrypted|deluxe|standard|upright|cockpit|sit[ -]?down|\d+ players?|\d+ lives|proto(?:type)?|beta(?: \d+)?|demo|location test|ng[hm]-\d{3}(?:\s*~\s*ng[hm]-\d{3})?)$`)

// A slash can separate supported cabinet types in one release descriptor,
// e.g. After Burner's deluxe/upright. Do not accept arbitrary slash suffixes.
var arcadeCabinetListRE = regexp.MustCompile(`(?i)^(?:deluxe|standard|upright|cockpit|sit[ -]?down)(?:\s*/\s*(?:deluxe|standard|upright|cockpit|sit[ -]?down))+$`)

var arcadeProductCodeRE = regexp.MustCompile(`(?i)^(?:GD[LST]-\d{4}[A-Z]?|CDV-\d{5}|CPS[123]|SAMPLE Version)$`)
var arcadeVersionCodeRE = regexp.MustCompile(`(?i)^ver\s+[A-Z0-9.*]+$`)
var arcadeConversionQualifierRE = regexp.MustCompile(`(?i)^(?:Taito [BH]-System|Nastar Conversion Kit|Master of Weapon Conversion Kit)$`)
var arcadeRegionQualifierRE = regexp.MustCompile(`(?i)^(?:u\.s\.a\.|usa|us|world|europe|euro|japan|asia|korea|taiwan|hong kong|australia|brazil|hispanic|italy|spain|germany|france|uk|canada)(?:\s+\d{6,8})?(?:\s+rent version)?$`)
var arcadeRevisionDetailRE = regexp.MustCompile(`(?i)^(?:checksum\s+[a-f0-9]+|unknown version|test version\??|older|newer|ride-on|deluxe sitdown|system [12]|(?:FD(?:1089[AB]|1094)\s+|8751\s+)?(?:317|315)-[0-9]+[a-z]?(?:-[0-9]+[a-z]?)?|[A-Z0-9]{2,8}[/ ]VER(?:\.\s*|\s+)[A-Z0-9.]+(?: alt)?)$`)
var arcadeDateQualifierRE = regexp.MustCompile(`(?i)^(?:\d{4}[-./ ]\d{1,2}[-./ ]\d{1,2}|\d{1,2}[-./]\d{1,2}[-./]\d{2,4}|\d{1,2}(?:st|nd|rd|th)?[ .-]*(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\.?\s*\d{2,4}|(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\.?\s+\d{1,2}\s+\d{4})[. ]*(?:(?:master|international)\s+ver(?:sion)?\.?(?:\s*[\d.]+)?)?$`)

func arcadeRegionQualifier(q string) bool {
	if arcadeRegionQualifierRE.MatchString(q) {
		return true
	}
	if strings.ContainsAny(q, "/+") {
		for _, part := range strings.FieldsFunc(q, func(r rune) bool { return r == '/' || r == '+' }) {
			if !arcadeRegionQualifier(strings.TrimSpace(part)) {
				return false
			}
		}
		return true
	}
	parts := strings.Fields(strings.ReplaceAll(q, "/", " "))
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !arcadeRegionQualifierRE.MatchString(part) {
			return false
		}
	}
	return true
}

// arcadeBaseTitle preserves unfamiliar qualifiers while removing recognized
// technical groups even when an unfamiliar group follows a version group.
func arcadeBaseTitle(title string) string {
	title = strings.TrimSpace(title)
	var kept []string
	for strings.HasSuffix(title, ")") {
		start := strings.LastIndex(title, " (")
		if start < 0 {
			break
		}
		q := title[start+2 : len(title)-1]
		if strings.ContainsAny(q, "()") {
			break
		}
		if !arcadeTechnicalQualifier(q) {
			kept = append([]string{" (" + q + ")"}, kept...)
		}
		title = strings.TrimSpace(title[:start])
	}
	return title + strings.Join(kept, "")
}

func arcadeTechnicalQualifier(q string) bool {
	if arcadeEditionKey("Game ("+q+")") != "" {
		return false
	}
	for _, part := range strings.Split(q, ",") {
		if part = strings.TrimSpace(part); !arcadeTechnicalPart(part) {
			return false
		}
	}
	return true
}

func arcadeTechnicalPart(part string) bool {
	if arcadeDateQualifierRE.MatchString(part) {
		return true
	}
	if part == "Standard version" {
		return true
	}
	if arcadeQualifierRE.MatchString(part) || arcadeCabinetListRE.MatchString(part) || arcadeProductCodeRE.MatchString(part) || arcadeRegionQualifier(part) || arcadeVersionCodeRE.MatchString(part) || arcadeConversionQualifierRE.MatchString(part) || arcadeRevisionDetailRE.MatchString(part) {
		return true
	}
	// Region followed by an explicit version is still technical (US v1.3).
	if first, rest, ok := strings.Cut(part, " "); ok && arcadeRegionQualifierRE.MatchString(first) {
		return arcadeTechnicalPart(strings.TrimSpace(rest))
	}
	return false
}

func arcadeCardTitle(g datGame) string {
	// NWK-TR's UDE is a program revision code, not a content subtitle.
	if g.Name == "thrilld" && arcadeFoldSource(g) == "konami/nwk-tr.cpp" && arcadeTitleStem(g.Description) == "thrill drive" {
		return "Thrill Drive"
	}
	copy := g
	copy.Description = arcadeBaseTitle(arcadeTechnicalDescription(g))
	return arcadeVariantBranchTitle(copy)
}

var arcadeParentheticalRE = regexp.MustCompile(`\([^()]*\)`)
var arcadeSpecialQualifierRE = regexp.MustCompile(`(?i)\b(?:proto(?:type)?|beta|demo|evaluation|location test|test version)\b`)

// MAME's Pac-Man driver documents these as separately sold games sharing code,
// even though listxml assigns them to puckman without an explicit hack marker.
// https://github.com/mamedev/mame/blob/mame0289/src/mame/pacman/pacman.cpp
func arcadeReviewedContentVariant(g datGame) bool {
	if strings.ReplaceAll(g.SourceFile, "\\", "/") != "pacman/pacman.cpp" || g.CloneOf != "puckman" {
		return false
	}
	switch g.Name {
	case "piranha", "piranhao", "abscam":
		return true
	}
	return false
}
