package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
	"strings"
	"testing"
)

func TestQualifiedCHDExportsKeepObservedEditionsAndDiscSets(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint("packed=", packed), func(t *testing.T) {
			o, g, data := chdExportFixture(t)
			o.Target = "launchbox"
			doc := "title: Game\nreleases:\n"
			for _, region := range []string{"USA", "Europe"} {
				for n := 1; n <= 2; n++ {
					for _, beta := range []bool{false, true} {
						name := fmt.Sprintf("Game (%s) (Disc %d)", region, n)
						suffix := ""
						if beta {
							suffix = " (Beta)"
							name += suffix + " (1996-02-05)"
						}
						doc += fmt.Sprintf("  %s: {regions: [%s], developer: '%s disc %d%s'}\n", name, region, region, n, suffix)
						observed := fmt.Sprintf("Game (%s) (Disc %s)%s.chd", region, romanDisc(n), suffix)
						put(t, filepath.Join(o.ROMRoot, observed), data)
					}
				}
			}
			// Remove the fixture's extra single-disc input.
			if err := os.Remove(filepath.Join(o.ROMRoot, "Game (USA).chd")); err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(g, "game.yaml"), []byte(doc))
			if packed {
				catalogfstest.Pack(t, filepath.Dir(g))
			}
			for pass := 0; pass < 2; pass++ {
				plan, err := Build(o)
				if err != nil {
					t.Fatal(err)
				}
				if plan.Matched != 8 || len(plan.LaunchBox.Groups) != 1 || len(plan.LaunchBox.Groups[0].Versions) != 4 {
					t.Fatalf("incorrect grouping: %+v", plan.LaunchBox)
				}
				for _, record := range plan.Records {
					if record.Release != "" || record.Variant != "" || record.MatchedBy != "game-name" || record.Payload != nil || record.TitleContext == nil {
						t.Fatalf("invented identity: %+v", record)
					}
					if !strings.Contains(record.Fields["developer"].Value, record.TitleContext.Regions[0]) {
						t.Fatalf("wrong regional metadata: %+v", record)
					}
					if strings.Contains(record.TitleContext.Release, "1996-02-05") {
						t.Fatal("invented beta date")
					}
				}
				for _, v := range plan.LaunchBox.Groups[0].Versions {
					if !v.GeneratedPlaylist || len(v.Records) != 2 || !strings.Contains(v.Name, "[2 discs]") {
						t.Fatalf("lost disc set: %+v", v)
					}
					if strings.Contains(v.Name, "Disc ") {
						t.Fatal("disc token in set title", v.Name)
					}
					first := plan.Records[v.Records[0]]
					second := plan.Records[v.Records[1]]
					if opticalStage(first.TitleContext.Release) != opticalStage(second.TitleContext.Release) || strings.Join(first.TitleContext.Regions, ",") != strings.Join(second.TitleContext.Regions, ",") {
						t.Fatal("mixed editions")
					}
				}
				primary := plan.LaunchBox.Groups[0].Versions[0]
				if strings.Contains(primary.Name, "Beta") {
					t.Fatal("beta preferred over original")
				}
			}
		})
	}
}

func TestQualifiedOpticalAliasesRemainGameOnlyAndBounded(t *testing.T) {
	index := newCatalogIndex()
	root := t.TempDir()
	add := func(folder, doc string) {
		t.Helper()
		parsed, err := catalogyaml.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		indexNames(index, &game{path: filepath.Join(root, folder), name: field(parsed.Root(), "title"), root: parsed.Root()})
	}
	add("story", "title: Great Story - The Lost Land\nsources:\n  launchbox:\n    alternate_titles: ['Translated Story: The Lost Land']\nreleases:\n  Great Story - The Lost Land (Japan): {}\n")
	add("other", "title: Other Story\nsources:\n  igdb:\n    alternate_titles: [Great Story]\nreleases:\n  Other Story (Japan): {}\n")
	add("game", "title: Exact Game\nreleases:\n  Exact Game (Japan): {}\n")
	add("remix", "title: Exact Game Remix\nsources:\n  launchbox:\n    alternate_titles: [Exact Game]\nreleases:\n  Exact Game Remix (Japan): {}\n")
	add("compilation", "title: Ys I & II\nreleases:\n  Ys I & II (Japan): {}\n")
	add("sequel", "title: Ys III\nreleases:\n  Ys III (Japan): {}\n")
	for _, tc := range []struct{ name, want, status string }{
		{"Translated Story (En v1.04) (Disc II).chd", "story", "matched"},
		{"Great Story (En beta).chd", "other", "matched"},  // exact provider alias precedes shortened titles
		{"Translated Story (Japan).chd", "", "unmatched"},  // no shorthand for ordinary releases
		{"Exact Game (Japan) (2M).chd", "game", "matched"}, // catalog spelling beats broad provider alias
		{"Ys I & II (Japan) (2M).chd", "compilation", "matched"},
		{"Ys III (Japan) (2M).chd", "sequel", "matched"},
		{"Exact Game Plus (Japan) (2M).chd", "", "unmatched"},
		{"Exact Game (Unknown Subtitle) (Japan).chd", "", "unmatched"},
	} {
		record := Record{ROM: tc.name, Status: "unmatched", Disc: &DiscObservation{}}
		if err := nameFallback(root, index, &record, Profile{}, "saturn"); err != nil {
			t.Fatal(err)
		}
		if record.Status != tc.status || tc.want != "" && record.Game != tc.want || record.Release != "" || record.Variant != "" {
			t.Fatalf("%s: %+v", tc.name, record)
		}
	}
	add("collision", "title: Collision\nsources:\n  launchbox:\n    alternate_titles: ['Translated Story: Another Game']\nreleases:\n  Collision (Japan): {}\n")
	record := Record{ROM: "Translated Story (En v1.04).chd", Status: "unmatched", Disc: &DiscObservation{}}
	if err := nameFallback(root, index, &record, Profile{}, "saturn"); err != nil || record.Status != "ambiguous" {
		t.Fatalf("shared shorthand guessed: %+v %v", record, err)
	}
	// The strict consumer resolver must not acquire the export-only alias tier.
	if len(index.gameCandidates("Translated Story")) != 0 || len(index.nameCandidates("Translated Story (Japan)")) != 0 {
		t.Fatal("export aliases leaked into consumer identity")
	}
}

func TestOpticalDiscNumeralsAreBoundedAndPreserveEditions(t *testing.T) {
	for _, tc := range []struct {
		token  string
		number int
	}{{"Disc I", 1}, {"Disc II", 2}, {"Disk XXXII", 32}, {"Disc IIII", 0}, {"Disc 0", 0}, {"Disc 33", 0}} {
		if got := opticalDiscNumber(tc.token); got != tc.number {
			t.Fatalf("%s: %d", tc.token, got)
		}
	}
	if opticalTitle("Game (II) (Japan)") == opticalTitle("Game (Japan)") {
		t.Fatal("parenthesized sequel erased")
	}
}

func TestQualifiedAliasesSurviveLookupCacheAndConsumerRemainsStrict(t *testing.T) {
	o, g, _ := chdExportFixture(t)
	put(t, filepath.Join(g, "game.yaml"), []byte("title: Game\nsources:\n  launchbox:\n    alternate_titles: ['Other Name: The Adventure']\nreleases:\n  Game (USA): {}\n"))
	for pass := 0; pass < 2; pass++ {
		catalog, err := NewLookupCatalog(o.DataRoot, "")
		if err != nil {
			t.Fatal(err)
		}
		resolver, err := catalog.OpenExport("Sega - Saturn", "saturn", "")
		if err != nil {
			t.Fatal(err)
		}
		record := Record{ROM: "Other Name (En v2).chd", Status: "unmatched", Disc: &DiscObservation{}}
		if err = nameFallback(o.DataRoot, resolver.index, &record, catalog.Profile, "saturn"); err != nil || record.Status != "matched" || record.Release != "" || record.Variant != "" {
			t.Fatalf("pass %d: %+v %v", pass, record, err)
		}
		consumer, err := catalog.Open("Sega - Saturn")
		if err != nil {
			t.Fatal(err)
		}
		if consumer.IndexStats.Cached != 1 {
			t.Fatalf("not testing persisted summary: %+v", consumer.IndexStats)
		}
		strict := Record{ROM: "Other Name (En v2).chd", Status: "unmatched"}
		if err = consumer.bindDiscName(&strict); err != nil || strict.Status != "unmatched" {
			t.Fatalf("consumer acquired weak association: %+v %v", strict, err)
		}
	}
}
