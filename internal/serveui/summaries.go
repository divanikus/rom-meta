package serveui

import (
	"context"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
)

// Only small list projections are cached. Every request checks game.yaml's stamp
// so a running fetch or manual edit is visible without restarting serve. The
// browser never opens the fetcher's writable SQLite cache.
type summaryEntry struct {
	size, mtime int64
	item        gameListItem
}

func safeCatalogName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`) && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

func (s *server) gameSummaries(ctx context.Context, platform string, folders []string) ([]gameListItem, error) {
	items := make([]gameListItem, 0, len(folders))
	for _, folder := range folders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, err := s.gameSummary(platform, folder)
		if err != nil {
			// One malformed or concurrently removed card must not hide the others.
			item = gameListItem{Folder: folder, SummaryError: err.Error()}
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *server) gameSummary(platform, folder string) (gameListItem, error) {
	path := filepath.Join(s.root, "platforms", platform, folder, "game.yaml")
	info, err := catalogfs.Stat(path)
	if err != nil {
		return gameListItem{}, err
	}
	s.summaryMu.Lock()
	entry, ok := s.summaries[path]
	s.summaryMu.Unlock()
	if ok && entry.size == info.Size() && entry.mtime == catalogfs.CacheStamp(info) {
		return entry.item, nil
	}
	item, err := readGameListItem(s.root, platform, folder)
	if err != nil {
		return gameListItem{}, err
	}
	s.summaryMu.Lock()
	// Bound memory when browsing many platforms; eviction never affects truth.
	if s.summaries == nil || len(s.summaries) >= 8192 {
		s.summaries = make(map[string]summaryEntry)
	}
	s.summaries[path] = summaryEntry{size: info.Size(), mtime: catalogfs.CacheStamp(info), item: item}
	s.summaryMu.Unlock()
	return item, nil
}

func (s *server) listGames(ctx context.Context, platform string) ([]gameListItem, error) {
	names, err := listGameNames(s.root, platform)
	if err != nil {
		return nil, err
	}
	folders := make([]string, len(names))
	for i, item := range names {
		folders[i] = item.Folder
	}
	items, err := s.gameSummaries(ctx, platform, folders)
	if err != nil {
		return nil, err
	}
	sortGameItems(items)
	return items, nil
}
