package serveui

import (
	"path/filepath"
	"rommeta/internal/catalogyaml"
	"testing"
)

func TestBrowserHidesRetiredGamelistMedia(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Example", "Game")
	current := "media/box-front.current.gamelist.sample.png"
	doc, err := catalogyaml.Parse([]byte("sources:\n  gamelist.sample:\n    matches:\n      - media: {box_front: [{url: " + current + "}]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{current, "media/box-front.retired.gamelist.sample.png", "media/box-front.launchbox.png"} {
		writeTestFile(t, filepath.Join(game, filepath.FromSlash(name)), "synthetic")
	}
	files, err := localMediaFiles(root, game, doc)
	if err != nil || len(files) != 2 {
		t.Fatal(files, err)
	}
}
