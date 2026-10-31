package catalogapi

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"rommeta/internal/exporter"
	"rommeta/internal/httpdocs"
)

// LookupServer reads catalog metadata only. Observations never become filesystem paths.
// The client must prove them against its native ROM files before importing.
type LookupServer struct {
	exportResolver *exporter.ExportResolver
	exportKey      string
	token          string
	catalog        *exporter.LookupCatalog
	mu             sync.Mutex
	resolver       *exporter.LookupResolver
	cache          map[string]boundFile
	assets         map[string]boundFile
	progress       func(string, string, int, int)
	options        Options
	version        string
	health         atomic.Value // immutable []exporter.LookupPlatform; independent of lookup lock
}
type LookupRequest struct {
	Platform    string                   `json:"platform"`
	Inputs      []exporter.ObservedInput `json:"inputs"`
	GameBinding bool                     `json:"game_binding,omitempty"`
	Media       string                   `json:"media,omitempty"`
}

func NewLookup(options Options) (*LookupServer, error) {
	if options.Token != "" && len(options.Token) < 32 {
		return nil, fmt.Errorf("ROMMETA_API_TOKEN must contain at least 32 characters")
	}
	catalog, err := exporter.NewLookupCatalog(options.Export.DataRoot, options.Export.ProfilePath)
	if err != nil {
		return nil, err
	}
	catalog.CacheDir = options.LookupCacheDir
	s := &LookupServer{options: options, progress: options.Export.Progress, token: options.Token, catalog: catalog, cache: map[string]boundFile{}, assets: map[string]boundFile{}}
	s.health.Store(catalog.Platforms)
	s.version, err = s.signature()
	if err != nil {
		return nil, err
	}
	return s, nil
}
func (s *LookupServer) lookup(query LookupRequest) (*Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	if s.resolver == nil || s.resolver.Platform.Platform != query.Platform {
		s.resolver = nil // Let the previous platform index be reclaimed during loading.
		resolver, err := s.catalog.Open(query.Platform)
		if err != nil {
			return nil, err
		}
		s.resolver = resolver
		if s.progress != nil {
			stats := resolver.IndexStats
			s.progress("catalog-index", fmt.Sprintf("%s: %d cached, %d rebuilt", query.Platform, stats.Cached, stats.Rebuilt), stats.Games, stats.Games)
		}
	}
	source, err := s.resolver.ResolveBindings(query.Inputs, query.GameBinding)
	if err != nil {
		return nil, err
	}
	observations := map[string]exporter.ObservedInput{}
	for _, input := range query.Inputs {
		observations[input.ROM] = input
	}
	snapshot, err := selectionFromPlan(Options{Token: s.token, SkipUnmatched: true, AllowGameBinding: query.GameBinding, Media: query.Media}, source, observations, s.cache, s.resolver.DocumentDigests)
	if err != nil {
		return nil, err
	}
	for id, bound := range snapshot.assets {
		s.assets[id] = bound
	}
	return snapshot, nil
}
func (s *LookupServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if httpdocs.Handle(w, r, "/openapi.json", "/docs", "RomMeta consumer API v1", func() any { return OpenAPI(true, s.token != "") }) {
		return
	}
	if s.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "queries unsupported", 400)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.URL.Path == "/v1/export" {
		s.serveExport(w, r)
		return
	}
	if r.URL.Path == "/v1/lookup" {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "lookup requires POST", 405)
			return
		}
		var query LookupRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&query)
		var trailing any
		if err != nil || decoder.Decode(&trailing) != io.EOF || query.Platform == "" || query.Inputs == nil || query.Media != "" && query.Media != "covers" && query.Media != "none" {
			http.Error(w, "invalid lookup request", 400)
			return
		}
		snapshot, err := s.lookup(query)
		if err != nil {
			http.Error(w, "lookup unavailable: "+err.Error(), 409)
			return
		}
		request := r.Clone(r.Context())
		request.Method = "GET"
		request.URL.Path = "/v1/selection"
		snapshot.ServeHTTP(w, request)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "read only", 405)
		return
	}
	if r.URL.Path == "/v1/platforms" || r.URL.Path == "/v1/health" {
		platforms := s.health.Load().([]exporter.LookupPlatform)
		if r.URL.Path == "/v1/platforms" {
			s.mu.Lock()
			if err := s.refresh(); err != nil {
				s.mu.Unlock()
				http.Error(w, "catalog refresh unavailable: "+err.Error(), 409)
				return
			}
			platforms = s.catalog.Platforms
			s.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]any{"schema_version": Version, "export_schema_version": 1, "export_features": []string{"chd-header-v1"}, "mode": "catalog", "platforms": platforms})
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/assets/") {
		id := strings.TrimPrefix(r.URL.Path, "/v1/assets/")
		s.mu.Lock()
		bound, ok := s.assets[id]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		(&Server{token: s.token, assets: map[string]boundFile{id: bound}}).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
