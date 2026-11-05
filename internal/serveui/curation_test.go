package serveui

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"rommeta/internal/catalogedit"
	"rommeta/internal/curation"
	"strings"
	"testing"
)

func TestNamedSourceHTTPAndExport(t *testing.T) {
	root := editFixture(t)
	h, e := HandlerWithEditing(root, true)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := curation.Load(root)
	w, result := catalogCall(t, h, "/api/catalog/curation", curationRequest{Revision: r.Revision, Name: "Personal"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := result["source"].(map[string]any)["id"].(string)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/game?platform=Console&game=One", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Personal") {
		t.Fatal("ordinary card lacks named labels", w.Body.String())
	}
	card, _ := catalogedit.Read(root, "Console", "One")
	w, _ = catalogCall(t, h, "/api/catalog/edit", editRequest{Platform: "Console", Game: "One", Revision: card.Revision, Edits: []catalogedit.Edit{
		{Target: catalogedit.Target{Kind: "source", Source: "launchbox", Match: 0}, Fields: catalogedit.Patch{"developer": "Fixed provider"}},
		{Target: catalogedit.Target{Kind: "manual", Source: id}, Fields: catalogedit.Patch{"developer": "My studio", "descriptions": map[string]any{"en": "My prose"}}},
	}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/export-options", nil))
	if !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), "Personal") {
		t.Fatal(w.Body.String())
	}
	var opts exportSourceOptions
	if e = json.Unmarshal(w.Body.Bytes(), &opts); e != nil {
		t.Fatal(e)
	}
	// Creating or saving a variant never silently selects it.
	for _, source := range opts.Metadata {
		if source == id {
			t.Fatal("manual source enabled automatically")
		}
	}
	w, _ = catalogCall(t, h, "/api/export-preview", exportPreviewRequest{Platform: "Console", Settings: exportSettings{Language: "en", Media: "none", MetadataSources: []string{id, "launchbox"}}, Games: []previewGameRequest{{Folder: "One"}}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "My studio") || !strings.Contains(w.Body.String(), "My prose") {
		t.Fatal(w.Body.String())
	}
	r, _ = curation.Load(root)
	w, _ = catalogCall(t, h, "/api/catalog/curation", curationRequest{Revision: r.Revision, ID: id, Name: "Renamed"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, h, "/api/export-preview", exportPreviewRequest{Platform: "Console", Settings: exportSettings{Language: "en", Media: "none", MetadataSources: []string{id}}, Games: []previewGameRequest{{Folder: "One"}}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "My studio") {
		t.Fatal(w.Body.String())
	}
}
func TestNamedSourceWriteGuards(t *testing.T) {
	root := editFixture(t)
	ro, _ := Handler(root)
	h, _ := HandlerWithEditing(root, true)
	w, _ := catalogCall(t, ro, "/api/catalog/curation", curationRequest{Name: "No"})
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	req := httptest.NewRequest("POST", "http://catalog.test/api/catalog/curation", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://other.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	unlock, e := catalogedit.Lock(root)
	if e != nil {
		t.Fatal(e)
	}
	w, _ = catalogCall(t, h, "/api/catalog/curation", curationRequest{Name: "Busy"})
	unlock()
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	r, _ := curation.Load(root)
	w, _ = catalogCall(t, h, "/api/catalog/curation", curationRequest{Revision: r.Revision, Name: "One"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, h, "/api/catalog/curation", curationRequest{Revision: r.Revision, Name: "Stale"})
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}

func TestFoldRetainsNamedSourceObservations(t *testing.T) {
	root := editFixture(t)
	registry, _ := curation.Load(root)
	_, source, e := curation.Save(root, registry.Revision, "", "Personal")
	if e != nil {
		t.Fatal(e)
	}
	for _, folder := range []string{"One", "Two"} {
		card, e := catalogedit.Read(root, "Console", folder)
		if e != nil {
			t.Fatal(e)
		}
		_, e = catalogedit.SaveEdits(root, "Console", folder, card.Revision, []catalogedit.Edit{{Target: catalogedit.Target{Kind: "manual", Source: source.ID}, Fields: catalogedit.Patch{"descriptions": map[string]any{"en": "Version " + folder}}}})
		if e != nil {
			t.Fatal(e)
		}
	}
	h, e := HandlerWithEditing(root, true)
	if e != nil {
		t.Fatal(e)
	}
	w, result := catalogCall(t, h, "/api/catalog/fold-plan", foldPlanRequest{Platform: "Console"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	groups := result["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["status"] != "ready" {
		t.Fatal(w.Body.String())
	}
	w, _ = catalogCall(t, h, "/api/catalog/fold-apply", map[string]any{"plan": result["id"], "decisions": []map[string]any{{"id": 0, "revision": 1, "action": "merge"}}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var saved string
	for _, folder := range []string{"One", "Two"} {
		card, e := catalogedit.Read(root, "Console", folder)
		if e == nil {
			saved = card.YAML
		}
	}
	for _, value := range []string{source.ID, "Version One", "Version Two"} {
		if !strings.Contains(saved, value) {
			t.Fatal(saved)
		}
	}
}
