package sourceassets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"rommeta/internal/taskprogress"
)

func TestFetchReadsCardsOnceWithFilteredProgress(t *testing.T) {
	root := t.TempDir()
	for _, platform := range []string{"Selected", "Excluded"} {
		for _, game := range []string{"First", "Second"} {
			writeGame(t, root, platform, game, "title: "+game+"\nreleases:\n  Game (USA): {}\n")
		}
	}
	var reads []taskprogress.Snapshot
	result, err := Fetch(Options{
		DataRoot: root, Source: "igdb", Platform: "Selected",
		Progress: func(snapshot taskprogress.Snapshot) {
			if snapshot.Stage != "scanning catalog" {
				reads = append(reads, snapshot)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlatformCount != 1 || result.GameBundleCount != 2 || result.CandidateCount != 0 {
		t.Fatalf("unexpected filtered totals: %#v", result)
	}
	if len(reads) != 2 {
		t.Fatalf("expected one read per selected card, got %#v", reads)
	}
	for i, snapshot := range reads {
		if snapshot.Stage != "planning downloads" || snapshot.Current != i+1 || snapshot.Total != 2 || !strings.HasPrefix(snapshot.Item, "Selected / ") {
			t.Fatalf("unexpected progress: %#v", snapshot)
		}
	}
}

// A later malformed card must not postpone earlier downloads. Also verify that
// a read error drains started workers before Fetch returns to its caller.
func TestFetchDownloadsBeforeLaterCatalogError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	root := t.TempDir()
	writeGame(t, root, "Platform", "A Good", fmt.Sprintf(`title: Good
releases:
  Good (USA): {}
sources:
  igdb:
    matches:
      - media:
          box_front:
            - url: %q
`, server.URL+"/front.jpg"))
	writeGame(t, root, "Platform", "Z Broken", "title: [broken\n")

	_, err := Fetch(Options{DataRoot: root, Source: "igdb", Apply: true, Concurrency: 1})
	if err == nil || !strings.Contains(err.Error(), "Z Broken") {
		t.Fatalf("expected later catalog parse error, got %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("earlier download did not start before the catalog error: %d requests", requests.Load())
	}
	path := filepath.Join(root, "platforms", "Platform", "A Good", "media", "box-front.igdb.jpg")
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "image" {
		t.Fatalf("Fetch returned without draining its earlier download: %q, %v", contents, err)
	}
}

func TestCandidateDownloaderBackpressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const workers = 2
		const candidates = 100
		release := make(chan struct{})
		var prepared atomic.Int32
		downloader := startCandidateDownloader("igdb", workers, func(snapshot taskprogress.Snapshot) {
			if snapshot.Stage == "preparing" {
				prepared.Add(1)
				<-release
			}
		}, nil)
		var enqueued atomic.Int32
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < candidates; i++ {
				// Parsing fails locally after the gate; no external request occurs.
				downloader.Enqueue(assetCandidate{URL: "://invalid"})
				enqueued.Add(1)
			}
		}()
		synctest.Wait()
		if got := prepared.Load(); got != workers {
			t.Errorf("active downloads = %d, want %d", got, workers)
		}
		if got := enqueued.Load(); got != workers {
			t.Errorf("producer ran ahead of busy workers: enqueued %d", got)
		}
		close(release)
		<-done
		_, failed, failures, err := downloader.CloseAndWait()
		if err != nil || failed != candidates || len(failures) != 10 {
			t.Errorf("unexpected bounded failure summary: failed=%d samples=%d err=%v", failed, len(failures), err)
		}
	})
}
