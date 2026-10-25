package catalogstate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/taskprogress"
)

// walkStatusGames reads game documents directly without opening the fetch cache.
// Keep one platform's names and at most four games in flight. Media payloads and
// fingerprints are unused; callbacks/aggregation preserve directory order.
func walkStatusGames(ctx context.Context, root, filter string, progress taskprogress.Func, prepare func(catalogcache.Game) (gameState, error), fn func(gameState) error) (int, int, error) {
	filter = strings.TrimSpace(filter)
	if filter != "" && (filter == "." || filter == ".." || strings.ContainsAny(filter, `/\:`) || strings.HasPrefix(filter, ".") || strings.HasPrefix(filter, "_")) {
		return 0, 0, fmt.Errorf("invalid platform")
	}
	platforms, err := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if err != nil {
		return 0, 0, err
	}
	count, platformCount := 0, 0
	for _, platform := range platforms {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		name := platform.Name()
		if !platform.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || filter != "" && filter != name {
			continue
		}
		progress.Update("scanning catalog", platformCount+1, len(platforms), name)
		entries, err := catalogfs.ReadDir(filepath.Join(root, "platforms", name))
		if err != nil {
			return 0, 0, err
		}
		platformCount++
		const batchSize = 4
		batch := make([]string, 0, batchSize)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			progress.Update("checking media", count+1, 0, name+" / "+batch[0])
			if err := ctx.Err(); err != nil {
				return err
			}
			type prepared struct {
				state gameState
				err   error
			}
			results := make([]prepared, len(batch))
			var group sync.WaitGroup
			for i, folder := range batch {
				group.Add(1)
				go func(i int, folder string) {
					defer group.Done()
					if err := ctx.Err(); err != nil {
						results[i].err = err
						return
					}
					game, _, err := catalogcache.ReadGameDocument(root, name, folder)
					if err == nil {
						results[i].state, err = prepare(game)
					}
					results[i].err = err
				}(i, folder)
			}
			group.Wait()
			for i, result := range results {
				if err := ctx.Err(); err != nil {
					return err
				}
				if result.err != nil {
					return result.err
				}
				count++
				progress.Update("counting games", count, 0, name+" / "+batch[i])
				if err := fn(result.state); err != nil {
					return err
				}
			}
			batch = batch[:0]
			return nil
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return 0, 0, err
			}
			folder := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(folder, ".") || strings.HasPrefix(folder, "_") || catalogrules.IsIgnoredGameFolder(folder) {
				continue
			}
			batch = append(batch, folder)
			if len(batch) == batchSize {
				if err := flush(); err != nil {
					return 0, 0, err
				}
			}
		}
		if err := flush(); err != nil {
			return 0, 0, err
		}
	}
	return platformCount, count, nil
}
