package scummvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/taskprogress"
)

const Platform = "ScummVM"

type ImportOptions struct {
	DataRoot string
	ListOptions
	Engines  []string
	Apply    bool
	Progress taskprogress.Func
}

type ImportResult struct {
	DataRoot       string
	Version        string
	ListSHA256     string
	SnapshotPath   string
	Supported      int
	Selected       int
	SkippedGeneric int
	Added          int
	Updated        int
	Unchanged      int
	WrittenFiles   int
	Apply          bool
	Official       *OfficialSource
}

type origin struct {
	ID             string `yaml:"id"`
	EngineID       string `yaml:"engine_id"`
	GameID         string `yaml:"game_id"`
	ListTitle      string `yaml:"list_title"`
	Version        string `yaml:"version"`
	ListSHA256     string `yaml:"list_sha256"`
	SourceURL      string `yaml:"source_url"`
	SourceRevision string `yaml:"source_revision"`
	Support        string `yaml:"support"`
}

type existingGame struct {
	path     string
	contents []byte
	document *catalogyaml.Document
}

type pendingWrite struct {
	path     string
	contents []byte
	previous []byte
}

// Import plans every change before writing. Existing cards are found by their
// qualified ID, never by a guessed title; curation, media and source data survive.
// Files absent from a later list are retained. Interrupted applies can be rerun.
func Import(options ImportOptions) (ImportResult, error) {
	result := ImportResult{Apply: options.Apply}
	report(options.Progress, "reading ScummVM list", 0, 0, "")
	options.ListOptions.CacheRoot = options.DataRoot
	list, err := Load(options.ListOptions)
	if err != nil {
		return result, err
	}
	filters := map[string]bool{}
	for _, value := range options.Engines {
		for _, engine := range strings.Split(value, ",") {
			engine = strings.TrimSpace(engine)
			if !engineID.MatchString(engine) {
				return result, fmt.Errorf("invalid ScummVM engine ID %q", engine)
			}
			filters[engine] = true
		}
	}
	knownEngines := map[string]bool{}
	var games []Game
	for _, game := range list.Games {
		engine := strings.SplitN(game.ID, ":", 2)[0]
		knownEngines[engine] = true
		if len(filters) > 0 && !filters[engine] {
			continue
		}
		if isGeneric(game) {
			result.SkippedGeneric++
			continue
		}
		games = append(games, game)
	}
	for engine := range filters {
		if !knownEngines[engine] {
			return result, fmt.Errorf("ScummVM list has no engine %q", engine)
		}
	}
	if len(games) == 0 {
		return result, fmt.Errorf("ScummVM selection has no concrete games")
	}
	root := options.DataRoot
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return result, err
	}
	platformPath := filepath.Join(root, "platforms", Platform)
	if err := checkPath(platformPath); err != nil {
		return result, err
	}
	hash := sha256.Sum256(list.Contents)
	digest := hex.EncodeToString(hash[:])
	// Include version in the snapshot directory identity: unchanged game lists
	// may appear in several releases of ScummVM.
	snapshotPath := filepath.Join(root, ".rommeta", "sources", "scummvm", snapshotKey(list.Version, digest))
	result.DataRoot, result.Version, result.ListSHA256 = root, list.Version, digest
	result.Supported, result.Selected = len(list.Games), len(games)
	result.SnapshotPath = snapshotPath
	result.Official = list.Official

	report(options.Progress, "reading existing cards", 0, 0, Platform)
	existing, err := readExisting(platformPath, options.Progress)
	if err != nil {
		return result, err
	}
	folderNames, err := gameFolderNames(list.Games)
	if err != nil {
		return result, err
	}
	var writes []pendingWrite
	for i, game := range games {
		report(options.Progress, "planning cards", i+1, len(games), game.Title)
		item, found := existing[game.ID]
		if !found {
			folder := folderNames[game.ID]

			item.path = filepath.Join(platformPath, folder, "game.yaml")
			if err := checkPath(item.path); err != nil {
				return result, err
			}
			entries, err := catalogfs.ReadDir(filepath.Dir(item.path))
			if err != nil && !os.IsNotExist(err) {
				return result, err
			}
			if len(entries) > 0 {
				return result, fmt.Errorf("unidentified existing folder at %s; import will not attach its files to a new card", filepath.Dir(item.path))
			}
			if _, err := catalogfs.Lstat(item.path); err == nil {
				return result, fmt.Errorf("unidentified existing card at %s; import will not overwrite it", item.path)
			} else if !os.IsNotExist(err) {
				return result, err
			}
			seed := fmt.Sprintf("title: %q\ndefault_release: %q\nreleases:\n  %q:\n    title: %q\n    authority:\n      source: scummvm\n      entry_id: %q\n", game.Title, safeTitle(game.Title), safeTitle(game.Title), game.Title, game.ID)
			item.document, err = catalogyaml.Parse([]byte(seed))
			if err != nil {
				return result, err
			}
		}
		parts := strings.SplitN(game.ID, ":", 2)
		provenance := origin{ID: game.ID, EngineID: parts[0], GameID: parts[1], ListTitle: game.Title, Version: list.Version, ListSHA256: digest}
		if list.Official != nil {
			provenance.SourceURL = list.Official.URL
			provenance.SourceRevision = list.Official.Revision
			provenance.Support = game.Support
		}
		oldNode := catalogyaml.Value(item.document.Root(), "scummvm")
		var previousOrigin origin
		decoded := found && oldNode.Decode(&previousOrigin) == nil
		// A byte-identical saved normalized list/version retains already verified
		// official provenance even when replayed through the explicit file mode.
		if decoded && list.Official == nil && previousOrigin.Version == list.Version && previousOrigin.ListSHA256 == digest {
			provenance.SourceURL = previousOrigin.SourceURL
			provenance.SourceRevision = previousOrigin.SourceRevision
			provenance.Support = previousOrigin.Support
		}
		node := &yaml.Node{}
		if err := node.Encode(provenance); err != nil {
			return result, err
		}
		// Avoid reformatting enriched or manually formatted cards on an exact replay.
		if decoded && previousOrigin == provenance {
			result.Unchanged++
			continue
		}
		target, err := catalogyaml.EnsureMapping(item.document.Root(), "scummvm")
		if err != nil {
			return result, err
		}
		if err := catalogyaml.Mapping(node, func(key string, value *yaml.Node) error {
			return catalogyaml.Set(target, key, value)
		}); err != nil {
			return result, err
		}
		if found && catalogyaml.Scalar(catalogyaml.Value(item.document.Root(), "title")) != game.Title {
			aliases := catalogyaml.Strings(catalogyaml.Value(item.document.Root(), "aliases"))
			present := false
			for _, alias := range aliases {
				if alias == game.Title {
					present = true
				}
			}
			if !present {
				aliases = append(aliases, game.Title)
				if err := catalogyaml.Set(item.document.Root(), "aliases", catalogyaml.StringSequence(aliases)); err != nil {
					return result, err
				}
			}
		}
		contents, err := item.document.Marshal()
		if err != nil {
			return result, err
		}
		if found {
			result.Updated++
		} else {
			result.Added++
		}
		writes = append(writes, pendingWrite{item.path, contents, item.contents})
	}
	// Platform files are created once. Existing user platform metadata survives.
	for _, file := range []struct{ name, contents string }{
		{"_platform.yaml", "name: ScummVM\ndisplay_name: ScummVM\nauthority:\n  source: scummvm\n  system: ScummVM\n"},
		{"_catalog.yaml", "title: ScummVM\nplatform: ScummVM\ngenerated_from:\n  source: scummvm\n  dataset: supported-games\nnotes:\n  - Cards come from official ScummVM lists, not ROM detection.\n  - The release entry is a metadata anchor, not a verified physical edition.\n"},
	} {
		path := filepath.Join(platformPath, file.name)
		if err := checkPath(path); err != nil {
			return result, err
		}
		if _, err := catalogfs.Stat(path); os.IsNotExist(err) {
			writes = append(writes, pendingWrite{path: path, contents: []byte(file.contents)})
		} else if err != nil {
			return result, err
		}
	}
	files, err := snapshotFiles(list, digest)
	if err != nil {
		return result, err
	}
	for _, file := range files {
		path := filepath.Join(snapshotPath, file.name)
		if err := checkPath(path); err != nil {
			return result, err
		}
		previous, err := catalogfs.ReadFile(path)
		if os.IsNotExist(err) {
			writes = append(writes, pendingWrite{path: path, contents: file.contents})
			continue
		}
		if err != nil {
			return result, err
		}
		if !bytes.Equal(previous, file.contents) {
			return result, fmt.Errorf("ScummVM snapshot differs at %s; restore it before importing", path)
		}
	}
	if list.Official != nil {
		path := officialPointer(root)
		if err := checkPath(path); err != nil {
			return result, err
		}
		contents := []byte(filepath.Base(snapshotPath) + "\n")
		previous, err := catalogfs.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return result, err
		}
		if !bytes.Equal(previous, contents) {
			writes = append(writes, pendingWrite{path, contents, previous})
		}
	}
	targets := map[string]bool{}
	for _, write := range writes {
		key := strings.ToLower(filepath.Clean(write.path))
		if targets[key] {
			return result, fmt.Errorf("conflicting ScummVM output paths: %s", write.path)
		}
		targets[key] = true
	}
	if !options.Apply {
		return result, nil
	}
	// One atomic file at a time; no copying of potentially large existing media.
	// Stop other catalog writers before applying, as for fetch and DAT import.
	for i, write := range writes {
		report(options.Progress, "writing catalog", i+1, len(writes), filepath.Base(filepath.Dir(write.path)))
		if err := checkPath(write.path); err != nil {
			return result, err
		}
		current, err := catalogfs.ReadFile(write.path)
		if write.previous == nil {
			if !os.IsNotExist(err) {
				return result, fmt.Errorf("target appeared during import: %s", write.path)
			}
		} else if err != nil || !bytes.Equal(current, write.previous) {
			return result, fmt.Errorf("card changed during import: %s; repeat after stopping other writers", write.path)
		}
		if err := atomicfile.WriteFile(write.path, write.contents, 0o644); err != nil {
			return result, fmt.Errorf("write %s: %w; completed files are retained, repeat the import to resume", write.path, err)
		}
		result.WrittenFiles++
	}
	return result, nil
}

func readExisting(platformPath string, callbacks ...taskprogress.Func) (map[string]existingGame, error) {
	var progress taskprogress.Func
	if len(callbacks) > 0 {
		progress = callbacks[0]
	}
	result := map[string]existingGame{}
	entries, err := catalogfs.ReadDir(platformPath)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		report(progress, "reading existing cards", i+1, len(entries), entry.Name())
		if strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ScummVM card must not be a symlink: %s", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(platformPath, entry.Name(), "game.yaml")
		if err := checkPath(path); err != nil {
			return nil, err
		}
		contents, err := catalogfs.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		document, err := catalogyaml.Parse(contents)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		node := catalogyaml.Value(document.Root(), "scummvm")
		if node == nil {
			continue
		}
		id := catalogyaml.Scalar(catalogyaml.Value(node, "id"))
		if !qualifiedID.MatchString(id) {
			return nil, fmt.Errorf("invalid scummvm.id in %s", path)
		}
		if _, ok := result[id]; ok {
			return nil, fmt.Errorf("duplicate ScummVM ID %s in existing catalog", id)
		}
		result[id] = existingGame{path, contents, document}
	}
	return result, nil
}

// Check the complete ancestor chain too, to avoid writing through a platform,
// game directory or runtime snapshot symlink.
func checkPath(path string) error {
	for {
		info, err := catalogfs.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ScummVM import refuses symlink path %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func safeTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	title = strings.Join(strings.Fields(b.String()), " ")
	// Bound titles for ordinary Windows paths and optional collision suffixes.
	runes := []rune(title)
	if len(runes) > 100 {
		title = string(runes[:100])
	}
	title = strings.TrimRightFunc(title, func(r rune) bool { return r == '.' || unicode.IsSpace(r) })
	if title == "" {
		title = "Untitled"
	}
	stem := strings.ToUpper(strings.SplitN(title, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" ||
		(len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
		title = "Game - " + title
	}
	if strings.HasPrefix(title, "_") || strings.HasPrefix(title, ".") {
		title = "Game - " + title
	}
	return title
}

func isGeneric(game Game) bool {
	switch game.ID {
	case "agi:agi", "sci:sci", "scumm:scumm", "glk:glk", "ags:ags", "director:director", "wintermute:wintermute", "sludge:sludge",
		"glk:adrift", "glk:agt", "glk:advsys", "glk:alan2", "glk:alan3",
		"glk:alan", "glk:archetype", "glk:glulx", "glk:hugo", "glk:jacl", "glk:level9",
		"glk:quest", "glk:scott", "glk:scottadams", "glk:zcode", "testbed:testbed":
		return true
	}
	return false
}

func report(progress taskprogress.Func, stage string, current, total int, item string) {
	progress.Update(stage, current, total, item)
}
