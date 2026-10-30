package serveui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"rommeta/internal/catalogcache"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
)

type fingerprintsResponse struct {
	Platform        string                     `json:"platform"`
	Game            string                     `json:"game"`
	Fingerprints    []catalogcache.Fingerprint `json:"fingerprints"`
	Total           int                        `json:"total"`
	RegisteredTotal int                        `json:"registered_total"`
	Offset          int                        `json:"offset"`
	Limit           int                        `json:"limit"`
}

// Fingerprints are read on demand for one named card, without caches or ROM I/O.
func (s *server) fingerprints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := r.URL.Query()
	platform, folder := strings.TrimSpace(q.Get("platform")), strings.TrimSpace(q.Get("game"))
	if !safeCatalogName(platform) || !safeCatalogName(folder) || catalogrules.IsIgnoredGameFolder(folder) {
		writeError(w, 400, fmt.Errorf("valid platform and game are required"))
		return
	}
	limit, offset := 100, 0
	for _, entry := range []struct {
		key   string
		value *int
	}{{"limit", &limit}, {"offset", &offset}} {
		if value := q.Get(entry.key); value != "" {
			number, err := strconv.Atoi(value)
			if err != nil || number < 0 {
				writeError(w, 400, fmt.Errorf("invalid %s", entry.key))
				return
			}
			*entry.value = number
		}
	}
	release, variant, search := q.Get("release"), q.Get("variant"), strings.ToLower(strings.TrimSpace(q.Get("q")))
	if limit < 1 || limit > 200 || utf8.RuneCountInString(search) > 256 || release != "" && variant != "" {
		writeError(w, 400, fmt.Errorf("invalid fingerprint filter or page"))
		return
	}
	found, _, err := catalogcache.ReadGameDocument(s.root, s.resolvePlatform(platform), folder)
	if err != nil {
		status := 500
		if os.IsNotExist(err) {
			status = 404
		}
		writeError(w, status, err)
		return
	}
	path := filepath.Join(found.Path, "fingerprints.yaml")
	real, err := catalogfs.EvalSymlinks(path)
	// Reject a symlink escaping the catalog, including logical packed paths.
	if err != nil && !os.IsNotExist(err) {
		writeError(w, 500, err)
		return
	}
	if err == nil {
		rel, e := filepath.Rel(s.realRoot, real)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			writeError(w, 400, fmt.Errorf("fingerprint path escapes catalog"))
			return
		}
	}
	all, err := catalogcache.ReadFingerprints(path)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	response := fingerprintsResponse{Platform: found.Platform, Game: found.Folder, Fingerprints: []catalogcache.Fingerprint{}, RegisteredTotal: len(all), Offset: offset, Limit: limit}
	for _, fp := range all {
		if r.Context().Err() != nil {
			return
		}
		if release != "" && fp.Release != release || variant != "" && fp.Variant != variant {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{fp.Name, fp.Release, fp.Variant, fp.CRC32, fp.MD5, fp.SHA1, fp.SHA256}, " ")), search) {
			continue
		}
		index := response.Total
		response.Total++
		if index >= offset && index-offset < limit {
			response.Fingerprints = append(response.Fingerprints, fp)
		}
	}
	writeJSON(w, response)
}
