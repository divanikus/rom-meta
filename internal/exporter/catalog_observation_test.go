package exporter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
