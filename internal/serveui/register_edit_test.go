package serveui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"rommeta/internal/catalogedit"
)

func TestRegisterHTTPPreviewSaveRefreshAndValidation(t *testing.T) {
	root := editFixture(t)
	h, err := HandlerWithEditing(root, true)
	if err != nil {
		t.Fatal(err)
	}
	read := func() editorCard {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/catalog/edit?platform=Console&game=One&register=1", nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var card editorCard
		if e := json.Unmarshal(w.Body.Bytes(), &card); e != nil {
			t.Fatal(e)
		}
		return card
	}
	original := read()
	if original.Register == nil {
		t.Fatal("missing register")
	}
	req := editRequest{Platform: "Console", Game: "One", Revision: original.Revision, Edits: []catalogedit.Edit{}, Register: &catalogedit.DocumentEdit{Revision: original.Register.Revision, Patch: catalogedit.RegisterPatch{Releases: []catalogedit.ReleaseEdit{{Name: "Europe", Fields: catalogedit.Patch{"title": "European edition"}}}, Fingerprints: []catalogedit.FingerprintEdit{{Index: -1, Fields: catalogedit.Patch{"release": "Europe", "file": map[string]any{"name": "Europe.bin", "size": 16, "crc32": "bad"}}}}}}}
	before, _ := os.ReadFile(filepath.Join(root, "platforms", "Console", "One", "fingerprints.yaml"))
	for _, path := range []string{"/api/catalog/edit-preview", "/api/catalog/edit"} {
		w, _ := catalogCall(t, h, path, req)
		if w.Code < 400 {
			t.Fatal("invalid hash accepted", w.Body.String())
		}
	}
	req.Register.Patch.Fingerprints[0].Fields["file"].(map[string]any)["crc32"] = "aabbccdd"
	w, _ := catalogCall(t, h, "/api/catalog/edit-preview", req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	after, _ := os.ReadFile(filepath.Join(root, "platforms", "Console", "One", "fingerprints.yaml"))
	if string(before) != string(after) || read().Revision != original.Revision {
		t.Fatal("preview wrote documents")
	}
	w, _ = catalogCall(t, h, "/api/catalog/edit", req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved := read()
	if len(saved.Register.Releases) != 2 || len(saved.Register.Fingerprints) != 1 {
		t.Fatal(saved.Register)
	}
	fp := httptest.NewRecorder()
	h.ServeHTTP(fp, httptest.NewRequest("GET", "/api/fingerprints?platform=Console&game=One", nil))
	var listing struct {
		RegisteredTotal int `json:"registered_total"`
	}
	if err = json.Unmarshal(fp.Body.Bytes(), &listing); err != nil || listing.RegisteredTotal != 1 {
		t.Fatal(fp.Body.String(), err)
	}
	req.Revision = saved.Revision
	w, _ = catalogCall(t, h, "/api/catalog/edit", req)
	if w.Code != 409 {
		t.Fatal("accepted stale fingerprint revision", w.Body.String())
	}
	asset := httptest.NewRecorder()
	h.ServeHTTP(asset, httptest.NewRequest("GET", "/catalog-register.js", nil))
	if asset.Code != 200 || asset.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatal("missing editor asset")
	}
}
