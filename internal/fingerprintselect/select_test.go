package fingerprintselect

import (
	"testing"

	"rommeta/internal/catalogcache"
)

func TestUsableForHashLookupSkipsMultiFileDiscTracks(t *testing.T) {
	tests := []catalogcache.Fingerprint{
		{Name: "Golden Axe (Japan).cue", Format: "cue"},
		{Name: "Golden Axe (Japan) (Track 01).bin", Format: "bin"},
		{Name: "Golden Axe (Japan) (Track 2).bin", Format: "bin"},
	}
	for _, fingerprint := range tests {
		if UsableForHashLookup(fingerprint, 3) {
			t.Fatalf("expected multi-file disc fingerprint to be skipped: %#v", fingerprint)
		}
	}
}

func TestUsableForHashLookupKeepsSingleFileROMs(t *testing.T) {
	tests := []catalogcache.Fingerprint{
		{Name: "Doom (USA).32x", Format: "32x"},
		{Name: "Star Fox 64 (USA).z64", Format: "z64"},
		{Name: "Bonk's Adventure (USA).pce", Format: "pce"},
	}
	for _, fingerprint := range tests {
		if !UsableForHashLookup(fingerprint, 1) {
			t.Fatalf("expected single-file ROM fingerprint to be usable: %#v", fingerprint)
		}
	}
}

func TestUsableForHashLookupKeepsSingleFileDiscImages(t *testing.T) {
	fingerprint := catalogcache.Fingerprint{Name: "Game (USA).iso", Format: "iso"}
	if !UsableForHashLookup(fingerprint, 1) {
		t.Fatalf("expected single-file disc image to be usable")
	}
}

func TestPriorityPrefersDefaultReleaseAndCartridgeFormats(t *testing.T) {
	first := Priority(catalogcache.Fingerprint{
		Release: "Doom (USA)",
		Name:    "Doom (USA).32x",
		Format:  "32x",
	}, "Doom (USA)", 1)
	second := Priority(catalogcache.Fingerprint{
		Release: "Doom (Europe)",
		Name:    "Doom (Europe).32x",
		Format:  "32x",
	}, "Doom (USA)", 1)
	if first <= second {
		t.Fatalf("default release should win: first=%d second=%d", first, second)
	}
}

func TestOfficialFingerprintOutranksUnofficialDefault(t *testing.T) {
	original := catalogcache.Fingerprint{Release: "Game (Japan)", Name: "Game.nes", Format: "nes"}
	clone := catalogcache.Fingerprint{Release: "Game (USA) (Aftermarket) (Pirate)", Name: "Clone.nes", Format: "nes"}
	if Priority(original, clone.Release, 1) <= Priority(clone, clone.Release, 1) {
		t.Fatal("unofficial default exhausted the hash-query budget")
	}
	another := clone
	another.Release = "Game (Asia) (Pirate)"
	if Priority(clone, clone.Release, 1) <= Priority(another, clone.Release, 1) {
		t.Fatal("all-unofficial relative preference changed")
	}
}
