package scummvm

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"rommeta/internal/catalog"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/store"
)

const testList = "Game ID                        Full Title\n------------------------------ --------------------\nscumm:tentacle                  Day of the Tentacle\nsci:sq1                        Space Quest I\nags:ags                        Adventure Game Studio games\n"

func savedOptions(t *testing.T, contents string) ImportOptions {
	t.Helper()
	path := filepath.Join(t.TempDir(), "games.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return ImportOptions{DataRoot: filepath.Join(t.TempDir(), "catalog"), ListOptions: ListOptions{ListFile: path, Version: "ScummVM test-build"}}
}

func TestParseList(t *testing.T) {
	for _, contents := range [][]byte{[]byte(testList), append([]byte{0xef, 0xbb, 0xbf}, []byte(testList)...), utf16List(testList)} {
		games, err := ParseList(contents)
		if err != nil {
			t.Fatal(err)
		}
		if len(games) != 3 || games[2].ID != "scumm:tentacle" || games[2].Title != "Day of the Tentacle" {
			t.Fatalf("unexpected games: %+v", games)
		}
	}
	for _, input := range []string{"", "Game ID   Full Title\n----- -----", "tentacle Day of the Tentacle", "scumm:tentacle\n", "scumm:../bad Title", "scumm:tentacle A\nscumm:tentacle B", testList + "truncated or diagnostic line\n"} {
		if _, err := ParseList([]byte(input)); err == nil {
			t.Fatalf("accepted malformed list %q", input)
		}
	}
	games, err := ParseList([]byte("sci:one Same title\nsci:two Same title\nsci:two Same title\n"))
	if err != nil || len(games) != 2 {
		t.Fatalf("expected IDs, not titles, to determine uniqueness: %+v %v", games, err)
	}
}

func utf16List(text string) []byte {
	result := []byte{0xff, 0xfe}
	for _, unit := range utf16.Encode([]rune(text)) {
		result = binary.LittleEndian.AppendUint16(result, unit)
	}
	return result
}

func TestLoadExecutable(t *testing.T) {
	var calls []string
	run := func(_ context.Context, executable string, args ...string) ([]byte, error) {
		if executable != "C:/Program Files/ScummVM/scummvm.exe" {
			t.Fatalf("executable path changed: %s", executable)
		}
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "--version":
			return []byte("ScummVM test-build\nBuild features: example\n"), nil
		case "--list-games":
			return []byte(testList), nil
		}
		t.Fatalf("unexpected command: %v", args)
		return nil, nil
	}
	list, err := Load(ListOptions{Executable: "C:/Program Files/ScummVM/scummvm.exe", Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if list.Version != "ScummVM test-build" || strings.Join(calls, ",") != "--version,--list-games" {
		t.Fatalf("unexpected acquisition: %+v %v", list, calls)
	}
	_, err = Load(ListOptions{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("missing executable") }})
	if err == nil || !strings.Contains(err.Error(), "--scummvm") {
		t.Fatalf("missing actionable executable error: %v", err)
	}
	if _, err := Load(ListOptions{Executable: "x", ListFile: "y"}); err == nil {
		t.Fatal("accepted conflicting input sources")
	}
	if _, err := Load(ListOptions{ListFile: "x"}); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatal("missing version accepted")
	}
}

func TestImportPreviewApplyReplayAndUpdate(t *testing.T) {
	options := savedOptions(t, testList)
	preview, err := Import(options)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Added != 2 || preview.SkippedGeneric != 1 || preview.Selected != 2 || preview.WrittenFiles != 0 {
		t.Fatalf("wrong preview: %+v", preview)
	}
	if _, err := os.Stat(options.DataRoot); !os.IsNotExist(err) {
		t.Fatalf("preview created data root: %v", err)
	}
	if _, err := store.Init(options.DataRoot, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	options.Apply = true
	applied, err := Import(options)
	if err != nil {
		t.Fatal(err)
	}
	if applied.WrittenFiles != 6 {
		t.Fatalf("expected cards, platform files and snapshot: %+v", applied)
	}
	if _, err := os.Stat(filepath.Join(applied.SnapshotPath, "games.txt")); err != nil {
		t.Fatal(err)
	}
	cards, err := readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	if err != nil {
		t.Fatal(err)
	}
	card := cards["scumm:tentacle"]
	// Preserve a curated title, unknown fields, provider metadata and downloaded
	// media, even after the card directory is renamed by the user.
	edited := strings.Replace(string(card.contents), `title: "Day of the Tentacle"`, `title: "Curated title"`, 1)
	edited = strings.Replace(edited, "scummvm:\n", "scummvm:\n  user_note: source-note\n", 1)
	edited += "\ncustom_field: keep-me\nsources:\n  launchbox:\n    matches: []\n"
	if err := os.WriteFile(card.path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(filepath.Dir(card.path), "media", "cover.launchbox.jpg")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("existing image"), 0o600); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(filepath.Dir(filepath.Dir(card.path)), "User folder")
	if err := os.Rename(filepath.Dir(card.path), renamed); err != nil {
		t.Fatal(err)
	}
	replay, err := Import(options)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Unchanged != 2 || replay.WrittenFiles != 0 {
		t.Fatalf("repeat import is not a no-op: %+v", replay)
	}
	got, err := os.ReadFile(filepath.Join(renamed, "game.yaml"))
	if err != nil || string(got) != edited {
		t.Fatalf("exact replay rewrote curated bytes: %v\n%s", err, got)
	}
	// A new list updates source evidence without renaming the release/media,
	// adds another ID with the same title, and retains the omitted SCI card.
	if err := os.WriteFile(options.ListFile, []byte("scumm:tentacle Day of the Tentacle (Updated title)\nscumm:other Day of the Tentacle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Version = "ScummVM newer-build"
	updated, err := Import(options)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Added != 1 || updated.Updated != 1 {
		t.Fatalf("unexpected update: %+v", updated)
	}
	got, _ = os.ReadFile(filepath.Join(renamed, "game.yaml"))
	for _, required := range []string{"Curated title", "custom_field: keep-me", "user_note: source-note", "launchbox:", "Day of the Tentacle (Updated title)", "newer-build"} {
		if !strings.Contains(string(got), required) {
			t.Fatalf("update lost %q: %s", required, got)
		}
	}
	media, err := os.ReadFile(filepath.Join(renamed, "media", "cover.launchbox.jpg"))
	if err != nil || string(media) != "existing image" {
		t.Fatalf("media changed: %s %v", media, err)
	}
	cards, err = readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	if err != nil || len(cards) != 3 {
		t.Fatalf("IDs lost or collapsed: %d %v", len(cards), err)
	}
	issues, err := catalog.Validate(options.DataRoot)
	if err != nil || len(issues) != 0 {
		t.Fatalf("generated catalog fails validation: %+v %v", issues, err)
	}
}

func TestImportFiltersAndSafePaths(t *testing.T) {
	options := savedOptions(t, testList)
	options.Engines = []string{"scumm"}
	result, err := Import(options)
	if err != nil || result.Selected != 1 {
		t.Fatalf("engine selection: %+v %v", result, err)
	}
	options.Engines = []string{"missing"}
	if _, err := Import(options); err == nil {
		t.Fatal("unknown engine silently accepted")
	}
	options = savedOptions(t, "scumm:a CON\nscumm:b _Hidden\nscumm:c ../Unsafe: title?\nscumm:d CON\n")
	options.Apply = true
	result, err = Import(options)
	if err != nil || result.Added != 4 {
		t.Fatalf("safe paths or identical titles failed: %+v %v", result, err)
	}
	issues, err := catalog.Validate(options.DataRoot)
	if err != nil || len(issues) != 0 {
		t.Fatalf("unsafe generated paths: %+v %v", issues, err)
	}
	entries, _ := os.ReadDir(filepath.Join(options.DataRoot, "platforms", Platform))
	for _, entry := range entries {
		if entry.IsDir() && (strings.HasPrefix(entry.Name(), "_") || strings.HasPrefix(entry.Name(), ".")) {
			t.Fatalf("hidden card %s", entry.Name())
		}
	}
}

func TestImportConflictChangesNothing(t *testing.T) {
	options := savedOptions(t, testList)
	options.Apply = true
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	cards, _ := readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	card := cards["scumm:tentacle"]
	second := filepath.Join(options.DataRoot, "platforms", Platform, "Duplicate", "game.yaml")
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, card.contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options.ListFile, []byte(testList+"scumm:new New game\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(options); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate identity accepted: %v", err)
	}
	current, _ := os.ReadFile(card.path)
	if string(current) != string(card.contents) {
		t.Fatal("conflict modified existing card")
	}
	entries, _ := os.ReadDir(filepath.Join(options.DataRoot, "platforms", Platform))
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "New game") {
			t.Fatal("conflict wrote new card")
		}
	}
}

func TestImportRefusesSymlink(t *testing.T) {
	options := savedOptions(t, testList)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(options.DataRoot, "platforms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(options.DataRoot, "platforms", Platform)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	options.Apply = true
	if _, err := Import(options); err == nil {
		t.Fatal("symlink accepted")
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("wrote outside data root")
	}
}

func TestOriginIsNotProviderMetadata(t *testing.T) {
	options := savedOptions(t, "scumm:tentacle Day of the Tentacle\n")
	options.Apply = true
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	cards, _ := readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	doc, _ := catalogyaml.Parse(cards["scumm:tentacle"].contents)
	if catalogyaml.Value(doc.Root(), "sources") != nil {
		t.Fatal("seed falsely claims fetched provider metadata")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cards["scumm:tentacle"].path), "fingerprints.yaml")); !os.IsNotExist(err) {
		t.Fatal("seed invented ROM fingerprints")
	}
}

func TestImportRejectsConflictingPathsAndUnidentifiedFilesBeforeWriting(t *testing.T) {
	options := savedOptions(t, "scumm:one Same title\nscumm:ONE Same title\n")
	options.Apply = true
	if _, err := Import(options); err == nil {
		t.Fatal("case-colliding output accepted")
	}
	if _, err := os.Stat(options.DataRoot); !os.IsNotExist(err) {
		t.Fatal("conflict wrote data root")
	}

	options = savedOptions(t, "scumm:tentacle Day of the Tentacle\n")
	dir := filepath.Join(options.DataRoot, "platforms", Platform, "Day of the Tentacle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "unidentified.jpg")
	if err := os.WriteFile(media, []byte("unidentified media"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Apply = true
	if _, err := Import(options); err == nil || !strings.Contains(err.Error(), "unidentified existing folder") {
		t.Fatalf("unidentified files adopted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "game.yaml")); !os.IsNotExist(err) {
		t.Fatal("wrote card over unidentified files")
	}
	contents, _ := os.ReadFile(media)
	if string(contents) != "unidentified media" {
		t.Fatal("unidentified files changed")
	}
}
