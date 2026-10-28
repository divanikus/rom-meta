package sourceassets

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"rommeta/internal/fetchstate"
	"rommeta/internal/taskprogress"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPartialFetchReportsFailureAndNormalResumeSkipsSuccess(t *testing.T) {
	var healthy atomic.Bool
	var good, bad atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.jpg" {
			bad.Add(1)
			if !healthy.Load() {
				http.Error(w, "unavailable", 503)
				return
			}
		} else {
			good.Add(1)
		}
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	root := t.TempDir()
	for _, name := range []string{"good", "bad"} {
		writeGame(t, root, "Platform", name, fmt.Sprintf("title: %s\nreleases: {Game: {}}\nsources: {igdb: {matches: [{media: {box_front: [{url: %q}]}}]}}\n", name, server.URL+"/"+name+".jpg"))
	}
	var final taskprogress.TransferSnapshot
	first, err := Fetch(Options{DataRoot: root, Source: "igdb", Apply: true, Concurrency: 2, Progress: func(s taskprogress.Snapshot) {
		if s.Transfers != nil && s.Transfers.Done {
			final = *s.Transfers
		}
	}})
	var partial *fetchstate.PartialError
	if !errors.As(err, &partial) || first.DownloadedFileCount != 1 || first.FailedDownloadCount != 1 || first.StopReason == "" {
		t.Fatalf("partial result=%#v err=%v", first, err)
	}
	if final.Downloaded != 1 || final.Failed != 1 || final.Bytes != 5 || !final.Done {
		t.Fatalf("dishonest progress: %#v", final)
	}
	report, err := os.ReadFile(first.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), `"status":"failed"`) || !strings.Contains(string(report), `"status":"downloaded"`) {
		t.Fatalf("lost outcomes: %s", report)
	}
	healthy.Store(true)
	second, err := Fetch(Options{DataRoot: root, Source: "igdb", Apply: true, Concurrency: 2})
	if err != nil || second.ExistingFileCount != 1 || second.DownloadedFileCount != 1 || second.FailedDownloadCount != 0 || good.Load() != 1 || bad.Load() != 2 {
		t.Fatalf("resume=%#v err=%v calls=%d/%d", second, err, good.Load(), bad.Load())
	}
	if first.ManifestPath == second.ManifestPath {
		t.Fatal("resume replaced the failed pass report")
	}
	saved, err := os.ReadFile(first.ManifestPath)
	if err != nil || string(saved) != string(report) {
		t.Fatal("old report changed")
	}
}
