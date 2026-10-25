package serveui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowseNamesAndFirstCardDoNotReadOtherBundlesOrFingerprints(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "platforms", "Arcade", "A Game")
	writeTestFile(t, filepath.Join(first, "game.yaml"), "title: First\nreleases:\n  set1:\n")
	// Fingerprint corruption cannot prevent browsing metadata. A malformed later
	// card must neither delay nor break the names or the selected-card response.
	writeTestFile(t, filepath.Join(first, "fingerprints.yaml"), "broken: [\n")
	writeTestFile(t, filepath.Join(root, "platforms", "Arcade", "Z Broken", "game.yaml"), "broken: [\n")
	handler, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	var names gamesResponse
	requestJSON(t, handler, "/api/games?platform=Arcade&names=1", &names)
	if len(names.Games) != 2 || names.Games[0].SummaryLoaded || names.Games[0].MissingMetadata || names.Games[0].SummaryError != "" {
		t.Fatalf("names must be available without interpreting metadata: %#v", names)
	}
	var detail gameResponse
	requestJSON(t, handler, "/api/game?platform=Arcade&game=A+Game", &detail)
	if detail.Game.Title != "First" || len(detail.Game.Fingerprints) != 0 {
		t.Fatalf("unexpected detail: %#v", detail.Game)
	}
	var batch gamesResponse
	requestJSON(t, handler, "/api/games?platform=Arcade&game=A+Game&game=Z+Broken", &batch)
	if len(batch.Games) != 2 || !batch.Games[0].SummaryLoaded || !batch.Games[0].MissingMetadata || batch.Games[1].SummaryError == "" || batch.Games[1].MissingMetadata {
		t.Fatalf("batch must retain readable cards without misclassifying errors: %#v", batch)
	}
	if _, err := os.Stat(filepath.Join(root, ".rommeta")); !os.IsNotExist(err) {
		t.Fatalf("browsing created runtime state: %v", err)
	}
}

func TestSummaryRefreshesAfterMetadataChangesAndRemovals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "platforms", "Arcade", "Game", "game.yaml")
	writeTestFile(t, path, "title: Before\n")
	handler, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	load := func() gameListItem {
		var batch gamesResponse
		requestJSON(t, handler, "/api/games?platform=Arcade&game=Game", &batch)
		return batch.Games[0]
	}
	if item := load(); item.Title != "Before" || !item.MissingMetadata {
		t.Fatalf("initial: %#v", item)
	}
	// Same-size edit must invalidate on mtime, as in a running fetch.
	stamp := time.Now().Add(2 * time.Second)
	writeTestFile(t, path, "title: Latest\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if item := load(); item.Title != "Latest" {
		t.Fatalf("stale title: %#v", item)
	}
	writeTestFile(t, path, "title: Latest\nsources:\n  launchbox:\n    matches:\n      - title: Latest\n")
	if item := load(); !item.HasMetadata || item.MissingMetadata {
		t.Fatalf("stale coverage: %#v", item)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if item := load(); item.SummaryError == "" || item.SummaryLoaded {
		t.Fatalf("removed card stayed cached: %#v", item)
	}
}

func TestSummaryRequestsAreBoundedValidatedAndCancellable(t *testing.T) {
	root := t.TempDir()
	handler, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/api/games?platform=..&names=1", "/api/games?platform=" + url.QueryEscape(`C:\outside`) + "&names=1",
		"/api/games?platform=Arcade&game=../outside", "/api/games?platform=Arcade&game=_support",
		"/api/games?platform=Arcade" + strings.Repeat("&game=Game", 33),
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items, err := handler.(*server).gameSummaries(ctx, "Arcade", []string{"Absent"})
	if err != context.Canceled || len(items) != 0 {
		t.Fatalf("cancelled request kept reading: %#v, %v", items, err)
	}
}
