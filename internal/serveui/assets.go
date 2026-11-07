package serveui

import (
	_ "embed"
	"io"
	"net/http"
)

//go:embed static/index.html
var indexHTML string

//go:embed static/catalog-fold.js
var catalogFoldJS string

//go:embed static/catalog-edit.js
var catalogInlineJS string

//go:embed static/catalog-register.js
var catalogRegisterJS string

//go:embed static/catalog-edit.css
var catalogEditCSS string

//go:embed static/library.js
var libraryJS string

//go:embed static/library.css
var libraryCSS string

//go:embed static/enrichment.js
var enrichmentJS string

//go:embed static/export.js
var exportSetupJS string

//go:embed static/export-review.js
var exportReviewJS string

//go:embed static/export-profile.js
var exportProfileJS string

// Keep the existing URLs and evaluation order: no frontend build step or extra
// requests are needed after splitting the source by workflow responsibility.
var catalogEditJS = catalogFoldJS + catalogInlineJS
var nativeExportJS = exportSetupJS + exportReviewJS + exportProfileJS

func registerStaticAssets(mux *http.ServeMux) {
	for path, asset := range map[string]struct{ contentType, body string }{
		"/catalog-edit.js":     {"text/javascript; charset=utf-8", catalogEditJS},
		"/catalog-register.js": {"text/javascript; charset=utf-8", catalogRegisterJS},
		"/catalog-edit.css":    {"text/css; charset=utf-8", catalogEditCSS},
		"/library.js":          {"text/javascript; charset=utf-8", libraryJS},
		"/library.css":         {"text/css; charset=utf-8", libraryCSS},
		"/enrichment.js":       {"text/javascript; charset=utf-8", enrichmentJS},
		"/export.js":           {"text/javascript; charset=utf-8", nativeExportJS},
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			w.Header().Set("Content-Type", asset.contentType)
			_, _ = io.WriteString(w, asset.body)
		})
	}
}
