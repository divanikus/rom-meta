package httpdocs

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

//go:embed static/*
var assets embed.FS
var page = template.Must(template.New("docs").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title><link rel="stylesheet" href="{{.Docs}}/assets/swagger-ui.css"><style>body{margin:0}nav{padding:14px 28px;background:#12202b;color:white;font:14px system-ui}nav a{color:#67d4ef;margin-right:20px}</style></head><body><nav><a href="{{.Home}}">RomMeta</a><a href="{{.Spec}}">OpenAPI JSON</a><span>{{.Title}} | Local assets; authorization is kept in memory only.</span></nav><div id="swagger-ui"></div><script src="{{.Docs}}/assets/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:{{.Spec}},dom_id:'#swagger-ui',presets:[SwaggerUIBundle.presets.apis],layout:'BaseLayout',deepLinking:true,displayRequestDuration:true,filter:true,validatorUrl:null,persistAuthorization:false,queryConfigEnabled:false,supportedSubmitMethods:['get','post','head']});</script></body></html>`))

// Handle exposes only public documentation, never catalog data. Call it before
// optional access protection at the common server boundary.
func Handle(w http.ResponseWriter, r *http.Request, specPath, docsPath, title string, definition func() any) bool {
	path := r.URL.Path
	if path != specPath && path != docsPath && !strings.HasPrefix(path, docsPath+"/assets/") {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "documentation requires GET or HEAD", http.StatusMethodNotAllowed)
		return true
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if path == specPath {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(definition())
		}
		return true
	}
	if path == docsPath {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_ = page.Execute(w, struct{ Spec, Docs, Title, Home string }{specPath, docsPath, title, "/"})
		}
		return true
	}
	name := strings.TrimPrefix(path, docsPath+"/assets/")
	kinds := map[string]string{"swagger-ui-bundle.js": "text/javascript; charset=utf-8", "swagger-ui.css": "text/css; charset=utf-8", "LICENSE": "text/plain; charset=utf-8", "NOTICE": "text/plain; charset=utf-8", "UPSTREAM.txt": "text/plain; charset=utf-8", "swagger-ui-bundle.js.LICENSE.txt": "text/plain; charset=utf-8"}
	kind, ok := kinds[name]
	if !ok {
		http.NotFound(w, r)
		return true
	}
	content, err := assets.ReadFile("static/" + name)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
	return true
}
