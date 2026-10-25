package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rommeta/internal/hasheous"
)

func TestHasheousProgressShowsRequestAndPreservesElapsedOnRefresh(t *testing.T) {
	out, err := os.Create(filepath.Join(t.TempDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	p := &terminalProgress{out: out, interactive: true}
	job := fetchJob{Source: "hasheous", Type: "metadata"}
	snapshot := hasheous.ImportProgress{Worker: 2, Workers: 4, Stage: "querying", Request: 2, Requests: 8, Game: "Platform / Game"}
	updateHasheousProgress(p, job, snapshot)
	key := workerProgressKey("hasheous/metadata", 2)
	item := p.byteItems[key]
	item.Started = time.Now().Add(-7 * time.Second)
	p.byteItems[key] = item
	updateHasheousProgress(p, job, snapshot)
	updateHasheousProgress(p, job, hasheous.ImportProgress{Stage: "matching", Current: 6313, Total: 33872, Queries: 4, Game: "4 active, 4 queued"})
	lines := strings.Join(p.progressLines(), "\n")
	for _, want := range []string{"hasheous/metadata", "matching", "queries=4", "4 active, 4 queued", "#2", "querying", "7s", "request 2/8", "Platform / Game"} {
		if !strings.Contains(lines, want) {
			t.Errorf("progress missing %q: %s", want, lines)
		}
	}
	if !p.byteItems[key].Started.Equal(item.Started) {
		t.Error("refresh reset the request timer")
	}
	snapshot.Request = 3
	updateHasheousProgress(p, job, snapshot)
	if !p.byteItems[key].Started.After(item.Started) {
		t.Error("a new request should restart the timer")
	}
	p.Done()
	if len(p.byteItems) != 0 {
		t.Error("completed job left worker rows behind")
	}
}

func TestHasheousWorkerStagesUseTimeInsteadOfDownloadBytes(t *testing.T) {
	for _, stage := range []string{"querying", "rate limit", "writing metadata"} {
		line := formatWorkerProgressLine(byteProgressItem{Worker: 1, Workers: 4, Stage: stage, Label: "Game", Started: time.Now().Add(-3 * time.Second)}, false)
		if !strings.Contains(line, stage) || !strings.Contains(line, "3s") || strings.Contains(line, "0 B") || strings.Contains(line, "/s") {
			t.Errorf("invalid metadata worker row: %s", line)
		}
	}
}
