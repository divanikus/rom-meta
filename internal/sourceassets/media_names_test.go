package sourceassets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadNamesAndOfflineUpgradeAgree(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	root := t.TempDir()
	writeGame(t, root, "Platform", "Example", `title: Example
releases:
  Example (USA): {}
sources:
  launchbox:
    matches:
      - release: Example (USA)
        media:
          box_front:
            - url: "`+server.URL+`/flat.jpg"
              region: USA
              source_type: Box - Front
            - url: "`+server.URL+`/case.png"
              region: USA
              source_type: Box - Front - Reconstructed
`)
	game := filepath.Join(root, "platforms", "Platform", "Example")
	legacyCandidates, err := readGameAssetCandidates(game, "launchbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyCandidates) != 2 {
		t.Fatal(legacyCandidates)
	}
	for _, candidate := range legacyCandidates {
		if candidate.SourceType == "" || !strings.Contains(candidate.Target, ".variant-") {
			t.Fatal(candidate)
		}
	}
	result, err := Fetch(Options{DataRoot: root, Source: "launchbox", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.DownloadedFileCount != 2 || requests != 2 {
		t.Fatal(result, requests)
	}
	flat := filepath.Join(game, "media", "box-front-01.usa.variant-original.launchbox.jpg")
	framed := filepath.Join(game, "media", "box-front-02.usa.variant-reconstructed.launchbox.png")
	if readFile(t, flat) != "image" || readFile(t, framed) != "image" {
		t.Fatal("wrong new targets")
	}
	oldFlat := strings.Replace(flat, ".variant-original", "", 1)
	oldFramed := strings.Replace(framed, ".variant-reconstructed", "", 1)
	if err := os.Rename(flat, oldFlat); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(framed, oldFramed); err != nil {
		t.Fatal(err)
	}
	result, err = Fetch(Options{DataRoot: root, Source: "launchbox", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExistingFileCount != 2 || result.DownloadedFileCount != 0 || requests != 2 {
		t.Fatal("redownloaded legacy assets", result)
	}
	plan, err := NameMedia(MediaNamesOptions{DataRoot: root, Source: "launchbox"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Planned != 2 || plan.Renamed != 0 || readFile(t, oldFlat) != "image" {
		t.Fatal(plan)
	}
	applied, err := NameMedia(MediaNamesOptions{DataRoot: root, Source: "launchbox", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if applied.Renamed != 2 || readFile(t, flat) != "image" || readFile(t, framed) != "image" {
		t.Fatal(applied)
	}
	repeated, err := NameMedia(MediaNamesOptions{DataRoot: root, Source: "launchbox", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Planned != 0 || repeated.Existing != 2 || requests != 2 {
		t.Fatal(repeated, requests)
	}
	if err := os.WriteFile(oldFlat, []byte("different"), 0644); err != nil {
		t.Fatal(err)
	}
	conflict, err := NameMedia(MediaNamesOptions{DataRoot: root, Source: "launchbox", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if conflict.Conflicts != 1 || conflict.Renamed != 0 || readFile(t, flat) != "image" || readFile(t, oldFlat) != "different" {
		t.Fatal(conflict)
	}
}

func TestConflictingSourceTypesStayUnqualified(t *testing.T) {
	records := []mediaURLRecord{
		{MediaType: "box_front", URL: "https://example.test/image.jpg", SourceType: "Box - Front"},
		{MediaType: "box_front", URL: "https://example.test/image.jpg", SourceType: "Box - Front - Reconstructed"},
	}
	candidates := assetCandidatesFromRecords(t.TempDir(), "launchbox", records, 1)
	if len(candidates) != 1 || strings.Contains(candidates[0].Target, ".variant-") {
		t.Fatal(candidates)
	}
}

func TestOfflineCategoryViewPreservesExactCurrentAndLegacyTargets(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Platform", "Card", `title: Card
releases:
  Card (USA): {}
sources:
  launchbox:
    matches:
      - release: Card (USA)
        media:
          box_front:
            - url: https://example.test/front.png
              region: USA
              source_type: Box - Front
`)
	game := filepath.Join(root, "platforms", "Platform", "Card")
	categories, err := LocalMediaCategories(game, "launchbox")
	if err != nil || len(categories) != 2 {
		t.Fatal(categories, err)
	}
	got := map[string]bool{}
	for _, item := range categories {
		if item.SourceType != "Box - Front" {
			t.Fatal(item)
		}
		got[filepath.Base(item.Path)] = true
	}
	if !got["box-front.usa.variant-original.launchbox.png"] || !got["box-front.usa.launchbox.png"] {
		t.Fatal(got)
	}
	if _, err = os.Stat(filepath.Join(game, "media")); !os.IsNotExist(err) {
		t.Fatal("offline category view materialized media")
	}
}
