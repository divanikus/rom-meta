package catalogedit

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/sourceyaml"
	"strings"
	"testing"
)

func TestEditRoundTripRefreshAndStale(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "platforms", "Console", "Game")
			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatal(err)
			}
			original := "title: Game\nreleases:\n  Game (USA): {}\ncustom:\n  owner: keep\nsources:\n  launchbox:\n    status: not_found\n"
			if err := os.WriteFile(filepath.Join(path, "game.yaml"), []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			if packed {
				catalogfstest.Pack(t, filepath.Join(root, "platforms", "Console"))
			}
			card, err := Read(root, "Console", "Game")
			if err != nil {
				t.Fatal(err)
			}
			saved, err := Save(root, "Console", "Game", card.Revision, Patch{"title": "My game", "description": "My description", "descriptions": map[string]any{"ru": "Моё описание"}, "genres": []any{"Puzzle"}, "players": "2"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(saved.YAML, "owner: keep") {
				t.Fatal(saved.YAML)
			}
			if err := sourceyaml.WriteSourceMetadata(filepath.Join(path, "game.yaml"), "launchbox", "    matches:\n      - title: Other title\n        descriptions:\n          en: Provider description\n"); err != nil {
				t.Fatal(err)
			}
			reopened, err := Read(root, "Console", "Game")
			if err != nil {
				t.Fatal(err)
			}
			if reopened.Fields["title"] != "My game" || reopened.Fields["description"] != "My description" {
				t.Fatal(reopened)
			}
			if _, err := Save(root, "Console", "Game", card.Revision, Patch{"title": "Stale"}); err == nil {
				t.Fatal("stale write accepted")
			}
			if _, err := Save(root, "Console", "Game", reopened.Revision, Patch{"sources": map[string]any{}}); err == nil {
				t.Fatal("provider identity edit accepted")
			}
			reset, err := Save(root, "Console", "Game", reopened.Revision, Patch{"description": nil})
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := reset.Fields["description"]; ok {
				t.Fatal("override not removed")
			}
		})
	}
}
func TestWriterBusyAndPathBoundaries(t *testing.T) {
	root := t.TempDir()
	release, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(root); err == nil {
		t.Fatal("concurrent writer accepted")
	}
	release()
	release, err = Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, folder := range []string{"../Game", "Game/Other", "..", " Game", "C:Game"} {
		if _, err := GamePath(root, "Console", folder); err == nil {
			t.Fatal(folder)
		}
	}
}
