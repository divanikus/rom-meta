package serveui

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rommeta/internal/catalogedit"
	"rommeta/internal/enrichment"
	"rommeta/internal/launchbox"
	"rommeta/internal/screenscraper"
	"rommeta/internal/sourceassets"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

func enrichmentTestServer(t *testing.T) (*server, *httptest.Server) {
	s, ts := importTestServer(t)
	for name, data := range map[string]string{
		"config/sources.yaml":              "sources:\n  launchbox:\n    enabled: true\n  openvgdb:\n    enabled: true\n  screenscraper:\n    enabled: true\n  igdb:\n    enabled: true\nfetch:\n  metadata: [launchbox, screenscraper]\n  artwork: [launchbox, screenscraper]\n",
		"config/secrets.yaml":              "sources:\n  screenscraper:\n    username: test-user\n    password: test-password\n    dev_id: test-id\n    dev_password: test-dev-password\n",
		"platforms/segacd/_platform.yaml":  "name: Sega CD\n",
		"platforms/segacd/Orbit/game.yaml": "schema_version: 1\ntitle: Orbit\nreleases:\n  Orbit (USA):\n    title: Orbit\n    regions: [USA]\n",
		"platforms/Other/Orbit/game.yaml":  "schema_version: 1\ntitle: Orbit\n",
	} {
		writeEnrichmentFixture(t, filepath.Join(s.root, filepath.FromSlash(name)), data)
	}
	return s, ts
}
func writeEnrichmentFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}
func enrichmentPlanFor(t *testing.T, s *server, jobs ...enrichmentSelection) libraryOperation {
	t.Helper()
	options, _, _, err := s.enrichmentChoices("segacd")
	if err != nil {
		t.Fatal(err)
	}
	w, _ := catalogCall(t, s, "/api/catalog/enrichment-plan", enrichmentRequest{Platform: "segacd", Revision: options.Revision, Jobs: jobs})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var op libraryOperation
	if err = json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	return op
}
func TestEnrichmentContinuesAfterDisconnectAndStartIsIdempotent(t *testing.T) {
	s, ts := enrichmentTestServer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var starts atomic.Int32
	s.enrichRun = func(job enrichment.Job, o enrichment.Options, c store.SourcesConfig, p taskprogress.Func) (any, error) {
		starts.Add(1)
		if o.Platform != "segacd" || !o.Apply || o.ForceOverwrite || o.DataRoot != s.root || o.PlatformRegistry == nil {
			t.Error("incorrect runner options", o)
		}
		p(taskprogress.Snapshot{Stage: "matching", Current: 1, Total: 2, Item: "test-password https://user:pass@example.test/path?token=private"})
		close(entered)
		<-release
		return launchbox.ImportResult{GameBundleCount: 2, GameFileCount: 2, WrittenFileCount: 2}, nil
	}
	op := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	body, _ := json.Marshal(importAction{ID: op.ID, Revision: op.Revision})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/catalog/enrichment-start", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	cancel()
	if response.StatusCode != 202 {
		close(release)
		t.Fatal(response.Status)
	}
	<-entered
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/operations", nil))
	var rows []libraryOperation
	json.Unmarshal(w.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].State != "enriching" || rows[0].Enrichment.Progress.Current != 1 {
		close(release)
		t.Fatal(w.Body.String())
	}
	for _, secret := range []string{"test-password", "user:pass", "token=private"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Error("secret leaked", secret)
		}
	}
	if unlock, err := catalogedit.Lock(s.root); err == nil {
		unlock()
		t.Error("CLI writer not excluded")
	}
	for _, path := range []string{"/api/catalog/import-plan", "/api/catalog/import-apply", "/api/operations/dismiss"} {
		w, _ = catalogCall(t, s, path, importAction{ID: op.ID, Revision: op.Revision})
		if w.Code != 409 {
			t.Error("unsafe cross-operation action", path, w.Code)
		}
	}
	w, _ = catalogCall(t, s, "/api/catalog/enrichment-start", importAction{ID: op.ID, Revision: op.Revision})
	if w.Code != 202 {
		t.Error(w.Code)
	}
	close(release)
	done := waitImport(t, s, op.ID, "succeeded")
	w, _ = catalogCall(t, s, "/api/catalog/enrichment-start", importAction{ID: op.ID, Revision: op.Revision})
	if w.Code != 202 || starts.Load() != 1 || done.Enrichment.Jobs[0].Counts.Written != 2 {
		t.Fatal(starts.Load(), done)
	}
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
func TestEnrichmentChoicesAndInvalidSelectionsNeverStartWork(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	options, _, _, err := s.enrichmentChoices("segacd")
	if err != nil {
		t.Fatal(err)
	}
	defaults, missing := false, false
	for _, c := range options.Choices {
		if c.Source == "launchbox" && c.Type == "metadata" {
			defaults = c.Default && c.Available
		}
		if c.Source == "igdb" && c.Type == "metadata" {
			missing = !c.Available && strings.Contains(c.Reason, "Credentials")
		}
	}
	if !defaults || !missing {
		t.Fatal(options)
	}
	cases := []enrichmentRequest{
		{Platform: "../segacd", Revision: options.Revision, Jobs: []enrichmentSelection{{"launchbox", "metadata"}}},
		{Platform: "segacd", Revision: options.Revision},
		{Platform: "segacd", Revision: options.Revision, Jobs: []enrichmentSelection{{"launchbox", "video"}}},
		{Platform: "segacd", Revision: options.Revision, Jobs: []enrichmentSelection{{"igdb", "metadata"}}},
		{Platform: "segacd", Revision: options.Revision, Jobs: []enrichmentSelection{{"launchbox", "metadata"}, {"launchbox", "metadata"}}},
	}
	for _, input := range cases {
		w, _ := catalogCall(t, s, "/api/catalog/enrichment-plan", input)
		if w.Code < 400 {
			t.Fatal(w.Code, input)
		}
	}
	if len(s.operations) != 0 {
		t.Fatal("invalid plans retained")
	}
	input := enrichmentRequest{Platform: "segacd", Revision: options.Revision, Jobs: []enrichmentSelection{{"launchbox", "metadata"}}}
	data, _ := json.Marshal(input)
	r := httptest.NewRequest("POST", "/api/catalog/enrichment-plan", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://elsewhere.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	s.allowEdit = false
	w, _ = catalogCall(t, s, "/api/catalog/enrichment-plan", input)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/catalog/enrichment-options?platform=segacd", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestEnrichmentStalePlanAndBusyWriter(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	op := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.beginEnrichment(op.ID, op.Revision); err == nil {
		t.Error("writer conflict accepted")
	}
	unlock()
	writeEnrichmentFixture(t, filepath.Join(s.root, "config", "catalog.yaml"), "regions: [Japan]\n")
	if err = s.beginEnrichment(op.ID, op.Revision); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal(err)
	}
	snapshot, _ := s.operationSnapshot(op.ID)
	if snapshot.State != "ready" {
		t.Fatal(snapshot)
	}
}
func TestEnrichmentQuotaSkipsProviderAndContinuesOthers(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	var starts atomic.Int32
	s.enrichRun = func(job enrichment.Job, _ enrichment.Options, _ store.SourcesConfig, _ taskprogress.Func) (any, error) {
		starts.Add(1)
		if job.Source == "screenscraper" {
			if job.Type != "metadata" {
				t.Error("quota provider was retried")
			}
			return screenscraper.ImportResult{WrittenFileCount: 1}, screenscraper.ErrDailyQuota
		}
		return sourceassets.Result{DownloadedFileCount: 2}, nil
	}
	op := enrichmentPlanFor(t, s, enrichmentSelection{"screenscraper", "artwork"}, enrichmentSelection{"launchbox", "artwork"}, enrichmentSelection{"screenscraper", "metadata"})
	if err := s.beginEnrichment(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	done := waitImport(t, s, op.ID, "incomplete")
	if starts.Load() != 2 || done.Enrichment.Jobs[0].State != "incomplete" || done.Enrichment.Jobs[1].Counts.Downloaded != 2 || done.Enrichment.Jobs[2].State != "skipped" {
		t.Fatal(done.Enrichment)
	}
	if err := s.beginEnrichment(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 {
		t.Fatal("replayed incomplete operation")
	}
}
func TestEnrichmentActiveRecordsAndSnapshots(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	op := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
	s.changeOperation(op.ID, func(current *libraryOperation) {
		current.State = "enriching"
		current.Updated = time.Now().Add(-48 * time.Hour)
	})
	s.operationsMu.Lock()
	s.expireOperationsLocked()
	s.operationsMu.Unlock()
	a, ok := s.operationSnapshot(op.ID)
	if !ok {
		t.Fatal("expired active operation")
	}
	a.Enrichment.Jobs[0].State = "changed outside lock"
	b, _ := s.operationSnapshot(op.ID)
	if b.Enrichment.Jobs[0].State != "pending" {
		t.Fatal("mutable snapshot")
	}
}
func TestEnrichmentRealProviderWritesOnlySelectedPlatformAndRepeatKeepsSource(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	var zipData bytes.Buffer
	zw := zip.NewWriter(&zipData)
	f, _ := zw.Create("Metadata.xml")
	fmt.Fprint(f, "<LaunchBox><Game><DatabaseID>42</DatabaseID><Name>Orbit</Name><Platform>Sega CD</Platform><Developer>Test Studio</Developer><Overview>A synthetic adventure.</Overview></Game></LaunchBox>")
	zw.Close()
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write(zipData.Bytes()) }))
	defer provider.Close()
	writeEnrichmentFixture(t, filepath.Join(s.root, "config", "sources.yaml"), "sources:\n  launchbox:\n    snapshot:\n      url: "+provider.URL+"/Metadata.zip\nfetch:\n  metadata: [launchbox]\n")
	original, _ := os.ReadFile(filepath.Join(s.root, "platforms", "Other", "Orbit", "game.yaml"))
	for pass := 0; pass < 2; pass++ {
		op := enrichmentPlanFor(t, s, enrichmentSelection{"launchbox", "metadata"})
		if requests.Load() > int32(pass) {
			t.Fatal("plan contacted provider")
		}
		if err := s.beginEnrichment(op.ID, op.Revision); err != nil {
			t.Fatal(err)
		}
		done := waitImport(t, s, op.ID, "succeeded")
		want := 1
		if pass > 0 {
			want = 0
		}
		if done.Enrichment.Jobs[0].Counts.Written != want {
			t.Fatal(done.Enrichment)
		}
	}
	got, _ := os.ReadFile(filepath.Join(s.root, "platforms", "segacd", "Orbit", "game.yaml"))
	other, _ := os.ReadFile(filepath.Join(s.root, "platforms", "Other", "Orbit", "game.yaml"))
	if !strings.Contains(string(got), "Test Studio") || !bytes.Equal(original, other) || requests.Load() != 1 {
		t.Fatal(string(got), string(other), requests.Load())
	}
	var game map[string]any
	requestJSON(t, s, "/api/game?platform=segacd&game=Orbit", &game)
	if !strings.Contains(fmt.Sprint(game), "Test Studio") {
		t.Fatal("browser still serves old fields", game)
	}
}

func TestEnrichmentArtworkUsesSavedLinksWithoutMetadataCredentialsAndResumes(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	var picture bytes.Buffer
	png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(picture.Bytes())
	}))
	defer provider.Close()
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "segacd", "Orbit", "game.yaml"), fmt.Sprintf("schema_version: 1\ntitle: Orbit\nreleases:\n  Orbit (USA):\n    title: Orbit\nsources:\n  igdb:\n    matches:\n      - release: Orbit (USA)\n        title: Orbit\n        media:\n          box_front:\n            - url: %s/box.png\n              region: USA\n", provider.URL))
	for pass := 0; pass < 2; pass++ {
		op := enrichmentPlanFor(t, s, enrichmentSelection{"igdb", "artwork"})
		if err := s.beginEnrichment(op.ID, op.Revision); err != nil {
			t.Fatal(err)
		}
		done := waitImport(t, s, op.ID, "succeeded")
		counts := done.Enrichment.Jobs[0].Counts
		if pass == 0 && counts.Downloaded != 1 {
			t.Fatal(counts)
		}
		if pass == 1 && (counts.Downloaded != 0 || counts.Existing != 1) {
			t.Fatal(counts)
		}
	}
	if requests.Load() != 1 {
		t.Fatal("existing artwork was downloaded again", requests.Load())
	}
}
