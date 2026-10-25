package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultGameDescriptionWorksForPreviouslyUnselectedRevision(t *testing.T) {
	for _, release := range []string{"Example (USA)", "Example (Japan) (Rev 2)"} {
		t.Run(release, func(t *testing.T) {
			options, card := descriptionFixture(t)
			b, _ := os.ReadFile(filepath.Join(card, "game.yaml"))
			body := strings.Replace(string(b), "  Example (Europe):", "  Example (Japan) (Rev 2):\n    regions: [Japan]\n  Example (Europe):", 1)
			put(t, filepath.Join(card, "game.yaml"), []byte(body))
			if release != "Example (USA)" {
				if err := os.Remove(filepath.Join(options.ROMRoot, "Example (USA).z64")); err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(options.ROMRoot, release+".z64"), []byte("different synthetic ROM"))
			}
			// No game paths, release choices or overrides in the profile.
			profile(t, &options, "selection:\n  description:\n    languages: [ru, en]\n  media: {}\n")
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			r := plan.Records[0]
			v := r.Fields["description"]
			if r.Release != release || v.Value != "Проверенное описание" || v.Source != "screenscraper" ||
				v.Selection != "game" || v.FromRelease != "Example (Europe)" {
				t.Fatalf("%+v", r)
			}
			if release == "Example (USA)" && r.Fields["release_date"].Value != "2000-01-01" {
				t.Fatal("donor date leaked")
			}
			if release != "Example (USA)" && r.Fields["release_date"].Value != "" {
				t.Fatal("donor date invented")
			}
		})
	}
}

func TestGameDescriptionLanguageFirstAndNestedSourceScope(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Renamed.z64"), payload)
	put(t, filepath.Join(card, "game.yaml"), []byte(`title: Example
releases:
  Example (USA): {}
  Example (Europe): {}
sources:
  launchbox:
    matches:
      - release: Example (USA)
        descriptions:
          en: Exact English
  screenscraper:
    matches:
      - descriptions:
          en: Shared English
        releases:
          - release: Example (USA)
          - release: Example (Europe)
            descriptions:
              ru: Русский текст игры
  igdb:
    matches:
      - release: Removed edition
        descriptions:
          ru: Stale scope must not win
`))
	profile(t, &options, "selection:\n  description:\n    languages: [ru, en]\n")
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	v := plan.Records[0].Fields["description"]
	if v.Value != "Русский текст игры" || v.FromRelease != "Example (Europe)" {
		t.Fatal(v)
	}
	profile(t, &options, "selection:\n  description:\n    scope: release\n    languages: [ru, en]\n")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Fields["description"].Value != "Exact English" {
		t.Fatal("strict option lost")
	}
}

func TestArtworkVariantUsesFilenameAcrossSameGameReleases(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Renamed.z64"), payload)
	// Byte content and saved checksums do not classify artwork at export time.
	original := []byte("arbitrary bytes")
	shared := filepath.Join(card, "media/box-front-99.usa.en.variant-original.launchbox.jpg")
	sibling := filepath.Join(card, "media/Example (Europe)/box-front-42.usa.en.variant-original.launchbox.png")
	put(t, shared, original)
	put(t, sibling, original)
	put(t, filepath.Join(card, "media/Example (USA)/box-front-01.usa.en.variant-reconstructed.launchbox.png"), original)
	put(t, filepath.Join(card, "media/box-front-01.usa.en.launchbox.png"), original)
	put(t, filepath.Join(card, "game.yaml"), []byte(`title: Example
releases:
  Example (USA): {regions: [USA]}
  Example (Europe): {regions: [Europe]}
sources:
  launchbox:
    matches:
      - release: Example (Europe)
        media:
          box_front:
            - crc32: '0'
              source_type: Box - Front - Reconstructed
`))
	profile(t, &options, `selection:
  media:
    cover:
      scope: game
      order: [variant, scope, region, source, language, format]
      variants: [original, reconstructed, unknown]
      sources: [launchbox]
      languages: [en]
`)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	a := plan.Records[0].Media["cover"]
	if a.Variant != "original" || a.VariantEvidence != "filename" || a.Language != "en" ||
		!strings.HasSuffix(a.SourcePath, "box-front-99.usa.en.variant-original.launchbox.jpg") {
		t.Fatal(a)
	}
	if err := os.Remove(shared); err != nil {
		t.Fatal(err)
	}
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	a = plan.Records[0].Media["cover"]
	if a.Variant != "original" || a.Selection != "game" || a.FromRelease != "Example (Europe)" {
		t.Fatal(a)
	}
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Media["cover"].Variant != "reconstructed" {
		t.Fatal("filename category priority lost")
	}
	profile(t, &options, "selection:\n  media:\n    cover:\n      variants: [unknown]\n      sources: [launchbox]\n")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	a = plan.Records[0].Media["cover"]
	if a.Variant != "unknown" || strings.Contains(a.SourcePath, ".variant-") {
		t.Fatal("legacy filename no longer eligible", a)
	}
}

func TestGenericScopeProfileRejectsWrongFieldsAndUnknownVariants(t *testing.T) {
	for _, body := range []string{
		"selection:\n  description:\n    scope: anything\n",
		"selection:\n  fields:\n    release_date:\n      scope: game\n",
		"selection:\n  media:\n    cover:\n      variants: [flat-because-png]\n",
		"selection:\n  media:\n    cover:\n      order: [variant]\n",
	} {
		options, _, _ := fixture(t)
		profile(t, &options, body)
		if _, err := Build(options); err == nil {
			t.Fatal("invalid rule accepted", body)
		}
	}
}
