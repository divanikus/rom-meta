package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlatFrontUsesBoundCategoryNotRatioSourceOrVariant(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	put(t, filepath.Join(card, "game.yaml"), []byte(`title: Example
releases:
  Example (USA): {regions: [USA]}
sources:
  screenscraper:
    matches:
      - release: Example (USA)
        media:
          box_front:
            - url: https://example.test/square.jpg
              region: Japan
              source_type: box-2D
`))
	flat := filepath.Join(card, "media/box-front.japan.screenscraper.jpg")
	put(t, flat, []byte("legitimate square Japanese front; dimensions are never inspected"))
	put(t, filepath.Join(card, "media/box-front.usa.emumovies.jpg"), []byte("unknown front and spine"))
	put(t, filepath.Join(card, "media/box-front-99.usa.variant-original.launchbox.jpg"), []byte("unbound filename must not establish flat style"))
	profile(t, &options, `selection:
  media:
    cover:
      scope: game
      order: [style, variant, region, source, scope, format]
      styles: [flat-front, unknown, front-spine]
      variants: [original, unknown, reconstructed, fanart]
      regions: [$release, japan, world, unspecified]
      sources: [emumovies, launchbox, screenscraper]
`)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	selected := plan.Records[0].Media["cover"]
	if selected.Source != "screenscraper" || selected.Style != "flat-front" || selected.SourceType != "box-2D" || selected.StyleEvidence != "provider-category:screenscraper:box-2D" {
		t.Fatal(selected)
	}
	assets, err := InventoryMedia(options.DataRoot, plan.Records)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range assets {
		if asset.Kind == "box-front" {
			if asset.Source == "screenscraper" && filepath.Base(asset.Path) == filepath.Base(flat) {
				if asset.Style != "flat-front" {
					t.Fatal(asset)
				}
			} else if asset.Style != "unknown" {
				t.Fatal("source/variant guessed style", asset)
			}
		}
	}
	if err = os.Remove(flat); err != nil {
		t.Fatal(err)
	}
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Media["cover"].Style != "unknown" {
		t.Fatal("unknown fallback hidden")
	}
}

func TestStylesRequireExplicitProfileAndValidMediaDimension(t *testing.T) {
	for _, body := range []string{
		"selection:\n  media:\n    cover:\n      order: [style]\n",
		"selection:\n  media:\n    cover:\n      styles: [flat-from-width]\n",
		"selection:\n  description:\n    styles: [flat-front]\n",
	} {
		options, _, _ := fixture(t)
		profile(t, &options, body)
		if _, err := Build(options); err == nil {
			t.Fatal("invalid style rule accepted", body)
		}
	}
	styles, err := savedMediaStyles(t.TempDir())
	if err == nil || styles != nil {
		t.Fatal("missing source document hidden")
	}
}

func TestLegacyDefaultAndExplicitCustomOrder(t *testing.T) {
	options, _, _ := fixture(t)
	p, err := loadProfile(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	for role, rule := range p.Selection.Media {
		if strings.Join(rule.Order, ",") != "region,source,format" || rule.Styles != nil || rule.Scope != "release" {
			t.Fatal("legacy default changed", role, rule)
		}
	}
	profile(t, &options, "selection:\n  media:\n    cover:\n      order: [source]\n")
	p, err = loadProfile(options.DataRoot, options.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Selection.Media["cover"].Order) != 1 || p.Selection.Media["cover"].Order[0] != "source" {
		t.Fatal("explicit owner policy overwritten")
	}
}

func TestLegacyScopeAndExplicitFlatPreference(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	put(t, filepath.Join(card, "game.yaml"), []byte(`title: Example
releases:
  Example (USA): {regions: [USA]}
sources:
  screenscraper:
    matches:
      - release: Example (USA)
        media:
          box_front:
            - url: https://example.test/front.jpg
              region: USA
              source_type: box-2D
`))
	put(t, filepath.Join(card, "media/box-front.usa.screenscraper.jpg"), []byte("flat card-wide front"))
	put(t, filepath.Join(card, "media/Example (USA)/box-front.usa.emumovies.jpg"), []byte("unknown exact release front"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if selected := plan.Records[0].Media["cover"]; selected.Style != "" || selected.Source != "emumovies" {
		t.Fatal("legacy exact-release priority changed", selected)
	}
	profile(t, &options, `selection:
  media:
    cover:
      order: [style, region, source, scope, format]
      styles: [flat-front, unknown, front-spine]
`)
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if selected := plan.Records[0].Media["cover"]; selected.Style != "flat-front" || selected.Source != "screenscraper" {
		t.Fatal("explicit flat preference lost", selected)
	}
}

func TestMultiRegionReleaseUsesExplicitPreferenceBeforeFilenameOrdinal(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	put(t, filepath.Join(card, "game.yaml"), []byte(`title: Example
releases:
  Example (USA): {regions: [USA, Europe, Brazil]}
sources:
  screenscraper:
    matches:
      - release: Example (USA)
        media:
          box_front:
            - url: https://example.test/brazil.jpg
              region: Brazil
              source_type: box-2D
            - url: https://example.test/usa.jpg
              region: USA
              source_type: box-2D
`))
	put(t, filepath.Join(card, "media/box-front-01.brazil.screenscraper.jpg"), []byte("first ordinal Brazilian regional front"))
	put(t, filepath.Join(card, "media/box-front-02.usa.screenscraper.jpg"), []byte("second ordinal USA front"))
	profile(t, &options, `selection:
  media:
    cover:
      order: [style, variant, region, region-preference, source, scope, format]
      styles: [flat-front, unknown, front-spine]
      variants: [original, unknown, reconstructed, fanart]
      regions: ['$release', world, unspecified, usa, japan, europe, '*']
      sources: [screenscraper]
`)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if media := plan.Records[0].Media["cover"]; media.Region != "usa" || !strings.Contains(media.SourcePath, "box-front-02.usa") {
		t.Fatal("filename ordinal defeated explicit release-region preference", media)
	}
}
func TestReleaseAffinityWinsAndRegionPreferenceIsNotHardcoded(t *testing.T) {
	priority := []string{"$release", "world", "unspecified", "europe", "usa", "japan", "*"}
	regions := []string{"USA", "Europe", "Brazil"}
	rank := func(region string, releases []string) []int {
		affinity := rankRegion(priority, region, releases)
		return []int{affinity, rankRegionPreference(priority, region, affinity)}
	}
	usa, europe, brazil := rank("usa", regions), rank("europe", regions), rank("brazil", regions)
	if usa[0] != europe[0] || usa[0] != brazil[0] {
		t.Fatal("legacy multi-region affinity must tie", europe, usa, brazil)
	}
	if !lessRank(europe, usa) || !lessRank(usa, brazil) {
		t.Fatal("explicit regional preference not preserved", europe, usa, brazil)
	}
	japan, nonreleaseUSA := rank("japan", []string{"Japan"}), rank("usa", []string{"Japan"})
	if !lessRank(japan, nonreleaseUSA) || nonreleaseUSA[1] != 0 {
		t.Fatal("affinity or neutral nonrelease rank changed", japan, nonreleaseUSA)
	}
	if rankRegion([]string{"$release"}, "usa", []string{"Japan"}) != -1 {
		t.Fatal("excluded region became eligible")
	}
	if rankRegionPreference([]string{"*", "$release", "usa"}, "usa", 0) != 0 {
		t.Fatal("wildcard tier acquired an implicit regional preference")
	}
}

func TestRegionPreferenceRequiresExplicitEarlierRegionCriterion(t *testing.T) {
	for _, body := range []string{
		"selection:\n  media:\n    cover:\n      order: [region-preference]\n",
		"selection:\n  media:\n    cover:\n      order: [region-preference, region]\n",
		"selection:\n  description:\n    order: [region, region-preference]\n",
	} {
		options, _, _ := fixture(t)
		profile(t, &options, body)
		if _, err := Build(options); err == nil {
			t.Fatal("invalid regional tie criterion accepted", body)
		}
	}
}

func TestRegionalPreferenceIsIndependentOfStylesAndExplicitPerRole(t *testing.T) {
	for _, tc := range []struct{ name, order, extra, want string }{
		{"legacy", "region, source, format", "", "brazil"},
		{"styles-only", "style, region, source, format", "      styles: [flat-front, unknown, front-spine]\n", "brazil"},
		{"regional-only", "region, region-preference, source, format", "", "usa"},
		{"both", "style, region, region-preference, source, format", "      styles: [flat-front, unknown, front-spine]\n", "usa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, _, payload := regionalFixture(t, false)
			put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
			profile(t, &options, "selection:\n  media:\n    cover:\n      order: ["+tc.order+"]\n"+tc.extra+"      regions: ['$release', world, unspecified, usa, japan, europe, '*']\n    screenshot: {}\n    logo: {}\n    video: {}\n    manual: {}\n")
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			for role, selected := range plan.Records[0].Media {
				want := "brazil"
				if role == "cover" {
					want = tc.want
				}
				if selected.Region != want {
					t.Fatal("preference leaked across criteria or roles", role, selected, want)
				}
			}
		})
	}
}
