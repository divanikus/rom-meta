package serveui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/enrichment"
	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
	"rommeta/internal/frontend"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

func TestOperationAccessDisjointScopesAndLastRelease(t *testing.T) {
	for _, fetchFirst := range []bool{true, false} {
		for _, fetchFinishesFirst := range []bool{true, false} {
			s, _ := importTestServer(t)
			var fetchDone, exportDone func()
			var err error
			if fetchFirst {
				fetchDone, err = s.acquireOperationAccess(true, []string{"n64", "segacd"})
				if err != nil {
					t.Fatal(err)
				}
				exportDone, err = s.acquireOperationAccess(false, []string{"snes"})
			} else {
				exportDone, err = s.acquireOperationAccess(false, []string{"snes"})
				if err != nil {
					t.Fatal(err)
				}
				fetchDone, err = s.acquireOperationAccess(true, []string{"n64", "segacd"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if fetchFinishesFirst {
				fetchDone()
			} else {
				exportDone()
			}
			assertCatalogBusy(t, s)
			if fetchFinishesFirst {
				exportDone()
			} else {
				fetchDone()
			}
			if !s.editMu.TryLock() {
				t.Fatal("web lock leaked")
			}
			s.editMu.Unlock()
			unlock, err := catalogedit.Lock(s.root)
			if err != nil {
				t.Fatal("catalog lock leaked", err)
			}
			unlock()
		}
	}
}

func assertCatalogBusy(t *testing.T, s *server) {
	t.Helper()
	if s.editMu.TryLock() {
		s.editMu.Unlock()
		t.Fatal("released web writer guard too early")
	}
	if unlock, err := catalogedit.Lock(s.root); err == nil {
		unlock()
		t.Fatal("released CLI writer guard too early")
	}
}

func TestOperationAccessConflicts(t *testing.T) {
	for _, fetchFirst := range []bool{false, true} {
		for _, scope := range [][]string{nil, {"n64"}, {"other", "N64"}} {
			s, _ := importTestServer(t)
			done, err := s.acquireOperationAccess(fetchFirst, []string{"n64"})
			if err != nil {
				t.Fatal(err)
			}
			if unexpected, err := s.acquireOperationAccess(!fetchFirst, scope); err == nil {
				unexpected()
				t.Fatal("overlapping access accepted")
			}
			if unexpected, err := s.acquireOperationAccess(fetchFirst, []string{"other"}); err == nil {
				unexpected()
				t.Fatal("second operation of the same kind accepted")
			}
			done()
		}
	}
}

func prepareFetchFor(t *testing.T, s *server, platform string) libraryOperation {
	t.Helper()
	writeEnrichmentFixture(t, filepath.Join(s.root, "config", "sources.yaml"), "sources:\n  launchbox:\n    enabled: true\n")
	options, _, _, err := s.enrichmentChoices(platform)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := catalogCall(t, s, "/api/catalog/enrichment-plan", enrichmentRequest{
		Platform: platform, Revision: options.Revision, Jobs: []enrichmentSelection{{"launchbox", "metadata"}},
	})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var op libraryOperation
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestExportPlanReviewApplyDuringOtherPlatformFetch(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "segacd", "_platform.yaml"), "name: Sega CD\n")
	fetch := prepareFetchFor(t, s, "segacd")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	s.enrichRun = func(_ enrichment.Job, _ enrichment.Options, _ store.SourcesConfig, _ taskprogress.Func) (any, error) {
		close(started)
		<-release
		name := filepath.Join(s.root, "platforms", "segacd", "Other", "game.yaml")
		if err := catalogchanges.Touch(name); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := s.beginEnrichment(fetch.ID, fetch.Revision); err != nil {
		t.Fatal(err)
	}
	<-started
	op := webExportPlan(t, s, d)
	ready := waitImport(t, s, op.ID, "ready")
	w, _ := catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: ready.ID, Revision: ready.Revision})
	if w.Code != 202 {
		t.Fatal("review blocked by other platform", w.Code, w.Body.String())
	}
	ready = waitImport(t, s, op.ID, "ready")
	writing, releaseWrite := make(chan struct{}), make(chan struct{})
	var writeOnce sync.Once
	finishWrite := func() { writeOnce.Do(func() { close(releaseWrite) }) }
	t.Cleanup(finishWrite)
	s.exportWrite = func(p *frontend.Plan) error { close(writing); <-releaseWrite; return p.Apply() }
	if err := s.beginExport(ready.ID, ready.Revision); err != nil {
		t.Fatal("apply blocked", err)
	}
	<-writing
	finish()
	waitImport(t, s, fetch.ID, "succeeded")
	waitQueueIdle(t, s)
	assertCatalogBusy(t, s)
	finishWrite()
	waitImport(t, s, op.ID, "succeeded")
}

func TestFetchCompletionDoesNotInvalidateUnrelatedExport(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "segacd", "_platform.yaml"), "name: Sega CD\n")
	fetch := prepareFetchFor(t, s, "segacd")
	planning, releasePlan := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(releasePlan) }) }
	t.Cleanup(finish)
	s.exportBuild = func(o exporter.Options, dest, media string, all bool) (*exportjob.Job, error) {
		close(planning)
		<-releasePlan
		return exportjob.Build(o, "", nil, all, dest, media, "")
	}
	op := webExportPlan(t, s, d)
	<-planning
	s.enrichRun = func(_ enrichment.Job, _ enrichment.Options, _ store.SourcesConfig, _ taskprogress.Func) (any, error) {
		return nil, catalogchanges.Touch(filepath.Join(s.root, "platforms", "segacd", "Other", "game.yaml"))
	}
	if err := s.beginEnrichment(fetch.ID, fetch.Revision); err != nil {
		t.Fatal(err)
	}
	waitImport(t, s, fetch.ID, "succeeded")
	waitQueueIdle(t, s)
	assertCatalogBusy(t, s)
	for _, dir := range []string{s.root, filepath.Join(s.root, "platforms", "segacd")} {
		if _, err := os.Stat(filepath.Join(dir, catalogchanges.RevisionFile)); err != nil {
			t.Fatal("missing revision", err)
		}
	}
	finish()
	ready := waitImport(t, s, op.ID, "ready")
	if err := s.beginExport(ready.ID, ready.Revision); err != nil {
		t.Fatal("unrelated revision invalidated plan", err)
	}
	waitImport(t, s, op.ID, "succeeded")
}

func TestFetchSamePlatformWaitsForExport(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	fetch := prepareFetchFor(t, s, "n64")
	planning, releasePlan := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(releasePlan) }) }
	t.Cleanup(finish)
	s.exportBuild = func(o exporter.Options, dest, media string, all bool) (*exportjob.Job, error) {
		close(planning)
		<-releasePlan
		return exportjob.Build(o, "", nil, all, dest, media, "")
	}
	op := webExportPlan(t, s, d)
	<-planning
	started := make(chan struct{})
	s.enrichRun = func(enrichment.Job, enrichment.Options, store.SourcesConfig, taskprogress.Func) (any, error) {
		close(started)
		return nil, nil
	}
	if err := s.beginEnrichment(fetch.ID, fetch.Revision); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("fetch started while its platform was being exported")
	case <-time.After(300 * time.Millisecond):
	}
	queued, _ := s.operationSnapshot(fetch.ID)
	if queued.State != "queued" {
		t.Fatal(queued.State)
	}
	finish()
	waitImport(t, s, op.ID, "ready")
	waitImport(t, s, fetch.ID, "succeeded")
	waitQueueIdle(t, s)
}

func TestExportSamePlatformRejectsPrepareReviewAndApply(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	op := webExportPlan(t, s, d)
	ready := waitImport(t, s, op.ID, "ready")
	done, err := s.acquireOperationAccess(true, []string{"n64"})
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	w, _ := catalogCall(t, s, "/api/export/prepare", nativeExportRequest{Destination: d})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "same platform") {
		t.Fatal(w.Code, w.Body.String())
	}
	w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: ready.ID, Revision: ready.Revision})
	if w.Code != 409 {
		t.Fatal("review accepted", w.Code)
	}
	if err := s.beginExport(ready.ID, ready.Revision); err == nil {
		t.Fatal("apply accepted")
	}
	d.All = true
	d.Platform = ""
	w, _ = catalogCall(t, s, "/api/export/prepare", nativeExportRequest{Destination: d})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "all platforms") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestExportPlatformRevisionInvalidatesPlan(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	op := webExportPlan(t, s, d)
	ready := waitImport(t, s, op.ID, "ready")
	batch := catalogchanges.Begin(func(string, string) error { return nil })
	if err := catalogchanges.Touch(filepath.Join(s.root, "platforms", "n64", "New", "game.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := batch.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := s.beginExport(ready.ID, ready.Revision); err == nil {
		t.Fatal("same-platform revision accepted")
	}
}

func TestExportScopeIncludesAliasesAndFamilyDatasets(t *testing.T) {
	s, _ := importTestServer(t)
	base, child := "Sony - PlayStation 3", "Sony - PlayStation 3 (PSN) (Content)"
	for _, platform := range []string{base, child} {
		writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", platform, "_platform.yaml"), "name: "+platform+"\n")
	}
	d := exportDestination{Platform: "ps3", ROMRoot: t.TempDir()}
	before, err := s.exportStamp(d)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.acquireOperationAccess(true, []string{child})
	if err != nil {
		t.Fatal(err)
	}
	if unexpected, err := s.exportAccess(d); err == nil {
		unexpected()
		t.Fatal("family child escaped export guard")
	}
	done()
	batch := catalogchanges.Begin(func(string, string) error { return nil })
	if err := catalogchanges.Touch(filepath.Join(s.root, "platforms", child, "Game", "game.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := batch.Finish(); err != nil {
		t.Fatal(err)
	}
	after, err := s.exportStamp(d)
	if err != nil || before == after {
		t.Fatal("family child missing from revision guard", err)
	}
}
