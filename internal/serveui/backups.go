package serveui

import (
	"fmt"
	"net/http"
	"rommeta/internal/catalogbackup"
	"rommeta/internal/catalogedit"
)

type backupDeleteRequest struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Confirm  bool   `json:"confirm"`
}

func (s *server) backups(w http.ResponseWriter, r *http.Request) {
	var input backupDeleteRequest
	if r.Method != http.MethodGet {
		if !s.editRequest(w, r, &input) {
			return
		}
		if !input.Confirm {
			writeError(w, 400, fmt.Errorf("confirm backup deletion"))
			return
		}
	} else if !s.allowEdit {
		writeError(w, 403, fmt.Errorf("catalog editing is disabled"))
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
	if r.Method == http.MethodPost {
		if err = catalogbackup.Delete(s.root, input.ID, input.Revision); err != nil {
			writeError(w, 409, err)
			return
		}
	}
	entries, err := catalogbackup.List(s.root)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"backups": entries})
}
