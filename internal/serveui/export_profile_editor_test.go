package serveui

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"rommeta/internal/exporter"
	"strings"
	"testing"
)

func resolveWebProfile(t *testing.T, s *server, input exportProfileRequest) exportPreviewResponse {
	t.Helper()
	body, _ := json.Marshal(input)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/export/profile", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result exportPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestInlineProfileAndSeparatePreviewUseSameNativePolicy(t *testing.T) {
	s, _, destination := webExportFixture(t, "es-de")
	initial := resolveWebProfile(t, s, exportProfileRequest{Target: "es-de", ProfileYAML: "schema_version: 1\ntitles:\n  mode: release\nlaunchbox:\n  regions: [Japan, USA]\nselection:\n  description:\n    languages: [ja, en]\n  media: {}\n"})
	p, err := exporter.ExportProfile(s.root, initial.ProfileYAML)
	if err != nil {
		t.Fatal(err)
	}
	p.Selection.Fields["title"] = exporter.Rule{Scope: "release", Order: []string{"source"}, Sources: []string{"manual.english", "launchbox"}}
	p.Selection.Description.Sources = []string{}
	edited := resolveWebProfile(t, s, exportProfileRequest{Target: "es-de", Profile: &p})
	preview := postPreview(t, s, exportPreviewRequest{Settings: exportSettings{Target: "es-de"}, ProfileYAML: edited.ProfileYAML})
	if preview.ProfileYAML != edited.ProfileYAML {
		t.Fatal("preview did not preserve the exact edited policy")
	}
	again, err := exporter.ExportProfile(s.root, preview.ProfileYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Selection.Description.Sources) != 0 || again.Selection.Fields["title"].Sources[0] != "manual.english" || len(again.Selection.Media) != 0 || again.LaunchBox.Regions[0] != "japan" || again.Titles.Mode != "release" {
		t.Fatal("profile lost edited or advanced fields", again)
	}
	destination.ProfileYAML = edited.ProfileYAML
	op := webExportPlan(t, s, destination)
	ready := waitImport(t, s, op.ID, "ready")
	if ready.Export.Destination.ProfileYAML != preview.ProfileYAML {
		t.Fatal("native plan differs from preview")
	}
	if entries, err := os.ReadDir(destination.Path); err == nil && len(entries) > 0 {
		t.Fatal("profile/plan wrote destination")
	}
}
func TestInlineProfileRejectsInvalidInputsWithoutMutation(t *testing.T) {
	s, _, _ := webExportFixture(t, "es-de")
	for _, body := range []string{
		`{"target":"bad"}`,
		`{"target":"es-de","profile_yaml":"schema_version: 99\n"}`,
		`{"target":"es-de","profile":{"schema_version":1,"unknown":true}}`,
		`{"target":"es-de","profile_yaml":"schema_version: 1\n","profile":{"schema_version":1}}`,
		`{"target":"es-de","profile_yaml":"schema_version: 1\nselection:\n  description:\n    scope: nonsense\n"}`,
		`{"target":"es-de","profile_yaml":"schema_version: 1\n"} {}`,
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/api/export/profile", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", strings.NewReader(`{"settings":{"target":"es-de"},"profile_yaml":"schema_version: 99\n"}`)))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
