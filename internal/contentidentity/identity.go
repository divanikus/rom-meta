// Package contentidentity defines content boundaries shared by import and fold.
// It deliberately has no game IDs, publisher lists or fuzzy title matching.
package contentidentity

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"rommeta/internal/regions"
)

type Profile struct {
	Kind    string
	Variant string
	Build   string
	Product string
	Edition string
	// Component identifies an explicitly labelled disc inside a complete edition.
	// Kind still describes its payload; ownership belongs to that edition.
	Component string
}

// Development identifies a release stage of a game, not a separate product.
func (p Profile) Development() bool {
	switch p.Kind {
	case "demo", "beta", "prototype", "sample", "preview", "debug", "promo":
		return true
	}
	return false
}

// Key is game/edition compatibility, not exact release identity. Kind and Build
// remain available to callers; exact release names and fingerprints are untouched.
func (p Profile) Key() string {
	kind := p.Kind
	if p.Development() || p.Component != "" {
		kind = "retail"
	}
	if p.Product != "" {
		kind = p.Product
	}
	return kind + "|" + p.Variant
}

var qualifier = regexp.MustCompile(`\s*[([]([^()\[\]]*)[)\]]\s*$`)
var technical = regexp.MustCompile(`(?i)^(rev(ision)?[ ._-]*[a-z0-9.]+|v[0-9][a-z0-9.]*|disc [0-9]+( of [0-9]+)?|disk [0-9]+|side [ab]|layer [0-9]+|[0-9x]{4}-[0-9x]{2}-[0-9x]{2}|[a-z]{4}[-_ ]?[0-9]{5}|[!])$`)
var edition = regexp.MustCompile(`(?i)(^|[ :–—-]+)((special|limited|collector'?s?|complete|definitive|deluxe|premium|ultimate|gold|extended|enhanced|anniversary|goty|game of the year) edition|director'?s? cut|special disc|bonus disc|expansion pack|append disc)$`)
var editionLabel = regexp.MustCompile(`(?i)(\bedition\b|director[']?s? cut)`)

var completePackage = regexp.MustCompile(`(?i)\b(game of the year|goty|complete|ultimate|legendary) edition\b`)
var bundledInstaller = regexp.MustCompile(`(?i)^(add-on (content |game |installation )?disc|additional content packs install disc|zusatzliche inhaltspacks installations-disc|tsuika contents disc)$`)
var requiredDisc = regexp.MustCompile(`(?i)^(install disc|content install disc|play disc|game disc)$`)

var role = regexp.MustCompile(`(?i)^(demo|trial|taikenban|beta|proto(type)?|sample|preview|debug|promo(tional)?|dlc|addon|add-on|expansion|update|patch|bonus disc|special disc|append disc|online test(?: version)?|network test|multiplayer alpha|multiplayer beta|pilot test)([ ._-].*)?$`)

// Only conventional single-game stage subtitles may disappear from a game
// title. Free-form demo-disc/issue names retain identity and require review.
var stageSubtitle = regexp.MustCompile(`(?i)^(demo|trial|beta|proto(type)?|sample|preview|debug|promo(tional)?)( (edition|version))?( (ver[.]?|v|build)[ ._-]*[a-z0-9.]+)?$|(?i)^preview disc$|(?i)^(online test( version)?|network test|multiplayer alpha|multiplayer beta|pilot test)$`)

// Analyze removes only recognized transport qualifiers. Unknown qualifiers are
// retained in both the display title and the content profile, failing closed.
func Analyze(name string, tags ...string) (string, Profile) {
	title := strings.TrimSpace(name)
	var fields []string
	for {
		m := qualifier.FindStringSubmatchIndex(title)
		if m == nil {
			break
		}
		fields = append(fields, title[m[2]:m[3]])
		title = strings.TrimSpace(title[:m[0]])
	}
	profile := Profile{Kind: "retail"}
	// Explicit complete-edition installer labels describe parts of that product.
	// A standalone addon has no such package declaration and keeps its boundary.
	packageDeclared := completePackage.MatchString(title)
	for _, field := range fields {
		packageDeclared = packageDeclared || completePackage.MatchString(field)
	}
	var variants, builds, retained, editions []string
	addRole := func(value string) bool {
		lower := strings.ToLower(strings.TrimSpace(value))
		if requiredDisc.MatchString(lower) || packageDeclared && bundledInstaller.MatchString(lower) {
			profile.Component = normalize(value)
			if bundledInstaller.MatchString(lower) {
				profile.Kind = "addon"
			}
			return true
		}
		kind, detail := "", ""
		switch {
		case strings.Contains(lower, "taikenban"), strings.Contains(lower, "cheheompan"), strings.HasSuffix(lower, " demo"):
			kind = "demo"
			if lower != "taikenban" && lower != "cheheompan" {
				detail = normalize(value)
			}
		case strings.HasPrefix(lower, "append-") || strings.HasPrefix(lower, "append "):
			kind = "addon"
			detail = normalize(value)
		case strings.HasSuffix(lower, "movie disc"), strings.HasSuffix(lower, "trailer disc"), strings.HasSuffix(lower, "bonus disc"), strings.HasSuffix(lower, "movie-ban"):
			kind = "supplement"
			detail = normalize(value)
		}
		if kind != "" {
			if profile.Kind != "retail" && profile.Kind != kind {
				if profile.Development() && (Profile{Kind: kind}).Development() {
					builds = append(builds, profile.Kind)
				} else {
					variants = append(variants, profile.Kind)
				}
			}
			profile.Kind = kind
			if detail != "" {
				if profile.Development() {
					builds = append(builds, detail)
				} else {
					variants = append(variants, detail)
				}
			}
			return true
		}
		m := role.FindStringSubmatch(strings.TrimSpace(value))
		if m == nil {
			return false
		}
		kind = strings.ToLower(m[1])
		switch kind {
		case "trial", "taikenban":
			kind = "demo"
		case "promotional":
			kind = "promo"
		case "online test", "online test version", "network test", "multiplayer alpha", "multiplayer beta", "pilot test":
			kind = "beta"
			builds = append(builds, normalize(value))
		case "proto", "prototype":
			kind = "prototype"
		case "addon", "add-on", "dlc":
			kind = "addon"
		case "patch":
			kind = "update"
		case "bonus disc", "special disc", "append disc":
			kind = "supplement"
		}
		if profile.Kind != "retail" && profile.Kind != kind {
			if profile.Development() && (Profile{Kind: kind}).Development() {
				builds = append(builds, profile.Kind)
			} else {
				variants = append(variants, profile.Kind)
			}
		}
		profile.Kind = kind
		// Role/build details remain release data and do not define another game.
		if m[4] != "" {
			if profile.Development() {
				builds = append(builds, normalize(value))
			} else {
				variants = append(variants, normalize(value))
			}
		}
		if kind == "supplement" {
			variants = append(variants, normalize(m[1]))
		}
		return true
	}
	for _, tag := range tags {
		addRole(tag)
	}
	// Determine the role before interpreting dates, regardless of qualifier order.
	for i := len(fields) - 1; i >= 0; i-- {
		addRole(fields[i])
	}
	// Dated development builds stay distinguishable inside their game.
	for i := len(fields) - 1; i >= 0; i-- {
		field := strings.TrimSpace(fields[i])
		if addRole(field) {
			continue
		}
		if neutral(field) {
			if profile.Development() && strings.Contains(field, "-") && len(field) == 10 {
				builds = append(builds, normalize(field))
			}
			continue
		}
		retained = append(retained, field)
		variants = append(variants, normalize(field))
		if editionLabel.MatchString(field) {
			editions = append(editions, normalize(field))
		}
	}
	if m := edition.FindStringSubmatch(title); m != nil {
		variants = append(variants, normalize(m[2]))
		editions = append(editions, normalize(m[2]))
	} else {
		// An explicit edition subtitle is a conservative boundary even when its
		// marketing adjective is unknown. Single-game stage boilerplate such as
		// Demo Edition remains a release stage, not a commercial product.
		for _, sep := range []string{" - ", ": "} {
			if at := strings.LastIndex(title, sep); at >= 0 {
				suffix := title[at+len(sep):]
				if editionLabel.MatchString(suffix) && !stageSubtitle.MatchString(suffix) {
					variants = append(variants, normalize(suffix))
					editions = append(editions, normalize(suffix))
					break
				}
			}
		}
	}
	// A role in a subtitle is explicit; a word inside a commercial title is not.
	for _, sep := range []string{" - ", ": "} {
		if at := strings.LastIndex(title, sep); at >= 0 {
			suffix := title[at+len(sep):]
			if addRole(suffix) && profile.Development() {
				if stageSubtitle.MatchString(suffix) {
					title = strings.TrimSpace(title[:at])
				} else {
					variants = append(variants, normalize(suffix))
				}
			}
		}
	}
	for _, field := range retained {
		title += " (" + field + ")"
	}
	sort.Strings(editions)
	profile.Edition = strings.Join(unique(editions), ";")
	sort.Strings(builds)
	profile.Build = strings.Join(unique(builds), ";")
	sort.Strings(variants)
	profile.Variant = strings.Join(unique(variants), ";")
	return title, profile
}

func neutral(field string) bool {
	if technical.MatchString(field) {
		return true
	}
	parts := strings.Split(field, ",")
	languages := ",ar,bg,ca,cs,cy,da,de,el,en,es,et,fa,fi,fr,he,hr,hu,id,it,ja,ko,lt,lv,ms,nl,no,pl,pt,ro,ru,sk,sl,sr,sv,th,tr,uk,vi,zh,"
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if _, ok := regions.NormalizeDATDisplay(part); ok {
			continue
		}
		if part != "" && strings.Contains(languages, ","+strings.ToLower(part)+",") {
			continue
		}
		return false
	}
	return len(parts) > 0
}

func normalize(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}
func unique(values []string) []string {
	var out []string
	for _, value := range values {
		if value != "" && (len(out) == 0 || out[len(out)-1] != value) {
			out = append(out, value)
		}
	}
	return out
}

// Boundary reports incompatible or already mixed content. An empty profile
// list means a legacy card without releases and cannot prove compatibility.
func Boundary(profiles ...[]Profile) string {
	var expected string
	for _, card := range profiles {
		if len(card) == 0 {
			return "release content is unknown; review the card before merging"
		}
		keys := map[string]bool{}
		for _, profile := range card {
			keys[profile.Key()] = true
		}
		if len(keys) > 1 {
			return "mixed product or edition classes within an existing card; review its releases first"
		}
		for key := range keys {
			if expected != "" && key != expected {
				return "different release content classes or edition qualifiers"
			}
			expected = key
		}
	}
	return ""
}

// Release applies explicit card-level content labels to legacy release names.
func Release(name, title string, tags ...string) Profile {
	_, profile := Analyze(name, tags...)
	_, fallback := Analyze(title)
	if profile.Variant == "" {
		profile.Variant = fallback.Variant
	}
	if profile.Edition == "" {
		profile.Edition = fallback.Edition
	}
	if profile.Kind == "retail" {
		profile.Kind = fallback.Kind
	}
	return profile
}

// SourceKind uses provider taxonomy, never title-specific exceptions.
func SourceKind(gameType string) string {
	switch gameType {
	case "Compilation":
		return "compilation"
	case "DLC / Addon":
		return "addon"
	case "Expansion":
		return "expansion"
	case "Standalone Expansion":
		return "standalone expansion"
	case "Demo":
		return "demo"
	case "Expanded Game":
		return "expanded edition"
	case "Remake":
		return "remake"
	case "Remaster":
		return "remaster"
	}
	return ""
}

// ApplyKind refines provider product taxonomy without erasing an early build's
// release role. An edition can contain both retail and development releases.
func ApplyKind(profiles []Profile, index int, kind string) []Profile {
	if kind == "" {
		return profiles
	}
	if profiles[index].Component != "" && (kind == "addon" || kind == "update") {
		return profiles
	}
	if profiles[index].Component != "" && (kind == "expanded edition" || kind == "remake" || kind == "remaster") {
		profiles[index].Product = kind
		return profiles
	}
	if profiles[index].Development() && !(Profile{Kind: kind}).Development() {
		if profiles[index].Product == "" || profiles[index].Product == kind {
			profiles[index].Product = kind
		} else {
			profiles = append(profiles, Profile{Kind: kind})
		}
	} else if profiles[index].Kind == "retail" {
		profiles[index].Kind = kind
	} else if profiles[index].Kind != kind && !(profiles[index].Development() && (Profile{Kind: kind}).Development()) {
		profiles = append(profiles, Profile{Kind: kind})
	}
	return profiles
}

// KnownBoundary checks explicit product/edition differences while retaining
// unknown labels in the original profiles. Only an exact saved-authority refresh
// or an explicit development-parent review may resolve those unknown labels.
func KnownBoundary(cards ...[]Profile) string {
	known := make([][]Profile, len(cards))
	for i, card := range cards {
		known[i] = append([]Profile(nil), card...)
		for j := range known[i] {
			known[i][j].Variant = known[i][j].Edition
		}
	}
	return Boundary(known...)
}
