package scummvm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/taskprogress"
)

func qualifiedFolder(title, id string) string {
	return safeTitle(title) + " [" + strings.ReplaceAll(id, ":", " - ") + "]"
}

// Use the complete concrete source list so engine-filtered imports make the
// same collision decisions as full imports. Existing paths are preserved by ID.
func gameFolderNames(games []Game) (map[string]string, error) {
	counts := map[string]int{}
	for _, game := range games {
		if !isGeneric(game) {
			counts[strings.ToLower(safeTitle(game.Title))]++
		}
	}
	names, occupied := map[string]string{}, map[string]string{}
	for _, game := range games {
		if isGeneric(game) {
			continue
		}
		name := safeTitle(game.Title)
		if counts[strings.ToLower(name)] > 1 {
			name = qualifiedFolder(game.Title, game.ID)
		}
		key := strings.ToLower(name)
		if id, ok := occupied[key]; ok && id != game.ID {
			return nil, fmt.Errorf("conflicting ScummVM folder names for %s and %s: %s", id, game.ID, name)
		}
		occupied[key], names[game.ID] = game.ID, name
	}
	return names, nil
}

type RenameFoldersOptions struct {
	DataRoot string
	Apply    bool
	Progress taskprogress.Func
}

type FolderRename struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type RenameFoldersResult struct {
	DataRoot      string
	Games         int
	Unchanged     int
	SkippedCustom int
	Renamed       int
	Failed        int
	Changes       []FolderRename
	PlanPath      string
}

// RenameFolders removes legacy importer suffixes only when titles are unique.
// It moves whole bundles, without rewriting metadata or opening media files.
func RenameFolders(options RenameFoldersOptions) (RenameFoldersResult, error) {
	var result RenameFoldersResult
	root := strings.TrimSpace(options.DataRoot)
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	result.DataRoot = root
	platformPath := filepath.Join(root, "platforms", Platform)
	if err := checkPath(platformPath); err != nil {
		return result, err
	}
	existing, err := readExisting(platformPath, options.Progress)
	if err != nil {
		return result, err
	}
	var games []Game
	for id, item := range existing {
		title := catalogyaml.Scalar(catalogyaml.Value(item.document.Root(), "title"))
		if strings.TrimSpace(title) == "" {
			return result, fmt.Errorf("missing title in %s", item.path)
		}
		games = append(games, Game{ID: id, Title: title})
	}
	sort.Slice(games, func(i, j int) bool { return games[i].ID < games[j].ID })
	result.Games = len(games)
	names, err := gameFolderNames(games)
	if err != nil {
		return result, err
	}
	for _, game := range games {
		item := existing[game.ID]
		from := filepath.Base(filepath.Dir(item.path))
		to := names[game.ID]
		if strings.EqualFold(from, to) {
			result.Unchanged++
			continue
		}
		var provenance origin
		if err := catalogyaml.Value(item.document.Root(), "scummvm").Decode(&provenance); err != nil {
			return result, err
		}
		if provenance.ListTitle == "" || from != qualifiedFolder(provenance.ListTitle, game.ID) {
			result.SkippedCustom++
			continue
		}
		change := FolderRename{ID: game.ID, From: from, To: to}
		if err := checkFolderRename(platformPath, change); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, change)
	}
	if !options.Apply || len(result.Changes) == 0 {
		return result, nil
	}
	// Save the complete intended mapping before the first rename. Completed
	// moves survive a filesystem error; repeat the command to resume by ID.
	plan := struct {
		SchemaVersion int            `json:"schema_version"`
		DataRoot      string         `json:"data_root"`
		Platform      string         `json:"platform"`
		Changes       []FolderRename `json:"changes"`
	}{1, root, Platform, result.Changes}
	contents, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return result, err
	}
	result.PlanPath = filepath.Join(root, ".rommeta", "runs", fmt.Sprintf("scummvm-folder-names-%d.json", time.Now().UnixNano()))
	if err := checkPath(result.PlanPath); err != nil {
		return result, err
	}
	if err := atomicfile.WriteFile(result.PlanPath, append(contents, '\n'), 0o600); err != nil {
		return result, err
	}
	var failures []error
	for index, change := range result.Changes {
		report(options.Progress, "renaming folders", index+1, len(result.Changes), change.To)
		if err := checkFolderRename(platformPath, change); err != nil {
			return result, err
		}
		item := existing[change.ID]
		current, err := catalogfs.ReadFile(item.path)
		if err != nil || !bytes.Equal(current, item.contents) {
			return result, fmt.Errorf("ScummVM card changed before rename: %s; stop other writers and repeat", item.path)
		}
		if err := retryFolderRename(func() error {
			return catalogfs.Rename(filepath.Join(platformPath, change.From), filepath.Join(platformPath, change.To))
		}, time.Sleep); err != nil {
			result.Failed++
			failures = append(failures, fmt.Errorf("rename ScummVM folder %s: %w; completed moves remain, repeat to resume; plan: %s", change.From, err, result.PlanPath))
			continue
		}
		result.Renamed++
	}
	return result, errors.Join(failures...)
}

func checkFolderRename(root string, change FolderRename) error {
	for _, name := range []string{change.From, change.To} {
		if name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("invalid ScummVM folder name %q", name)
		}
		path := filepath.Join(root, name)
		relative, err := filepath.Rel(root, path)
		if err != nil || relative != name {
			return fmt.Errorf("ScummVM folder escapes platform root: %q", name)
		}
		if err := checkPath(path); err != nil {
			return err
		}
	}
	if _, err := catalogfs.Lstat(filepath.Join(root, change.To)); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("ScummVM rename target already exists: %s; no folders are merged or overwritten", change.To)
	}
	return nil
}

// SMB or antivirus handles can briefly deny an otherwise valid rename. Retry
// only permission errors, with the same bounded backoff as catalog folding.
func retryFolderRename(rename func() error, wait func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		err := rename()
		if err == nil || !os.IsPermission(err) || attempt == 4 {
			return err
		}
		wait(100 * time.Millisecond << attempt)
	}
}
