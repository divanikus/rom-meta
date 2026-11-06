package serveui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
	"rommeta/internal/exportsettings"
	"rommeta/internal/frontend"
	"rommeta/internal/taskprogress"
)

type nativeExportRequest struct {
	Destination exportDestination `json:"destination"`
}
type exportTotals struct {
	Issues      []exportIssue            `json:"issues"`
	Matched     int                      `json:"matched"`
	Unmatched   int                      `json:"unmatched"`
	Ambiguous   int                      `json:"ambiguous"`
	Unsupported int                      `json:"unsupported"`
	Files       int                      `json:"files"`
	Games       int                      `json:"games"`
	Changes     []frontend.Change        `json:"changes"`
	Conflicts   []string                 `json:"conflicts"`
	Warnings    []string                 `json:"warnings"`
	Skipped     []exporter.SkippedSystem `json:"skipped"`
}
type exportState struct {
	Bindings    []exportjob.ManualBinding `json:"bindings,omitempty"`
	Excluded    []string                  `json:"excluded,omitempty"`
	Destination exportDestination         `json:"destination"`
	Totals      *exportTotals             `json:"totals,omitempty"`
	Progress    taskprogress.Snapshot     `json:"progress"`
	Written     int                       `json:"written"`
}

func (s *server) exportStamp() (string, error) {
	hash := sha256.New()
	for _, name := range []string{catalogchanges.RevisionFile, "config/catalog.yaml", "config/platforms.yaml", "config/sources.yaml"} {
		data, err := os.ReadFile(filepath.Join(s.root, name))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write(data)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
func exportDocuments(job *exportjob.Job) (map[string]string, error) {
	result := map[string]string{}
	for _, plan := range job.Plans {
		for _, record := range plan.Records {
			if record.Game == "" {
				continue
			}
			for _, base := range []string{"game.yaml", "fingerprints.yaml"} {
				path := filepath.Join(plan.DataRoot, filepath.FromSlash(record.Game), base)
				if _, ok := result[path]; ok {
					continue
				}
				data, err := catalogfs.ReadFile(path)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return nil, err
				}
				result[path] = fmt.Sprintf("%x", sha256.Sum256(data))
			}
		}
	}
	return result, nil
}
func checkExportDocuments(expected map[string]string) error {
	for path, want := range expected {
		data, err := catalogfs.ReadFile(path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != want {
			return fmt.Errorf("selected catalog data changed; prepare a new export plan")
		}
	}
	return nil
}
func (s *server) exportPlan(w http.ResponseWriter, r *http.Request) {
	var input nativeExportRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	d := input.Destination
	if err := exportsettings.ResolveDestinationProfile(s.realRoot, &d); err != nil {
		writeError(w, 400, err)
		return
	}
	// Keep an immutable policy snapshot in this operation, independent of later saved edits.
	d.ProfileID = ""
	if d.Name == "" {
		d.Name = "Export"
	}
	if err := validateExportDestination(&d); err != nil {
		writeError(w, 400, err)
		return
	}
	if d.ROMRoot == "" {
		writeError(w, 400, fmt.Errorf("choose a ROM folder on the server"))
		return
	}
	if info, err := os.Stat(d.ROMRoot); err != nil || !info.IsDir() {
		writeError(w, 400, fmt.Errorf("ROM folder is unavailable on the server"))
		return
	}
	if d.All {
		d.Platform = ""
	} else if !catalogedit.Component(d.Platform) {
		writeError(w, 400, fmt.Errorf("choose a platform or all platform folders"))
		return
	}
	profileStamp, err := s.exportStamp()
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if strings.TrimSpace(d.ProfileYAML) == "" {
		d.ProfileYAML = "schema_version: 1\n"
	}
	profile, err := exporter.ExportProfile(s.root, d.ProfileYAML)
	if err != nil {
		writeError(w, 400, fmt.Errorf("profile: %w", err))
		return
	}
	if err = exporter.ValidateTargetMedia(profile, d.Target); err != nil {
		writeError(w, 400, err)
		return
	}
	// Freeze and expose the resolved policy, including inherited source priorities.
	resolved, err := portableExportProfile(profile)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	d.ProfileYAML = string(resolved)
	// Configuration revisions still guard the plan.
	if d.ProfileName == "" {
		d.ProfileName = "Library settings"
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	s.expireOperationsLocked()
	if len(s.operations)+s.uploads >= maxLibraryOperations {
		writeError(w, 409, fmt.Errorf("dismiss an old operation before planning another"))
		return
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another library operation is running; try again when it finishes"))
		return
	}
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		s.editMu.Unlock()
		writeError(w, 409, err)
		return
	}
	stamp, err := s.exportStamp()
	if err == nil && stamp != profileStamp {
		err = fmt.Errorf("library changed while reading the profile; prepare again")
	}
	if err != nil {
		unlock()
		s.editMu.Unlock()
		writeError(w, 400, err)
		return
	}
	op := &libraryOperation{ID: operationID(), Kind: "export", State: "planning", Platform: d.Platform, Revision: 1, Created: time.Now().UTC(), Updated: time.Now().UTC(), stamp: stamp, Export: &exportState{Destination: d, Progress: taskprogress.Snapshot{Stage: "starting"}}}
	if s.operations == nil {
		s.operations = map[string]*libraryOperation{}
	}
	s.operations[op.ID] = op
	snapshot := cloneOperation(op)
	go func() { defer s.editMu.Unlock(); defer unlock(); s.runExportPlan(snapshot) }()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, snapshot)
}
func (s *server) exportProgress(id string, applying bool) func(string, string, int, int) {
	return func(stage, name string, current, total int) {
		s.changeOperation(id, func(op *libraryOperation) {
			op.Export.Progress = taskprogress.Snapshot{Stage: stage, Item: name, Current: current, Total: total}
			if applying && stage == "write" {
				op.Export.Written = current
			}
		})
	}
}
func summarizeExport(job *exportjob.Job) *exportTotals {
	n := job.Native
	totals := &exportTotals{Issues: exportInputs(job, true, nil), Files: len(n.Files), Games: len(n.Changes), Changes: []frontend.Change{}, Conflicts: n.Conflicts, Warnings: n.Warnings, Skipped: job.Skipped}
	for _, p := range job.Plans {
		totals.Matched += p.Matched
		totals.Unmatched += p.Unmatched
		totals.Ambiguous += p.Ambiguous
		totals.Unsupported += p.Unsupported
	}
	limit := min(100, len(n.Changes))
	totals.Changes = append(totals.Changes, n.Changes[:limit]...)
	return totals
}
func (s *server) runExportPlan(op libraryOperation) {
	d := op.Export.Destination
	options := exporter.Options{Context: context.Background(), DataRoot: s.root, ROMRoot: d.ROMRoot, Platform: d.Platform, Target: d.Target, ProfileContent: d.ProfileYAML, Progress: s.exportProgress(op.ID, false)}
	run := s.exportBuild
	if run == nil {
		run = func(o exporter.Options, destination, mediaRoot string, all bool) (*exportjob.Job, error) {
			return exportjob.Build(o, "", nil, all, destination, mediaRoot, "")
		}
	}
	job, err := run(options, d.Path, d.MediaRoot, d.All)
	var documents map[string]string
	if err == nil {
		documents, err = exportDocuments(job)
	}
	if err == nil {
		stamp, e := s.exportStamp()
		if e != nil || stamp != op.stamp {
			err = fmt.Errorf("library changed while preparing the export; prepare a new plan")
		}
	}
	s.changeOperation(op.ID, func(current *libraryOperation) {
		if err != nil {
			current.State = "failed"
			current.Error = err.Error()
			return
		}
		current.exportJob = job
		current.exportReviewJob = job
		current.exportDocuments = documents
		current.Export.Totals = summarizeExport(job)
		current.State = "ready"
		if len(job.Native.Conflicts) > 0 {
			current.State = "needs_review"
		}
		current.Export.Progress = taskprogress.Snapshot{Stage: "planned"}
	})
}
func (s *server) exportApply(w http.ResponseWriter, r *http.Request) {
	var input importAction
	if !s.editRequest(w, r, &input) {
		return
	}
	if err := s.beginExport(input.ID, input.Revision); err != nil {
		s.changeOperation(input.ID, func(op *libraryOperation) {
			if op.Kind == "export" && !op.exportStarted {
				op.Error = err.Error()
			}
		})
		writeError(w, 409, err)
		return
	}
	op, _ := s.operationSnapshot(input.ID)
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, op)
}
func (s *server) beginExport(id string, revision int) error {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[id]
	if op == nil || op.Kind != "export" {
		return fmt.Errorf("export plan is unavailable; prepare it again")
	}
	if op.exportStarted {
		return nil
	}
	if op.State != "ready" || op.exportJob == nil {
		return fmt.Errorf("prepare a conflict-free export plan first")
	}
	if op.Revision != revision {
		return fmt.Errorf("export plan changed; review the current plan")
	}
	if !s.editMu.TryLock() {
		return fmt.Errorf("another library operation is running; try again when it finishes")
	}
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		s.editMu.Unlock()
		return err
	}
	stamp, err := s.exportStamp()
	if err != nil || stamp != op.stamp {
		unlock()
		s.editMu.Unlock()
		return fmt.Errorf("library settings or catalog changed; prepare a new export plan")
	}
	op.State = "exporting"
	op.Export.Progress = taskprogress.Snapshot{Stage: "checking", Item: "Reviewed inputs and destination"}
	op.exportStarted = true
	op.Error = ""
	op.Updated = time.Now().UTC()
	snapshot := cloneOperation(op)
	go func() { defer s.editMu.Unlock(); defer unlock(); s.runExportApply(snapshot) }()
	return nil
}
func (s *server) runExportApply(op libraryOperation) {
	job := op.exportJob
	job.Native.Progress = s.exportProgress(op.ID, true)
	err := checkExportDocuments(op.exportDocuments)
	if err == nil {
		run := s.exportWrite
		if run == nil {
			run = func(p *frontend.Plan) error { return p.Apply() }
		}
		err = run(job.Native)
	}
	s.changeOperation(op.ID, func(current *libraryOperation) {
		current.State = "succeeded"
		if err != nil {
			current.Error = err.Error()
			current.State = "failed"
			if current.Export.Written > 0 {
				current.State = "incomplete"
			}
		}
		current.Export.Progress.Stage = "complete"
		// Release all retained native fields and asset closures after an accepted apply.
		current.exportJob = nil
		current.exportReviewJob = nil
		current.exportDocuments = nil
	})
}
func (s *server) exportPlanDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.allowEdit {
		writeError(w, 403, fmt.Errorf("export requires serve --edit"))
		return
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[r.URL.Query().Get("id")]
	if op == nil || op.Kind != "export" || op.exportJob == nil || op.State == "planning" || op.State == "exporting" {
		writeError(w, 409, fmt.Errorf("reviewable plan is unavailable"))
		return
	}
	n := op.exportJob.Native
	files := append([]string(nil), n.Files...)
	sort.Strings(files)
	w.Header().Set("Content-Disposition", "attachment; filename=rommeta-export-plan.json")
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"destination": op.Export.Destination, "totals": op.Export.Totals, "games": n.Changes, "bindings": op.Export.Bindings, "inputs": reviewedExportInputs(op.exportReviewJob, op.exportJob, false, op.Export.Excluded), "changed_files": files, "conflicts": n.Conflicts, "warnings": n.Warnings})
}
