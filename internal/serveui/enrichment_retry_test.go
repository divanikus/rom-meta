package serveui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rommeta/internal/enrichment"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

func TestRetryOneStageQueuesOnceAndReusesNormalRunner(t *testing.T) {
	for _, selection := range []enrichmentSelection{{"launchbox", "metadata"}, {"screenscraper", "artwork"}, {"emumovies", "video"}} {
		t.Run(selection.Source+"/"+selection.Type, func(t *testing.T) {
			s, _ := enrichmentTestServer(t)
			path := filepath.Join(s.root, "config", "sources.yaml")
			data, _ := os.ReadFile(path)
			writeEnrichmentFixture(t, path, strings.Replace(string(data), "sources:\n", "sources:\n  emumovies:\n    enabled: true\n", 1))
			path = filepath.Join(s.root, "config", "secrets.yaml")
			data, _ = os.ReadFile(path)
			writeEnrichmentFixture(t, path, strings.Replace(string(data), "sources:\n", "sources:\n  emumovies:\n    username: example\n    password: example\n", 1))
			parent := enrichmentPlanFor(t, s, selection)
			s.changeOperation(parent.ID, func(op *libraryOperation) {
				op.State = "enriching"
				op.Enrichment.Concurrency = 2
				op.Enrichment.Jobs[0].State = "incomplete"
				op.Enrichment.Jobs[0].Counts.Downloaded = 12
				op.Enrichment.Jobs = append(op.Enrichment.Jobs, enrichmentTask{Source: "launchbox", Type: "metadata", State: "running", Platform: "Other"})
			})
			var calls atomic.Int32
			s.enrichRun = func(job enrichment.Job, o enrichment.Options, _ store.SourcesConfig, _ taskprogress.Func) (any, error) {
				calls.Add(1)
				if job.Source != selection.Source || job.Type != selection.Type || o.Platform != "segacd" || o.Concurrency != 2 || o.ForceOverwrite || !o.Apply {
					t.Errorf("incorrect retry: %#v %#v", job, o)
				}
				return nil, nil
			}
			// The first operation still owns the writer. Retrying must enqueue, not fail.
			s.editMu.Lock()
			locked := true
			defer func() {
				if locked {
					s.editMu.Unlock()
				}
			}()
			request := map[string]any{"id": parent.ID, "task": 0}
			w, _ := catalogCall(t, s, "/api/catalog/enrichment-retry", request)
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			var child libraryOperation
			json.Unmarshal(w.Body.Bytes(), &child)
			if child.State != "queued" || len(child.Enrichment.Jobs) != 1 || child.ID == parent.ID {
				t.Fatal(child)
			}
			w, _ = catalogCall(t, s, "/api/catalog/enrichment-retry", request)
			var duplicate libraryOperation
			json.Unmarshal(w.Body.Bytes(), &duplicate)
			if w.Code != 202 || duplicate.ID != child.ID || calls.Load() != 0 {
				t.Fatal("retry duplicated or ignored active writer")
			}
			original, _ := s.operationSnapshot(parent.ID)
			if original.Enrichment.Jobs[0].Counts.Downloaded != 12 || original.Enrichment.Jobs[1].State != "running" {
				t.Fatal("parent history changed")
			}
			s.editMu.Unlock()
			locked = false
			waitImport(t, s, child.ID, "succeeded")
			waitQueueIdle(t, s)
			w, _ = catalogCall(t, s, "/api/catalog/enrichment-retry", request)
			json.Unmarshal(w.Body.Bytes(), &duplicate)
			if w.Code != 202 || duplicate.ID != child.ID || calls.Load() != 1 {
				t.Fatal("accepted retry replayed")
			}
		})
	}
}

func TestRetryRejectsUnfinishedTasksAndInvalidRequests(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	parent := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	for _, state := range []string{"pending", "running", "succeeded"} {
		s.changeOperation(parent.ID, func(op *libraryOperation) { op.Enrichment.Jobs[0].State = state })
		w, _ := catalogCall(t, s, "/api/catalog/enrichment-retry", map[string]any{"id": parent.ID, "task": 0})
		if w.Code != 409 {
			t.Fatal(state, w.Code)
		}
	}
	for _, body := range []map[string]any{{"id": parent.ID}, {"id": parent.ID, "task": -1}, {"id": parent.ID, "task": 99}, {"id": "missing", "task": 0}} {
		w, _ := catalogCall(t, s, "/api/catalog/enrichment-retry", body)
		if w.Code < 400 {
			t.Fatal(body, w.Code)
		}
	}
	s.changeOperation(parent.ID, func(op *libraryOperation) { op.Enrichment.Jobs[0].State = "failed" })
	s.allowEdit = false
	w, _ := catalogCall(t, s, "/api/catalog/enrichment-retry", map[string]any{"id": parent.ID, "task": 0})
	if w.Code != 403 {
		t.Fatal("read-only retry allowed", w.Code)
	}
}

func TestRetryRevalidatesUnavailableSourceAndDoesNotCreateDeadJob(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	parent := enrichmentPlanFor(t, s, enrichmentSelection{"screenscraper", "artwork"})
	s.changeOperation(parent.ID, func(op *libraryOperation) { op.Enrichment.Jobs[0].State = "skipped" })
	writeEnrichmentFixture(t, filepath.Join(s.root, "config", "secrets.yaml"), "sources: {}\n")
	w, _ := catalogCall(t, s, "/api/catalog/enrichment-retry", map[string]any{"id": parent.ID, "task": 0})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Credentials") {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(s.operations) != 1 {
		t.Fatal("unavailable retry retained")
	}
}
