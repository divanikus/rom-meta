package catalog

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVariantReferencesAndMedia(t *testing.T) {
	root := t.TempDir()
	platform := filepath.Join(root, "platforms", "Example")
	path := filepath.Join(platform, "Game")
	writeFile(t, filepath.Join(platform, "_catalog.yaml"), "schema_version: 0\n")
	writeFile(t, filepath.Join(platform, "_platform.yaml"), "name: Example\n")
	valid := "title: Game\nreleases:\n  Base: {}\nvariants:\n  English:\n    kind: translation\n    based_on: Base\n    languages: [en]\n"
	writeFile(t, filepath.Join(path, "game.yaml"), valid)
	good := "items:\n  - variant: English\n    file:\n      name: English.nes\n      size: 100\n      sha1: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
	writeFile(t, filepath.Join(path, "fingerprints.yaml"), good)
	writeFile(t, filepath.Join(path, "media", "English", "box-front.png"), "synthetic")
	issues, err := Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("%v %v", issues, err)
	}
	for _, bad := range []string{
		strings.Replace(good, "variant: English", "variant: Missing", 1),
		strings.Replace(good, "variant: English", "variant: English\n    release: Base", 1),
		good + "  - release: Base\n    file:\n      size: 100\n      sha1: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
	} {
		writeFile(t, filepath.Join(path, "fingerprints.yaml"), bad)
		issues, err := Validate(root)
		if err != nil || len(issues) == 0 {
			t.Fatalf("accepted invalid derivative %v %v", issues, err)
		}
	}
}
