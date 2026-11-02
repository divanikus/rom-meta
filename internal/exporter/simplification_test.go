package exporter

import (
	"fmt"
	"rommeta/internal/catalogyaml"
	"testing"
)

func discProjectionFixture(root string, count, first int) *Plan {
	plan := &Plan{System: "psx", ROMRoot: root}
	for i := count - 1; i >= 0; i-- {
		number := (i+first-1)%32 + 1
		name := fmt.Sprintf("Game (USA) (Disc %d)", number)
		r := versionRecord(fmt.Sprintf("disc-%d.iso", i+first), "platforms/psx/Game", name, "USA", "en", "original")
		r.TitleContext.Disc = fmt.Sprintf("Disc %d", number)
		plan.Records = append(plan.Records, r)
	}
	return plan
}

func TestLaunchBoxDiscSetLimitsAndOrder(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, first int
		grouped      bool
	}{
		{"complete", 32, 1, true}, {"missing-first", 31, 2, false}, {"too-many", 33, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := discProjectionFixture(t.TempDir(), tc.count, tc.first)
			projection, err := buildLaunchBoxProjection(plan)
			if err != nil {
				t.Fatal(err)
			}
			versions := projection.Groups[0].Versions
			if tc.grouped {
				if len(versions) != 1 || !versions[0].GeneratedPlaylist || len(versions[0].Records) != tc.count {
					t.Fatal(versions)
				}
				for n, index := range versions[0].Records {
					if plan.Records[index].ROM != fmt.Sprintf("disc-%d.iso", n+1) {
						t.Fatal("disc order", versions[0])
					}
				}
			} else {
				if len(versions) != tc.count || len(projection.Warnings) != tc.count {
					t.Fatal(projection)
				}
				for _, v := range versions {
					if v.GeneratedPlaylist || len(v.Records) != 1 {
						t.Fatal(v)
					}
				}
			}
		})
	}
}

func BenchmarkLaunchBoxDiscProjection(b *testing.B) {
	for _, tc := range []struct {
		name         string
		count, first int
	}{
		{"complete", 32, 1}, {"incomplete", 31, 2}, {"oversized", 64, 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			plan := discProjectionFixture(b.TempDir(), tc.count, tc.first)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := buildLaunchBoxProjection(plan); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestFieldChoicePreservesTiePrecedence(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"release-before-game", "description: game text\nreleases:\n  Edition: {description: release text}\n", "release text"},
		{"first-source-observation", "releases: {Edition: {}}\nsources:\n  launchbox:\n    matches:\n      - {release: Edition, descriptions: {en: first}}\n      - {release: Edition, descriptions: {en: second}}\n", "first"},
		{"explicit-local-prose", "description: literal text\nreleases:\n  Edition: {descriptions: {en: translated text}}\n", "literal text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := catalogyaml.Parse([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			profile, err := loadProfile("", "")
			if err != nil {
				t.Fatal(err)
			}
			value := resolveFields(&game{name: "Example", root: doc.Root()}, "Edition", profile)["description"]
			if value.Value != tc.want {
				t.Fatal(value)
			}
		})
	}
}
