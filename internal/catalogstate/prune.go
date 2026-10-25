package catalogstate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogyaml"
)

// An unmatched catalog entry is not permission to remove unrelated user files.
// Empty directories and the two generated bundle files are the only allowed data.
func hasExtraGameFiles(gamePath string) (bool, error) {
	found := errors.New("extra game file")
	err := catalogfs.WalkDir(gamePath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(gamePath, path)
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() || (relative != "game.yaml" && relative != "fingerprints.yaml") {
			return found
		}
		return nil
	})
	if errors.Is(err, found) {
		return true, nil
	}
	return false, err
}

func canPruneGame(root string, game GameRef) (bool, error) {
	extra, err := hasExtraGameFiles(game.Path)
	if err != nil || extra {
		return false, err
	}
	ignored := catalogrules.IsIgnoredGameFolder(game.Game)
	curated, err := hasCuratedFields(game.Path, ignored)
	if err != nil || curated {
		return false, err
	}
	if !ignored {
		current, err := catalogcache.ReadGameBundle(root, game.Platform, game.Game)
		if err != nil {
			return false, err
		}
		return isEmptyPlaceholder(current, 0), nil
	}
	return true, nil
}

func hasCuratedFields(path string, ignored bool) (bool, error) {
	contents, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
	if os.IsNotExist(err) && ignored {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	mapping := document.Root()
	for index := 0; index < len(mapping.Content); index += 2 {
		switch mapping.Content[index].Value {
		case "title", "default_release", "releases":
		case "sources":
			if ignored {
				return true, nil
			}
		default:
			return true, nil
		}
	}
	return false, nil
}
