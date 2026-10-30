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
		"mode": "browser", "read_only": true, "documentation": "/docs", "openapi": "/openapi.json",
		"endpoints": map[string]string{
			"platforms": "/api/platforms", "names": "/api/games?platform=<folder>&names=1",
			"summaries":      "/api/games?platform=<folder>&game=<folder>",
			"game":           "/api/game?platform=<folder>&game=<folder>",
			"export_preview": "POST /api/export-preview",
			"export_options": "/api/export-options",
			"stats":          "/api/stats?platform=<folder>", "media": "/media?path=<relative-path>",
		},
	})
}
