package exporter

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogyaml"
	"strings"
	"testing"
)

func TestReviewedMediaOverrideRetainsSourceAndExactScope(t *testing.T) {
	options, card, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	put(t, filepath.Join(card, "media", "Example (Europe)", "box-front.japan.igdb.jpg"), []byte("reviewed image"))
	profile(t, &options, "schema_version: 1\ntarget: es-de\ntitles: {mode: regional, labels: keep, variants: auto}\nmedia_overrides:\n  - game: platforms/Nintendo - Nintendo 64/Example\n    release: Example (USA)\n    role: cover\n    path: media/Example (Europe)/box-front.japan.igdb.jpg\n")
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
	profile(t, &options, "schema_version: 1\ntarget: es-de\ntitles: {mode: regional, labels: keep, variants: auto}\nmedia_overrides:\n  - game: platforms/Nintendo - Nintendo 64/Example\n    release: Example (USA)\n    role: cover\n    path: media/missing.png\n")
	if _, err := Build(options); err == nil {
		t.Fatal("missing pinned file silently fell back")
	}
}

func TestMediaOwnershipKeepsEditionsSeparateForUnverifiedNames(t *testing.T) {
	root := t.TempDir()
	card := filepath.Join(root, "platforms", "Test", "Example")
	doc, err := catalogyaml.Parse([]byte("title: Example\nreleases:\n  Example (USA): {title: Example, regions: [USA]}\n  Example+ (USA): {title: Example+, regions: [USA]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	g := &game{path: card, name: "Example", root: doc.Root()}
	p, err := loadProfileReader("", strings.NewReader("schema_version: 1\ntarget: es-de\nselection:\n  media:\n    cover: {scope: game}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Example (USA)", "Example+ (USA)"} {
		put(t, filepath.Join(card, "media", name, "box-front.usa.launchbox.jpg"), []byte(name))
	}
	for _, verified := range []bool{false, true} {
		for _, release := range []string{"Example (USA)", "Example+ (USA)"} {
			media, _, err := resolveMedia(root, g, release, "example.zip", p, verified, "test")
			if err != nil || !strings.Contains(media["cover"].SourcePath, "/"+release+"/") {
				t.Fatalf("%s verified=%v: %+v %v", release, verified, media, err)
			}
		}
	}
	if err := os.Remove(filepath.Join(card, "media", "Example (USA)", "box-front.usa.launchbox.jpg")); err != nil {
		t.Fatal(err)
	}
	media, _, err := resolveMedia(root, g, "Example (USA)", "example.zip", p, false, "test")
	if err != nil || media["cover"].SourcePath != "" {
		t.Fatalf("borrowed Plus cover: %+v %v", media, err)
	}
	rule := p.Selection.Media["cover"]
	rule.Scope = "release"
	p.Selection.Media["cover"] = rule
	media, _, err = resolveMedia(root, g, "Example+ (USA)", "example.zip", p, false, "test")
	if err != nil || !strings.Contains(media["cover"].SourcePath, "/Example+ (USA)/") {
		t.Fatalf("name-only metadata lost its own media: %+v %v", media, err)
	}
}
