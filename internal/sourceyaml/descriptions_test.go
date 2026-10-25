package sourceyaml

import (
	"strings"
	"testing"

	"rommeta/internal/catalogyaml"
)

func TestRenderDescriptionsNormalizesLineBreaks(t *testing.T) {
	for _, tc := range []struct{ name, separator string }{
		{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"},
		{"NEL", "\u0085"}, {"line separator", "\u2028"}, {"paragraph separator", "\u2029"},
		{"mixed", "\r\n\r\u0085\u2028\u2029\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{"First paragraph.", "", "  Indented continuation", "\tTabbed", "---", "sources: literal text", "Second paragraph."}
			description := strings.Join(lines, tc.separator)
			normalizedSeparator := "\n"
			if tc.name == "mixed" {
				normalizedSeparator = strings.Repeat("\n", 6)
			}
			expected := strings.Join(lines, normalizedSeparator)
			var b strings.Builder
			b.WriteString("sources:\n  screenscraper:\n    matches:\n      - title: Example\n")
			RenderDescriptions(&b, "        ", map[string]string{"en": description, "fr": "Texte suivant."})
			b.WriteString("        publisher: Preserved\n")
			doc, err := catalogyaml.Parse([]byte(b.String()))
			if err != nil {
				t.Fatalf("generated descriptions are not valid YAML: %v", err)
			}
			source := catalogyaml.Value(catalogyaml.Value(doc.Root(), "sources"), "screenscraper")
			match := catalogyaml.Value(source, "matches").Content[0]
			descriptions := catalogyaml.Value(match, "descriptions")
			if got := catalogyaml.Value(descriptions, "en").Value; got != expected {
				t.Fatalf("description changed: got %q, want %q", got, expected)
			}
			if got := catalogyaml.Value(descriptions, "fr").Value; got != "Texte suivant." {
				t.Fatalf("following language changed: %q", got)
			}
			if got := catalogyaml.Value(match, "publisher").Value; got != "Preserved" {
				t.Fatalf("following field changed: %q", got)
			}
		})
	}
}
