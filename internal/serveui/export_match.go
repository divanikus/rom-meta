package serveui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
)

type exportMatchOption struct {
	Game     string `json:"game"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}
type exportMatchOptionsResponse struct {
	Games  []exportMatchOption    `json:"games"`
	Total  int                    `json:"total"`
	Target *exporter.ManualTarget `json:"target,omitempty"`
}

func exportReviewInput(job *exportjob.Job, id string) (*exporter.Plan, exporter.Record, bool) {
	for _, p := range job.Plans {
		for _, record := range p.Records {
			if exportjob.InputKey(p.Platform, record.ROM) == id {
				return p, record, true
			}
		}
	}
	return nil, exporter.Record{}, false
}
func reviewableExport(op *libraryOperation) bool {
	return op != nil && op.Kind == "export" && op.exportReviewJob != nil && !op.exportStarted && (op.State == "ready" || op.State == "needs_review")
}
func (s *server) exportMatchOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.allowEdit {
		writeError(w, 403, fmt.Errorf("manual selection requires serve --edit"))
		return
	}
	q := r.URL.Query()
	revision, err := strconv.Atoi(q.Get("revision"))
	if err != nil {
		writeError(w, 400, fmt.Errorf("plan revision required"))
		return
	}
	s.operationsMu.Lock()
	op := s.operations[q.Get("id")]
	if !reviewableExport(op) || op.Revision != revision {
		s.operationsMu.Unlock()
		writeError(w, 409, fmt.Errorf("plan changed; reopen the selection"))
		return
	}
	plan, record, ok := exportReviewInput(op.exportReviewJob, q.Get("input"))
	s.operationsMu.Unlock()
	if !ok || !exporter.CanManuallyMatch(record) {
		writeError(w, 400, fmt.Errorf("select an unresolved or uncertain input from this plan"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	result := exportMatchOptionsResponse{Games: []exportMatchOption{}}
	if name := q.Get("game"); name != "" {
		target, err := exporter.ReadManualTarget(plan, name)
		if err != nil {
			writeError(w, 400, fmt.Errorf("cannot select card: %s", strings.ReplaceAll(err.Error(), s.root, "library")))
			return
		}
		result.Target = &target
	} else {
		query := strings.ToLower(strings.TrimSpace(q.Get("q")))
		if len(query) > 256 {
			writeError(w, 400, fmt.Errorf("search is too long"))
			return
		}
		offset := 0
		if value := q.Get("offset"); value != "" {
			offset, err = strconv.Atoi(value)
			if err != nil || offset < 0 {
				writeError(w, 400, fmt.Errorf("invalid offset"))
				return
			}
		}
		platforms, err := exporter.ManualMatchPlatforms(plan)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		for _, platform := range platforms {
			games, err := listGameNames(s.root, platform)
			if err != nil {
				writeError(w, 400, err)
				return
			}
			for _, game := range games {
				if !strings.Contains(strings.ToLower(game.Folder), query) {
					continue
				}
				if result.Total >= offset && len(result.Games) < 40 {
					result.Games = append(result.Games, exportMatchOption{Game: "platforms/" + platform + "/" + game.Folder, Name: game.Folder, Platform: platform})
				}
				result.Total++
			}
		}
	}
	writeJSON(w, result)
}

// Keep all originally uncertain rows visible, overlaying their reviewed match.
func reviewedExportInputs(original, current *exportjob.Job, onlyIssues bool, excluded []string) []exportIssue {
	rows := exportInputs(original, onlyIssues, excluded)
	selected := map[string]exportIssue{}
	for _, row := range exportInputs(current, false, nil) {
		selected[row.ID] = row
	}
	for i, row := range rows {
		if revised, ok := selected[row.ID]; ok && !row.Excluded {
			rows[i] = revised
		}
	}
	return rows
}
