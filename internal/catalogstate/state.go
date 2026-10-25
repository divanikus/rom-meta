package catalogstate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

// StatusOptions controls catalog state reporting.
type StatusOptions struct {
	Context  context.Context
	ReadOnly bool
	DataRoot string
	Platform string
	Progress taskprogress.Func
}

// Status summarizes the current portable catalog contents.
type Status struct {
	DataRoot               string
	PlatformFilter         string
	PlatformCount          int
	GameCount              int
	ReleaseCount           int
	GamesWithSourceData    int
	GamesWithoutSourceData int
	GamesWithSourceMatches int
	GamesCheckedNoMatches  int
	GamesWithLocalMedia    int
	GamesWithMediaURLs     int
	LocalMediaFileCount    int
	SourceMediaURLCount    int
	EmptyPlaceholderCount  int
	SourceCounts           []SourceCount
	Platforms              []PlatformStatus
	EmptyPlaceholders      []GameRef
}

// SourceCount records how many game bundles contain one source stanza.
type SourceCount struct {
	Source           string
	GameCount        int
	MatchedGameCount int
	NoMatchGameCount int
}

// PlatformStatus summarizes one platform folder.
type PlatformStatus struct {
	Platform               string
	GameCount              int
	ReleaseCount           int
	GamesWithSourceData    int
	GamesWithoutSourceData int
	GamesWithSourceMatches int
	GamesCheckedNoMatches  int
	GamesWithLocalMedia    int
	GamesWithMediaURLs     int
	LocalMediaFileCount    int
	EmptyPlaceholderCount  int
}

// GameRef identifies one game bundle in the catalog.
type GameRef struct {
	Platform string
	Game     string
	Title    string
	Path     string
}

// PruneOptions controls empty-game cleanup.
type PruneOptions struct {
	DataRoot string
	Platform string
	Apply    bool
	Progress taskprogress.Func
}

// PruneResult describes a dry-run or applied empty-game cleanup.
type PruneResult struct {
	Status
	Apply            bool
	DeletedGameCount int
}

type gameState struct {
	game            catalogcache.Game
	localMediaFiles int
	empty           bool
}

// BuildStatus scans the catalog and returns aggregate state.
func BuildStatus(options StatusOptions) (Status, error) {
	options.Progress.Update("resolving root", 0, 0, firstNonEmpty(options.DataRoot, "."))
	dataRoot, err := absoluteRoot(options.DataRoot)
	if err != nil {
		return Status{}, err
	}
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	readOptions := catalogcache.ReadOptions{Platforms: platformFilters(options.Platform), ReadingStage: "counting games"}
	var total int
	progress := func(snapshot taskprogress.Snapshot) {
		if snapshot.Stage == "counting games" {
			total = snapshot.Total
		}
		if options.Progress != nil {
			options.Progress(snapshot)
		}
	}

	sourceCounts := map[string]SourceCount{}
	platforms := map[string]*PlatformStatus{}
	result := Status{
		DataRoot:       dataRoot,
		PlatformFilter: strings.TrimSpace(options.Platform),
	}

	prepare := func(game catalogcache.Game) (gameState, error) {
		return statusGameState(ctx, game)
	}
	aggregate := func(state gameState) error {
		game := state.game
		platform := platforms[game.Platform]
		if platform == nil {
			platform = &PlatformStatus{Platform: game.Platform}
			platforms[game.Platform] = platform
		}
		addGame(&result, platform, state, sourceCounts)
		if state.empty {
			result.EmptyPlaceholders = append(result.EmptyPlaceholders, gameRef(state.game))
		}
		return nil
	}
	var gameCount int
	if options.ReadOnly {
		_, gameCount, err = walkStatusGames(ctx, dataRoot, options.Platform, progress, prepare, aggregate)
	} else {
		options.Progress.Update("opening catalog cache", 0, 0, catalogcache.Path(dataRoot))
		gameIndex := 0
		_, gameCount, err = catalogcache.ForEachGameWithOptions(dataRoot, readOptions, progress, func(game catalogcache.Game) error {
			gameIndex++
			options.Progress.Update("checking media", gameIndex, total, game.Platform+" / "+game.Folder)
			state, err := prepare(game)
			if err != nil {
				return err
			}
			return aggregate(state)
		})
	}
	if err != nil {
		return Status{}, err
	}
	result.GameCount = gameCount
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	ignoredFolders, err := ignoredGameFolders(dataRoot, options.Platform)
	if err != nil {
		return Status{}, err
	}
	for _, ignored := range ignoredFolders {
		platform := platforms[ignored.Platform]
		if platform == nil {
			platform = &PlatformStatus{Platform: ignored.Platform}
			platforms[ignored.Platform] = platform
		}
		result.EmptyPlaceholderCount++
		platform.EmptyPlaceholderCount++
		result.EmptyPlaceholders = append(result.EmptyPlaceholders, ignored)
	}

	result.PlatformCount = len(platforms)
	result.SourceCounts = sortedSourceCounts(sourceCounts)
	result.Platforms = sortedPlatformStatuses(platforms)
	return result, nil
}

// statusGameState is independent per game; aggregation/progress remain serial.
func statusGameState(ctx context.Context, game catalogcache.Game) (gameState, error) {
	if err := ctx.Err(); err != nil {
		return gameState{}, err
	}
	localMediaFiles, err := countLocalMediaFiles(game.Path)
	if err != nil {
		return gameState{}, err
	}
	if err := ctx.Err(); err != nil {
		return gameState{}, err
	}
	empty := isEmptyPlaceholder(game, localMediaFiles)
	if empty {
		extra, err := hasExtraGameFiles(game.Path)
		if err != nil {
			return gameState{}, err
		}
		empty = !extra
		if empty {
			curated, err := hasCuratedFields(game.Path, false)
			if err != nil {
				return gameState{}, err
			}
			empty = !curated
		}
	}
	return gameState{game: game, localMediaFiles: localMediaFiles, empty: empty}, nil
}

// PruneEmpty deletes empty game folders when Apply is true. Without
// Apply it only returns the cleanup plan.
func PruneEmpty(options PruneOptions) (PruneResult, error) {
	status, err := BuildStatus(StatusOptions{
		DataRoot: options.DataRoot,
		Platform: options.Platform,
		Progress: options.Progress,
	})
	if err != nil {
		return PruneResult{}, err
	}
	result := PruneResult{Status: status, Apply: options.Apply}
	if !options.Apply {
		return result, nil
	}

	platformRoot := filepath.Join(status.DataRoot, "platforms")
	for index, game := range status.EmptyPlaceholders {
		options.Progress.Update("deleting", index+1, len(status.EmptyPlaceholders), game.Platform+" / "+game.Game)
		if err := ensureGamePath(platformRoot, game.Path); err != nil {
			return PruneResult{}, err
		}
		eligible, err := canPruneGame(status.DataRoot, game)
		if err != nil {
			return result, err
		}
		if !eligible {
			return result, fmt.Errorf("refusing to delete changed or non-empty game folder: %s", game.Path)
		}
		if err := catalogfs.RemoveAll(game.Path); err != nil {
			return PruneResult{}, err
		}
		result.DeletedGameCount++
	}
	return result, nil
}

func addGame(result *Status, platform *PlatformStatus, state gameState, sourceCounts map[string]SourceCount) {
	game := state.game
	releaseCount := len(game.Releases)
	hasSourceData := len(game.Sources) > 0
	hasSourceMatches := hasMatchedSource(game.SourceStats)
	hasMediaURLs := len(game.MediaURLs) > 0
	hasLocalMedia := state.localMediaFiles > 0

	result.ReleaseCount += releaseCount
	platform.GameCount++
	platform.ReleaseCount += releaseCount

	if hasSourceData {
		result.GamesWithSourceData++
		platform.GamesWithSourceData++
		for _, source := range game.SourceStats {
			count := sourceCounts[source.Source]
			count.Source = source.Source
			count.GameCount++
			if source.MatchCount > 0 {
				count.MatchedGameCount++
			} else {
				count.NoMatchGameCount++
			}
			sourceCounts[source.Source] = count
		}
	} else {
		result.GamesWithoutSourceData++
		platform.GamesWithoutSourceData++
	}
	if hasSourceMatches {
		result.GamesWithSourceMatches++
		platform.GamesWithSourceMatches++
	} else if hasSourceData {
		result.GamesCheckedNoMatches++
		platform.GamesCheckedNoMatches++
	}
	if hasLocalMedia {
		result.GamesWithLocalMedia++
		platform.GamesWithLocalMedia++
	}
	if hasMediaURLs {
		result.GamesWithMediaURLs++
		platform.GamesWithMediaURLs++
	}
	if state.empty {
		result.EmptyPlaceholderCount++
		platform.EmptyPlaceholderCount++
	}
	result.LocalMediaFileCount += state.localMediaFiles
	result.SourceMediaURLCount += len(game.MediaURLs)
	platform.LocalMediaFileCount += state.localMediaFiles
}

func hasMatchedSource(sources []catalogcache.SourceStat) bool {
	for _, source := range sources {
		if source.MatchCount > 0 && !store.SourceIsIdentityOnly(source.Source) {
			return true
		}
	}
	return false
}

// HasDescriptiveMetadata reports whether at least one non-identity-only source
// has a successful match for a game.
func HasDescriptiveMetadata(sources []catalogcache.SourceStat) bool {
	return hasMatchedSource(sources)
}

func absoluteRoot(dataRoot string) (string, error) {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	return filepath.Abs(dataRoot)
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func countLocalMediaFiles(gamePath string) (int, error) {
	mediaPath := filepath.Join(gamePath, "media")
	info, err := catalogfs.Stat(mediaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}
	count := 0
	err = catalogfs.WalkDir(mediaPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count, err
}

func ignoredGameFolders(dataRoot, platformFilter string) ([]GameRef, error) {
	platformRoot := filepath.Join(dataRoot, "platforms")
	platforms, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	platformFilter = strings.TrimSpace(platformFilter)
	var refs []GameRef
	for _, platform := range platforms {
		if !platform.IsDir() || strings.HasPrefix(platform.Name(), ".") {
			continue
		}
		if platformFilter != "" && platform.Name() != platformFilter {
			continue
		}
		platformPath := filepath.Join(platformRoot, platform.Name())
		games, err := catalogfs.ReadDir(platformPath)
		if err != nil {
			return nil, err
		}
		for _, game := range games {
			if !game.IsDir() || !catalogrules.IsIgnoredGameFolder(game.Name()) {
				continue
			}
			candidate := GameRef{Platform: platform.Name(), Game: game.Name(), Path: filepath.Join(platformPath, game.Name())}
			eligible, err := canPruneGame(dataRoot, candidate)
			if err != nil {
				return nil, err
			}
			if !eligible {
				continue
			}
			refs = append(refs, GameRef{
				Platform: platform.Name(),
				Game:     game.Name(),
				Title:    game.Name(),
				Path:     filepath.Join(platformPath, game.Name()),
			})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Platform != refs[j].Platform {
			return refs[i].Platform < refs[j].Platform
		}
		return refs[i].Game < refs[j].Game
	})
	return refs, nil
}

func isEmptyPlaceholder(game catalogcache.Game, localMediaFiles int) bool {
	return !hasMatchedSource(game.SourceStats) && len(game.MediaURLs) == 0 && localMediaFiles == 0
}

func gameRef(game catalogcache.Game) GameRef {
	return GameRef{
		Platform: game.Platform,
		Game:     game.Folder,
		Title:    firstNonEmpty(game.Title, game.Folder),
		Path:     game.Path,
	}
}

func sortedSourceCounts(counts map[string]SourceCount) []SourceCount {
	var result []SourceCount
	for _, count := range counts {
		result = append(result, count)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Source < result[j].Source
	})
	return result
}

func sortedPlatformStatuses(platforms map[string]*PlatformStatus) []PlatformStatus {
	result := make([]PlatformStatus, 0, len(platforms))
	for _, platform := range platforms {
		result = append(result, *platform)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Platform < result[j].Platform
	})
	return result
}

func ensureGamePath(platformRoot, gamePath string) error {
	platformRoot, err := filepath.Abs(platformRoot)
	if err != nil {
		return err
	}
	gamePath, err = filepath.Abs(gamePath)
	if err != nil {
		return err
	}
	platformRoot, err = catalogfs.EvalSymlinks(platformRoot)
	if err != nil {
		return err
	}
	gamePath, err = catalogfs.EvalSymlinks(gamePath)
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(platformRoot, gamePath)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to delete path outside platforms root: %s", gamePath)
	}
	if len(strings.Split(rel, string(filepath.Separator))) < 2 {
		return fmt.Errorf("refusing to delete platform folder instead of a game folder: %s", gamePath)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
