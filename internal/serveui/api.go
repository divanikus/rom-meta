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
			"operations": "/api/operations", "operation_dismiss": "POST /api/operations/dismiss", "enrichment_options": "/api/catalog/enrichment-options", "enrichment_plan": "POST /api/catalog/enrichment-plan", "enrichment_start": "POST /api/catalog/enrichment-start", "enrichment_retry": "POST /api/catalog/enrichment-retry", "dat_upload": "POST /api/catalog/import-upload", "dat_plan": "POST /api/catalog/import-plan", "dat_apply": "POST /api/catalog/import-apply", "dat_dismiss": "POST /api/catalog/import-dismiss",
			"catalog": "/api/catalog", "settings": "GET/POST /api/catalog/settings", "curation": "GET/POST /api/catalog/curation", "edit": "GET/POST /api/catalog/edit", "fold_plan": "POST /api/catalog/fold-plan", "fold_preview": "POST /api/catalog/fold-preview", "fold_apply": "POST /api/catalog/fold-apply",
			"platforms": "/api/platforms", "names": "/api/games?platform=<folder>&names=1",
			"summaries":            "/api/games?platform=<folder>&game=<folder>",
			"game":                 "/api/game?platform=<folder>&game=<folder>",
			"fingerprints":         "/api/fingerprints?platform=<folder>&game=<folder>",
			"export_match_options": "/api/export/match-options", "export_review": "POST /api/export/review",
			"export_profiles": "GET/POST /api/export/profiles", "export_destinations": "GET/POST /api/export/destinations", "export_folders": "/api/export/folders", "export_prepare": "POST /api/export/prepare", "export_apply": "POST /api/export/apply", "export_plan": "/api/export/plan?id=<operation>",
			"export_preview": "POST /api/export-preview",
			"export_options": "/api/export-options",
			"stats":          "/api/stats?platform=<folder>", "media": "/media?path=<relative-path>",
		},
	})
}
