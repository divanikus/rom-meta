package exporter

import (
	"path/filepath"
	"testing"
)

func TestCompleteNameSeparatesRomanCompilationFromSequel(t *testing.T) {
	root := t.TempDir()
	platform := "NEC - PC Engine CD & TurboGrafx CD"
	put(t, filepath.Join(root, "platforms", platform, "_platform.yaml"), []byte("name: "+platform+"\n"))
	names := []string{"Ys I & II (Japan)", "Ys III (Japan)"}
	for _, name := range names {
		put(t, filepath.Join(root, "platforms", platform, name, "game.yaml"), []byte("title: "+name+"\nreleases:\n  "+name+": {title: "+name+"}\n"))
	}
	for pass := 0; pass < 2; pass++ {
		catalog, err := NewLookupCatalog(root, "")
		if err != nil {
			t.Fatal(err)
		}
		resolver, err := catalog.Open(platform)
		if err != nil {
			t.Fatal(err)
		}
		if pass == 1 && resolver.IndexStats.Cached != 2 {
			t.Fatalf("complete names not cached: %#v", resolver.IndexStats)
		}
		for _, name := range names {
			disc := Record{ROM: name + ".chd", Status: "unmatched"}
			if err := resolver.bindDiscName(&disc); err != nil || disc.Status != "matched" || disc.MetadataRelease != name {
				t.Fatalf("CHD %s: %#v %v", name, disc, err)
			}
			input := ObservedInput{ROM: name + ".rvz", Outer: FileDigest{Size: 1}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Files: []CatalogFile{{Name: name + ".rvz", Size: 1, MtimeNS: 1}}}}
			record, err := resolver.bindCatalog(input, true)
			if err != nil || record.Status != "matched" || record.Release != name {
				t.Fatalf("catalog %s: %#v %v", name, record, err)
			}
			local := Record{ROM: name + ".iso", Status: "unmatched"}
			if err := nameFallback(root, resolver.index, &local, catalog.Profile, "pcenginecd"); err != nil || local.Status != "matched" || local.Release != name {
				t.Fatalf("local %s: %#v %v", name, local, err)
			}
		}
		ambiguous := Record{ROM: "YsIII(Japan).chd", Status: "unmatched"}
		if err := resolver.bindDiscName(&ambiguous); err != nil || ambiguous.Status != "ambiguous" {
			t.Fatalf("compact collision guessed: %#v %v", ambiguous, err)
		}
	}
}
