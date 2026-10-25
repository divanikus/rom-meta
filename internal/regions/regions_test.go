package regions

import "testing"

func TestNormalizeDisplayUsesObservedDATRegionNames(t *testing.T) {
	cases := map[string]string{
		"jp":                   "Japan",
		"Jp":                   "Japan",
		"jpn":                  "Japan",
		"us":                   "USA",
		"United States":        "USA",
		"eu":                   "Europe",
		"asi":                  "Asia",
		"wor":                  "World",
		"uk":                   "UK",
		"United Kingdom":       "UK",
		"kr":                   "Korea",
		"South Korea":          "Korea",
		"zh":                   "China",
		"latam":                "Latin America",
		"United Arab Emirates": "United Arab Emirates",
		"Iceland":              "Iceland",
	}
	for input, want := range cases {
		if got := NormalizeDisplay(input); got != want {
			t.Fatalf("NormalizeDisplay(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeDisplayPreservesUnknownRegions(t *testing.T) {
	if got := NormalizeDisplay("Moon Base"); got != "Moon Base" {
		t.Fatalf("unknown region changed to %q", got)
	}
}

func TestNormalizeDATDisplayUsesOnlyDATRegionLabels(t *testing.T) {
	cases := map[string]string{
		"USA":            "USA",
		"Japan":          "Japan",
		"United Kingdom": "UK",
		"Hong Kong":      "Hong Kong",
		"Latin America":  "Latin America",
		"Iceland":        "Iceland",
	}
	for input, want := range cases {
		got, ok := NormalizeDATDisplay(input)
		if !ok || got != want {
			t.Fatalf("NormalizeDATDisplay(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestNormalizeDATDisplayRejectsLanguageAliases(t *testing.T) {
	for _, input := range []string{"Ja", "Zh", "Pt", "En", "Fr"} {
		if got, ok := NormalizeDATDisplay(input); ok {
			t.Fatalf("NormalizeDATDisplay(%q) = %q, true; want false", input, got)
		}
	}
}
