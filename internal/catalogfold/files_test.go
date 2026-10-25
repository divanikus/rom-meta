package catalogfold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
)

func TestFoldPlansAllMediaCollisionsAndPreservesBytes(t *testing.T) {
	root, platform := t.TempDir(), "Console"
	for _, region := range []string{"USA", "Japan", "Europe"} {
		writeFoldGame(t, root, platform, region, "Game ("+region+")", "Game", "10")
	}
	file := func(folder, rel string) string {
		return filepath.Join(root, "platforms", platform, folder, filepath.FromSlash(rel))
	}
	writeFoldFile(t, file("USA", "media/same.jpg"), "same bytes")
	writeFoldFile(t, file("Japan", "media/same.jpg"), "same bytes")
	// No copy in the keep card: these two still collide with each other.
	writeFoldFile(t, file("Europe", "media/logo.launchbox.png"), "Europe bytes")
	writeFoldFile(t, file("Japan", "media/logo.launchbox.png"), "Japan bytes")
	writeFoldFile(t, file("Japan", "notes/game.yaml"), "nested extra")
	dry, err := Fold(Options{DataRoot: root})
	if err != nil || dry.HeldGroupCount != 0 {
		t.Fatal(err, dry)
	}
	actions := map[string]int{}
	for _, change := range dry.Groups[0].FileChanges {
		actions[change.Action]++
	}
	if actions["deduplicate"] != 1 || actions["relocate"] != 1 {
		t.Fatal(actions, dry.Groups[0])
	}
	if _, err := os.Stat(file("USA", "notes/game.yaml")); !os.IsNotExist(err) {
		t.Fatal("preview wrote files", err)
	}
	applied, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || applied.AppliedGroupCount != 1 {
		t.Fatal(err, applied)
	}
	for _, change := range dry.Groups[0].FileChanges {
		original := readFoldFile(t, filepath.Join(applied.Backups[0], change.FromFolder, change.Path))
		if got := readFoldFile(t, file("USA", change.Target)); got != original {
			t.Fatalf("lost %s: %q != %q", change.Path, got, original)
		}
	}
	repeat, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || repeat.CandidateGroupCount != 0 || len(repeat.Backups) != 0 {
		t.Fatal(err, repeat)
	}
}

func TestFoldHoldsUnknownExtraFileCollisions(t *testing.T) {
	root := t.TempDir()
	for _, region := range []string{"USA", "Japan"} {
		writeFoldGame(t, root, "Console", region, "Game ("+region+")", "Game", "10")
		writeFoldFile(t, filepath.Join(root, "platforms", "Console", region, "notes.txt"), region)
	}
	result, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.AppliedGroupCount != 0 || result.HeldGroupCount != 1 || !strings.Contains(result.Groups[0].HoldReason, "non-media") || len(result.Backups) != 0 {
		t.Fatal(err, result)
	}
}

func TestFoldKeepsExistingRelocatedVariant(t *testing.T) {
	root := t.TempDir()
	for _, region := range []string{"USA", "Japan"} {
		writeFoldGame(t, root, "Console", region, "Game ("+region+")", "Game", "10")
		writeFoldFile(t, filepath.Join(root, "platforms", "Console", region, "media", "image.jpg"), region)
	}
	keep := filepath.Join(root, "platforms", "Console", "USA")
	writeFoldFile(t, filepath.Join(keep, "media", "_fold", "Japan", "image.jpg"), "older variant")
	result, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.AppliedGroupCount != 1 {
		t.Fatal(err, result)
	}
	if got := readFoldFile(t, filepath.Join(keep, "media", "_fold", "Japan", "image.jpg")); got != "older variant" {
		t.Fatal(got)
	}
	if got := readFoldFile(t, filepath.Join(keep, "media", "_fold", "Japan~2", "image.jpg")); got != "Japan" {
		t.Fatal(got)
	}
}

func TestFoldCollisionResultPassesCatalogValidation(t *testing.T) {
	root, platform := t.TempDir(), "Console"
	writeFoldFile(t, filepath.Join(root, "platforms", platform, "_catalog.yaml"), "schema_version: 0\n")
	writeFoldFile(t, filepath.Join(root, "platforms", platform, "_platform.yaml"), "name: Console\nauthority: redump\n")
	writeFoldFile(t, filepath.Join(root, "source-policies.yaml"), "sources:\n  launchbox:\n    media:\n      redistributable: false\n")
	for _, region := range []string{"USA", "Japan"} {
		writeFoldGame(t, root, platform, region, "Game ("+region+")", "Game", "10")
		writeFoldFile(t, filepath.Join(root, "platforms", platform, region, "media", "box-front.launchbox.jpg"), region)
	}
	result, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.AppliedGroupCount != 1 {
		t.Fatalf("fold failed: %+v %v", result, err)
	}
	relocated := 0
	for _, change := range result.Groups[0].FileChanges {
		if change.Action == "relocate" {
			relocated++
		}
	}
	if relocated != 1 {
		t.Fatalf("expected a preserved colliding variant: %+v", result.Groups[0].FileChanges)
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) != 0 {
		t.Fatalf("fold result does not validate: %#v %v", issues, err)
	}
}
