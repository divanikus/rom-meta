package serveui

import (
	"fmt"
	"net/http"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/exporter"
	"rommeta/internal/librarysettings"
)

func (s *server) librarySettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		if !s.allowEdit {
			writeError(w, 403, fmt.Errorf("library settings require serve --edit"))
			return
		}
		sections, err := librarysettings.Load(s.root)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, sections)
		return
	}
	var input librarysettings.Request
	if !s.editRequest(w, r, &input) {
		return
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another catalog operation is running"))
		return
	}
	defer s.editMu.Unlock()
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		writeError(w, 409, err)
		return
	}
	defer unlock()
	batch := catalogchanges.Begin(exporter.RefreshPlatformIndex)
	section, err := librarysettings.Save(s.root, input)
	indexErr := batch.Finish()
	if err != nil {
		writeError(w, 409, err)
		return
	}
	s.summaryMu.Lock()
	s.summaries = nil
	s.summaryMu.Unlock()
	result := map[string]any{"saved": true, "revision": section.Revision}
	if indexErr != nil {
		result["warning"] = indexErr.Error()
	}
	writeJSON(w, result)
}
