package serveui

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogstate"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/httpdocs"
	"rommeta/internal/medianames"
	"rommeta/internal/platformnames"
)

//go:embed static/index.html
var indexHTML string

// Handler returns a read-only HTTP handler for a portable metadata data root.
func Handler(dataRoot string) (http.Handler, error) {
	root := strings.TrimSpace(dataRoot)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	realRoot, err := catalogfs.EvalSymlinks(absRoot)
	if err != nil {
		return nil, err
	}
	server := &server{root: absRoot, realRoot: realRoot}
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.index)
	mux.HandleFunc("/api", server.apiIndex)
	mux.HandleFunc("/api/stats", server.stats)
	mux.HandleFunc("/api/platforms", server.platforms)
	mux.HandleFunc("/api/games", server.games)
	mux.HandleFunc("/api/game", server.game)
	mux.HandleFunc("/media", server.media)
	server.handler = mux
	return server, nil
}

type server struct {
	root      string
	realRoot  string
	handler   http.Handler
	summaryMu sync.Mutex
	summaries map[string]summaryEntry
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if httpdocs.Handle(w, r, "/openapi.json", "/docs", "RomMeta browser API", browserOpenAPI) {
		return
	}

	s.handler.ServeHTTP(w, r)
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	if platform != "" && !safeCatalogName(platform) {
		writeError(w, http.StatusBadRequest, errors.New("invalid platform"))
		return
	}
	options := catalogstate.StatusOptions{DataRoot: s.root, Platform: s.resolvePlatform(platform), ReadOnly: true, Context: r.Context()}
	if r.URL.Query().Get("stream") == "1" {
		s.streamStats(w, r, options)
		return
	}
	status, err := catalogstate.BuildStatus(options)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, status)
}

func (s *server) platforms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	result, err := listPlatforms(s.root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, result)
}

func (s *server) games(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	if platform == "" {
		writeError(w, http.StatusBadRequest, errors.New("platform is required"))
		return
	}
	platform = s.resolvePlatform(platform)
	if !safeCatalogName(platform) {
		writeError(w, http.StatusBadRequest, errors.New("invalid platform"))
		return
	}
	folders := r.URL.Query()["game"]
	if len(folders) > 32 {
		writeError(w, http.StatusBadRequest, errors.New("at most 32 game summaries per request"))
		return
	}
	for _, folder := range folders {
		if !safeCatalogName(folder) || catalogrules.IsIgnoredGameFolder(folder) {
			writeError(w, http.StatusBadRequest, errors.New("invalid game folder"))
			return
		}
	}
	var result []gameListItem
	var err error
	if len(folders) > 0 {
		result, err = s.gameSummaries(r.Context(), platform, folders)
	} else if r.URL.Query().Get("names") == "1" {
		result, err = listGameNames(s.root, platform)
	} else {
		result, err = s.listGames(r.Context(), platform)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, gamesResponse{Platform: platform, Games: result})
}

func (s *server) game(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	folder := strings.TrimSpace(r.URL.Query().Get("game"))
	if platform == "" || folder == "" {
		writeError(w, http.StatusBadRequest, errors.New("platform and game are required"))
		return
	}
	platform = s.resolvePlatform(platform)
	found, document, err := catalogcache.ReadGameDocument(s.root, platform, folder)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	media, err := localMediaFiles(s.root, found.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	blocks := sourceBlocks(document, found.SourceStats)
	writeJSON(w, gameResponse{
		Game:            found,
		HasMetadata:     gameHasMetadata(found),
		MissingMetadata: !gameHasMetadata(found),
		Profile:         buildGameProfile(firstNonEmpty(found.Title, found.Folder), found.Platform, blocks),
		SourceBlocks:    blocks,
		LocalMedia:      media,
	})
}

// The platform list exposes actual folder names. Do not redirect those names
// through provider/CLI aliases, even when both catalogs exist. Aliases remain a
// fallback for API callers; filesystem errors on explicit paths stay visible.
func (s *server) resolvePlatform(platform string) string {
	if !safeCatalogName(platform) {
		return platform
	}
	canonical := platformnames.ResolveCLI(platform)
	if canonical == platform {
		return platform
	}
	if _, err := catalogfs.Stat(filepath.Join(s.root, "platforms", platform)); !os.IsNotExist(err) {
		return platform
	}
	return canonical
}

func (s *server) media(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rel, err := cleanRelativePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !isCatalogMediaPath(rel) {
		writeError(w, http.StatusBadRequest, errors.New("path is not catalog media"))
		return
	}
	target := filepath.Join(s.root, filepath.FromSlash(rel))
	absTarget, err := filepath.Abs(target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !isInside(s.root, absTarget) {
		writeError(w, http.StatusBadRequest, errors.New("media path escapes data root"))
		return
	}
	resolvedTarget, err := catalogfs.EvalSymlinks(absTarget)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !isInside(s.realRoot, resolvedTarget) {
		writeError(w, http.StatusBadRequest, errors.New("media path escapes data root"))
		return
	}
	resolvedRel, err := filepath.Rel(s.realRoot, resolvedTarget)
	if err != nil || !isCatalogMediaPath(filepath.ToSlash(resolvedRel)) {
		writeError(w, http.StatusBadRequest, errors.New("resolved path is not catalog media"))
		return
	}
	info, err := catalogfs.Stat(resolvedTarget)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, errors.New("media path is not a regular file"))
		return
	}
	file, err := catalogfs.Open(resolvedTarget)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	http.ServeContent(w, r, filepath.Base(resolvedTarget), info.ModTime(), file)
}

type platformItem struct {
	Platform             string `json:"platform"`
	GameCount            int    `json:"game_count"`
	ReleaseCount         int    `json:"release_count"`
	MatchedGameCount     int    `json:"matched_game_count"`
	MissingMetadataCount int    `json:"missing_metadata_count"`
	CheckedNoMatchCount  int    `json:"checked_no_match_count"`
	NotCheckedCount      int    `json:"not_checked_count"`
}

type gamesResponse struct {
	Platform string         `json:"platform"`
	Games    []gameListItem `json:"games"`
}

type gameListItem struct {
	SummaryLoaded       bool                      `json:"summary_loaded,omitempty"`
	SummaryError        string                    `json:"summary_error,omitempty"`
	Folder              string                    `json:"folder"`
	Title               string                    `json:"title,omitempty"`
	DefaultRelease      string                    `json:"default_release,omitempty"`
	VariantCount        int                       `json:"variant_count,omitempty"`
	ReleaseCount        int                       `json:"release_count,omitempty"`
	SourceStats         []catalogcache.SourceStat `json:"source_stats,omitempty"`
	HasMetadata         bool                      `json:"has_metadata,omitempty"`
	MissingMetadata     bool                      `json:"missing_metadata,omitempty"`
	CheckedNoMatch      bool                      `json:"checked_no_match,omitempty"`
	SourceMediaURLCount int                       `json:"source_media_url_count,omitempty"`
}

type gameResponse struct {
	Game            catalogcache.Game `json:"game"`
	HasMetadata     bool              `json:"has_metadata"`
	MissingMetadata bool              `json:"missing_metadata"`
	Profile         gameProfile       `json:"profile"`
	SourceBlocks    []sourceBlock     `json:"source_blocks"`
	LocalMedia      []localMediaFile  `json:"local_media"`
}

type sourceBlock struct {
	Source     string        `json:"source"`
	Status     string        `json:"status,omitempty"`
	MatchCount int           `json:"match_count"`
	Profile    sourceProfile `json:"profile,omitempty"`
	Matches    []sourceMatch `json:"matches,omitempty"`
	Raw        string        `json:"raw,omitempty"`
}

type localMediaFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	URL    string `json:"url"`
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
	Format string `json:"format,omitempty"`
	Type   string `json:"type"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func listPlatforms(root string) ([]platformItem, error) {
	platformRoot := filepath.Join(root, "platforms")
	entries, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	var result []platformItem
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		item := platformItem{Platform: entry.Name()}
		games, err := catalogfs.ReadDir(filepath.Join(platformRoot, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, game := range games {
			if isCatalogGameEntry(game) {
				item.GameCount++
			}
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Platform < result[j].Platform
	})
	return result, nil
}

func listGameNames(root, platform string) ([]gameListItem, error) {
	platformPath := filepath.Join(root, "platforms", platform)
	entries, err := catalogfs.ReadDir(platformPath)
	if err != nil {
		return nil, err
	}
	var result []gameListItem
	for _, entry := range entries {
		if !isCatalogGameEntry(entry) {
			continue
		}
		result = append(result, gameListItem{Folder: entry.Name()})
	}
	sortGameItems(result)
	return result, nil
}

func sortGameItems(result []gameListItem) {
	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(firstNonEmpty(result[i].Title, result[i].Folder))
		right := strings.ToLower(firstNonEmpty(result[j].Title, result[j].Folder))
		if left != right {
			return left < right
		}
		return result[i].Folder < result[j].Folder
	})
}

func isCatalogGameEntry(entry os.DirEntry) bool {
	return entry.IsDir() &&
		!strings.HasPrefix(entry.Name(), ".") &&
		!strings.HasPrefix(entry.Name(), "_") &&
		!catalogrules.IsIgnoredGameFolder(entry.Name())
}

func readGameListItem(root, platform, folder string) (gameListItem, error) {
	game, _, err := catalogcache.ReadGameDocument(root, platform, folder)
	if err != nil {
		return gameListItem{}, err
	}
	item := gameListItem{
		SummaryLoaded:       true,
		Folder:              folder,
		Title:               game.Title,
		DefaultRelease:      game.DefaultRelease,
		ReleaseCount:        len(game.Releases),
		VariantCount:        len(game.Variants),
		SourceStats:         game.SourceStats,
		SourceMediaURLCount: len(game.MediaURLs),
	}
	hasMetadata := catalogstate.HasDescriptiveMetadata(item.SourceStats)
	item.HasMetadata = hasMetadata
	item.MissingMetadata = !hasMetadata
	item.CheckedNoMatch = !hasMetadata && len(item.SourceStats) > 0
	return item, nil
}

func gameHasMetadata(game catalogcache.Game) bool {
	return catalogstate.HasDescriptiveMetadata(game.SourceStats)
}

func sourceBlocks(document *catalogyaml.Document, stats []catalogcache.SourceStat) []sourceBlock {
	statsBySource := map[string]catalogcache.SourceStat{}
	for _, stat := range stats {
		statsBySource[stat.Source] = stat
	}
	var result []sourceBlock
	_ = catalogyaml.Mapping(catalogyaml.Value(document.Root(), "sources"), func(source string, stanza *yaml.Node) error {
		stat := statsBySource[source]
		matches := parseSourceMatchesNode(stanza)
		result = append(result, sourceBlock{
			Source:     source,
			Status:     stat.Status,
			MatchCount: stat.MatchCount,
			Profile:    sourceProfileFromMatches(matches),
			Matches:    matches,
			Raw:        renderSourceNode(source, stanza),
		})
		return nil
	})
	return result
}

func renderSourceNode(source string, stanza *yaml.Node) string {
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: source}, stanza,
	}}
	contents, err := yaml.Marshal(root)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}

func localMediaFiles(root, gamePath string) ([]localMediaFile, error) {
	mediaRoot := filepath.Join(gamePath, "media")
	info, err := catalogfs.Stat(mediaRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var result []localMediaFile
	err = catalogfs.WalkDir(mediaRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		item, ok := mediaFile(root, path)
		if ok {
			result = append(result, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Path < result[j].Path
	})
	return result, nil
}

func mediaFile(root, path string) (localMediaFile, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	mediaType := mediaTypeForExt(ext)
	if mediaType == "" {
		return localMediaFile{}, false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return localMediaFile{}, false
	}
	rel = filepath.ToSlash(rel)
	name := filepath.Base(path)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	parts := medianames.SourceParts(strings.Split(stem, "."))
	kind := parts[0]
	source := ""
	if len(parts) > 1 {
		source = parts[len(parts)-1]
	}
	return localMediaFile{
		Name:   name,
		Path:   rel,
		URL:    "/media?path=" + url.QueryEscape(rel),
		Kind:   kind,
		Source: source,
		Format: strings.TrimPrefix(ext, "."),
		Type:   mediaType,
	}, true
}

func mediaTypeForExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif":
		return "image"
	case ".mp4", ".webm", ".mkv", ".avi", ".mov":
		return "video"
	default:
		return ""
	}
}

func cleanRelativePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("path is required")
	}
	if strings.Contains(raw, "\x00") || strings.Contains(raw, ":") || strings.Contains(raw, "\\") || pathpkg.IsAbs(raw) {
		return "", fmt.Errorf("invalid media path %q", raw)
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("invalid media path %q", raw)
		}
	}
	clean := pathpkg.Clean(raw)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("invalid media path %q", raw)
	}
	return clean, nil
}

func isCatalogMediaPath(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 5 || parts[0] != "platforms" || parts[1] == "" || parts[2] == "" || parts[3] != "media" {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return mediaTypeForExt(strings.ToLower(filepath.Ext(parts[len(parts)-1]))) != ""
}

func isInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func leadingSpaces(value string) int {
	count := 0
	for _, r := range value {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}

func trimComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
