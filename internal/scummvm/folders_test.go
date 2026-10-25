package scummvm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogyaml"
)

func TestNewFolderNamesUseTitlesAndQualifyOnlyCollisions(t *testing.T) {
	options := savedOptions(t, "scumm:unique A unique game\nagi:one Same title\nsci:two Same title\nscumm:unsafe1 A:B\nscumm:unsafe2 A?B\nscumm:reserved NUL\n")
	options.Apply = true
	options.Engines = []string{"scumm"}
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	cards, err := readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"scumm:unique": "A unique game", "scumm:reserved": "Game - NUL",
		"scumm:unsafe1": "A B [scumm - unsafe1]", "scumm:unsafe2": "A B [scumm - unsafe2]",
	} {
		if got := filepath.Base(filepath.Dir(cards[id].path)); got != want {
			t.Errorf("%s folder=%q, want %q", id, got, want)
		}
	}
	options.Engines = []string{"agi"}
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	cards, _ = readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	if got := filepath.Base(filepath.Dir(cards["agi:one"].path)); got != "Same title [agi - one]" {
		t.Fatalf("engine filtering changed collision decision: %s", got)
	}
}

func TestRenameFoldersPreservesBundlesAndReplay(t *testing.T) {
	options := savedOptions(t, "scumm:one First game\nsci:two Second game\nagi:duplicate Same title\nsci:duplicate Same title\nscumm:custom Custom game\n")
	options.Apply = true
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	platformPath := filepath.Join(options.DataRoot, "platforms", Platform)
	cards, _ := readExisting(platformPath)
	for id, card := range cards {
		var provenance origin
		if err := catalogyaml.Value(card.document.Root(), "scummvm").Decode(&provenance); err != nil {
			t.Fatal(err)
		}
		name := qualifiedFolder(provenance.ListTitle, id)
		if id == "scumm:custom" {
			name = "My curated directory"
		}
		target := filepath.Join(platformPath, name)
		if filepath.Dir(card.path) != target {
			if err := os.Rename(filepath.Dir(card.path), target); err != nil {
				t.Fatal(err)
			}
		}
		contents := string(card.contents) + "sources:\n  launchbox:\n    status: not_found\nuser_extra: preserved\n"
		if err := os.WriteFile(filepath.Join(target, "game.yaml"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		mediaPath := filepath.Join(target, "media", "First release", "cover.launchbox.jpg")
		if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mediaPath, []byte("media for "+id), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := folderBundleContents(t, platformPath)
	if _, _, err := catalogcache.ReadGames(options.DataRoot, nil); err != nil {
		t.Fatal(err)
	}
	renameOptions := RenameFoldersOptions{DataRoot: options.DataRoot}
	preview, err := RenameFolders(renameOptions)
	if err != nil || len(preview.Changes) != 2 || preview.Renamed != 0 || preview.SkippedCustom != 1 || preview.Unchanged != 2 {
		t.Fatalf("bad preview: %+v; %v", preview, err)
	}
	if !reflect.DeepEqual(before, folderBundleContents(t, platformPath)) {
		t.Fatal("preview changed bundle files")
	}
	if preview.PlanPath != "" {
		t.Fatal("preview wrote rename plan")
	}
	renameOptions.Apply = true
	applied, err := RenameFolders(renameOptions)
	if err != nil || applied.Renamed != 2 || applied.PlanPath == "" {
		t.Fatalf("bad apply: %+v; %v", applied, err)
	}
	plan, err := os.ReadFile(applied.PlanPath)
	if err != nil || !json.Valid(plan) || !strings.Contains(string(plan), "First game [scumm - one]") {
		t.Fatalf("missing recoverable plan: %s; %v", plan, err)
	}
	after := folderBundleContents(t, platformPath)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("metadata or nested media bytes changed")
	}
	cached, _, err := catalogcache.ReadGames(options.DataRoot, nil)
	if err != nil || len(cached) != 5 {
		t.Fatalf("catalog cache did not follow moved bundles: %+v; %v", cached, err)
	}
	for _, game := range cached {
		if !catalogcache.HasSource(game.Sources, "launchbox") {
			t.Fatal("moved bundle lost provider evidence")
		}
	}
	replay, err := RenameFolders(renameOptions)
	if err != nil || replay.Renamed != 0 || len(replay.Changes) != 0 {
		t.Fatalf("rename replay is not empty: %+v; %v", replay, err)
	}
	result, err := Import(options)
	if err != nil || result.Added != 0 || result.Updated != 0 || result.Unchanged != 5 || result.WrittenFiles != 0 {
		t.Fatalf("import recreated legacy paths: %+v; %v", result, err)
	}
}

func TestRenameFoldersRejectsOccupiedTargetsBeforeAnyMove(t *testing.T) {
	options := savedOptions(t, "scumm:a Alpha\nscumm:b Beta\n")
	options.Apply = true
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(options.DataRoot, "platforms", Platform)
	for _, name := range []string{"Alpha", "Beta"} {
		id := map[string]string{"Alpha": "scumm:a", "Beta": "scumm:b"}[name]
		if err := os.Rename(filepath.Join(root, name), filepath.Join(root, qualifiedFolder(name, id))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "Beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := RenameFolders(RenameFoldersOptions{DataRoot: options.DataRoot, Apply: true})
	if err == nil || !strings.Contains(err.Error(), "already exists") || result.Renamed != 0 || result.PlanPath != "" {
		t.Fatalf("occupied target was not rejected before mutation: %+v; %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, qualifiedFolder("Alpha", "scumm:a"), "game.yaml")); err != nil {
		t.Fatal("earlier card was moved before preflight finished")
	}
}

func TestRenameFolderPathsStayInsidePlatform(t *testing.T) {
	for _, name := range []string{"..", ".", "../escape", `..\escape`} {
		if err := checkFolderRename(t.TempDir(), FolderRename{From: "Valid", To: name}); err == nil {
			t.Errorf("accepted escaping target %s", name)
		}
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "Linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := checkFolderRename(root, FolderRename{From: "Linked", To: "New"}); err == nil {
		t.Fatal("accepted symlink source")
	}
}

func folderBundleContents(t *testing.T, root string) map[string]map[string]string {
	t.Helper()
	cards, err := readExisting(root)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]string{}
	for id, card := range cards {
		base := filepath.Dir(card.path)
		result[id] = map[string]string{}
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			contents, err := os.ReadFile(path)
			result[id][relative] = string(contents)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func TestFolderRenameRetriesOnlyTransientPermissionErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  int
		err       error
		attempts  int
		wantError bool
	}{
		{"transient", 2, os.ErrPermission, 3, false},
		{"persistent", 10, os.ErrPermission, 5, true},
		{"other error", 2, os.ErrNotExist, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts, waits := 0, 0
			err := retryFolderRename(func() error {
				attempts++
				if attempts <= tc.failures {
					return &os.PathError{Op: "rename", Path: "game", Err: tc.err}
				}
				return nil
			}, func(time.Duration) { waits++ })
			if (err != nil) != tc.wantError || attempts != tc.attempts || waits != attempts-1 {
				t.Fatalf("err=%v attempts=%d waits=%d", err, attempts, waits)
			}
			if err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("lost cause: %v", err)
			}
		})
	}
}
