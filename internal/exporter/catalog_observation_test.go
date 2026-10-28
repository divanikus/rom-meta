package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"rommeta/internal/catalogfstest"
)

func TestCatalogObservationFolderMembershipAndStatDrift(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Game (USA)", "content")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "game.pkg")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := ObserveCatalogInput(root, "Game (USA)")
	if err != nil || !ValidObservation(before) || !before.Catalog.Directory || before.Outer.Size != 8 || before.Catalog.Files[0].Name != "content/game.pkg" {
		t.Fatal(before, err)
	}
	if err := os.WriteFile(file, []byte("changed size"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := ObserveCatalogInput(root, "Game (USA)")
	if err != nil || reflect.DeepEqual(before, after) {
		t.Fatal("drift lost", err)
	}
	if _, err = ObserveCatalogInput(root, "../Game (USA)"); err == nil {
		t.Fatal("escaped path")
	}
	if err = os.Symlink(file, filepath.Join(folder, "link.pkg")); err == nil {
		if _, err = ObserveCatalogInput(root, "Game (USA)"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}

func TestCatalogObservationAmbiguityCannotChooseACard(t *testing.T) {
	input := ObservedInput{ROM: "Example (USA).rvz", Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Files: []CatalogFile{{Name: "Example (USA).rvz", Size: 1, MtimeNS: 1}}}}
	index := newCatalogIndex()
	index.ByName[nameKey("Example (USA)")] = []fingerprint{{game: &game{path: "first"}, release: "Example (USA)"}, {game: &game{path: "second"}, release: "Example (USA)"}}
	resolver := LookupResolver{index: index}
	record, err := resolver.bindCatalog(input, true)
	if err != nil || record.Status != "ambiguous" || record.Game != "" {
		t.Fatal(record, err)
	}
}

func TestCatalogMAMERequiresExactSetAuthority(t *testing.T) {
	options, _, _ := fixture(t)
	card := arcadeFixture(t, options)
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte("items: []\n"))
	for _, name := range []string{"game1.zip", "Arcade Game.zip"} {
		put(t, filepath.Join(options.ROMRoot, name), []byte("unverified archive"))
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open("Arcade")
	if err != nil {
		t.Fatal(err)
	}
	var observations []ObservedInput
	for _, name := range []string{"game1.zip", "Arcade Game.zip"} {
		obs, e := ObserveCatalogInput(options.ROMRoot, name)
		if e != nil {
			t.Fatal(e)
		}
		observations = append(observations, obs)
	}
	plan, err := resolver.ResolveBindings(observations, true)
	if err != nil {
		t.Fatal(err)
	}
	first := plan.Records[0]
	if first.Status != "matched" || first.Release != "game1" || first.ArcadeSnapshot != "test-snapshot" || first.SHA1 != "" || first.Payload != nil || plan.Records[1].Status == "matched" {
		t.Fatal(plan.Records)
	}
}

func TestCatalogMAMEIgnoresPayloadNamesAndPreservesSetUnderscores(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint("packed=", packed), func(t *testing.T) {
			options, _, _ := fixture(t)
			arcadeFixture(t, options)
			cards := []struct{ title, set, payload string }{
				{"Current Game", "exact2", "shared.bin"},
				{"Different Game", "exact_2", "exact2.bin"},
				{"Another Game", "other", "exact2.bin"},
			}
			for _, card := range cards {
				folder := filepath.Join(options.DataRoot, "platforms", "Arcade", card.title)
				game := []byte(fmt.Sprintf("title: %s\nreleases:\n  %s:\n    authority: {source: mame, entry_name: %s}\n    mame: {snapshot: test-snapshot}\n", card.title, card.set, card.set))
				fingerprints := []byte(fmt.Sprintf("items:\n  - release: %s\n    file: {name: %s}\n", card.set, card.payload))
				put(t, filepath.Join(folder, "game.yaml"), game)
				put(t, filepath.Join(folder, "fingerprints.yaml"), fingerprints)
			}
			if packed {
				catalogfstest.Pack(t, filepath.Join(options.DataRoot, "platforms", "Arcade"))
			}
			catalog, err := NewLookupCatalog(options.DataRoot, "")
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := catalog.Open("Arcade")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"exact2.zip", "exact_2.zip", "exact2"} {
				directory := filepath.Ext(name) == ""
				input := ObservedInput{ROM: name, Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Directory: directory, Files: []CatalogFile{{Name: name, Size: 1, MtimeNS: 1}}}}
				plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
				if err != nil {
					t.Fatal(err)
				}
				record := plan.Records[0]
				want := name
				if !directory {
					want = name[:len(name)-4]
				}
				if record.Status != "matched" || record.Release != want || record.ArcadeSnapshot != "test-snapshot" || record.MatchedBy != "catalog-name" || record.Payload != nil || record.SHA1 != "" {
					t.Fatalf("%s: %+v", name, record)
				}
			}
			input := ObservedInput{ROM: "shared.zip", Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Files: []CatalogFile{{Name: "shared.zip", Size: 1, MtimeNS: 1}}}}
			plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
			if err != nil || plan.Records[0].Status == "matched" || plan.Records[0].Game != "" || len(plan.Records[0].Fields) != 0 {
				t.Fatal(plan, err)
			}
		})
	}
}

func TestCatalogMAMEKeepsDuplicateAuthorityAmbiguous(t *testing.T) {
	options, _, _ := fixture(t)
	card := arcadeFixture(t, options)
	game, err := os.ReadFile(filepath.Join(card, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(filepath.Dir(card), "Duplicate", "game.yaml"), game)
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open("Arcade")
	if err != nil {
		t.Fatal(err)
	}
	input := ObservedInput{ROM: "game1.zip", Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Files: []CatalogFile{{Name: "game1.zip", Size: 1, MtimeNS: 1}}}}
	plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status != "ambiguous" || plan.Records[0].Game != "" {
		t.Fatal(plan, err)
	}
}

func TestCatalogRVZMultidiscNamesPreserveEditionAndGame(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			options, _, _ := fixture(t)
			platform := "Nintendo - GameCube"
			path := familyCard(t, options.DataRoot, platform, "Adventure", "Adventure (USA) (Disc 1)")
			put(t, filepath.Join(path, "game.yaml"), []byte("title: Adventure\ndefault_release: Adventure (USA) (Disc 1)\nreleases:\n  Adventure (USA) (Disc 1): {}\n  Adventure (USA) (Disc 2): {}\n"))
			name := "Adventure (USA)"
			put(t, filepath.Join(options.ROMRoot, name, "Adventure (USA) (Disc 1).rvz"), []byte("disc1"))
			second := "Adventure (USA) (Disc 2).rvz"
			if foreign {
				second = "Adventure (Europe) (Disc 2).rvz"
			}
			put(t, filepath.Join(options.ROMRoot, name, second), []byte("disc2"))
			observed, err := ObserveCatalogInput(options.ROMRoot, name)
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewLookupCatalog(options.DataRoot, "")
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := catalog.Open(platform)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := resolver.ResolveBindings([]ObservedInput{observed}, true)
			if err != nil {
				t.Fatal(err)
			}
			record := plan.Records[0]
			if foreign && record.Status == "matched" || !foreign && (record.Status != "matched" || record.Release != "" || record.MatchedBy != "game-name") {
				t.Fatalf("foreign=%v: %+v", foreign, record)
			}
		})
	}
}

func TestCatalogPlayAndInstallDiscsUseCompleteMemberNames(t *testing.T) {
	options, _, _ := fixture(t)
	platform := "Microsoft - Xbox 360"
	path := familyCard(t, options.DataRoot, platform, "Racing", "Racing (USA) (En,Fr) (Disc 1) (Play Disc) (Rev 1)")
	put(t, filepath.Join(path, "game.yaml"), []byte("title: Racing\ndefault_release: Racing (USA) (En,Fr) (Disc 1) (Play Disc) (Rev 1)\nreleases:\n  Racing (USA) (En,Fr) (Disc 1) (Play Disc) (Rev 1): {}\n  Racing (USA) (En,Fr) (Disc 2) (Content Install Disc) (Rev 1): {}\n"))
	folder := "Racing (USA) [abcdef12]"
	for _, name := range []string{"Racing (USA) (En,Fr) (Disc 1) (Play Disc) (Rev 1).iso", "Racing (USA) (En,Fr) (Disc 2) (Content Install Disc) (Rev 1).iso"} {
		put(t, filepath.Join(options.ROMRoot, folder, name), []byte("synthetic disc"))
	}
	input, err := ObserveCatalogInput(options.ROMRoot, folder)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open(platform)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status != "matched" || plan.Records[0].Release != "" {
		t.Fatalf("%+v %v", plan, err)
	}
	// Directory names do not authorize mixed revisions.
	input.Catalog.Files[1].Name = "Racing (USA) (En,Fr) (Disc 2) (Content Install Disc).iso"
	plan, err = resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal("mixed revision accepted", plan, err)
	}
}
