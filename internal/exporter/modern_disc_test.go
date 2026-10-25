package exporter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModernDiscReadersAndPS3Serial(t *testing.T) {
	for _, system := range []string{"ps3", "xbox", "xbox360"} {
		o, _, _ := fixture(t)
		o.Platform = system
		addPlatform(t, o, system, "Modern (USA)", []byte("synthetic iso"), "Modern (USA).iso")
		put(t, filepath.Join(o.ROMRoot, "Modern (USA).iso"), []byte("synthetic iso"))
		p, e := Build(o)
		if e != nil {
			t.Fatal(e)
		}
		if p.HashMatches != 1 || p.Records[0].Release != "Modern (USA)" {
			t.Fatalf("%s: %+v", system, p)
		}
	}
	o, _, _ := fixture(t)
	o.Platform = "ps3"
	card := addPlatform(t, o, "ps3", "Modern (USA)", []byte("synthetic iso"), "Modern (USA).iso")
	os.Remove(filepath.Join(o.ROMRoot, "Modern (USA).iso"))
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Modern\nreleases:\n  Modern (USA): {regions: [USA]}\n"))
	put(t, filepath.Join(o.ROMRoot, "Modern.ps3"), []byte("NPUB00001\n"))
	p, e := Build(o)
	if e != nil {
		t.Fatal(e)
	}
	if p.GameOnly != 1 || p.HashMatches != 0 || p.Records[0].Release != "" {
		t.Fatal("descriptor treated as disc identity")
	}
	put(t, filepath.Join(o.ROMRoot, "Modern.ps3"), []byte("file:///invalid"))
	p, e = Build(o)
	if e != nil || p.Unsupported != 1 {
		t.Fatal("invalid descriptor accepted")
	}
}
func TestPlaylistCannotMixCatalogGames(t *testing.T) {
	o, _, _ := fixture(t)
	o.Platform = "psx"
	first := addPlatform(t, o, "psx", "First (USA)", []byte("first"), "First (USA).chd")
	if err := os.Rename(first, filepath.Join(filepath.Dir(first), "First")); err != nil {
		t.Fatal(err)
	}
	addPlatform(t, o, "psx", "Second (USA)", []byte("second"), "Second (USA).chd")
	dir := filepath.Join(o.ROMRoot, "Mixed.m3u")
	put(t, filepath.Join(dir, "Mixed.m3u"), []byte("First (USA).chd\nSecond (USA).chd\n"))
	put(t, filepath.Join(dir, "First (USA).chd"), []byte("first"))
	put(t, filepath.Join(dir, "Second (USA).chd"), []byte("second"))
	p, e := Build(o)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range p.Records {
		if r.Representation == "playlist-directory" && r.Status != "unsupported" {
			t.Fatal("mixed games accepted")
		}
	}
}
