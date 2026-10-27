package exporter

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalogfstest"
	"strings"
	"testing"
)

func variantFixture(t *testing.T) (Options, string, []byte) {
	t.Helper()
	options, path, _ := fixture(t)
	put(t, filepath.Join(path, "game.yaml"), []byte(`title: Original
default_release: Original (USA)
releases:
  Original (USA):
    title: American title
    description: Wrong default description
    developer: Wrong default developer
  Original (Japan):
    title: Japanese title
    regions: [Japan]
    languages: [ja]
    description: Original Japanese release description
    developer: Original developer
    publisher: Original publisher
    genres: [Adventure]
    players: 2
    release_date: '1991-05-06'
variants:
  English:
    title: Translated Adventure
    kind: translation
    based_on: Original (Japan)
    languages: [en]
    group: Translation team
    version: '1.0'
  Fixed:
    kind: bugfix
    based_on: English
    publisher: Explicit variant publisher
`))
	payload := []byte("synthetic independently patched bytes")
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - variant: Fixed\n    file:\n      name: Source Name.z64\n      size: %d\n      sha1: %x\n", len(payload), sha1.Sum(payload))))
	put(t, filepath.Join(options.ROMRoot, "translations", "Entirely renamed.z64"), payload)
	return options, path, payload
}

func assertVariant(t *testing.T, r Record) {
	t.Helper()
	if r.Status != "matched" || r.MatchedBy != "hash" || r.Variant != "Fixed" || r.Release != "" || r.MetadataRelease != "Original (Japan)" {
		t.Fatalf("wrong derivative identity: %+v", r)
	}
	if r.Fields["description"].Value != "Original Japanese release description" || r.Fields["developer"].Value != "Original developer" ||
		r.Fields["publisher"].Value != "Explicit variant publisher" || r.Fields["release_date"].Value != "1991-05-06" ||
		r.Fields["genres"].Value != "Adventure" || r.Fields["players"].Value != "2" {
		t.Fatalf("wrong inherited/override fields: %+v", r.Fields)
	}
	if r.Fields["description"].FromRelease != "Original (Japan)" || r.Fields["publisher"].FromVariant != "Fixed" {
		t.Fatalf("lost inheritance provenance: %+v", r.Fields)
	}
	if r.VariantInfo == nil || strings.Join(r.VariantInfo.Languages, ",") != "en" || r.VariantInfo.BasedOn != "English" {
		t.Fatalf("wrong variant information: %+v", r.VariantInfo)
	}
	if len(r.VariantInfo.Ancestors) != 1 || r.VariantInfo.Ancestors[0].Group != "Translation team" || r.VariantInfo.Ancestors[0].Version != "1.0" {
		t.Fatalf("ancestor credits lost: %+v", r.VariantInfo)
	}
	if r.TitleContext == nil || strings.Join(r.TitleContext.Languages, ",") != "en" || r.TitleContext.Regional != "Translated Adventure" {
		t.Fatalf("wrong translated title/language: %+v", r.TitleContext)
	}
	if r.Media["cover"].SourcePath == "" {
		t.Fatal("base cover not inherited")
	}
}

func TestVariantExportLookupAndPersistentIndex(t *testing.T) {
	options, _, payload := variantFixture(t)
	before := treeHashes(t, options.ROMRoot)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 1 {
		t.Fatal(plan)
	}
	assertVariant(t, plan.Records[0])
	if !strings.Contains(plan.Records[0].Fields["title"].Value, "Translated Adventure") {
		t.Fatal(plan.Records[0].Fields)
	}
	if err = Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	xml, err := os.ReadFile(filepath.Join(options.Out, "gamelists", "n64", "gamelist.xml"))
	if err != nil || !strings.Contains(string(xml), "./translations/Entirely renamed.z64") || !strings.Contains(string(xml), "Original Japanese release description") {
		t.Fatalf("%s %v", xml, err)
	}
	if fmt.Sprint(before) != fmt.Sprint(treeHashes(t, options.ROMRoot)) {
		t.Fatal("ROM files changed")
	}
	// Two opens exercise a rebuilt index and its persisted representation.
	for i := 0; i < 2; i++ {
		catalog, err := NewLookupCatalog(options.DataRoot, "")
		if err != nil {
			t.Fatal(err)
		}
		resolver, err := catalog.Open("Nintendo - Nintendo 64")
		if err != nil {
			t.Fatal(err)
		}
		observed, err := ObserveInput(options.ROMRoot, "n64", "translations/Entirely renamed.z64")
		if err != nil {
			t.Fatal(err)
		}
		result, err := resolver.Resolve([]ObservedInput{observed})
		if err != nil {
			t.Fatal(err)
		}
		assertVariant(t, result.Records[0])
		if i == 1 && resolver.IndexStats.Cached != 1 {
			t.Fatalf("variant not cached: %+v", resolver.IndexStats)
		}
	}
	// The modified hash must not become the original's fingerprint.
	if fmt.Sprintf("%x", sha1.Sum(payload)) != plan.Records[0].SHA1 {
		t.Fatal("payload proof changed")
	}
}

func TestVariantMediaOverrideAndInvalidGraph(t *testing.T) {
	options, path, _ := variantFixture(t)
	put(t, filepath.Join(path, "media", "English", "box-front.world.local.png"), []byte("synthetic variant cover"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	assertVariant(t, plan.Records[0])
	if r := plan.Records[0].Media["cover"]; r.FromVariant != "English" || r.FromRelease != "" {
		t.Fatalf("variant cover not preferred: %+v", r)
	}
	raw, _ := os.ReadFile(filepath.Join(path, "game.yaml"))
	put(t, filepath.Join(path, "game.yaml"), []byte(strings.Replace(string(raw), "based_on: English", "based_on: Fixed", 1)))
	if _, err := Build(options); err == nil {
		t.Fatal("cyclic base silently accepted")
	}
}

func TestVariantAndReleaseHashCollisionIsAmbiguous(t *testing.T) {
	options, path, payload := variantFixture(t)
	fp, _ := os.ReadFile(filepath.Join(path, "fingerprints.yaml"))
	fp = append(fp, []byte(fmt.Sprintf("  - release: Original (USA)\n    file:\n      name: Original.z64\n      size: %d\n      sha1: %x\n", len(payload), sha1.Sum(payload)))...)
	put(t, filepath.Join(path, "fingerprints.yaml"), fp)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Status != "ambiguous" {
		t.Fatalf("hash collision chose a target: %+v", plan.Records[0])
	}
}

func TestPackedVariantsEqualLoose(t *testing.T) {
	options, path, _ := variantFixture(t)
	put(t, filepath.Join(path, "media", "English", "box-front.world.local.png"), []byte("variant cover"))
	loose, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	catalogfstest.Pack(t, filepath.Dir(path))
	packed, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loose.Records, packed.Records) {
		t.Fatalf("packed variants changed selection: %+v / %+v", loose.Records, packed.Records)
	}
}

func TestVariantNameDoesNotReplaceWrongPayloadHash(t *testing.T) {
	options, _, _ := variantFixture(t)
	// Even an exact registered variant name must not turn unknown bytes into proof.
	put(t, filepath.Join(options.ROMRoot, "translations", "Fixed.z64"), []byte("different version with unknown bytes"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan.Records {
		if strings.HasSuffix(r.ROM, "Fixed.z64") && (r.Status != "unmatched" || r.Variant != "" || r.Release != "") {
			t.Fatalf("name bypassed changed hash: %+v", r)
		}
	}
}

func TestGameOnlyVariantsExportAndCachedLookup(t *testing.T) {
	for _, tc := range []struct {
		name, target, defaultRelease string
		packed, override             bool
	}{
		{name: "direct", target: "English", defaultRelease: "Original (USA)"},
		{name: "chained packed", target: "Fixed", defaultRelease: "Original (USA)", packed: true, override: true},
		{name: "conventional fallback", target: "Fixed", defaultRelease: "Missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, path, payload := variantFixture(t)
			raw, err := os.ReadFile(filepath.Join(path, "game.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			data := strings.Replace(string(raw), "    based_on: Original (Japan)\n", "", 1)
			data = strings.Replace(data, "default_release: Original (USA)", "default_release: "+tc.defaultRelease, 1)
			data = strings.Replace(data, "Wrong default description", "Game card description", 1)
			data = strings.Replace(data, "Wrong default developer", "Game card developer", 1)
			data = strings.Replace(data, "  Original (Japan):", "    publisher: Game card publisher\n    genres: [Adventure]\n    players: 2\n    release_date: '1991-05-06'\n  Original (Japan):", 1)
			put(t, filepath.Join(path, "game.yaml"), []byte(data))
			fp, _ := os.ReadFile(filepath.Join(path, "fingerprints.yaml"))
			put(t, filepath.Join(path, "fingerprints.yaml"), []byte(strings.Replace(string(fp), "variant: Fixed", "variant: "+tc.target, 1)))
			if tc.override {
				put(t, filepath.Join(path, "media", "English", "box-front.world.local.png"), []byte("synthetic translation art"))
			}
			if tc.packed {
				catalogfstest.Pack(t, filepath.Dir(path))
			}
			before := treeHashes(t, options.ROMRoot)
			check := func(r Record) {
				t.Helper()
				if r.Status != "matched" || r.Variant != tc.target || r.Release != "" || r.MetadataRelease != "Original (USA)" || r.SHA1 != fmt.Sprintf("%x", sha1.Sum(payload)) {
					t.Fatalf("game-only identity: %+v", r)
				}
				if r.Fields["description"].Value != "Game card description" || r.Fields["developer"].Value != "Game card developer" || r.Fields["genres"].Value != "Adventure" || r.Fields["players"].Value != "2" || r.Fields["release_date"].Value != "1991-05-06" {
					t.Fatalf("missing game metadata: %+v", r.Fields)
				}
				if r.Fields["description"].Selection != "game" || r.Fields["description"].FromRelease != "Original (USA)" || r.TitleContext.Regional != "Translated Adventure" || strings.Join(r.TitleContext.Languages, ",") != "en" || len(r.TitleContext.Regions) != 0 {
					t.Fatalf("metadata selection became original identity: %+v", r)
				}
				if r.VariantInfo == nil || tc.target == "English" && r.VariantInfo.BasedOn != "" || tc.target == "Fixed" && (r.VariantInfo.BasedOn != "English" || len(r.VariantInfo.Ancestors) != 1 || r.VariantInfo.Ancestors[0].BasedOn != "") {
					t.Fatalf("wrong ancestry: %+v", r.VariantInfo)
				}
				if r.Media["cover"].SourcePath == "" {
					t.Fatal("missing inherited media")
				}
				if tc.override && r.Media["cover"].FromVariant != "English" {
					t.Fatal("lost parent variant art")
				}
				if !tc.override && (r.Media["cover"].Selection != "game" || r.Media["cover"].FromRelease != "Original (USA)") {
					t.Fatalf("wrong cover provenance: %+v", r.Media)
				}
			}
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			check(plan.Records[0])
			if err = Apply(plan, nil); err != nil {
				t.Fatal(err)
			}
			xml, err := os.ReadFile(filepath.Join(options.Out, "gamelists", "n64", "gamelist.xml"))
			if err != nil || !strings.Contains(string(xml), "Game card description") || !strings.Contains(string(xml), "./translations/Entirely renamed.z64") {
				t.Fatalf("export: %s %v", xml, err)
			}
			for i := 0; i < 2; i++ {
				catalog, err := NewLookupCatalog(options.DataRoot, "")
				if err != nil {
					t.Fatal(err)
				}
				resolver, err := catalog.Open("Nintendo - Nintendo 64")
				if err != nil {
					t.Fatal(err)
				}
				observed, err := ObserveInput(options.ROMRoot, "n64", "translations/Entirely renamed.z64")
				if err != nil {
					t.Fatal(err)
				}
				got, err := resolver.Resolve([]ObservedInput{observed})
				if err != nil {
					t.Fatal(err)
				}
				check(got.Records[0])
				if i == 1 && resolver.IndexStats.Cached != 1 {
					t.Fatalf("cache not reused: %+v", resolver.IndexStats)
				}
			}
			if !reflect.DeepEqual(before, treeHashes(t, options.ROMRoot)) {
				t.Fatal("export changed ROMs")
			}
		})
	}
}
