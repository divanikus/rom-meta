package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func descriptionFixture(t *testing.T) (Options, string) {
	t.Helper()
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	put(t, filepath.Join(card, "game.yaml"), []byte(`title: Example
default_release: Example (Europe)
releases:
  Example (USA):
    regions: [USA]
    release_date: '2000-01-01'
  Example (Europe):
    regions: [Europe]
sources:
  launchbox:
    matches:
      - release: Example (Europe)
        release_date: '1999-01-01'
        descriptions:
          en: European prose
  screenscraper:
    matches:
      - release: Example (Europe)
        descriptions:
          ru: Проверенное описание
`))
	profile(t, &options, "selection:\n  description:\n    scope: release\n")
	return options, card
}

const descriptionChoice = `schema_version: 1
target: es-de
selection:
  description:
    scope: release
    languages: [ru, en]
description_fallbacks:
  - game: platforms/Nintendo - Nintendo 64/Example
    release: Example (USA)
    from_release: Example (Europe)
`

func TestDescriptionFallbackRetainsBindingFieldsAndProvenance(t *testing.T) {
	options, _ := descriptionFixture(t)
	strict, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if strict.Records[0].Fields["description"].Value != "" {
		t.Fatal("default crossed release scope")
	}
	profile(t, &options, descriptionChoice)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	r := plan.Records[0]
	v := r.Fields["description"]
	if r.Release != "Example (USA)" || r.MatchedBy != "hash" || r.Fields["release_date"].Value != "2000-01-01" ||
		v.Value != "Проверенное описание" || v.Source != "screenscraper" || v.Language != "ru" ||
		v.Selection != "reviewed-fallback" || v.FromRelease != "Example (Europe)" {
		t.Fatalf("lost binding, field or provenance: %+v", r)
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	xml, err := os.ReadFile(filepath.Join(options.Out, "gamelists/n64/gamelist.xml"))
	if err != nil || !strings.Contains(string(xml), "<desc>Проверенное описание</desc>") {
		t.Fatal(string(xml), err)
	}
}

func TestDescriptionFallbackDoesNotReplaceEligibleDescription(t *testing.T) {
	options, card := descriptionFixture(t)
	b, _ := os.ReadFile(filepath.Join(card, "game.yaml"))
	put(t, filepath.Join(card, "game.yaml"), []byte(strings.Replace(string(b), "    release_date: '2000-01-01'", "    release_date: '2000-01-01'\n    description: Explicit local prose", 1)))
	profile(t, &options, descriptionChoice)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Records[0].Fields["description"]; got.Value != "Explicit local prose" || got.Selection != "" {
		t.Fatal(got)
	}
}

func TestDescriptionFallbackExactGameReleaseAndKnownBinding(t *testing.T) {
	options, _ := descriptionFixture(t)
	p, err := loadProfile(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	p.DescriptionFallbacks = []DescriptionFallback{{Game: "platforms/Nintendo - Nintendo 64/Example", Release: "Example (USA)", FromRelease: "Example (Europe)"}}
	for _, r := range []Record{
		{Game: "platforms/Nintendo - Nintendo 64/Other", Release: "Example (USA)"},
		{Game: "platforms/Nintendo - Nintendo 64/Example", Release: "Example (Japan)"},
		{Game: "platforms/Nintendo - Nintendo 64/Example", MetadataRelease: "Example (USA)"},
	} {
		r.Fields = map[string]Value{}
		if err := applyDescriptionFallback(&game{}, &r, p); err != nil || len(r.Fields) != 0 {
			t.Fatal("choice leaked across binding", r, err)
		}
	}
}

func TestDescriptionFallbackInvalidOrUnavailable(t *testing.T) {
	base := DescriptionFallback{Game: "platforms/P/G", Release: "G (USA)", FromRelease: "G (Europe)"}
	for _, item := range []DescriptionFallback{
		{Game: "../outside", Release: "G", FromRelease: "Other"}, {Game: "platforms/P/G", Release: "G", FromRelease: "G"}, {Game: "platforms/P/G", Release: "G"},
	} {
		if validateDescriptionFallbacks(Profile{DescriptionFallbacks: []DescriptionFallback{item}}) == nil {
			t.Fatal(item)
		}
	}
	if validateDescriptionFallbacks(Profile{DescriptionFallbacks: []DescriptionFallback{base, base}}) == nil {
		t.Fatal("duplicate")
	}
	for _, replacement := range []string{"Example (Missing)", "Example (Europe)"} {
		options, card := descriptionFixture(t)
		if replacement == "Example (Europe)" {
			b, _ := os.ReadFile(filepath.Join(card, "game.yaml"))
			put(t, filepath.Join(card, "game.yaml"), []byte(strings.ReplaceAll(strings.ReplaceAll(string(b), "en: European prose", "de: German prose"), "ru: Проверенное описание", "fr: French prose")))
		}
		profile(t, &options, strings.Replace(descriptionChoice, "from_release: Example (Europe)", "from_release: "+replacement, 1))
		if _, err := Build(options); err == nil {
			t.Fatal("unavailable reviewed text silently ignored")
		}
	}
}
