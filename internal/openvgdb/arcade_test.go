package openvgdb

import (
	"rommeta/internal/catalogcache"
	"testing"
)

func TestMAMERequiresExactArchiveName(t *testing.T) {
	index := platformReleaseIndex{byName: map[string]releaseMetadata{normalizeKey("clone_a"): {ROMFileName: "clone_a.zip", Title: "Clone"}}}
	if m, ok := index.matchMAMESet("clone_a"); !ok || m.Title != "Clone" {
		t.Fatalf("%#v %v", m, ok)
	}
	if _, ok := index.matchMAMESet("clonea"); ok {
		t.Fatal("loose title mistaken for set")
	}
	index.byName[normalizeKey("clone_a")] = releaseMetadata{ROMFileName: "parent.zip", Title: "Parent"}
	if _, ok := index.matchMAMESet("clone_a"); ok {
		t.Fatal("wrong filename accepted")
	}
	game := catalogGameFromCached(catalogcache.Game{Releases: []catalogcache.Release{{Name: "renamed", MAMESet: "clone_a"}}})
	if game.MAMESets["renamed"] != "clone_a" {
		t.Fatal("authority lost")
	}
}
