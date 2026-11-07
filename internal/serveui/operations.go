package serveui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"rommeta/internal/dat"
	"rommeta/internal/exportjob"
	"rommeta/internal/taskprogress"
)

const maxLibraryOperations = 32

// Operation records belong to this server, never to the initiating HTTP request.
// Inactive receipts expire after 24 hours, dismissal or server restart. Uploaded
// inputs are removed after success/dismissal/expiry in this process.
type libraryOperation struct {
	QueuedAt        *time.Time   `json:"queued_at,omitempty"`
	Export          *exportState `json:"export,omitempty"`
	exportJob       *exportjob.Job
	exportReviewJob *exportjob.Job
	exportDocuments map[string]string
	exportStarted   bool
	ID              string             `json:"id"`
	Kind            string             `json:"kind"`
	Enrichment      *enrichmentState   `json:"enrichment,omitempty"`
	Filename        string             `json:"filename"`
	State           string             `json:"state"`
	Created         time.Time          `json:"created"`
	Updated         time.Time          `json:"updated"`
	Revision        int                `json:"revision"`
	Member          string             `json:"member,omitempty"`
	Members         []string           `json:"members,omitempty"`
	Platform        string             `json:"platform,omitempty"`
	Progress        dat.ImportProgress `json:"progress"`
	Summary         *importSummary     `json:"summary,omitempty"`
	Error           string             `json:"error,omitempty"`
	Warning         string             `json:"warning,omitempty"`
	path            string
	stamp           string
	dir             string
}

func operationBusyState(state string) bool {
	return state == "queued" || state == "planning" || state == "importing" || state == "enriching" || state == "exporting"
}
func operationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func cloneOperation(op *libraryOperation) libraryOperation {
	result := *op
	if op.Export != nil {
		state := *op.Export
		result.Export = &state
	}
	if op.Enrichment != nil {
		state := *op.Enrichment
		state.Jobs = append([]enrichmentTask(nil), state.Jobs...)
		state.Platforms = append([]string(nil), state.Platforms...)
		state.Workers = map[int]taskprogress.Snapshot{}
		for id, p := range op.Enrichment.Workers {
			state.Workers[id] = p
		}
		result.Enrichment = &state
	}
	return result
}

func (s *server) operationSnapshot(id string) (libraryOperation, bool) {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op, ok := s.operations[id]
	if !ok {
		return libraryOperation{}, false
	}
	return cloneOperation(op), true
}
func (s *server) changeOperation(id string, update func(*libraryOperation)) {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	if op := s.operations[id]; op != nil {
		update(op)
		op.Updated = time.Now().UTC()
	}
}
func (s *server) expireOperationsLocked() {
	for id, op := range s.operations {
		if !operationBusyState(op.State) && time.Since(op.Updated) > 24*time.Hour {
			if op.dir != "" {
				_ = os.RemoveAll(op.dir)
			}
			delete(s.operations, id)
		}
	}
}
func (s *server) operationsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.operationsMu.Lock()
	s.expireOperationsLocked()
	result := make([]libraryOperation, 0, len(s.operations))
	for _, op := range s.operations {
		result = append(result, cloneOperation(op))
	}
	s.operationsMu.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, result)
}
func (s *server) operationDismiss(w http.ResponseWriter, r *http.Request) {
	var input importAction
	if !s.editRequest(w, r, &input) {
		return
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[input.ID]
	if op == nil {
		writeJSON(w, map[string]bool{"dismissed": true})
		return
	}
	if operationBusyState(op.State) && op.State != "queued" {
		writeError(w, 409, fmt.Errorf("operation is still running; closing the browser will not interrupt it"))
		return
	}
	if op.dir != "" {
		if err := os.RemoveAll(op.dir); err != nil {
			writeError(w, 500, err)
			return
		}
	}
	delete(s.operations, input.ID)
	writeJSON(w, map[string]bool{"dismissed": true})
}
