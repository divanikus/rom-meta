package sourceassets

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"rommeta/internal/screenscraper"
)

func TestScreenScraperQuotaPreservesDownloadsAndResumes(t *testing.T) {
	var quota atomic.Bool
	quota.Store(true)
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		if r.URL.Path == "/B.jpg" && quota.Load() {
			http.Error(w, "quota exceeded", 430)
			return
		}
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	root := t.TempDir()
	writeScreenScraperConfig(t, root)
	originals := map[string]string{}
	for _, name := range []string{"A", "B", "C"} {
		content := fmt.Sprintf("title: %s\nreleases:\n  %s: {}\nsources:\n  screenscraper:\n    matches:\n      - media:\n          box_front:\n            - url: %q\n", name, name, server.URL+"/"+name+".jpg")
		writeGame(t, root, "Arcade", name, content)
		originals[filepath.Join(root, "platforms", "Arcade", name, "game.yaml")] = content
	}
	options := Options{DataRoot: root, Source: "screenscraper", Apply: true, Concurrency: 1}
	result, err := Fetch(options)
	if !errors.Is(err, screenscraper.ErrDailyQuota) {
		t.Fatalf("expected daily quota error, got %v", err)
	}
	if result.DownloadedFileCount != 1 || result.FailedDownloadCount != 1 || result.ManifestItemCount != 2 {
		t.Fatalf("lost partial result: %#v", result)
	}
	if result.StopReason == "" || result.PlatformCount != 1 || result.GameBundleCount < 2 {
		t.Fatalf("missing stop reason or partial scan totals: %#v", result)
	}
	mu.Lock()
	if requests["/A.jpg"] != 1 || requests["/B.jpg"] != 1 || requests["/C.jpg"] != 0 {
		t.Errorf("requests continued after quota: %#v", requests)
	}
	mu.Unlock()
	manifest := readFile(t, result.ManifestPath)
	if !strings.Contains(manifest, `"status":"downloaded"`) || !strings.Contains(manifest, `"status":"failed"`) || !strings.Contains(manifest, "daily quota") {
		t.Fatalf("partial manifest not saved: %s", manifest)
	}
	if strings.Contains(manifest, "sspassword") || strings.Contains(manifest, "devpassword") {
		t.Fatal("manifest contains credentials")
	}
	for _, name := range []string{"B", "C"} {
		path := filepath.Join(root, "platforms", "Arcade", name, "media", "box-front.screenscraper.jpg")
		for _, candidate := range []string{path, path + ".tmp"} {
			if _, err := os.Stat(candidate); !os.IsNotExist(err) {
				t.Fatalf("unexpected file after quota: %s (%v)", candidate, err)
			}
		}
	}
	quota.Store(false)
	resumed, err := Fetch(options)
	if err != nil || resumed.DownloadedFileCount != 2 || resumed.ExistingFileCount != 1 || resumed.FailedDownloadCount != 0 {
		t.Fatalf("resume failed: %#v, %v", resumed, err)
	}
	mu.Lock()
	if requests["/A.jpg"] != 1 || requests["/B.jpg"] != 2 || requests["/C.jpg"] != 1 {
		t.Errorf("resume must request only missing files: %#v", requests)
	}
	mu.Unlock()
	for path, before := range originals {
		if got := readFile(t, path); got != before {
			t.Errorf("metadata changed: %s", path)
		}
		asset := filepath.Join(filepath.Dir(path), "media", "box-front.screenscraper.jpg")
		if got := readFile(t, asset); got != "image" {
			t.Errorf("wrong saved artwork: %q", got)
		}
	}
}

func TestDownloaderQuotaIsProviderScopedAndBounded(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		status       int
		stops        bool
	}{
		{"ScreenScraper quota", "screenscraper", 430, true},
		{"ScreenScraper missing image", "screenscraper", 404, false},
		{"unrelated provider status", "igdb", 430, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			const workers, items = 4, 20
			downloader := startCandidateDownloader(tc.source, workers, nil, nil)
			var enqueueErr error
			for i := 0; i < items; i++ {
				if enqueueErr = downloader.Enqueue(assetCandidate{URL: server.URL}); enqueueErr != nil {
					break
				}
			}
			downloaded, failed, _, err := downloader.CloseAndWait()
			if err != nil || downloaded != 0 || failed != int(requests.Load()) {
				t.Fatalf("unexpected result: downloaded=%d failed=%d requests=%d err=%v", downloaded, failed, requests.Load(), err)
			}
			if tc.stops {
				if !errors.Is(enqueueErr, screenscraper.ErrDailyQuota) || !errors.Is(downloader.Err(), screenscraper.ErrDailyQuota) || requests.Load() > workers {
					t.Fatalf("quota did not stop workers: requests=%d err=%v", requests.Load(), enqueueErr)
				}
			} else if enqueueErr != nil || downloader.Err() != nil || requests.Load() != items {
				t.Fatalf("ordinary failure stopped the run: requests=%d err=%v", requests.Load(), enqueueErr)
			}
		})
	}
}
