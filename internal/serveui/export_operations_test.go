package serveui

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rommeta/internal/catalogedit"
	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
	"rommeta/internal/frontend"
)

func webExportFixture(t *testing.T, target string) (*server, *httptest.Server, exportDestination) {
	t.Helper()
	s, ts := importTestServer(t)
	roms := t.TempDir()
	destination := t.TempDir()
	put := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	card := filepath.Join(s.root, "platforms", "n64", "Orbit")
	payload := "synthetic ROM bytes"
	put(filepath.Join(s.root, "platforms", "n64", "_platform.yaml"), "name: Nintendo - Nintendo 64\n")
	put(filepath.Join(card, "game.yaml"), "title: Orbit\ndefault_release: Orbit (USA)\nreleases:\n  Orbit (USA): {regions: [USA]}\nsources:\n  launchbox:\n    status: matched\n    matches:\n      - title: Orbit\n        developer: Synthetic Studio\n")
	put(filepath.Join(card, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Orbit (USA)\n    file: {name: Orbit.z64, size: %d, sha1: '%x'}\n", len(payload), sha1.Sum([]byte(payload))))
	put(filepath.Join(roms, "Orbit.z64"), payload)
	if target == "launchbox" {
		if err := os.MkdirAll(filepath.Join(destination, "Data", "Platforms"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return s, ts, exportDestination{Name: "Test frontend", Target: target, Path: destination, ROMRoot: roms, Platform: "n64"}
}
func webExportPlan(t *testing.T, s *server, d exportDestination) libraryOperation {
	t.Helper()
	w, _ := catalogCall(t, s, "/api/export/prepare", nativeExportRequest{Destination: d})
	if w.Code != 202 {
		t.Fatalf("prepare %d %s", w.Code, w.Body.String())
	}
	var op libraryOperation
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	return op
}
func exportFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		out[p] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestWebExportReviewedApplyAndNoOpRepeat(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			s, _, d := webExportFixture(t, target)
			cardPath := filepath.Join(s.root, "platforms", "n64", "Orbit", "game.yaml")
			cardBefore, _ := os.ReadFile(cardPath)
			romBefore := exportFiles(t, d.ROMRoot)
			destBefore := exportFiles(t, d.Path)
			for pass := 0; pass < 2; pass++ {
				op := webExportPlan(t, s, d)
				ready := waitImport(t, s, op.ID, "ready")
				if ready.Export.Totals.Matched != 1 {
					t.Fatal(ready.Export.Totals)
				}
				if pass == 0 && len(exportFiles(t, d.Path)) != len(destBefore) {
					t.Fatal("plan wrote destination")
				}
				if pass == 1 && ready.Export.Totals.Files != 0 {
					t.Fatal("repeat changed destination", ready.Export.Totals)
				}
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest("GET", "/api/export/plan?id="+op.ID, nil))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "changed_files") {
					t.Fatal(w.Code, w.Body.String())
				}
				w, _ = catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: op.Revision})
				if w.Code != 202 {
					t.Fatal(w.Body.String())
				}
				done := waitImport(t, s, op.ID, "succeeded")
				if done.Export.Written != ready.Export.Totals.Files {
					t.Fatal("incorrect completed writes", done.Export.Written)
				}
				w, _ = catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: op.Revision})
				if w.Code != 202 {
					t.Fatal("retry not idempotent")
				}
				if done.exportJob != nil {
					t.Fatal("retained completed native plan")
				}
			}
			after, _ := os.ReadFile(cardPath)
			if !bytes.Equal(after, cardBefore) {
				t.Fatal("export changed catalog payload")
			}
			if fmt.Sprint(exportFiles(t, d.ROMRoot)) != fmt.Sprint(romBefore) {
				t.Fatal("export changed ROMs")
			}
			output := fmt.Sprint(exportFiles(t, d.Path))
			if !strings.Contains(output, "Synthetic Studio") || !strings.Contains(output, "Orbit") {
				t.Fatal("missing native metadata", output)
			}
		})
	}
}
func TestWebExportPlanningAndWritingSurviveRequestDisconnect(t *testing.T) {
	s, ts, d := webExportFixture(t, "es-de")
	planning, releasePlan := make(chan struct{}), make(chan struct{})
	s.exportBuild = func(o exporter.Options, destination, media string, all bool) (*exportjob.Job, error) {
		close(planning)
		<-releasePlan
		if err := o.Context.Err(); err != nil {
			t.Error("request cancelled worker", err)
		}
		return exportjob.Build(o, "", nil, all, destination, media, "")
	}
	body, _ := json.Marshal(nativeExportRequest{Destination: d})
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/export/prepare", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var op libraryOperation
	json.NewDecoder(response.Body).Decode(&op)
	response.Body.Close()
	cancel()
	<-planning
	if unlock, err := catalogedit.Lock(s.root); err == nil {
		unlock()
		t.Fatal("planning did not hold catalog lock")
	}
	w, _ := catalogCall(t, s, "/api/operations/dismiss", importAction{ID: op.ID})
	if w.Code != 409 {
		t.Fatal("dismissed active plan")
	}
	close(releasePlan)
	waitImport(t, s, op.ID, "ready")
	writing, releaseWrite := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.exportWrite = func(plan *frontend.Plan) error {
		calls.Add(1)
		plan.Progress("write", "synthetic.xml", 0, len(plan.Files))
		close(writing)
		<-releaseWrite
		return plan.Apply()
	}
	body, _ = json.Marshal(importAction{ID: op.ID, Revision: op.Revision})
	ctx, cancel = context.WithCancel(context.Background())
	request, _ = http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/export/apply", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	cancel()
	<-writing
	snapshot, _ := s.operationSnapshot(op.ID)
	if snapshot.State != "exporting" || snapshot.Export.Progress.Item != "synthetic.xml" {
		t.Fatal(snapshot)
	}
	response, err = http.Get(ts.URL + "/api/operations")
	if err != nil {
		t.Fatal(err)
	}
	var ops []libraryOperation
	json.NewDecoder(response.Body).Decode(&ops)
	response.Body.Close()
	if len(ops) != 1 || ops[0].ID != op.ID {
		t.Fatal(ops)
	}
	w, _ = catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: op.Revision})
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	close(releaseWrite)
	waitImport(t, s, op.ID, "succeeded")
	if calls.Load() != 1 {
		t.Fatal("apply repeated")
	}
}
func TestWebExportStaleInputsAndWriterGuards(t *testing.T) {
	for _, changed := range []string{"rom", "destination", "catalog", "settings"} {
		t.Run(changed, func(t *testing.T) {
			s, _, d := webExportFixture(t, "es-de")
			op := webExportPlan(t, s, d)
			ready := waitImport(t, s, op.ID, "ready")
			unlock, err := catalogedit.Lock(s.root)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.beginExport(op.ID, op.Revision); err == nil {
				t.Fatal("ignored catalog writer")
			}
			unlock()
			switch changed {
			case "rom":
				os.WriteFile(filepath.Join(d.ROMRoot, "Orbit.z64"), []byte("changed ROM payload"), 0644)
			case "destination":
				path := filepath.Join(d.Path, "gamelists", "n64", "gamelist.xml")
				os.MkdirAll(filepath.Dir(path), 0755)
				os.WriteFile(path, []byte("<gameList><game><name>Existing</name></game></gameList>"), 0644)
			case "catalog":
				os.WriteFile(filepath.Join(s.root, "platforms", "n64", "Orbit", "game.yaml"), []byte("title: Changed\n"), 0644)
			case "settings":
				os.MkdirAll(filepath.Join(s.root, "config"), 0755)
				os.WriteFile(filepath.Join(s.root, "config", "sources.yaml"), []byte("priority:\n  metadata: [igdb]\n"), 0644)
			}
			before := fmt.Sprint(exportFiles(t, d.Path))
			w, _ := catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: ready.Revision})
			if changed == "settings" {
				if w.Code != 409 {
					t.Fatal(w.Code, w.Body.String())
				}
			} else {
				if w.Code != 202 {
					t.Fatal(w.Code, w.Body.String())
				}
				failed := waitImport(t, s, op.ID, "failed")
				if failed.Export.Written != 0 {
					t.Fatal("stale apply wrote files")
				}
			}
			if fmt.Sprint(exportFiles(t, d.Path)) != before {
				t.Fatal("stale plan changed destination")
			}
		})
	}
}
func TestWebExportDestinationsPersistAndPreserveUnknownFields(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	_, initial, _, err := s.readExportDestinations()
	if err != nil {
		t.Fatal(err)
	}
	w, _ := catalogCall(t, s, "/api/export/destinations", exportDestinationRequest{Revision: initial.Revision, Destination: d})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved exportDestinationsResponse
	json.Unmarshal(w.Body.Bytes(), &saved)
	if len(saved.Destinations) != 1 || saved.Destinations[0].ID == "" {
		t.Fatal(saved)
	}
	path, _ := s.exportDestinationsFile()
	data, _ := os.ReadFile(path)
	data = append(data, []byte("owner_note: keep me\n")...)
	os.WriteFile(path, data, 0600)
	_, current, _, _ := s.readExportDestinations()
	d = saved.Destinations[0]
	d.Name = "Renamed"
	w, _ = catalogCall(t, s, "/api/export/destinations", exportDestinationRequest{Revision: saved.Revision, Destination: d})
	if w.Code != 409 {
		t.Fatal("accepted stale settings")
	}
	w, _ = catalogCall(t, s, "/api/export/destinations", exportDestinationRequest{Revision: current.Revision, Destination: d})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	data, _ = os.ReadFile(path)
	if !bytes.Contains(data, []byte("owner_note: keep me")) {
		t.Fatal("lost unknown YAML")
	}
	h, err := HandlerWithEditing(s.root, true)
	if err != nil {
		t.Fatal(err)
	}
	_, reopened, _, _ := h.(*server).readExportDestinations()
	if reopened.Destinations[0].Name != "Renamed" {
		t.Fatal("destination did not survive restart")
	}
	w, _ = catalogCall(t, s, "/api/export/destinations", exportDestinationRequest{Revision: reopened.Revision, Destination: d, Delete: true})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err = os.Stat(d.Path); err != nil {
		t.Fatal("delete removed frontend folder")
	}
}
func TestWebExportValidatesAccessProfileAndFolders(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	for _, bad := range []exportDestination{
		{Name: "x", Target: "other", Path: d.Path, ROMRoot: d.ROMRoot, Platform: "n64"},
		{Name: "x", Target: "es-de", Path: "relative", ROMRoot: d.ROMRoot, Platform: "n64"},
		{Name: "x", Target: "es-de", Path: d.Path, ROMRoot: d.ROMRoot, Platform: "../n64"},
		{Name: "x", Target: "es-de", Path: d.Path, ROMRoot: d.ROMRoot, Platform: "n64", ProfileYAML: "schema_version: 99\n"},
	} {
		w, _ := catalogCall(t, s, "/api/export/prepare", nativeExportRequest{Destination: bad})
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	data, _ := json.Marshal(nativeExportRequest{Destination: d})
	r := httptest.NewRequest("POST", "/api/export/prepare", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://elsewhere.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin export accepted")
	}
	h, _ := Handler(s.root)
	for _, path := range []string{"/api/export/destinations", "/api/export/folders"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 403 {
			t.Fatal("read-only path listing", path)
		}
	}
	w, _ = catalogCall(t, h, "/api/export/prepare", nativeExportRequest{Destination: d})
	if w.Code != 403 {
		t.Fatal("read-only export accepted")
	}
}

func TestWebExportBatchInlineProfileAndPartialFailure(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	parent := t.TempDir()
	nested := filepath.Join(parent, "n64")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(d.ROMRoot, "Orbit.z64"))
	os.WriteFile(filepath.Join(nested, "Orbit.z64"), data, 0644)
	d.ROMRoot = parent
	d.All = true
	d.Platform = ""
	d.ProfileYAML = "schema_version: 1\nselection:\n  media: {}\n  fields:\n    title:\n      sources: [launchbox]\n"
	op := webExportPlan(t, s, d)
	ready := waitImport(t, s, op.ID, "ready")
	if ready.Export.Totals.Matched != 1 || len(ready.exportJob.Plans) != 1 {
		t.Fatal("batch did not use selected profile", ready.Export.Totals)
	}
	calls := 0
	s.exportWrite = func(p *frontend.Plan) error {
		calls++
		p.Progress("write", "synthetic partial write", 1, 2)
		return fmt.Errorf("synthetic destination failure")
	}
	if err := s.beginExport(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	done := waitImport(t, s, op.ID, "incomplete")
	if done.Export.Written != 1 || done.Error == "" {
		t.Fatal("lost partial result", done)
	}
	if err := s.beginExport(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("replayed failed apply")
	}
}
