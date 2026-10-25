package sourceyaml

import (
	"strings"
	"testing"
)

func TestRenderDescriptionsOrdersEnglishFirst(t *testing.T) {
	var b strings.Builder

	RenderDescriptions(&b, "        ", map[string]string{
		"fr": "Texte francais.",
		"en": "English text.",
		"de": "",
	})

	rendered := b.String()
	enIndex := strings.Index(rendered, "en: |-")
	frIndex := strings.Index(rendered, "fr: |-")
	if enIndex < 0 || frIndex < 0 || enIndex > frIndex {
		t.Fatalf("expected English description first:\n%s", rendered)
	}
	if strings.Contains(rendered, "de:") {
		t.Fatalf("empty description should not be rendered:\n%s", rendered)
	}
}

func TestRenderDescriptionsDropsForbiddenControlCharacters(t *testing.T) {
	var b strings.Builder

	RenderDescriptions(&b, "        ", map[string]string{
		"en": "Link Dragon (é¾\u008d) keeps\nvalid lines.",
	})

	rendered := b.String()
	if strings.ContainsRune(rendered, '\u008d') {
		t.Fatalf("description retained a forbidden control character: %q", rendered)
	}
	if !strings.Contains(rendered, "Link Dragon (é¾) keeps") || !strings.Contains(rendered, "valid lines.") {
		t.Fatalf("description content was not preserved:\n%s", rendered)
	}
}

func TestRenderMediaStripsSecretURLParts(t *testing.T) {
	var b strings.Builder

	RenderMedia(&b, "        ", map[string][]MediaCandidate{
		"box_front": {{
			URL:    "https://user:pass@example.test/front.png?media=box&softname=RomMeta&ssid=user&sspassword=pass&devid=dev&devpassword=devpass&token=secret",
			Region: "Jp",
		}},
	})

	rendered := b.String()
	if !strings.Contains(rendered, `- url: "https://example.test/front.png?media=box"`) {
		t.Fatalf("media URL was not sanitized:\n%s", rendered)
	}
	if !strings.Contains(rendered, `region: "Japan"`) || strings.Contains(rendered, `region: "Jp"`) {
		t.Fatalf("media region was not normalized:\n%s", rendered)
	}
	for _, secret := range []string{"user:pass", "softname", "ssid", "sspassword", "devid", "devpassword", "token", "secret"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("rendered media leaked %q:\n%s", secret, rendered)
		}
	}
}

func TestRenderTitlesNormalizesRegions(t *testing.T) {
	var b strings.Builder

	RenderTitles(&b, "        ", []TitleCandidate{
		{Title: "Lylat Wars", Region: "eu"},
		{Title: "Star Fox 64", Region: "us", Language: "en"},
		{Title: "Star Fox 64", Region: "us", Language: "en"},
	})

	rendered := b.String()
	for _, expected := range []string{
		`titles:`,
		`- title: "Lylat Wars"`,
		`region: "Europe"`,
		`- title: "Star Fox 64"`,
		`region: "USA"`,
		`language: "en"`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered titles missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Count(rendered, `- title: "Star Fox 64"`) != 1 {
		t.Fatalf("duplicate title candidate was not removed:\n%s", rendered)
	}
}
