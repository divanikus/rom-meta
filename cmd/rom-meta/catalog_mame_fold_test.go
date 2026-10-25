package main

import (
	"strings"
	"testing"

	"rommeta/internal/catalogfold"
)

func TestCatalogMAMEUsesOrdinaryFoldInterface(t *testing.T) {
	if err := runCatalog([]string{"repair-arcade", "--help"}); err == nil || !strings.Contains(err.Error(), "unknown catalog command") {
		t.Fatal("one-off repair command remains public", err)
	}
	group := catalogfold.Group{Platform: "Arcade", Keep: catalogfold.GameRef{Folder: "Legacy title"}, TargetFolder: "Clean title", Applied: true}
	output := renderCatalogFoldResult(catalogfold.Result{Groups: []catalogfold.Group{group}, RenamedGameCount: 1, Backups: []string{"retained-backup"}, Apply: true}, true, false)
	for _, want := range []string{"Card renames: 1", "resulting folder: Clean title", "Original catalog backup: retained-backup"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
}

func TestCatalogFoldReportsHeldAliasesAndMediaPreservation(t *testing.T) {
	group := catalogfold.Group{Platform: "Console", Keep: catalogfold.GameRef{Folder: "Game"}, HoldReason: "conflicting igdb IDs: 10, 20", FileChanges: []catalogfold.FileChange{{FromFolder: "Localized", Path: "media/box.jpg", Target: "media/_fold/Localized/box.jpg", Action: "relocate"}}}
	output := renderCatalogFoldResult(catalogfold.Result{HeldGroupCount: 1, Groups: []catalogfold.Group{group}}, true, false)
	for _, want := range []string{"Held: 1 groups", "skipped by --apply", "(held)", "conflicting igdb IDs", "relocate: Localized/media/box.jpg -> media/_fold/Localized/box.jpg"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
}

func TestSerialDiscoveryReviewOutput(t *testing.T) {
	group := catalogfold.Group{Platform: "Microsoft - Xbox 360", Keep: catalogfold.GameRef{Folder: "English Game", Regions: []string{"usa"}}, Merge: []catalogfold.GameRef{{Folder: "Localized Game", Regions: []string{"japan"}}}, ReviewRequired: "shared serial requires confirmation", Evidence: []catalogfold.Evidence{{Source: "redump", Field: "xbox360_serial", Value: "EA-2276"}}}
	output := renderCatalogFoldResult(catalogfold.Result{CandidateGroupCount: 1, ReviewRequiredCount: 1, Groups: []catalogfold.Group{group}}, true, false)
	for _, want := range []string{"Review required: 1 groups", "--interactive", "(review required)", "regions: English Game: usa", "regions: Localized Game: japan", "redump.xbox360_serial=EA-2276", "no automatically eligible groups"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
	if strings.Contains(output, "rerun with --apply to merge candidate groups") {
		t.Fatal(output)
	}
}
