package serveui

import (
	"fmt"
	"net/http"
	"path/filepath"
	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
	"rommeta/internal/taskprogress"
	"strings"
	"time"
)

type exportIssue struct {
	CanBind    bool     `json:"can_bind"`
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
			if onlyIssues && r.Status == "matched" && r.MatchedBy != "game-name" && r.MatchedBy != "manual" && len(r.Candidates) < 2 {
				continue
			}
			id := exportjob.InputKey(p.Platform, r.ROM)
			result = append(result, exportIssue{CanBind: exporter.CanManuallyMatch(r), ID: id, Platform: p.Platform, ROM: r.ROM, Status: r.Status, Reason: r.Reason, Game: r.Game, Release: r.Release, MatchedBy: r.MatchedBy, Candidates: r.Candidates, Warnings: r.Warnings, Excluded: omit[id]})
		}
	}
	return result
}

type exportReviewRequest struct {
	Bindings []exportjob.ManualBinding `json:"bindings,omitempty"`
	ID       string                    `json:"id"`
	Revision int                       `json:"revision"`
	Excluded []string                  `json:"excluded"`
}

func (s *server) exportReview(w http.ResponseWriter, r *http.Request) {
	var input exportReviewRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[input.ID]
	if !reviewableExport(op) {
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
	seen := map[string]bool{}
	for _, b := range input.Bindings {
		if !allowed[b.ID] || excluded[b.ID] || seen[b.ID] {
			writeError(w, 400, fmt.Errorf("select each unresolved input once, either a card or exclusion"))
			return
		}
		seen[b.ID] = true
	}
	unlock, err := s.exportAccess(op.Export.Destination)
	if err != nil {
		writeError(w, 409, err)
		return
	}
	stamp, err := s.exportStamp(op.Export.Destination)
	if err == nil && stamp != op.stamp {
		err = fmt.Errorf("library changed; prepare a new export plan")
	}
	if err == nil {
		err = checkExportDocuments(op.exportDocuments)
	}
	if err == nil {
		for _, b := range input.Bindings {
			p, record, ok := exportReviewInput(op.exportReviewJob, b.ID)
			if !ok {
				err = fmt.Errorf("unknown input")
				break
			}
			if err = exporter.ValidateManualMatch(p, record, b.ManualMatch); err != nil {
				break
			}
		}
	}
	if err != nil {
		unlock()
		writeError(w, 409, err)
		return
	}
	previousState := op.State
	op.State = "planning"
	op.Updated = time.Now().UTC()
	op.Export.Progress = taskprogress.Snapshot{Stage: "reviewing", Item: "Rebuilding selected inputs"}
	op.Error = ""
	op.Revision++
	snapshot := cloneOperation(op)
	go func() {
		defer unlock()
		job, err := snapshot.exportReviewJob.WithReview(excluded, input.Bindings)
		var documents map[string]string
		if err == nil {
			documents, err = exportDocuments(job)
		}
		if err == nil {
			// Compare the selected document with the bytes actually used by the projection.
			for _, b := range input.Bindings {
				p, _, _ := exportReviewInput(snapshot.exportReviewJob, b.ID)
				if documents[filepath.Join(p.DataRoot, filepath.FromSlash(b.Game), "game.yaml")] != b.GameRevision {
					err = fmt.Errorf("selected card changed; choose it again")
					break
				}
			}
		}
		s.changeOperation(snapshot.ID, func(current *libraryOperation) {
			if err != nil {
				current.State = previousState
				current.Error = strings.ReplaceAll(err.Error(), s.root, "library")
				return
			}
			current.exportJob = job
			current.Export.Excluded = append([]string(nil), input.Excluded...)
			current.Export.Bindings = append([]exportjob.ManualBinding(nil), input.Bindings...)
			// Retain original document guards as well: restoring must remain safe.
			for path, digest := range snapshot.exportDocuments {
				documents[path] = digest
			}
			current.exportDocuments = documents
			current.Export.Totals = summarizeExport(job)
			current.Export.Totals.Issues = reviewedExportInputs(current.exportReviewJob, job, true, current.Export.Excluded)
			current.State = "ready"
			if len(job.Native.Conflicts) > 0 {
				current.State = "needs_review"
			}
		})
	}()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, cloneOperation(op))
}
