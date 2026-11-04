package serveui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfold"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"time"
)

type editRequest struct {
	Platform string            `json:"platform"`
	Game     string            `json:"game"`
	Revision string            `json:"revision"`
	Fields   catalogedit.Patch `json:"fields"`
}
type editorCard struct {
	catalogedit.Card
	Profile gameProfile `json:"profile"`
}
type foldPlanRequest struct {
	Platform string   `json:"platform"`
	Games    []string `json:"games,omitempty"`
}
type foldPreviewRequest struct {
	Plan  string             `json:"plan"`
	Group int                `json:"group"`
	Draft *catalogfold.Draft `json:"draft,omitempty"`
}
type foldApplyRequest struct {
	Continue  bool                   `json:"continue,omitempty"`
	Plan      string                 `json:"plan"`
	Decisions []catalogfold.Decision `json:"decisions"`
}
type storedPlan struct {
	plan    *catalogfold.Plan
	created time.Time
}
type foldPlanResponse struct {
	ID       string                     `json:"id"`
	Platform string                     `json:"platform"`
	Groups   []catalogfold.PlannedGroup `json:"groups"`
}

func (s *server) catalogCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, map[string]any{"editing": s.allowEdit, "fields": catalogedit.Fields})
}
func (s *server) editRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, 405, fmt.Errorf("use POST"))
		return false
	}
	if !s.allowEdit {
		writeError(w, 403, fmt.Errorf("catalog editing is disabled; start serve with --edit"))
		return false
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		writeError(w, 415, fmt.Errorf("use application/json"))
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			writeError(w, 403, fmt.Errorf("cross-origin catalog actions are not allowed"))
			return false
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		writeError(w, 403, fmt.Errorf("cross-site catalog actions are not allowed"))
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, 400, err)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, fmt.Errorf("send one JSON object"))
		return false
	}
	return true
}
func (s *server) editorCard(card catalogedit.Card, platform string) editorCard {
	doc, err := catalogyaml.Parse([]byte(card.YAML))
	if err != nil {
		return editorCard{Card: card}
	}
	return editorCard{Card: card, Profile: libraryProfile(doc.Root(), catalogyaml.Scalar(catalogyaml.Value(doc.Root(), "title")), platform, sourceBlocks(doc, nil))}
}
func (s *server) catalogEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.allowEdit {
			writeError(w, 403, fmt.Errorf("catalog editing is disabled"))
			return
		}
		platform := s.resolvePlatform(r.URL.Query().Get("platform"))
		card, err := catalogedit.Read(s.root, platform, r.URL.Query().Get("game"))
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, s.editorCard(card, platform))
		return
	}
	var request editRequest
	if !s.editRequest(w, r, &request) {
		return
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another catalog operation is running"))
		return
	}
	defer s.editMu.Unlock()
	request.Platform = s.resolvePlatform(request.Platform)
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		writeError(w, 409, err)
		return
	}
	defer unlock()
	batch := catalogchanges.Begin(exporter.RefreshPlatformIndex)
	card, err := catalogedit.Save(s.root, request.Platform, request.Game, request.Revision, request.Fields)
	indexErr := batch.Finish()
	if err != nil {
		writeError(w, 409, err)
		return
	}
	s.summaryMu.Lock()
	s.summaries = nil
	s.summaryMu.Unlock()
	result := map[string]any{"card": s.editorCard(card, request.Platform), "saved": true}
	if indexErr != nil {
		result["warning"] = indexErr.Error()
	}
	writeJSON(w, result)
}
func (s *server) catalogEditPreview(w http.ResponseWriter, r *http.Request) {
	var request editRequest
	if !s.editRequest(w, r, &request) {
		return
	}
	request.Platform = s.resolvePlatform(request.Platform)
	card, err := catalogedit.Read(s.root, request.Platform, request.Game)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if card.Revision != request.Revision {
		writeError(w, 409, fmt.Errorf("card changed; reload before previewing"))
		return
	}
	data, err := catalogedit.Apply([]byte(card.YAML), request.Fields)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	preview, err := catalogedit.Inspect(data)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, s.editorCard(preview, request.Platform))
}
func (s *server) foldPlan(w http.ResponseWriter, r *http.Request) {
	var request foldPlanRequest
	if !s.editRequest(w, r, &request) {
		return
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another catalog operation is running"))
		return
	}
	defer s.editMu.Unlock()
	request.Platform = s.resolvePlatform(request.Platform)
	if !safeCatalogName(request.Platform) {
		writeError(w, 400, fmt.Errorf("choose a platform"))
		return
	}
	config, err := store.LoadCatalogConfig(s.root)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	registry, _, err := platformnames.LoadRegistry(s.root)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	options := catalogfold.Options{DataRoot: s.root, Platform: request.Platform, Context: r.Context(), RegionPriority: config.Fold.RegionPriority, RegionGroups: config.Fold.RegionGroups, PlatformRegionPriority: map[string][]string{}}
	for name, value := range config.Fold.Platforms {
		options.PlatformRegionPriority[registry.ResolveCLI(name)] = value.RegionPriority
	}
	streaming := r.URL.Query().Get("stream") == "1"
	encode := json.NewEncoder(w)
	last := time.Time{}
	if streaming {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		options.Progress = func(p taskprogress.Snapshot) {
			if time.Since(last) < 200*time.Millisecond {
				return
			}
			last = time.Now()
			_ = encode.Encode(map[string]any{"progress": p})
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
	plan, err := catalogfold.BuildPlan(options, request.Games)
	if err != nil {
		if streaming {
			_ = encode.Encode(map[string]any{"error": err.Error()})
		} else {
			writeError(w, 400, err)
		}
		return
	}
	if err := r.Context().Err(); err != nil {
		plan.Close()
		return
	}
	response, err := s.registerFoldPlan(plan)
	if err != nil {
		if streaming {
			_ = encode.Encode(map[string]any{"error": err.Error()})
		} else {
			writeError(w, 500, err)
		}
		return
	}
	if streaming {
		_ = encode.Encode(map[string]any{"result": response})
	} else {
		writeJSON(w, response)
	}
}

// registerFoldPlan requires editMu. Each continuation receives a new, expiring ID.
func (s *server) registerFoldPlan(plan *catalogfold.Plan) (foldPlanResponse, error) {
	if s.plans == nil {
		s.plans = map[string]storedPlan{}
	}
	for id, old := range s.plans {
		if time.Since(old.created) > 30*time.Minute {
			old.plan.Close()
			delete(s.plans, id)
		}
	}
	if len(s.plans) >= 4 {
		var oldest string
		var when time.Time
		for id, old := range s.plans {
			if oldest == "" || old.created.Before(when) {
				oldest = id
				when = old.created
			}
		}
		s.plans[oldest].plan.Close()
		delete(s.plans, oldest)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		plan.Close()
		return foldPlanResponse{}, err
	}
	id := hex.EncodeToString(nonce[:])
	s.plans[id] = storedPlan{plan: plan, created: time.Now()}
	time.AfterFunc(30*time.Minute, func() {
		s.editMu.Lock()
		defer s.editMu.Unlock()
		if old, ok := s.plans[id]; ok {
			old.plan.Close()
			delete(s.plans, id)
		}
	})
	groups := append([]catalogfold.PlannedGroup{}, plan.Groups...)
	for i := range groups {
		groups[i].Result = nil
	}
	return foldPlanResponse{ID: id, Platform: plan.Platform, Groups: groups}, nil
}
func (s *server) foldPreview(w http.ResponseWriter, r *http.Request) {
	var request foldPreviewRequest
	if !s.editRequest(w, r, &request) {
		return
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another catalog operation is running"))
		return
	}
	defer s.editMu.Unlock()
	stored, ok := s.plans[request.Plan]
	if !ok {
		writeError(w, 409, fmt.Errorf("plan expired; build a new plan"))
		return
	}
	group, err := stored.plan.Inspect(request.Group)
	if err == nil && request.Draft != nil {
		group, err = stored.plan.Preview(request.Group, *request.Draft)
	}
	if err != nil {
		writeError(w, 409, err)
		return
	}
	response := map[string]any{"group": group, "draft": catalogfold.Draft{Primary: group.Group.Keep.Folder, Fields: group.Group.Edits, Confirm: group.Group.ReviewAccepted, Reason: group.Group.ReviewReason}}
	if group.Result != nil {
		response["preview"] = s.editorCard(*group.Result, stored.plan.Platform)
	}
	writeJSON(w, response)
}
func (s *server) foldApply(w http.ResponseWriter, r *http.Request) {
	var request foldApplyRequest
	if !s.editRequest(w, r, &request) {
		return
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another catalog operation is running"))
		return
	}
	defer s.editMu.Unlock()
	stored, ok := s.plans[request.Plan]
	if !ok {
		writeError(w, 409, fmt.Errorf("plan expired; build a new plan"))
		return
	}
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		writeError(w, 409, err)
		return
	}
	defer unlock()
	batch := catalogchanges.Begin(exporter.RefreshPlatformIndex)
	result, err := stored.plan.Apply(r.Context(), request.Decisions)
	indexErr := batch.Finish()
	// Every apply attempt consumes the plan: MAME staging or earlier platform work
	// may have changed even on error. No blind replay of a partially applied plan.
	stored.plan.Close()
	delete(s.plans, request.Plan)
	s.summaryMu.Lock()
	s.summaries = nil
	s.summaryMu.Unlock()
	if err != nil {
		writeJSONStatus(w, 409, map[string]any{"error": err.Error(), "result": result, "consumed": true})
		return
	}
	response := map[string]any{"result": result, "consumed": true}
	if request.Continue {
		next, nextErr := stored.plan.Continue(r.Context(), request.Decisions)
		if nextErr == nil {
			var registered foldPlanResponse
			registered, nextErr = s.registerFoldPlan(next)
			if nextErr == nil {
				response["next_plan"] = registered
			}
		}
		// Saving succeeded even when preparing the next review fails. Never invite replay.
		if nextErr != nil {
			response["continuation_error"] = nextErr.Error()
		}
	}
	if indexErr != nil {
		response["warning"] = indexErr.Error()
	}
	writeJSON(w, response)
}
func (s *server) foldDiscard(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Plan string `json:"plan"`
	}
	if !s.editRequest(w, r, &request) {
		return
	}
	s.editMu.Lock()
	defer s.editMu.Unlock()
	if old, ok := s.plans[request.Plan]; ok {
		old.plan.Close()
		delete(s.plans, request.Plan)
	}
	writeJSON(w, map[string]bool{"discarded": true})
}
func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
