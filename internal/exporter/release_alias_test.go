package exporter

import (
	"fmt"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"testing"
)

func TestDeclaredReleaseAliasesRemainScopedAndUnverified(t *testing.T) {
	for _, packed := range []bool{false, true} {
		for _, collision := range []bool{false, true} {
			t.Run(fmt.Sprintf("packed=%v/collision=%v", packed, collision), func(t *testing.T) {
				options, card, _ := fixture(t)
				extra := ""
				if collision {
					extra = "    aliases: ['Local Example (USA)']\n"
				}
				body := "title: Example\ndefault_release: Example (USA)\nreleases:\n  Example (USA):\n    aliases: ['Local Example (USA)']\n    authority: {source: no-intro, entry_name: 'Example (USA)'}\n  Example (Europe):\n" + extra + "    regions: [Europe]\n"
				put(t, filepath.Join(card, "game.yaml"), []byte(body))
				if packed {
					catalogfstest.Pack(t, filepath.Dir(card))
				}
				for range 2 {
					catalog, err := NewLookupCatalog(options.DataRoot, "")
					if err != nil {
						t.Fatal(err)
					}
					resolver, err := catalog.Open("Nintendo - Nintendo 64")
					if err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{"Local Example (USA).zip", "Local Example (USA) (v02.00).zip", "Local Example (Europe).zip"} {
						input := ObservedInput{ROM: name, Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Files: []CatalogFile{{Name: name, Size: 1, MtimeNS: 1}}}}
						plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
						if err != nil {
							t.Fatal(err)
						}
						got := plan.Records[0]
						if name != "Local Example (USA).zip" {
							if got.Status == "matched" {
								t.Fatal("undeclared edition accepted", got)
							}
							continue
						}
						if collision {
							if got.Status != "ambiguous" {
								t.Fatal("colliding alias selected", got)
							}
							continue
						}
						if got.Status != "matched" || got.Release != "Example (USA)" || got.MatchedBy != "catalog-name" || got.SHA1 != "" || got.Payload != nil {
							t.Fatal(got)
						}
					}
				}
			})
		}
	}
}
