package serveui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"rommeta/internal/catalogedit"
	"rommeta/internal/enrichment"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"testing"
	"time"
)

func waitQueueIdle(t *testing.T, s *server) {
	t.Helper()
	for i := 0; i < 200; i++ {
		s.operationsMu.Lock()
		busy := s.enrichmentQueueRunning
		s.operationsMu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("queue did not drain")
}
func TestEnrichmentQueueFIFOAfterWriterAndCatalogChange(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	calls := make(chan int, 4)
	s.enrichRun = func(_ enrichment.Job, o enrichment.Options, _ store.SourcesConfig, p taskprogress.Func) (any, error) {
		calls <- o.Concurrency
		p(taskprogress.Snapshot{Worker: 1, Item: "first", BytesCurrent: 10, BytesTotal: 100})
		p(taskprogress.Snapshot{Worker: 2, Item: "second", BytesCurrent: 20, BytesTotal: 100})
		return nil, nil
	}
	a := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	b := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	s.operationsMu.Lock()
	s.operations[a.ID].Enrichment.Concurrency = 2
	s.operations[b.ID].Enrichment.Concurrency = 4
	s.operationsMu.Unlock()
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.beginEnrichment(a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.beginEnrichment(b.ID, b.Revision); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	for _, id := range []string{a.ID, b.ID} {
		op, _ := s.operationSnapshot(id)
		if op.State != "queued" {
			t.Fatalf("while CLI writer owns lock: %s", op.State)
		}
	}
	writeEnrichmentFixture(t, filepath.Join(s.root, "_catalog-revision"), "new catalog data")
	unlock()
	waitImport(t, s, a.ID, "succeeded")
	waitImport(t, s, b.ID, "succeeded")
	waitQueueIdle(t, s)
	if <-calls != 2 || <-calls != 4 {
		t.Fatal("not FIFO")
	}
	op, _ := s.operationSnapshot(b.ID)
	if len(op.Enrichment.Workers) != 2 {
		t.Fatal("parallel progress lost")
	}
	op.Enrichment.Workers[1] = taskprogress.Snapshot{Item: "changed"}
	again, _ := s.operationSnapshot(b.ID)
	if again.Enrichment.Workers[1].Item != "first" {
		t.Fatal("mutable worker snapshot")
	}
}
func TestEnrichmentQueueSettingsChangeAndCancel(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	s.enrichRun = func(enrichment.Job, enrichment.Options, store.SourcesConfig, taskprogress.Func) (any, error) {
		t.Error("unexpected acquisition")
		return nil, nil
	}
	a := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	b := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	s.editMu.Lock()
	if err := s.beginEnrichment(a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.beginEnrichment(b.ID, b.Revision); err != nil {
		t.Fatal(err)
	}
	w, _ := catalogCall(t, s, "/api/operations/dismiss", map[string]any{"id": b.ID, "revision": b.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	writeEnrichmentFixture(t, filepath.Join(s.root, "config", "catalog.yaml"), "regions: [Japan]\n")
	s.editMu.Unlock()
	waitImport(t, s, a.ID, "needs_review")
	waitQueueIdle(t, s)
	if _, ok := s.operationSnapshot(b.ID); ok {
		t.Fatal("cancelled job remains")
	}
	if _, err := os.Stat(filepath.Join(s.root, ".rommeta", "catalog-write.lock")); !os.IsNotExist(err) {
		t.Fatal("lock leaked", err)
	}
}
func TestEnrichmentMultiPlatformAndConcurrency(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	opts, _, _, _ := s.enrichmentChoices("segacd")
	input := enrichmentRequest{Platforms: []string{"segacd", "Other"}, Concurrency: 8, Revision: opts.Revision, Jobs: []enrichmentSelection{{"launchbox", "metadata"}}}
	w, _ := catalogCall(t, s, "/api/catalog/enrichment-plan", input)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var op libraryOperation
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if len(op.Enrichment.Jobs) != 2 || op.Enrichment.Concurrency != 8 {
		t.Fatal(op.Enrichment)
	}
	calls := make(chan string, 2)
	s.enrichRun = func(j enrichment.Job, o enrichment.Options, _ store.SourcesConfig, _ taskprogress.Func) (any, error) {
		if !o.ConcurrencySet || o.Concurrency != 8 || o.Platform != j.Platform {
			t.Error(o)
		}
		calls <- o.Platform
		return nil, nil
	}
	if err := s.beginEnrichment(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	waitImport(t, s, op.ID, "succeeded")
	waitQueueIdle(t, s)
	if len(calls) != 2 || <-calls != "segacd" || <-calls != "Other" {
		t.Fatal("both platforms must reach the provider's dynamic check")
	}
	input.Concurrency = 33
	w, _ = catalogCall(t, s, "/api/catalog/enrichment-plan", input)
	if w.Code != 400 {
		t.Fatal("invalid concurrency accepted")
	}
}
