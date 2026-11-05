package serveui

import (
	"fmt"
	"net/http"
	"rommeta/internal/catalogedit"
	"rommeta/internal/exportjob"
	"rommeta/internal/taskprogress"
	"strings"
	"time"
)

type exportIssue struct {
	ID         string   `json:"id"`
	Platform   string   `json:"platform"`
	ROM        string   `json:"rom"`
	Status     string   `json:"status"`
	Reason     string   `json:"reason,omitempty"`
	Game       string   `json:"game,omitempty"`
	Release    string   `json:"release,omitempty"`
	MatchedBy  string   `json:"matched_by,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Excluded   bool     `json:"excluded"`
}

func exportInputs(job *exportjob.Job, onlyIssues bool, excluded []string) []exportIssue {
	omit := map[string]bool{}
	for _, id := range excluded {
		omit[id] = true
	}
	result := []exportIssue{}
	if job == nil {
		return result
	}
	for _, p := range job.Plans {
		for _, r := range p.Records {
			if onlyIssues && r.Status == "matched" && r.MatchedBy != "game-name" && len(r.Candidates) < 2 {
				continue
			}
			id := exportjob.InputKey(p.Platform, r.ROM)
			result = append(result, exportIssue{ID: id, Platform: p.Platform, ROM: r.ROM, Status: r.Status, Reason: r.Reason, Game: r.Game, Release: r.Release, MatchedBy: r.MatchedBy, Candidates: r.Candidates, Warnings: r.Warnings, Excluded: omit[id]})
		}
	}
	return result
}

type exportReviewRequest struct {
	ID       string   `json:"id"`
	Revision int      `json:"revision"`
	Excluded []string `json:"excluded"`
}

func (s *server) exportReview(w http.ResponseWriter, r *http.Request) {
	var input exportReviewRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[input.ID]
	if op == nil || op.Kind != "export" || op.exportReviewJob == nil || op.exportStarted || (op.State != "ready" && op.State != "needs_review") {
		writeError(w, 409, fmt.Errorf("reviewable export plan is unavailable"))
		return
	}
	if input.Revision != op.Revision {
		writeError(w, 409, fmt.Errorf("plan changed; review it again"))
		return
	}
	allowed := map[string]bool{}
	for _, x := range exportInputs(op.exportReviewJob, true, nil) {
		allowed[x.ID] = true
	}
	excluded := map[string]bool{}
	for _, id := range input.Excluded {
		if !allowed[id] || excluded[id] {
			writeError(w, 400, fmt.Errorf("select only unresolved or uncertain inputs from this plan"))
			return
		}
		excluded[id] = true
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another library operation is running; update this plan after it finishes"))
		return
	}
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		s.editMu.Unlock()
		writeError(w, 409, err)
		return
	}
	stamp, err := s.exportStamp()
	if err == nil && stamp != op.stamp {
		err = fmt.Errorf("library changed; prepare a new export plan")
	}
	if err == nil {
		err = checkExportDocuments(op.exportDocuments)
	}
	if err != nil {
		unlock()
		s.editMu.Unlock()
		writeError(w, 409, err)
		return
	}
	op.State = "planning"
	op.Updated = time.Now().UTC()
	op.Export.Progress = taskprogress.Snapshot{Stage: "reviewing", Item: "Rebuilding selected inputs"}
	op.Error = ""
	op.Revision++
	op.Export.Excluded = append([]string(nil), input.Excluded...)
	snapshot := cloneOperation(op)
	go func() {
		defer s.editMu.Unlock()
		defer unlock()
		job, err := snapshot.exportReviewJob.WithExcluded(excluded)
		s.changeOperation(snapshot.ID, func(current *libraryOperation) {
			if err != nil {
				current.State = "failed"
				current.Error = strings.ReplaceAll(err.Error(), s.root, "library")
				return
			}
			current.exportJob = job
			current.Export.Totals = summarizeExport(job)
			current.Export.Totals.Issues = exportInputs(current.exportReviewJob, true, current.Export.Excluded)
			current.State = "ready"
			if len(job.Native.Conflicts) > 0 {
				current.State = "needs_review"
			}
		})
	}()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, cloneOperation(op))
}
