package sourceassets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/taskprogress"
)

// MediaNamesOptions controls an offline upgrade of previously materialized names.
type MediaNamesOptions struct {
	DataRoot, Platform, Source, Match string
	Apply                             bool
	Progress                          taskprogress.Func
}

type MediaNameChange struct{ From, To string }
type MediaNamesResult struct {
	Planned, Renamed, Existing, Conflicts int
	Changes                               []MediaNameChange
}

// NameMedia replays the saved downloader naming convention, adding explicit
// source categories. It never downloads, inspects image pixels or overwrites a
// destination. The saved candidate order must still describe the legacy files.
func NameMedia(options MediaNamesOptions) (MediaNamesResult, error) {
	var result MediaNamesResult
	root, err := filepath.Abs(options.DataRoot)
	if err != nil {
		return result, err
	}
	if options.Source == "" {
		return result, fmt.Errorf("source is required")
	}
	_, _, err = streamCatalogGames(root, options.Platform, "planning media names", options.Progress, func(game catalogGame) error {
		if options.Match != "" && !strings.Contains(strings.ToLower(game.Folder), strings.ToLower(options.Match)) {
			return nil
		}
		candidates := gameAssetCandidates(game, options.Source)
		sortAssetCandidates(candidates)
		for _, candidate := range candidates {
			if candidate.LegacyTarget == candidate.Target {
				continue
			}
			if err := checkMediaNameParent(root, candidate.LegacyTarget); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			legacy, err := catalogfs.Lstat(candidate.LegacyTarget)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			_, targetErr := catalogfs.Lstat(candidate.Target)
			if targetErr != nil && !os.IsNotExist(targetErr) {
				return targetErr
			}
			if targetErr == nil {
				if legacy != nil {
					result.Conflicts++
				} else {
					result.Existing++
				}
				continue
			}
			if legacy == nil {
				continue
			}
			if !legacy.Mode().IsRegular() || legacy.Size() == 0 {
				result.Conflicts++
				continue
			}
			result.Changes = append(result.Changes, MediaNameChange{From: candidate.LegacyTarget, To: candidate.Target})
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Planned = len(result.Changes)
	if !options.Apply {
		return result, nil
	}
	for _, change := range result.Changes {
		if err := checkMediaNameParent(root, change.From); err != nil {
			return result, err
		}
		info, err := catalogfs.Lstat(change.From)
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return result, fmt.Errorf("legacy media changed: %s", change.From)
		}
		if _, err := catalogfs.Lstat(change.To); !os.IsNotExist(err) {
			return result, fmt.Errorf("media destination is no longer absent: %s", change.To)
		}
		if err := catalogfs.Rename(change.From, change.To); err != nil {
			return result, err
		}
		result.Renamed++
	}
	return result, nil
}

func checkMediaNameParent(root, path string) error {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("media path escapes data root: %s", path)
	}
	parent := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		parent = filepath.Join(parent, part)
		info, err := catalogfs.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("media parent is not an ordinary directory: %s", parent)
		}
	}
	return nil
}

// Existing legacy names are still downloaded assets, not missing downloads.
// Use catalog media-names to label them without contacting a provider.
func existingCandidatePath(candidate assetCandidate) string {
	if candidate.LegacyTarget == "" || candidate.LegacyTarget == candidate.Target {
		return candidate.Target
	}
	if _, err := catalogfs.Lstat(candidate.Target); !os.IsNotExist(err) {
		return candidate.Target
	}
	if info, err := catalogfs.Lstat(candidate.LegacyTarget); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return candidate.LegacyTarget
	}
	return candidate.Target
}
