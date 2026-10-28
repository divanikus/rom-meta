package exporter

import (
	"fmt"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"strings"
	"testing"
)

func familyCard(t *testing.T, root, platform, title, release string) string {
	t.Helper()
	card := filepath.Join(root, "platforms", platform, title)
	put(t, filepath.Join(filepath.Dir(card), "_platform.yaml"), []byte("name: "+platform+"\n"))
	put(t, filepath.Join(card, "game.yaml"), []byte(fmt.Sprintf("title: %q\ndefault_release: %q\nreleases:\n  %q: {}\n", title, release, release)))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte("items: []\n"))
	return card
}

func TestCatalogFamilyLookupAndLocalExport(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			options, _, _ := fixture(t)
			base, digital := "Sony - PlayStation 3", "Sony - PlayStation 3 (PSN) (Content)"
			familyCard(t, options.DataRoot, base, "Disc", "Disc (USA)")
			card := familyCard(t, options.DataRoot, digital, "Download", "Download (USA)")
			if packed {
				catalogfstest.Pack(t, filepath.Dir(card))
			}
			catalog, err := NewLookupCatalog(options.DataRoot, "")
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := catalog.Open(base)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Disc (USA)", "Download (USA)"} {
				input := ObservedInput{ROM: name, Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Directory: true, Files: []CatalogFile{{Name: "data.bin", Size: 1, MtimeNS: 1}}}}
				plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
				if err != nil || len(plan.Records) != 1 || plan.Records[0].Status != "matched" {
					t.Fatalf("%s: %+v %v", name, plan.Records, err)
				}
				if name == "Download (USA)" && !strings.Contains(plan.Records[0].Game, digital) {
					t.Fatal(plan.Records[0])
				}
			}
			explicit, err := catalog.Open(digital)
			if err != nil {
				t.Fatal(err)
			}
			if explicit.IndexStats.Games != 1 || len(explicit.Platform.ExportSystems) != 0 {
				t.Fatal("explicit digital scope changed", explicit.IndexStats)
			}
			options.Platform, options.System = "ps3", "ps3"
			put(t, filepath.Join(options.ROMRoot, "Download (USA).ps3"), []byte("NPUB00001\n"))
			plan, err := Build(options)
			if err != nil || plan.Matched != 1 {
				t.Fatalf("local: %+v %v", plan, err)
			}
			remote, err := catalog.OpenExport(base, "ps3", "")
			if err != nil {
				t.Fatal(err)
			}
			inputs, err := ObserveExport(options)
			if err != nil {
				t.Fatal(err)
			}
			api, err := remote.Resolve(inputs, catalog.Profile)
			if err != nil || api.Matched != 1 || api.Records[0].Game != plan.Records[0].Game {
				t.Fatalf("API: %+v %v", api, err)
			}
		})
	}
}

func TestCatalogFamilyCrossCatalogCollisionIsAmbiguous(t *testing.T) {
	options, _, _ := fixture(t)
	base := "Sony - PlayStation 3"
	for _, platform := range []string{base, base + " (PSN) (Content)"} {
		familyCard(t, options.DataRoot, platform, "Shared", "Shared (USA)")
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.OpenExport(base, "ps3", "")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(options.ROMRoot, "Shared (USA).ps3"), []byte("NPUB00001\n"))
	options.System = "ps3"
	inputs, err := ObserveExport(options)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.Resolve(inputs, catalog.Profile)
	if err != nil || plan.Ambiguous != 1 {
		t.Fatalf("collision: %+v %v", plan, err)
	}
}

func TestCatalogFamilyCapabilitiesAndDigitalOnly(t *testing.T) {
	for _, base := range []string{"Nintendo - Wii", "Nintendo - Wii U", "Sony - PlayStation 3", "Sony - PlayStation Portable"} {
		for _, disc := range []bool{true, false} {
			t.Run(fmt.Sprint(base, disc), func(t *testing.T) {
				options, _, _ := fixture(t)
				digital := base + " (Digital) (CDN)"
				if base == "Sony - PlayStation 3" {
					digital = base + " (PSN) (Content)"
				}
				if base == "Sony - PlayStation Portable" {
					digital = base + " (PSN) (Minis) (Encrypted)"
				}
				if disc {
					familyCard(t, options.DataRoot, base, "Disc", "Disc (USA)")
				}
				familyCard(t, options.DataRoot, digital, "Digital", "Digital (USA)")
				catalog, err := NewLookupCatalog(options.DataRoot, "")
				if err != nil {
					t.Fatal(err)
				}
				anchor := digital
				want := 1
				if disc {
					anchor = base
					want = 2
				}
				resolver, err := catalog.Open(anchor)
				if err != nil || resolver.IndexStats.Games != want || len(resolver.Platform.Catalogs) != want {
					t.Fatalf("%+v %v", resolver, err)
				}
				if base == "Nintendo - Wii" || base == "Nintendo - Wii U" {
					if len(resolver.Platform.ExportSystems) != 0 {
						t.Fatal("metadata routing advertised an absent launch reader")
					}
				}
			})
		}
	}
}
