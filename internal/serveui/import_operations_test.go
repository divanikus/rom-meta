package serveui

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/dat"
)

const webTestDAT = `<?xml version="1.0"?><datafile><header><name>Sega - Mega Drive - Genesis</name><author>No-Intro</author></header><game name="Orbit (USA)"><rom name="Orbit.bin" size="1" crc="12345678"/></game><game name="Garden (USA)"><rom name="Garden.bin" size="2" crc="87654321"/></game></datafile>`

func importTestServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	h, err := HandlerWithEditing(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	s := h.(*server)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}
func uploadTestDAT(t *testing.T, h http.Handler, name string, data []byte) (*httptest.ResponseRecorder, importOperation) {
	t.Helper()
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)
	part, err := mp.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = mp.Close()
	r := httptest.NewRequest("POST", "/api/catalog/import-upload", &body)
	r.Header.Set("Content-Type", mp.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var op importOperation
	_ = json.Unmarshal(w.Body.Bytes(), &op)
	return w, op
}
func waitImport(t *testing.T, s *server, id, state string) importOperation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		op, _ := s.importSnapshot(id)
		if op.State == state {
			return op
		}
		if !importBusy(op.State) {
			t.Fatalf("wanted %s: %#v", state, op)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("operation timeout")
	return importOperation{}
}
func TestDATImportContinuesWithoutBrowserAndApplyIsIdempotent(t *testing.T) {
	s, ts := importTestServer(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var applies atomic.Int32
	s.datImport = func(path string, options dat.ImportOptions) (dat.ImportResult, error) {
		if options.Apply {
			applies.Add(1)
			options.Progress(dat.ImportProgress{Stage: "writing", Current: 1, Total: 2, Item: "Orbit"})
			close(entered)
			<-release
		}
		return dat.Import(path, options)
	}
	w, op := uploadTestDAT(t, s, "example.dat", []byte(webTestDAT))
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	ready := waitImport(t, s, op.ID, "ready")
	if ready.Summary.Games != 2 {
		t.Fatal(ready)
	}
	if _, err := os.Stat(filepath.Join(s.root, "platforms", ready.Summary.Platform)); !os.IsNotExist(err) {
		t.Fatal("planning wrote cards")
	}
	body, _ := json.Marshal(importAction{ID: op.ID, Revision: ready.Revision})
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/catalog/import-apply", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	cancel()
	if response.StatusCode != 202 {
		close(release)
		t.Fatal(response.StatusCode)
	}
	<-entered
	// A different browser has no previous client state or original request context.
	response, err = http.Get(ts.URL + "/api/operations")
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	var operations []importOperation
	err = json.NewDecoder(response.Body).Decode(&operations)
	response.Body.Close()
	if err != nil || len(operations) != 1 || operations[0].State != "importing" || operations[0].Progress.Current != 1 {
		close(release)
		t.Fatal(operations, err)
	}
	if unlock, e := catalogedit.Lock(s.root); e == nil {
		unlock()
		close(release)
		t.Fatal("CLI writer was not excluded")
	}
	repeated, _ := catalogCall(t, s, "/api/catalog/import-apply", importAction{ID: op.ID, Revision: ready.Revision})
	if repeated.Code != 202 {
		close(release)
		t.Fatal(repeated.Body.String())
	}
	close(release)
	done := waitImport(t, s, op.ID, "succeeded")
	if applies.Load() != 1 || done.Summary.Written == 0 {
		t.Fatal(applies.Load(), done)
	}
	repeated, _ = catalogCall(t, s, "/api/catalog/import-apply", importAction{ID: op.ID, Revision: ready.Revision})
	if repeated.Code != 202 || applies.Load() != 1 {
		t.Fatal(repeated.Body.String(), applies.Load())
	}
	if _, err := os.Stat(done.dir); !os.IsNotExist(err) {
		t.Fatal("completed upload retained", err)
	}
	var platforms []platformItem
	requestJSON(t, s, "/api/platforms", &platforms)
	if len(platforms) != 1 || platforms[0].GameCount != 2 {
		t.Fatal(platforms)
	}
	if _, err := os.Stat(filepath.Join(s.root, catalogchanges.RevisionFile)); err != nil {
		t.Fatal("consumer revision missing", err)
	}
	// Compare real imported card data with the same CLI core path.
	cli := t.TempDir()
	path := filepath.Join(t.TempDir(), "example.dat")
	if err = os.WriteFile(path, []byte(webTestDAT), 0600); err != nil {
		t.Fatal(err)
	}
	options, err := dat.LibraryOptions(dat.ImportOptions{DataRoot: cli, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := dat.Import(path, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"Orbit", "Garden"} {
		for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
			a, e := os.ReadFile(filepath.Join(s.root, "platforms", done.Summary.Platform, folder, name))
			if e != nil {
				t.Fatal(e)
			}
			b, e := os.ReadFile(filepath.Join(result.PlatformPath, folder, name))
			if e != nil || !bytes.Equal(a, b) {
				t.Fatalf("web/CLI mismatch: %s/%s: %v", folder, name, e)
			}
		}
	}
}
func TestDATZIPSelectionAndChangedCatalogRequireNewPlan(t *testing.T) {
	s, _ := importTestServer(t)
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for _, name := range []string{"one.dat", "nested/two.xml"} {
		f, _ := z.Create(name)
		_, _ = io.WriteString(f, webTestDAT)
	}
	_ = z.Close()
	w, op := uploadTestDAT(t, s, "collection.zip", data.Bytes())
	if w.Code != 202 || op.State != "needs_selection" || len(op.Members) != 2 {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, s, "/api/catalog/import-plan", importAction{ID: op.ID, Revision: op.Revision, Member: "../outside.dat"})
	if w.Code != 409 {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, s, "/api/catalog/import-plan", importAction{ID: op.ID, Revision: op.Revision, Member: "nested/two.xml", Platform: "Custom Console"})
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	ready := waitImport(t, s, op.ID, "ready")
	if ready.Summary.Platform != "Custom Console" {
		t.Fatal(ready)
	}
	writeTestFile(t, filepath.Join(s.root, catalogchanges.RevisionFile), "another writer")
	w, _ = catalogCall(t, s, "/api/catalog/import-apply", importAction{ID: op.ID, Revision: ready.Revision})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "changed") {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, s, "/api/catalog/import-plan", importAction{ID: op.ID, Revision: ready.Revision, Member: "one.dat"})
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	fresh := waitImport(t, s, op.ID, "ready")
	w, _ = catalogCall(t, s, "/api/catalog/import-apply", importAction{ID: op.ID, Revision: ready.Revision})
	if w.Code != 409 {
		t.Fatal("stale revision", w.Body.String())
	}
	w, _ = catalogCall(t, s, "/api/catalog/import-dismiss", importAction{ID: op.ID, Revision: fresh.Revision})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := os.Stat(op.dir); !os.IsNotExist(err) {
		t.Fatal("dismissed upload retained")
	}
}
func TestDATUploadGuardsAndPlanningErrors(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h, err := HandlerWithEditing(t.TempDir(), enabled)
		if err != nil {
			t.Fatal(err)
		}
		if !enabled {
			w, _ := uploadTestDAT(t, h, "example.dat", []byte(webTestDAT))
			if w.Code != 403 {
				t.Fatal(w.Code)
			}
			continue
		}
		for _, name := range []string{"example.exe", "bad.zip"} {
			w, _ := uploadTestDAT(t, h, name, []byte("invalid"))
			if w.Code != 400 {
				t.Fatal(name, w.Code)
			}
		}
		r := httptest.NewRequest("POST", "/api/catalog/import-upload", nil)
		r.Header.Set("Origin", "https://other.test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
		s := h.(*server)
		w, op := uploadTestDAT(t, h, "broken.dat", []byte("not XML"))
		if w.Code != 202 {
			t.Fatal(w.Body.String())
		}
		failed := waitImport(t, s, op.ID, "failed")
		if failed.Error == "" || strings.Contains(failed.Error, s.root) {
			t.Fatal(failed.Error)
		}
		w, _ = catalogCall(t, s, "/api/catalog/import-dismiss", importAction{ID: op.ID})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		w, op = uploadTestDAT(t, h, "mame.xml", []byte("<mame build=\"0.284\"></mame>"))
		if w.Code != 202 {
			t.Fatal(w.Body.String())
		}
		failed = waitImport(t, s, op.ID, "failed")
		if !strings.Contains(failed.Error, "classified arcade") {
			t.Fatal(failed)
		}
		_, _ = catalogCall(t, s, "/api/catalog/import-dismiss", importAction{ID: op.ID})
	}
}
func TestEmptyLibraryPlatformList(t *testing.T) {
	s, _ := importTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "platforms"), 0755); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/platforms", nil))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDATImportUsesSharedLibraryPolicy(t *testing.T) {
	s, _ := importTestServer(t)
	writeTestFile(t, filepath.Join(s.root, "config", "catalog.yaml"), "fold:\n  region_priority: [japan, usa]\n")
	options, err := dat.LibraryOptions(dat.ImportOptions{DataRoot: s.root})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(options.RegionPriority) != "[japan usa]" {
		t.Fatal(options)
	}
}
