package serveui

import (
	"fmt"
	"net/http"
	"time"

	"rommeta/internal/enrichment"
)

type enrichmentRetryRequest struct {
	ID   string `json:"id"`
	Task *int   `json:"task"`
}

func (s *server) enrichmentRetry(w http.ResponseWriter, r *http.Request) {
	var input enrichmentRetryRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	if input.Task == nil {
		writeError(w, 400, fmt.Errorf("choose a failed task"))
		return
	}
	op, err := s.retryEnrichmentTask(input.ID, *input.Task)
	if err != nil {
		writeError(w, 409, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, op)
}

// A retry is a new normal pass for one terminal task. Repeated requests return
// the same child operation, even after it finishes, until that child is dismissed.
func (s *server) retryEnrichmentTask(id string, index int) (libraryOperation, error) {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	parent := s.operations[id]
	if parent == nil || parent.Enrichment == nil || index < 0 || index >= len(parent.Enrichment.Jobs) {
		return libraryOperation{}, fmt.Errorf("task is unavailable; prepare another pass")
	}
	task := &parent.Enrichment.Jobs[index]
	if task.State != "failed" && task.State != "incomplete" && task.State != "skipped" {
		return libraryOperation{}, fmt.Errorf("only failed, incomplete or skipped tasks can be retried")
	}
	if previous := s.operations[task.RetryOperation]; previous != nil {
		return cloneOperation(previous), nil
	}
	platform := task.Platform
	if platform == "" {
		platform = parent.Platform
	}
	options, config, _, err := s.enrichmentChoices(platform)
	if err != nil {
		return libraryOperation{}, err
	}
	available := false
	for _, choice := range options.Choices {
		if choice.Source == task.Source && choice.Type == task.Type {
			if !choice.Available {
				return libraryOperation{}, fmt.Errorf("%s: %s", task.Source, choice.Reason)
			}
			available = true
			break
		}
	}
	if !available {
		return libraryOperation{}, fmt.Errorf("this source task is no longer supported")
	}
	if len(s.operations)+s.uploads >= maxLibraryOperations {
		return libraryOperation{}, fmt.Errorf("dismiss an old operation before retrying")
	}
	concurrency := parent.Enrichment.Concurrency
	copyTask := enrichmentTask{Platform: platform, Source: task.Source, Type: task.Type, State: "pending",
		Workers: enrichment.Concurrency(enrichment.Options{Concurrency: concurrency, ConcurrencySet: concurrency > 0}, config, enrichment.Job{Source: task.Source, Type: task.Type})}
	now := time.Now().UTC()
	child := &libraryOperation{ID: operationID(), Kind: "enrichment", Platform: platform, State: "ready", Revision: 1, Created: now, Updated: now, stamp: options.Revision,
		Enrichment: &enrichmentState{Platforms: []string{platform}, Concurrency: concurrency, Jobs: []enrichmentTask{copyTask}}}
	s.operations[child.ID] = child
	task.RetryOperation = child.ID
	parent.Updated = now
	s.queueEnrichmentLocked(child)
	return cloneOperation(child), nil
}
