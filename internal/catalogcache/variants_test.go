package catalogcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVariantsSurviveDisposableCache(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "platforms", "Example", "Game")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "game.yaml"), []byte("title: Game\nreleases:\n  Base: {}\nvariants:\n  English:\n    title: English Title\n    kind: translation\n    based_on: Base\n    languages: [en]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "fingerprints.yaml"), []byte("items:\n  - variant: English\n    file:\n      size: 10\n      sha1: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		games, _, err := ReadGames(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(games) != 1 || len(games[0].Variants) != 1 || games[0].Variants[0].BasedOn != "Base" || len(games[0].Releases) != 1 ||
			games[0].Fingerprints[0].Variant != "English" || games[0].Fingerprints[0].Release != "" {
			t.Fatalf("%+v", games)
		}
	}
}
