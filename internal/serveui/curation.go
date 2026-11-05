package serveui

import (
	"fmt"
	"net/http"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/curation"
	"rommeta/internal/exporter"
)

type curationRequest struct {
	Revision string `json:"revision"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
}

func (s *server) curationSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		if !s.allowEdit {
			writeError(w, 403, fmt.Errorf("catalog editing is disabled"))
			return
		}
		registry, err := curation.Load(s.root)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, registry)
		return
	}
	var input curationRequest
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
	registry, source, err := curation.Save(s.root, input.Revision, input.ID, input.Name)
	indexErr := batch.Finish()
	if err != nil {
		writeError(w, 409, err)
		return
	}
	result := map[string]any{"registry": registry, "source": source, "saved": true}
	if indexErr != nil {
		result["warning"] = indexErr.Error()
	}
	writeJSON(w, result)
}
