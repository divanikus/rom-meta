package serveui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/librarysettings"
	"rommeta/internal/store"
	"testing"
)

func TestLibrarySettingsWriteGuardsAndRevision(t *testing.T) {
	root := t.TempDir()
	if _, err := store.Init(root, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	readonly, _ := Handler(root)
	w := httptest.NewRecorder()
	readonly.ServeHTTP(w, httptest.NewRequest("GET", "/api/catalog/settings", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	editable, _ := HandlerWithEditing(root, true)
	var sections []librarysettings.Section
	requestJSON(t, editable, "/api/catalog/settings", &sections)
	revision := sections[0].Revision
	body := map[string]any{"section": "priorities", "revision": revision, "values": map[string]any{"metadata": []string{"igdb", "launchbox"}}}
	w, _ = catalogCall(t, readonly, "/api/catalog/settings", body)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	unlock, err := catalogedit.Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	w, _ = catalogCall(t, editable, "/api/catalog/settings", body)
	unlock()
	if w.Code != 409 {
		t.Fatal("CLI lock bypassed", w.Code)
	}
	w, result := catalogCall(t, editable, "/api/catalog/settings", body)
	if w.Code != 200 || result["saved"] != true {
		t.Fatal(w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, catalogchanges.RevisionFile)); err != nil {
		t.Fatal("missing revision", err)
	}
	w, _ = catalogCall(t, editable, "/api/catalog/settings", body)
	if w.Code != 409 {
		t.Fatal("stale revision accepted", w.Code)
	}
	request := httptest.NewRequest("POST", "/api/catalog/settings", nil)
	request.Header.Set("Origin", "https://other.test")
	w = httptest.NewRecorder()
	editable.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
