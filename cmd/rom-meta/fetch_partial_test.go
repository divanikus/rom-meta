package main

import (
	"bytes"
	"os"
	"rommeta/internal/fetchstate"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"strings"
	"testing"
	"time"
)

func TestFetchPartialFailuresContinueAndRemainUnsuccessful(t *testing.T) {
	var out bytes.Buffer
	ran := 0
	jobs := []fetchJob{{Source: "igdb", Type: "artwork"}, {Source: "emumovies", Type: "video"}, {Source: "launchbox", Type: "metadata", SkipReason: "not configured"}}
	err := runFetchJobs(&out, jobs, fetchOptions{}, store.SourcesConfig{}, func(job fetchJob, _ fetchOptions, _ store.SourcesConfig) error {
		ran++
		if job.Source == "igdb" {
			return fetchstate.Incomplete(3)
		}
		return nil
	})
	if err == nil || ran != 2 || !strings.Contains(out.String(), "1 completed, 1 incomplete, 0 failed, 1 skipped") || strings.Contains(out.String(), "Result: failed;") {
		t.Fatalf("dishonest job outcome: %s err=%v", out.String(), err)
	}
}
func TestTransferProgressShowsFinalTotalsInNonTerminalLog(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	p := newTerminalProgress(file)
	p.UpdateTransfers("igdb/artwork", taskprogress.TransferSnapshot{Queued: 3, Downloaded: 1, Failed: 1, Bytes: 1024, Elapsed: time.Second})
	p.UpdateTransfers("igdb/artwork", taskprogress.TransferSnapshot{Queued: 3, Downloaded: 2, Failed: 1, Bytes: 2048, Elapsed: 2 * time.Second, Done: true})
	text, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "2 downloaded, 0 existing, 1 failed, 0 pending") || !strings.Contains(string(text), "elapsed 2s") || !strings.Contains(string(text), "/s") {
		t.Fatalf("missing final progress: %s", text)
	}
}
