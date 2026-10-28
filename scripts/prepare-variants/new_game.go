package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

func checkNewGame(o options, e entry) error {
	folder := filepath.Join(o.Catalog, "platforms", e.Platform, e.Game)
	if _, err := catalogfs.Lstat(folder); !os.IsNotExist(err) {
		return fmt.Errorf("new original game already exists or is unavailable: %s/%s", e.Platform, e.Game)
	}
	// Reject case-only collisions on case-sensitive filesystems as well.
	xs, err := catalogfs.ReadDir(filepath.Dir(folder))
	if err != nil {
		return err
	}
	for _, x := range xs {
		if strings.EqualFold(x.Name(), e.Game) {
			return fmt.Errorf("new original game has a case collision: %s", e.Game)
		}
	}
	return nil
}
func checkNewGames(o options, es []entry) error {
	for _, e := range es {
		if e.NewGame != nil {
			if err := checkNewGame(o, e); err != nil {
				return err
			}
		}
	}
	return nil
}
func newGameFiles(key string, e entry) (*catalogyaml.Document, *catalogyaml.Document, map[string]*copyFile, error) {
	source := map[string]any{"name": "reviewed-original-game", "relationship_scope": "game", "edition_precision": "unspecified", "patched_input_asserted": false}
	raw, err := yaml.Marshal(map[string]any{"title": e.NewGame.Title, "default_release": e.Game, "releases": map[string]any{e.Game: map[string]any{"title": e.NewGame.Title, "source": source}}, "provenance": map[string]any{"original_game_review": e.Evidence.Links}})
	if err != nil {
		return nil, nil, nil, err
	}
	g, err := catalogyaml.Parse(raw)
	if err != nil {
		return nil, nil, nil, err
	}
	f, err := catalogyaml.Parse([]byte("items: []\n"))
	if err != nil {
		return nil, nil, nil, err
	}
	files := map[string]*copyFile{}
	for name, doc := range map[string]*catalogyaml.Document{"game.yaml": g, "fingerprints.yaml": f} {
		b, err := doc.Marshal()
		if err != nil {
			return nil, nil, nil, err
		}
		files[name] = &copyFile{Path: filepath.ToSlash(filepath.Join(key, name)), Size: int64(len(b)), SHA256: digest(b), data: b, generated: true}
	}
	return g, f, files, nil
}
