package regions

import "strings"

var datDisplayNames = map[string]string{
	"argentina":          "Argentina",
	"asia":               "Asia",
	"australia":          "Australia",
	"austria":            "Austria",
	"belarus":            "Belarus",
	"belgium":            "Belgium",
	"brazil":             "Brazil",
	"bulgaria":           "Bulgaria",
	"canada":             "Canada",
	"china":              "China",
	"croatia":            "Croatia",
	"czech":              "Czech",
	"denmark":            "Denmark",
	"estonia":            "Estonia",
	"europe":             "Europe",
	"export":             "Export",
	"finland":            "Finland",
	"france":             "France",
	"germany":            "Germany",
	"greece":             "Greece",
	"hongkong":           "Hong Kong",
	"hungary":            "Hungary",
	"iceland":            "Iceland",
	"india":              "India",
	"ireland":            "Ireland",
	"israel":             "Israel",
	"italy":              "Italy",
	"japan":              "Japan",
	"korea":              "Korea",
	"latinamerica":       "Latin America",
	"lithuania":          "Lithuania",
	"mexico":             "Mexico",
	"netherlands":        "Netherlands",
	"newzealand":         "New Zealand",
	"norway":             "Norway",
	"peru":               "Peru",
	"poland":             "Poland",
	"portugal":           "Portugal",
	"romania":            "Romania",
	"russia":             "Russia",
	"scandinavia":        "Scandinavia",
	"serbia":             "Serbia",
	"singapore":          "Singapore",
	"slovakia":           "Slovakia",
	"southafrica":        "South Africa",
	"spain":              "Spain",
	"sweden":             "Sweden",
	"switzerland":        "Switzerland",
	"taiwan":             "Taiwan",
	"thailand":           "Thailand",
	"turkey":             "Turkey",
	"uk":                 "UK",
	"ukraine":            "Ukraine",
	"unitedarabemirates": "United Arab Emirates",
	"unitedkingdom":      "UK",
	"unknown":            "Unknown",
	"usa":                "USA",
	"world":              "World",
}

var sourceAliases = map[string]string{
	"ae":                    "United Arab Emirates",
	"ar":                    "Argentina",
	"are":                   "United Arab Emirates",
	"arg":                   "Argentina",
	"asi":                   "Asia",
	"at":                    "Austria",
	"aut":                   "Austria",
	"au":                    "Australia",
	"aus":                   "Australia",
	"be":                    "Belgium",
	"bel":                   "Belgium",
	"br":                    "Brazil",
	"bra":                   "Brazil",
	"bg":                    "Bulgaria",
	"bgr":                   "Bulgaria",
	"blr":                   "Belarus",
	"by":                    "Belarus",
	"ca":                    "Canada",
	"can":                   "Canada",
	"che":                   "Switzerland",
	"ch":                    "Switzerland",
	"cn":                    "China",
	"chn":                   "China",
	"cz":                    "Czech",
	"cze":                   "Czech",
	"de":                    "Germany",
	"deu":                   "Germany",
	"deutschland":           "Germany",
	"dk":                    "Denmark",
	"ee":                    "Estonia",
	"est":                   "Estonia",
	"europeanunion":         "Europe",
	"eu":                    "Europe",
	"eur":                   "Europe",
	"es":                    "Spain",
	"esp":                   "Spain",
	"fi":                    "Finland",
	"fr":                    "France",
	"fra":                   "France",
	"fre":                   "France",
	"gb":                    "UK",
	"gbr":                   "UK",
	"greatbritain":          "UK",
	"gr":                    "Greece",
	"gre":                   "Greece",
	"hk":                    "Hong Kong",
	"hkg":                   "Hong Kong",
	"hr":                    "Croatia",
	"hrv":                   "Croatia",
	"hu":                    "Hungary",
	"hun":                   "Hungary",
	"ie":                    "Ireland",
	"is":                    "Iceland",
	"il":                    "Israel",
	"in":                    "India",
	"irl":                   "Ireland",
	"isl":                   "Iceland",
	"isr":                   "Israel",
	"it":                    "Italy",
	"ita":                   "Italy",
	"ja":                    "Japan",
	"jap":                   "Japan",
	"jp":                    "Japan",
	"jpn":                   "Japan",
	"kor":                   "Korea",
	"kr":                    "Korea",
	"latam":                 "Latin America",
	"lt":                    "Lithuania",
	"ltu":                   "Lithuania",
	"mex":                   "Mexico",
	"mx":                    "Mexico",
	"nl":                    "Netherlands",
	"nld":                   "Netherlands",
	"no":                    "Norway",
	"nor":                   "Norway",
	"nz":                    "New Zealand",
	"nzl":                   "New Zealand",
	"pe":                    "Peru",
	"per":                   "Peru",
	"pl":                    "Poland",
	"pol":                   "Poland",
	"prc":                   "China",
	"pt":                    "Portugal",
	"por":                   "Portugal",
	"ro":                    "Romania",
	"rou":                   "Romania",
	"ru":                    "Russia",
	"rus":                   "Russia",
	"russianfederation":     "Russia",
	"rs":                    "Serbia",
	"se":                    "Sweden",
	"sg":                    "Singapore",
	"sgp":                   "Singapore",
	"sk":                    "Slovakia",
	"southkorea":            "Korea",
	"spa":                   "Spain",
	"srb":                   "Serbia",
	"svk":                   "Slovakia",
	"swe":                   "Sweden",
	"th":                    "Thailand",
	"tha":                   "Thailand",
	"tr":                    "Turkey",
	"tur":                   "Turkey",
	"tw":                    "Taiwan",
	"twn":                   "Taiwan",
	"ukr":                   "Ukraine",
	"unitedstates":          "USA",
	"unitedstatesofamerica": "USA",
	"unk":                   "Unknown",
	"us":                    "USA",
	"ua":                    "Ukraine",
	"uae":                   "United Arab Emirates",
	"wor":                   "World",
	"ww":                    "World",
	"za":                    "South Africa",
	"zaf":                   "South Africa",
	"zh":                    "China",
}

var displayNames = mergedNames(datDisplayNames, sourceAliases)

// NormalizeDisplay returns RomMeta's conventional human-readable source-media
// region label. Unknown values are preserved after trimming so source-specific
// values are not collapsed into a wrong bucket.
//
// The display labels are backed by the region-like values observed in the
// current No-Intro and Redump DAT files; short aliases are only used for
// external source fields such as ScreenScraper's "Jp" media region.
func NormalizeDisplay(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if normalized, ok := displayNames[regionKey(value)]; ok {
		return normalized
	}
	return value
}

// NormalizeDATDisplay recognizes full region labels from the first qualifier
// group in the current No-Intro/Redump DAT files. It deliberately avoids short
// language-like aliases such as Ja, Zh, or Pt because those usually appear as
// language tags in later DAT qualifiers.
func NormalizeDATDisplay(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	normalized, ok := datDisplayNames[regionKey(value)]
	return normalized, ok
}

func mergedNames(maps ...map[string]string) map[string]string {
	result := map[string]string{}
	for _, values := range maps {
		for key, value := range values {
			result[key] = value
		}
	}
	return result
}

func regionKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}
