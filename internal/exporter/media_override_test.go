package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewedMediaOverrideRetainsSourceAndExactScope(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	put(t, filepath.Join(card, "media", "Example (Europe)", "box-front.japan.igdb.jpg"), []byte("reviewed image"))
	profile(t, &options, "schema_version: 1\ntarget: es-de\ntitle_mode: catalog\nmedia_overrides:\n  - game: platforms/Nintendo - Nintendo 64/Example\n    release: Example (USA)\n    role: cover\n    path: media/Example (Europe)/box-front.japan.igdb.jpg\n")
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	media := plan.Records[0].Media["cover"]
	if media.Selection != "override" || media.Source != "igdb" || media.Region != "japan" || !strings.Contains(media.SourcePath, "Example (Europe)") {
		t.Fatalf("lost reviewed provenance: %+v", media)
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(options.Out, filepath.FromSlash(media.OutputPath))); err != nil {
		t.Fatal(err)
	}
	p, err := loadProfile(options.DataRoot, options.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	g := &game{path: card}
	if pin, err := pinnedMedia(options.DataRoot, g, "Example (Europe)", "example.z64", p, true, "cover", "n64"); err != nil || pin != nil {
		t.Fatal("override crossed release", pin, err)
	}
	if pin, err := pinnedMedia(options.DataRoot, g, "Example (USA)", "example.z64", p, false, "cover", "n64"); err != nil || pin != nil {
		t.Fatal("game-only binding used release pin", pin, err)
	}
}

func TestMediaOverrideRejectsUnsafeDuplicateAndMissingFiles(t *testing.T) {
	options, _, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example.z64"), payload)
	base := MediaOverride{Game: "platforms/Nintendo - Nintendo 64/Example", Release: "Example (USA)", Role: "cover", Path: "media/box-front.usa.launchbox.png"}
	p := Profile{Selection: Selection{Media: map[string]Rule{"cover": {}}}}
	for _, path := range []string{"../outside.png", "media/../../outside.png", "/absolute.png", "media\\file.png", "media/C:file.png"} {
		pin := base
		pin.Path = path
		p.MediaOverrides = []MediaOverride{pin}
		if validateMediaOverrides(p) == nil {
			t.Fatal("unsafe pin", path)
		}
	}
	p.MediaOverrides = []MediaOverride{base, base}
	if validateMediaOverrides(p) == nil {
		t.Fatal("duplicate pin")
	}
	profile(t, &options, "schema_version: 1\ntarget: es-de\ntitle_mode: catalog\nmedia_overrides:\n  - game: platforms/Nintendo - Nintendo 64/Example\n    release: Example (USA)\n    role: cover\n    path: media/missing.png\n")
	if _, err := Build(options); err == nil {
		t.Fatal("missing pinned file silently fell back")
	}
}
