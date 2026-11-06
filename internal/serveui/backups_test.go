package serveui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/catalogbackup"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfold"
	"testing"
)

func TestWebFoldBackupOptInAndManager(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "keep"}[keep], func(t *testing.T) {
			root, h := batchFoldFixture(t)
			plan := buildFoldBatch(t, h)
			w, r := catalogCall(t, h, "/api/catalog/fold-apply", foldApplyRequest{Plan: plan.ID, KeepBackup: keep, Decisions: []catalogfold.Decision{{ID: 0, Revision: 1, Action: "merge"}}})
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			entries, err := catalogbackup.List(root)
			want := 0
			if keep {
				want = 1
			}
			if err != nil || len(entries) != want {
				t.Fatal(entries, err, r)
			}
			if !keep {
				return
			}
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/api/catalog/backups", nil))
			var list struct {
				Backups []catalogbackup.Entry `json:"backups"`
			}
			json.Unmarshal(w.Body.Bytes(), &list)
			if w.Code != 200 || len(list.Backups) != 1 {
				t.Fatal(w.Body.String())
			}
			entry := list.Backups[0]
			input := backupDeleteRequest{ID: entry.ID, Revision: entry.Revision}
			w, _ = catalogCall(t, h, "/api/catalog/backups", input)
			if w.Code != 400 {
				t.Fatal(w.Body.String())
			}
			input.Confirm = true
			input.Revision = "stale"
			w, _ = catalogCall(t, h, "/api/catalog/backups", input)
			if w.Code != 409 {
				t.Fatal(w.Body.String())
			}
			input.Revision = entry.Revision
			unlock, err := catalogedit.Lock(root)
			if err != nil {
				t.Fatal(err)
			}
			w, _ = catalogCall(t, h, "/api/catalog/backups", input)
			unlock()
			if w.Code != 409 {
				t.Fatal(w.Body.String())
			}
			w, _ = catalogCall(t, h, "/api/catalog/backups", input)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			entries, _ = catalogbackup.List(root)
			if len(entries) != 0 {
				t.Fatal(entries)
			}
			if _, err := os.Stat(filepath.Join(root, "platforms", "Console")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestBackupManagerReadOnly(t *testing.T) {
	root := editFixture(t)
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/catalog/backups", nil))
		if w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
