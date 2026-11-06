package serveui

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
)

func matchOptions(t *testing.T, s *server, op libraryOperation, input, game string) (exportMatchOptionsResponse, int) {
	t.Helper()
	q := url.Values{"id": {op.ID}, "revision": {fmt.Sprint(op.Revision)}, "input": {input}, "game": {game}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/export/match-options?"+q.Encode(), nil))
	var result exportMatchOptionsResponse
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return result, w.Code
}
func manualFixture(t *testing.T, target string) (*server, exportDestination, libraryOperation, string, exportjob.ManualBinding) {
	t.Helper()
	s, _, d := webExportFixture(t, target)
	writeEnrichmentFixture(t, filepath.Join(d.ROMRoot, "Unknown.z64"), "different synthetic payload")
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "n64", "Nebula", "game.yaml"), "title: Nebula\ndefault_release: Nebula (USA)\nreleases:\n  Nebula (USA): {regions: [USA]}\n  Nebula (Europe): {regions: [Europe]}\nsources:\n  launchbox:\n    status: matched\n    matches:\n      - title: Nebula\n        developer: Manual Choice Studio\n")
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "n64", "Nebula", "media", "box-front.europe.launchbox.png"), "synthetic European artwork")
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "n64", "Nebula", "media", "box-front.usa.launchbox.png"), "synthetic American artwork")
	op := webExportPlan(t, s, d)
	op = waitImport(t, s, op.ID, "ready")
	id := d.Platform + "\nUnknown.z64"
	options, code := matchOptions(t, s, op, id, "platforms/n64/Nebula")
	if code != 200 || options.Target == nil {
		t.Fatal(code, options)
	}
	b := exportjob.ManualBinding{ID: id, ManualMatch: exporter.ManualMatch{Game: options.Target.Game, Release: "Nebula (Europe)", GameRevision: options.Target.GameRevision}}
	return s, d, op, id, b
}
func TestManualExportReviewAndApply(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			s, d, op, id, b := manualFixture(t, target)
			beforeROM := exportFiles(t, d.ROMRoot)
			beforeCatalog := exportFiles(t, filepath.Join(s.root, "platforms"))
			beforeDestination := exportFiles(t, d.Path)
			list, code := matchOptions(t, s, op, id, "")
			if code != 200 || list.Total != 2 {
				t.Fatal(code, list)
			}
			originalPlan, original, ok := exportReviewInput(op.exportReviewJob, id)
			if !ok || original.Status != "unmatched" {
				t.Fatal(original)
			}
			w, _ := catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: op.Revision, Bindings: []exportjob.ManualBinding{b}})
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			revised := waitImport(t, s, op.ID, "ready")
			_, r, _ := exportReviewInput(revised.exportJob, id)
			if r.Game != b.Game || r.Release != b.Release || r.MatchedBy != "manual" || r.InputGuard != original.InputGuard || r.SHA1 != original.SHA1 {
				t.Fatalf("%+v", r)
			}
			if !strings.HasSuffix(r.Media["cover"].SourcePath, "box-front.europe.launchbox.png") || !strings.Contains(r.Media["cover"].OutputPath, "Unknown") {
				t.Fatal(r.Media)
			}
			if r.Fields["developer"].Value != "Manual Choice Studio" {
				t.Fatal(r.Fields)
			}
			if revised.exportJob.Plans[0].HashMatches != originalPlan.HashMatches || revised.Export.Totals.Matched != 2 || revised.Export.Totals.Unmatched != 0 {
				t.Fatal(revised.Export.Totals)
			}
			_, kept, _ := exportReviewInput(revised.exportReviewJob, id)
			if kept.Status != "unmatched" || kept.Game != "" {
				t.Fatal("original scan mutated")
			}
			if len(revised.Export.Bindings) != 1 || revised.Export.Totals.Issues[0].MatchedBy != "manual" {
				t.Fatal("manual selection missing after reconnect")
			}
			details := httptest.NewRecorder()
			s.ServeHTTP(details, httptest.NewRequest("GET", "/api/export/plan?id="+op.ID, nil))
			if details.Code != 200 || !strings.Contains(details.Body.String(), "\"matched_by\": \"manual\"") {
				t.Fatal(details.Body.String())
			}
			if !reflect.DeepEqual(beforeDestination, exportFiles(t, d.Path)) {
				t.Fatal("review wrote output")
			}
			// Undo returns to the original scan, then game-only binding uses default metadata.
			w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: revised.Revision})
			if w.Code != 202 {
				t.Fatal(w.Body.String())
			}
			restored := waitImport(t, s, op.ID, "ready")
			_, r, _ = exportReviewInput(restored.exportJob, id)
			if r.Status != "unmatched" || r.Game != "" || len(restored.Export.Bindings) != 0 {
				t.Fatal("undo failed")
			}
			b.Release = ""
			w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: restored.Revision, Bindings: []exportjob.ManualBinding{b}})
			if w.Code != 202 {
				t.Fatal(w.Body.String())
			}
			ready := waitImport(t, s, op.ID, "ready")
			_, r, _ = exportReviewInput(ready.exportJob, id)
			if r.Release != "" || r.MetadataRelease != "Nebula (USA)" {
				t.Fatal(r.Release, r.MetadataRelease)
			}
			w, _ = catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: ready.Revision})
			if w.Code != 202 {
				t.Fatal(w.Body.String())
			}
			waitImport(t, s, op.ID, "succeeded")
			output := fmt.Sprint(exportFiles(t, d.Path))
			if !strings.Contains(output, "Manual Choice Studio") || !strings.Contains(output, "Unknown.z64") || !strings.Contains(output, "Nebula") {
				t.Fatal(output)
			}
			if !reflect.DeepEqual(beforeROM, exportFiles(t, d.ROMRoot)) || !reflect.DeepEqual(beforeCatalog, exportFiles(t, filepath.Join(s.root, "platforms"))) {
				t.Fatal("manual review changed input data")
			}
		})
	}
}
func TestManualExportRejectsInvalidOrStaleSelection(t *testing.T) {
	for _, kind := range []string{"traversal", "other-platform", "release", "revision", "duplicate", "excluded", "recognized", "changed-card", "changed-rom"} {
		t.Run(kind, func(t *testing.T) {
			s, d, op, _, b := manualFixture(t, "es-de")
			req := exportReviewRequest{ID: op.ID, Revision: op.Revision}
			switch kind {
			case "traversal":
				b.Game = "platforms/n64/../Nebula"
			case "other-platform":
				b.Game = "platforms/other/Nebula"
			case "release":
				b.Release = "Unknown edition"
			case "revision":
				b.GameRevision = "stale"
			case "duplicate":
				req.Bindings = append(req.Bindings, b)
			case "excluded":
				req.Excluded = []string{b.ID}
			case "recognized":
				b.ID = d.Platform + "\nOrbit.z64"
			case "changed-card":
				writeEnrichmentFixture(t, filepath.Join(s.root, filepath.FromSlash(b.Game), "game.yaml"), "title: Changed\n")
			case "changed-rom":
				writeEnrichmentFixture(t, filepath.Join(d.ROMRoot, "Unknown.z64"), "changed payload")
			}
			req.Bindings = append(req.Bindings, b)
			w, _ := catalogCall(t, s, "/api/export/review", req)
			if w.Code != 400 && w.Code != 409 {
				t.Fatal(kind, w.Code, w.Body.String())
			}
			s.operationsMu.Lock()
			current := cloneOperation(s.operations[op.ID])
			s.operationsMu.Unlock()
			if current.State != "ready" || current.Revision != op.Revision || len(current.Export.Bindings) != 0 {
				t.Fatal("invalid choice changed plan")
			}
		})
	}
}
func TestManualExportSelectedCardGuardAtApply(t *testing.T) {
	s, _, op, _, b := manualFixture(t, "es-de")
	w, _ := catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: op.Revision, Bindings: []exportjob.ManualBinding{b}})
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	ready := waitImport(t, s, op.ID, "ready")
	writeEnrichmentFixture(t, filepath.Join(s.root, filepath.FromSlash(b.Game), "game.yaml"), "title: Changed after review\n")
	w, _ = catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: ready.Revision})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	failed := waitImport(t, s, op.ID, "failed")
	if failed.Export.Written != 0 || !strings.Contains(failed.Error, "catalog data changed") {
		t.Fatal(failed.Error)
	}
}
func TestManualExportPickerScopePagingAndCapability(t *testing.T) {
	s, _, op, id, _ := manualFixture(t, "es-de")
	for i := 0; i < 41; i++ {
		writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", "n64", fmt.Sprintf("Search %02d", i), "game.yaml"), "title: Search\n")
	}
	q := url.Values{"id": {op.ID}, "revision": {fmt.Sprint(op.Revision)}, "input": {id}, "q": {"Search"}, "offset": {"40"}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/export/match-options?"+q.Encode(), nil))
	var result exportMatchOptionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Total != 41 || len(result.Games) != 1 {
		t.Fatal(w.Code, result)
	}
	s.allowEdit = false
	if _, code := matchOptions(t, s, op, id, ""); code != 403 {
		t.Fatal(code)
	}
	s.allowEdit = true
	if _, code := matchOptions(t, s, op, id, "platforms/other/game"); code != 400 {
		t.Fatal(code)
	}
	op.Revision++
	if _, code := matchOptions(t, s, op, id, ""); code != 409 {
		t.Fatal(code)
	}
}

func TestSuggestedTranslationNeedsExplicitReview(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	input := "translations/en/Orbit (Japan) [T-En by Team v1.0].z64"
	writeEnrichmentFixture(t, filepath.Join(d.ROMRoot, filepath.FromSlash(input)), "synthetic patched payload")
	first := webExportPlan(t, s, d)
	op := waitImport(t, s, first.ID, "ready")
	if op.Export.Totals.Matched != 1 || op.Export.Totals.Ambiguous != 1 {
		t.Fatal(op.Export.Totals)
	}
	issue := op.Export.Totals.Issues[0]
	if !issue.CanBind || len(issue.Suggestions) != 1 || issue.Game != "" {
		t.Fatal(issue)
	}
	options, code := matchOptions(t, s, op, issue.ID, issue.Suggestions[0].Game)
	if code != 200 {
		t.Fatal(code)
	}
	binding := exportjob.ManualBinding{ID: issue.ID, ManualMatch: exporter.ManualMatch{Game: options.Target.Game, GameRevision: options.Target.GameRevision}}
	before := exportFiles(t, d.Path)
	w, _ := catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: op.Revision, Bindings: []exportjob.ManualBinding{binding}})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	reviewed := waitImport(t, s, op.ID, "ready")
	_, record, ok := exportReviewInput(reviewed.exportJob, issue.ID)
	if !ok || record.MatchedBy != "manual" || record.Release != "" || record.Variant != "" || len(record.Suggestions) != 0 {
		t.Fatal(record)
	}
	if !reflect.DeepEqual(before, exportFiles(t, d.Path)) {
		t.Fatal("review wrote frontend")
	}
	w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: reviewed.ID, Revision: reviewed.Revision})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	restored := waitImport(t, s, op.ID, "ready")
	if len(restored.Export.Totals.Issues[0].Suggestions) != 1 || restored.Export.Totals.Matched != 1 {
		t.Fatal("suggestion lost after reset")
	}
}
