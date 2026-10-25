package exporter

import (
	"encoding/xml"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
)

func TestProviderDescriptionEntitiesDecodeOnlyForDisplay(t *testing.T) {
	text := `title: Example
releases:
  Example (USA): {}
sources:
  launchbox:
    matches:
      - release: Example (USA)
        descriptions:
          en: 'A &quot;Dragonmaster&quot; &amp; friend; &#169; &lt;literal&gt; &amp;quot;'
`
	for _, provider := range []string{"screenscraper", "launchbox"} {
		for _, local := range []bool{false, true} {
			input := strings.Replace(text, "launchbox:", provider+":", 1)
			if local {
				input += "description: 'Local &quot;literal&quot;'\n"
			}
			doc, err := catalogyaml.Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			profile, err := loadProfile("", "")
			if err != nil {
				t.Fatal(err)
			}
			g := &game{name: "Example", root: doc.Root()}
			before, err := yaml.Marshal(g.root)
			if err != nil {
				t.Fatal(err)
			}
			value := resolveFields(g, "Example (USA)", profile)["description"]
			wanted := "A \"Dragonmaster\" & friend; \u00a9 <literal> &quot;"
			source := provider
			if local {
				wanted = "Local &quot;literal&quot;"
				source = "local"
			}
			if value.Value != wanted || value.Source != source {
				t.Fatal(value)
			}
			// Projection leaves the provider evidence untouched and XML escapes exactly once.
			again := resolveFields(g, "Example (USA)", profile)["description"]
			after, err := yaml.Marshal(g.root)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("provider evidence changed")
			}
			if again != value {
				t.Fatal("source mutated")
			}
			plan := &Plan{System: "psx", Records: []Record{{ROM: "Example.chd", Status: "matched", Fields: map[string]Value{"description": value}}}}
			data, err := RenderGamelist(plan)
			if err != nil {
				t.Fatal(err)
			}
			var decoded xmlList
			if err = xml.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Games) != 1 || decoded.Games[0].Description != wanted {
				t.Fatal(string(data))
			}
			if !local && strings.Contains(string(data), "&amp;quot;Dragonmaster") {
				t.Fatal("provider entity escaped twice")
			}
		}
	}
}
