package serveui

import (
	"rommeta/internal/catalogedit"
	"strings"
	"testing"
)

func TestRawPreviewAndSaveValidateBeforeWriting(t *testing.T) {
	root := editFixture(t)
	h, e := HandlerWithEditing(root, true)
	if e != nil {
		t.Fatal(e)
	}
	card, e := catalogedit.Read(root, "Console", "One")
	if e != nil {
		t.Fatal(e)
	}
	raw := strings.Replace(card.YAML, "title: Orbit", "title: Edited", 1)
	request := editRequest{Platform: "Console", Game: "One", Revision: card.Revision, Edits: []catalogedit.Edit{{Target: catalogedit.Target{Kind: "raw"}, YAML: &raw}}}
	w, _ := catalogCall(t, h, "/api/catalog/edit-preview", request)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	unchanged, _ := catalogedit.Read(root, "Console", "One")
	if unchanged.Revision != card.Revision {
		t.Fatal("preview mutated card")
	}
	raw = "title: One\ntitle: Two\n"
	for _, endpoint := range []string{"/api/catalog/edit-preview", "/api/catalog/edit"} {
		w, _ = catalogCall(t, h, endpoint, request)
		if w.Code < 400 || !strings.Contains(w.Body.String(), "duplicate") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	unchanged, _ = catalogedit.Read(root, "Console", "One")
	if unchanged.Revision != card.Revision {
		t.Fatal("invalid raw mutated card")
	}
	raw = strings.Replace(card.YAML, "title: Orbit", "title: Edited", 1)
	w, _ = catalogCall(t, h, "/api/catalog/edit", request)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved, _ := catalogedit.Read(root, "Console", "One")
	if saved.YAML != raw {
		t.Fatal(saved.YAML)
	}
	w, _ = catalogCall(t, h, "/api/catalog/edit", request)
	if w.Code != 409 {
		t.Fatal("stale raw saved", w.Code)
	}
}
