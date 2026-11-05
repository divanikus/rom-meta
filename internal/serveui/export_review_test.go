package serveui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportReviewExcludesOnlyProblemInputsAndRetainsEvidence(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			s, _, d := webExportFixture(t, target)
			if err := os.WriteFile(filepath.Join(d.ROMRoot, "Unknown.z64"), []byte("unknown payload"), 0644); err != nil {
				t.Fatal(err)
			}
			first := webExportPlan(t, s, d)
			op := waitImport(t, s, first.ID, "ready")
			if len(op.Export.Totals.Issues) != 1 {
				t.Fatalf("%+v", op.Export.Totals)
			}
			issue := op.Export.Totals.Issues[0]
			if issue.ROM != "Unknown.z64" || issue.Status != "unmatched" {
				t.Fatal(issue)
			}
			before := exportFiles(t, d.Path)
			w, _ := catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: op.Revision, Excluded: []string{d.Platform + "\nOrbit.z64"}})
			if w.Code != 400 {
				t.Fatal(w.Code, "recognized input exclusion accepted")
			}
			w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: op.Revision, Excluded: []string{issue.ID}})
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			revised := waitImport(t, s, op.ID, "ready")
			if revised.Revision != op.Revision+1 || len(revised.Export.Excluded) != 1 || !revised.Export.Totals.Issues[0].Excluded {
				t.Fatal(revised.Export)
			}
			if len(revised.exportJob.Plans[0].Records) != 1 || len(revised.exportReviewJob.Plans[0].Records) != 2 {
				t.Fatal("retained scan mutated")
			}
			if len(exportFiles(t, d.Path)) != len(before) {
				t.Fatal("review wrote destination")
			}
			w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: op.Revision})
			if w.Code != 409 {
				t.Fatal("stale review accepted")
			}
			w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: revised.Revision})
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			restored := waitImport(t, s, op.ID, "ready")
			if len(restored.exportJob.Plans[0].Records) != 2 || restored.Export.Totals.Issues[0].Excluded {
				t.Fatal("restore did not use original scan")
			}

			w, _ = catalogCall(t, s, "/api/export/review", exportReviewRequest{ID: op.ID, Revision: restored.Revision, Excluded: []string{issue.ID}})
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			final := waitImport(t, s, op.ID, "ready")
			w, _ = catalogCall(t, s, "/api/export/apply", importAction{ID: op.ID, Revision: final.Revision})
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			done := waitImport(t, s, op.ID, "succeeded")
			if done.exportReviewJob != nil {
				t.Fatal("retained completed original scan")
			}
			var output strings.Builder
			for _, body := range exportFiles(t, d.Path) {
				output.WriteString(body)
			}
			if strings.Contains(output.String(), "Unknown") || !strings.Contains(output.String(), "Orbit") {
				t.Fatal("exclusion did not reach native output")
			}

		})
	}
}
func TestExportDefaultProfileShowsResolvedPolicy(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	writeEnrichmentFixture(t, filepath.Join(s.root, "config", "sources.yaml"), "priority:\n  metadata: [custom-name, launchbox]\n")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/export/profile?target=es-de", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var shown struct {
		YAML string `json:"profile_yaml"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &shown); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.YAML, "custom-name") {
		t.Fatal("saved priorities omitted")
	}
	op := webExportPlan(t, s, d)
	ready := waitImport(t, s, op.ID, "ready")
	if shown.YAML != ready.Export.Destination.ProfileYAML {
		t.Fatal("shown policy differs from planned policy")
	}
}
