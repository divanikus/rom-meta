package serveui

import "net/http"

// The raw browser component index is replaced by common discovery in webserver.
func (s *server) apiIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, map[string]any{
		"mode": "browser", "read_only": !s.allowEdit, "documentation": "/docs", "openapi": "/openapi.json",
		"endpoints": map[string]string{
			"operations": "/api/operations", "dat_upload": "POST /api/catalog/import-upload", "dat_plan": "POST /api/catalog/import-plan", "dat_apply": "POST /api/catalog/import-apply", "dat_dismiss": "POST /api/catalog/import-dismiss",
			"catalog": "/api/catalog", "edit": "GET/POST /api/catalog/edit", "fold_plan": "POST /api/catalog/fold-plan", "fold_preview": "POST /api/catalog/fold-preview", "fold_apply": "POST /api/catalog/fold-apply",
			"platforms": "/api/platforms", "names": "/api/games?platform=<folder>&names=1",
			"summaries":      "/api/games?platform=<folder>&game=<folder>",
			"game":           "/api/game?platform=<folder>&game=<folder>",
			"fingerprints":   "/api/fingerprints?platform=<folder>&game=<folder>",
			"export_preview": "POST /api/export-preview",
			"export_options": "/api/export-options",
			"stats":          "/api/stats?platform=<folder>", "media": "/media?path=<relative-path>",
		},
	})
}
