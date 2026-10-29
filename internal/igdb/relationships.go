package igdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/store"
)

func relationshipsFromGame(game apiGame) []sourceyaml.Relationship {
	var out []sourceyaml.Relationship
	if game.VersionParent > 0 && game.VersionParent != game.ID {
		out = append(out, sourceyaml.Relationship{Type: "edition_of", TargetID: strconv.Itoa(game.VersionParent), SourceField: "version_parent"})
	}
	if game.ParentGame > 0 && game.ParentGame != game.ID && game.ParentGame != game.VersionParent {
		kind := map[string]string{"Expanded Game": "expanded_edition_of", "Port": "port_of", "Remake": "remake_of", "Remaster": "remaster_of", "DLC / Addon": "dlc_of", "Expansion": "expansion_of", "Standalone Expansion": "standalone_expansion_of"}[game.GameType.Type]
		if kind == "" {
			kind = "related_to"
		}
		out = append(out, sourceyaml.Relationship{Type: kind, TargetID: strconv.Itoa(game.ParentGame), SourceField: "parent_game"})
	}
	return out
}

func relationMatches(doc *catalogyaml.Document) []*yaml.Node {
	node := catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(doc.Root(), "sources"), "igdb"), "matches")
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	return node.Content
}
func relationID(match *yaml.Node) int {
	id, _ := strconv.Atoi(catalogyaml.Scalar(catalogyaml.Value(match, "igdb_id")))
	return id
}
func needsRelationships(match *yaml.Node, force bool) bool {
	return relationID(match) > 0 && (force || catalogyaml.Value(match, "relationships") == nil)
}

// refreshRelationships keeps every existing match and all curated metadata.
// A saved relationships list (including []) is the per-match resume marker.
func refreshRelationships(options ImportOptions) (ImportResult, error) {
	root := strings.TrimSpace(options.DataRoot)
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return ImportResult{}, err
	}
	cfg, err := store.LoadSources(root)
	if err != nil {
		return ImportResult{}, err
	}
	settings, err := igdbSettings(cfg)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{DataRoot: root, APIBaseURL: settings.APIBaseURL, Apply: options.Apply, Limit: options.Limit, RateLimitPerSecond: settings.RateLimitPerSecond}
	var paths []string
	ids := map[int]bool{}
	files, platformCount, err := relationshipCatalogFiles(root, options.Platform)
	if err != nil {
		return result, err
	}
	scanned, err := visitRelationshipFiles(files, func(done int, path string) {
		reportStage(options.Progress, options.Apply, "reading relationships", done, len(files), filepath.Base(filepath.Dir(path)))
	}, func(path string) (relationshipFile, error) {
		data, err := catalogfs.ReadFile(path)
		if err != nil {
			return relationshipFile{}, err
		}
		doc, err := catalogyaml.Parse(data)
		if err != nil {
			return relationshipFile{}, err
		}
		item := relationshipFile{Path: path}
		item.Preserved, err = catalogyaml.SourcePreserved(doc.Root(), "igdb")
		if err != nil {
			return item, err
		}
		if item.Preserved {
			return item, nil
		}
		for _, match := range relationMatches(doc) {
			if needsRelationships(match, options.ForceOverwrite) {
				item.IDs = append(item.IDs, relationID(match))
			}
		}
		return item, nil
	})
	if err != nil {
		return result, err
	}
	for _, file := range scanned {
		if file.Preserved {
			result.SkippedPreservedCount++
			continue
		}
		if len(file.IDs) == 0 {
			result.SkippedExistingCount++
			continue
		}
		if options.Limit > 0 && len(paths) >= options.Limit {
			result.SkippedByLimitCount++
			continue
		}
		paths = append(paths, file.Path)
		for _, id := range file.IDs {
			ids[id] = true
		}
	}
	result.PlatformCount = platformCount
	result.GameBundleCount = len(files)
	result.SelectedGameCount = len(paths)
	result.PlannedSearchCount = (len(ids) + 499) / 500
	if err != nil || !options.Apply || len(ids) == 0 {
		return result, err
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	token, err := ensureAccessToken(client, root, settings)
	if err != nil {
		return result, err
	}
	result.TokenChecked = true
	result.UsedCachedToken = token.FromCache
	result.TokenCachePath = token.CachePath
	cache, err := openQueryCache(root, options.ForceOverwrite)
	if err != nil {
		return result, err
	}
	defer cache.close()
	ordered := make([]int, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Ints(ordered)
	records := map[int]apiGame{}
	limiter := newRateLimiter(settings.RateLimitPerSecond)
	for start := 0; start < len(ordered); start += 500 {
		end := start + 500
		if end > len(ordered) {
			end = len(ordered)
		}
		reportStage(options.Progress, true, "querying relationships", start, len(ordered), "IGDB")
		batch, cached, err := queryRelationships(client, cache, limiter, settings, token, ordered[start:end])
		if err != nil {
			return result, err
		}
		if cached {
			result.CachedSearchCount++
		} else {
			result.QueriedSearchCount++
		}
		for _, game := range batch {
			if ids[game.ID] {
				records[game.ID] = game
			}
		}
	}
	writes, writeErr := visitRelationshipFiles(paths, func(done int, path string) {
		reportStage(options.Progress, true, "writing relationships", done, len(paths), filepath.Base(filepath.Dir(path)))
	}, func(path string) (relationshipWrite, error) {
		data, err := catalogfs.ReadFile(path)
		if err != nil {
			return relationshipWrite{}, err
		}
		updated, changed, missing, err := updateRelationships(data, records, options.ForceOverwrite)
		item := relationshipWrite{Path: path, Missing: missing}
		if err != nil {
			return item, err
		}
		if changed {
			if err = atomicfile.WriteFile(path, updated, 0644); err != nil {
				return item, err
			}
			item.Changed = true
		}
		return item, nil
	})
	for _, item := range writes {
		if item.Missing > 0 {
			result.FailedSearchCount++
			if len(result.Failures) < 10 {
				result.Failures = append(result.Failures, ImportFailure{Game: filepath.Base(filepath.Dir(item.Path)), Error: "IGDB omitted an existing ID; its match was preserved"})
			}
		}
		if item.Changed {
			result.WrittenFileCount++
			result.MatchedGameCount++
			result.GameFileCount++
		}
	}
	if writeErr != nil {
		return result, writeErr
	}
	return result, nil
}
func updateRelationships(data []byte, records map[int]apiGame, force bool) ([]byte, bool, int, error) {
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, false, 0, err
	}
	preserved, err := catalogyaml.SourcePreserved(doc.Root(), "igdb")
	if err != nil {
		return nil, false, 0, err
	}
	if preserved {
		return data, false, 0, nil
	}
	changed := false
	missing := 0
	for _, match := range relationMatches(doc) {
		if !needsRelationships(match, force) {
			continue
		}
		record, ok := records[relationID(match)]
		if !ok {
			missing++
			continue
		}
		relationships := relationshipsFromGame(record)
		if relationships == nil {
			relationships = []sourceyaml.Relationship{}
		}
		node := &yaml.Node{}
		if err = node.Encode(relationships); err != nil {
			return nil, false, missing, err
		}
		if err = catalogyaml.Set(match, "relationships", node); err != nil {
			return nil, false, missing, err
		}
		if record.GameType.Type != "" {
			if err = catalogyaml.Set(match, "game_type", &yaml.Node{Kind: yaml.ScalarNode, Value: record.GameType.Type}); err != nil {
				return nil, false, missing, err
			}
		}
		changed = true
	}
	if !changed {
		return data, false, missing, nil
	}
	data, err = doc.Marshal()
	return data, true, missing, err
}
func queryRelationships(client *http.Client, cache *queryCache, limiter *rateLimiter, settings sourceSettings, token accessToken, ids []int) ([]apiGame, bool, error) {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.Itoa(id)
	}
	query := strings.Join(values, ",")
	key := cacheKey("relationships", settings.APIBaseURL, query)
	if cache != nil && !cache.bypass {
		var raw string
		err := cache.db.QueryRow("SELECT response_json FROM igdb_query_cache WHERE cache_key = ?", key).Scan(&raw)
		if err == nil {
			var games []apiGame
			if json.Unmarshal([]byte(raw), &games) == nil {
				return games, true, nil
			}
		} else if err != sql.ErrNoRows {
			return nil, false, err
		}
	}
	limiter.wait()
	body := "fields id,name,game_type.type,parent_game,version_parent; where id = (" + query + "); limit 500;"
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(settings.APIBaseURL, "/")+"/games", strings.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Client-ID", settings.ClientID)
	request.Header.Set("Authorization", "Bearer "+token.Value)
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("IGDB relationships request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, false, fmt.Errorf("IGDB relationships returned %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	var games []apiGame
	if err = json.NewDecoder(response.Body).Decode(&games); err != nil {
		return nil, false, err
	}
	if cache != nil {
		if err = cache.storeGames(key, settings.APIBaseURL, "relationships:"+query, games); err != nil {
			return nil, false, err
		}
	}
	return games, false, nil
}

type relationshipFile struct {
	Preserved bool
	Path      string
	IDs       []int
}
type relationshipWrite struct {
	Path    string
	Missing int
	Changed bool
}

// Read only the canonical files needed by this field refresh. The general
// catalog cache also loads fingerprints and is expensive for this pass on SMB.
func relationshipCatalogFiles(root, platform string) ([]string, int, error) {
	platforms, err := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if err != nil {
		return nil, 0, err
	}
	var files []string
	count := 0
	for _, p := range platforms {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") || strings.HasPrefix(p.Name(), "_") || platform != "" && platform != p.Name() {
			continue
		}
		dir := filepath.Join(root, "platforms", p.Name())
		games, err := catalogfs.ReadDir(dir)
		if err != nil {
			return nil, count, err
		}
		count++
		for _, g := range games {
			if !g.IsDir() || strings.HasPrefix(g.Name(), ".") || strings.HasPrefix(g.Name(), "_") || catalogrules.IsIgnoredGameFolder(g.Name()) {
				continue
			}
			files = append(files, filepath.Join(dir, g.Name(), "game.yaml"))
		}
	}
	return files, count, nil
}

// Bounded independent file I/O; results keep catalog order and progress is
// delivered by the caller goroutine. Every started write completes before return.
func visitRelationshipFiles[T any](paths []string, progress func(int, string), visit func(string) (T, error)) ([]T, error) {
	type completion struct {
		index int
		value T
		err   error
	}
	jobs := make(chan int)
	done := make(chan completion, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				value, err := visit(paths[index])
				done <- completion{index, value, err}
			}
		}()
	}
	go func() {
		for index := range paths {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		close(done)
	}()
	values := make([]T, len(paths))
	var firstErr error
	count := 0
	for item := range done {
		values[item.index] = item.value
		count++
		if item.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", paths[item.index], item.err)
		}
		if progress != nil && (count%25 == 0 || count == len(paths)) {
			progress(count, paths[item.index])
		}
	}
	return values, firstErr
}
