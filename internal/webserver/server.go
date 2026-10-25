// Package webserver composes the browser and API behind one HTTP address.
package webserver

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
	"rommeta/internal/httpdocs"
	"rommeta/internal/serveui"
)

type Options struct {
	DataRoot, Addr, ProfilePath, LookupCacheDir, Token string
	ROMRoot, Platform, Match                           string
	Limit                                              int
	SkipUnmatched                                      bool
	Progress                                           func(string, string, int, int)
}

type Server struct {
	browser, api http.Handler
	token        string
	definition   any
}

func New(options Options) (*Server, error) {
	if options.Token != "" && len(options.Token) < 32 {
		return nil, fmt.Errorf("ROMMETA_API_TOKEN, when set, must contain at least 32 characters")
	}
	if options.Limit < 0 {
		return nil, fmt.Errorf("limit must be non-negative (0: all)")
	}
	browser, err := serveui.Handler(options.DataRoot)
	if err != nil {
		return nil, err
	}
	snapshot := options.ROMRoot != "" || options.Platform != ""
	if snapshot && (options.ROMRoot == "" || options.Platform == "") {
		return nil, fmt.Errorf("fixed selection requires both --rom-root and --platform")
	}
	if !snapshot && (options.Match != "" || options.Limit != 0 || options.SkipUnmatched) {
		return nil, fmt.Errorf("--match, --limit and --skip-unmatched require a fixed --rom-root/--platform selection")
	}
	// Access control belongs to this common boundary, not only /v1 routes.
	apiOptions := catalogapi.Options{Export: exporter.Options{DataRoot: options.DataRoot, ProfilePath: options.ProfilePath, ROMRoot: options.ROMRoot, Platform: options.Platform, Match: options.Match, Limit: options.Limit, Progress: options.Progress}, LookupCacheDir: options.LookupCacheDir, SkipUnmatched: options.SkipUnmatched}
	var api http.Handler
	if snapshot {
		api, err = catalogapi.New(apiOptions)
	} else {
		api, err = catalogapi.NewLookup(apiOptions)
	}
	if err != nil {
		return nil, err
	}
	definition, err := combinedOpenAPI(!snapshot, options.Token != "")
	if err != nil {
		return nil, err
	}
	return &Server{browser: browser, api: api, token: options.Token, definition: definition}, nil
}

// Listen uses no response deadline: selected asset hashing can take minutes.
func Listen(addr string, handler http.Handler) error {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:8787"
	}
	return (&http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}).ListenAndServe()
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Documentation is catalog-independent and lets Swagger obtain credentials.
	if httpdocs.Handle(w, r, "/openapi.json", "/docs", "RomMeta API", func() any { return s.definition }) {
		return
	}
	if !s.authorized(r) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("WWW-Authenticate", "Bearer")
		} else {
			w.Header().Set("WWW-Authenticate", `Basic realm="RomMeta", charset="UTF-8"`)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		s.api.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "read only", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method == http.MethodGet {
			paths := s.definition.(map[string]any)["paths"].(map[string]any)
			endpoints := make([]string, 0, len(paths))
			for path := range paths {
				endpoints = append(endpoints, path)
			}
			sort.Strings(endpoints)
			_ = json.NewEncoder(w).Encode(map[string]any{"mode": "unified", "documentation": "/docs", "openapi": "/openapi.json", "authentication": s.token != "", "browser": "/", "endpoints": endpoints})
		}
		return
	}
	s.browser.ServeHTTP(w, r)
}
func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) == 1 {
		return true
	}
	// One optional shared secret, no users, roles, sessions or credential database.
	// Native browser Basic authentication keeps ordinary media/fetch requests usable.
	username, password, ok := r.BasicAuth()
	return ok && username == "rommeta" && subtle.ConstantTimeCompare([]byte(password), []byte(s.token)) == 1
}

func combinedOpenAPI(lookup, protected bool) (any, error) {
	clone := func(value any) (map[string]any, error) {
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var result map[string]any
		err = json.Unmarshal(b, &result)
		return result, err
	}
	doc, err := clone(serveui.OpenAPI())
	if err != nil {
		return nil, err
	}
	api, err := clone(catalogapi.OpenAPI(lookup, false))
	if err != nil {
		return nil, err
	}
	paths := doc["paths"].(map[string]any)
	for path, item := range api["paths"].(map[string]any) {
		paths[path] = item
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, value := range api["components"].(map[string]any)["schemas"].(map[string]any) {
		if old, ok := schemas[name]; ok {
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(value)
			if string(a) != string(b) {
				return nil, fmt.Errorf("conflicting OpenAPI schema: %s", name)
			}
		}
		schemas[name] = value
	}
	info := doc["info"].(map[string]any)
	info["title"] = "RomMeta API"
	info["description"] = "One read-only HTTP service for catalog browsing, lookup and media. /api routes support the browser; /v1 routes provide portable consumer contracts. CLI commands perform catalog mutations. Profile is optional; default selection follows source priorities. Initial platform loads and full statistics may be expensive."
	if protected {
		doc["components"].(map[string]any)["securitySchemes"] = map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "description": "Optional ROMMETA_API_TOKEN."}, "basicAuth": map[string]any{"type": "http", "scheme": "basic", "description": "Browser access: user rommeta, password ROMMETA_API_TOKEN. This is a shared service secret, not a user account."}}
		doc["security"] = []any{map[string]any{"bearerAuth": []string{}}, map[string]any{"basicAuth": []string{}}}
		for _, item := range paths {
			for _, operation := range item.(map[string]any) {
				if op, ok := operation.(map[string]any); ok {
					if responses, ok := op["responses"].(map[string]any); ok {
						responses["401"] = map[string]any{"description": "Missing or invalid shared credentials (Bearer or HTTP Basic)."}
					}
				}
			}
		}
		info["description"] = info["description"].(string) + " Optional shared-token protection is enabled for every catalog/media route; documentation remains public. Use TLS outside a trusted network."
	} else {
		delete(doc, "security")
		info["description"] = info["description"].(string) + " No authentication is configured. The default listen address is loopback."
		for _, item := range paths {
			for _, operation := range item.(map[string]any) {
				if op, ok := operation.(map[string]any); ok {
					if responses, ok := op["responses"].(map[string]any); ok {
						delete(responses, "401")
					}
				}
			}
		}
	}
	paths["/api"].(map[string]any)["get"].(map[string]any)["description"] = "Discover the common browser, API and documentation endpoints."
	return doc, nil
}
