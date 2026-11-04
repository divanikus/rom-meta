package serveui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, folder := range []string{"One", "Two"} {
		dir := filepath.Join(root, "platforms", "Console", folder)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		region := "USA"
		if folder == "Two" {
			region = "Japan"
		}
		body := "title: Orbit\nreleases:\n  Orbit (" + region + "): {}\ncustom_note: keep\nsources:\n  launchbox:\n    matches:\n      - database_id: '10'\n        title: Orbit\n"
		if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fingerprints.yaml"), []byte("items: []\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func catalogCall(t *testing.T, h http.Handler, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(w.Body.String(), err)
	}
	return w, result
}
func TestCatalogEditorReadOnlyAndOriginProtection(t *testing.T) {
	root := editFixture(t)
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := catalogCall(t, h, "/api/catalog/fold-plan", map[string]any{"platform": "Console"})
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	writable, err := HandlerWithEditing(root, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://other.example", "null"} {
		r := httptest.NewRequest("POST", "http://catalog.test/api/catalog/edit", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		writable.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/catalog/edit", strings.NewReader("{}"))
	w = httptest.NewRecorder()
	writable.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
}
func TestCatalogEditPreviewSaveAndStaleHTTP(t *testing.T) {
	root := editFixture(t)
	h, err := HandlerWithEditing(root, true)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/catalog/edit?platform=Console&game=One", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var original editorCard
	if err := json.Unmarshal(w.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	req := editRequest{Platform: "Console", Game: "One", Revision: original.Revision, Fields: map[string]any{"description": "My description"}}
	preview, _ := catalogCall(t, h, "/api/catalog/edit-preview", req)
	if preview.Code != 200 {
		t.Fatal(preview.Body.String())
	}
	saved, _ := catalogCall(t, h, "/api/catalog/edit", req)
	if saved.Code != 200 {
		t.Fatal(saved.Body.String())
	}
	stale, _ := catalogCall(t, h, "/api/catalog/edit", req)
	if stale.Code != 409 {
		t.Fatal(stale.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(root, "platforms", "Console", "One", "game.yaml"))
	if !strings.Contains(string(data), "custom_note: keep") || !strings.Contains(string(data), "My description") {
		t.Fatal(string(data))
	}
}
func TestCatalogFoldPlanApplyAndConsumedHTTP(t *testing.T) {
	root := editFixture(t)
	h, err := HandlerWithEditing(root, true)
	if err != nil {
		t.Fatal(err)
	}
	w, response := catalogCall(t, h, "/api/catalog/fold-plan", foldPlanRequest{Platform: "Console"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := response["id"].(string)
	groups := response["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["status"] != "ready" {
		t.Fatal(response)
	}
	w, _ = catalogCall(t, h, "/api/catalog/fold-apply", map[string]any{"plan": id, "decisions": []map[string]any{{"id": 0, "revision": 1, "action": "merge"}}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, h, "/api/catalog/fold-apply", map[string]any{"plan": id, "decisions": []map[string]any{{"id": 0, "revision": 1, "action": "merge"}}})
	if w.Code != 409 {
		t.Fatal(w.Body.String())
	}
}
