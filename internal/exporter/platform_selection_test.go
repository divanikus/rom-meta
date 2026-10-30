package exporter

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/platformnames"
)

func TestPlatformListEveryAdvertisedSelectorResolves(t *testing.T) {
	p := NewPlatformSelector(platformnames.DefaultRegistry(), nil)
	for _, row := range p.Options() {
		for _, value := range append([]string{row.Code, row.Name}, row.Aliases...) {
			got, err := p.Resolve(value)
			if err != nil || got.Name != row.Name {
				t.Fatalf("%q for %s: %+v %v", value, row.Name, got, err)
			}
		}
	}
	for _, id := range SupportedSystems() {
		got, err := p.Resolve(id)
		if err != nil || got.System != id || len(got.ExportTargets) != 2 {
			t.Fatalf("reader %s not selectable: %+v %v", id, got, err)
		}
	}
	got, err := p.Resolve("IBM - PC and Compatibles")
	if err != nil || len(got.ExportTargets) != 0 {
		t.Fatalf("metadata lookup advertised as export: %+v %v", got, err)
	}
}

func TestPlatformFolderNamesAndAliasesSingleBatchParity(t *testing.T) {
	options, _, payload := fixture(t)
	long := "Nintendo - Nintendo 64"
	folder := filepath.Join(options.ROMRoot, long)
	put(t, filepath.Join(folder, "retail", "renamed.z64"), payload)
	for _, target := range []string{"es-de", "launchbox"} {
		for _, selector := range []string{"n64", long, "Nintendo 64", "NINTENDO_nintendo-64"} {
			one := options
			one.Target = target
			one.Platform = selector
			one.ROMRoot = folder
			single, err := Build(one)
			if err != nil {
				t.Fatal(selector, err)
			}
			one.Platform = ""
			one.ROMRoot = options.ROMRoot
			batch, err := BuildBatch(BatchOptions{Options: one, Systems: []string{selector}})
			if err != nil {
				t.Fatal(selector, err)
			}
			if len(batch.Plans) != 1 || len(batch.Skipped) != 0 || !reflect.DeepEqual(single.Records, batch.Plans[0].Records) || single.System != batch.Plans[0].System {
				t.Fatalf("different selector behavior: %s %+v", selector, batch)
			}
			if single.PlatformSelection == nil || !strings.Contains(single.PlatformSelection.MatchedBy, "canonical") {
				t.Fatal("missing folder explanation")
			}
		}
	}
	options.Platform = ""
	batch, err := BuildBatch(BatchOptions{Options: options, All: true})
	if err != nil || len(batch.Plans) != 1 {
		t.Fatalf("long folder undiscovered: %+v %v", batch, err)
	}
	p, err := LoadPlatformSelector(options.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	selection, _, err := p.Single("n64", filepath.Join(folder, "retail"))
	if err != nil || !strings.Contains(selection.MatchedBy, "ancestor") {
		t.Fatalf("nested folder: %+v %v", selection, err)
	}
}

func TestPlatformFolderDiscoveryRejectsGuessingAndCollisions(t *testing.T) {
	names := platformnames.DefaultRegistry()
	names.AddAliases("Nintendo - Game Boy", "shared")
	names.AddAliases("Nintendo - Game Boy Advance", "shared")
	p := NewPlatformSelector(names, nil)
	root := t.TempDir()
	put(t, filepath.Join(root, "Mystery", "game.gba"), []byte("extensions do not identify hardware"))
	put(t, filepath.Join(root, "Nintendo - Game Boy", "bad.zip"), []byte("never opened by discovery"))
	put(t, filepath.Join(root, "Nintendo - Game Boy Advance", "bad.zip"), []byte("never opened by discovery"))
	put(t, filepath.Join(root, "shared", "x.bin"), []byte("ambiguous alias"))
	put(t, filepath.Join(root, "Atari - Atari 2600", "x.a26"), []byte("metadata only"))
	selected, skipped, err := p.Folders(root, nil, true)
	if err != nil || len(selected) != 2 || len(skipped) != 3 {
		t.Fatalf("discovery: %+v %+v %v", selected, skipped, err)
	}
	for _, value := range []string{"Mystery", "shared", "../gb"} {
		if _, err := p.Resolve(value); err == nil {
			t.Fatal("guessed platform", value)
		}
	}
	if _, _, err := p.Single("gb", filepath.Join(root, "Nintendo - Game Boy Advance")); err == nil {
		t.Fatal("known conflicting folder accepted")
	}
	if _, _, err := p.Single("gb", root); err == nil {
		t.Fatal("collection scanned as one platform")
	}
	if _, _, err := p.Single("gb", filepath.Join(root, "Mystery")); err != nil {
		t.Fatal("explicit generic directory rejected", err)
	}
	explicit := t.TempDir()
	put(t, filepath.Join(explicit, "shared", "invalid.zip"), []byte("do not inspect"))
	if found, _, err := p.Folders(explicit, []string{"gb"}, false); err != nil || len(found) != 1 {
		t.Fatalf("explicit code cannot disambiguate folder: %+v %v", found, err)
	}

	put(t, filepath.Join(root, "gb", "bad.zip"), []byte("duplicate folder"))
	if _, _, err := p.Folders(root, []string{"gb"}, false); err == nil {
		t.Fatal("picked first duplicate directory")
	}
	selected, skipped, err = p.Folders(root, nil, true)
	if err != nil || len(selected) != 1 || len(skipped) != 5 {
		t.Fatalf("duplicate all: %+v %+v %v", selected, skipped, err)
	}
	if _, _, err := p.Folders(root, []string{"gb", "Nintendo Game Boy"}, false); err == nil {
		t.Fatal("duplicate selected alias accepted")
	}
}

func TestPlatformSelectorCatalogAmbiguityAndCustomAliases(t *testing.T) {
	options, _, _ := fixture(t)
	put(t, filepath.Join(options.DataRoot, "config", "platforms.yaml"), []byte("Nintendo - Nintendo 64:\n  aliases: [My Console]\n"))
	put(t, filepath.Join(options.DataRoot, "config", "sources.yaml"), []byte("platform_aliases: {old-console: 'Nintendo - Nintendo 64'}\n"))
	p, err := LoadPlatformSelector(options.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"My Console", "old-console"} {
		if got, err := p.Resolve(value); err != nil || got.Code != "n64" {
			t.Fatalf("local alias %s: %+v %v", value, got, err)
		}
	}
	put(t, filepath.Join(options.DataRoot, "platforms", "second", "_platform.yaml"), []byte("name: Nintendo - Nintendo 64\n"))
	p, err = LoadPlatformSelector(options.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	choice, _ := p.Resolve("n64")
	if _, err = p.CatalogFor(choice, "n64", "n64"); err == nil {
		t.Fatal("duplicate catalogs silently merged")
	}
	if got, err := p.CatalogFor(choice, "second", "n64"); err != nil || got != "second" {
		t.Fatal("explicit catalog cannot disambiguate", got, err)
	}
}

func TestPlatformDiscoveryDirectoryLinkCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "gb")); err != nil {
		t.Skip("symlink unavailable", err)
	}
	p := NewPlatformSelector(platformnames.DefaultRegistry(), nil)
	selected, _, err := p.Folders(root, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Fatal("escaped directory selected")
	}
}
