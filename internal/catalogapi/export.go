package catalogapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"rommeta/internal/catalogfs"

	"rommeta/internal/exporter"
)

type ExportRequest struct {
	Platform       string                 `json:"platform"`
	System         string                 `json:"system"`
	Inputs         []exporter.ExportInput `json:"inputs"`
	ArcadeSnapshot string                 `json:"catalog_snapshot,omitempty"`
	Profile        string                 `json:"profile,omitempty"`
	Revision       string                 `json:"revision,omitempty"`
}

type ExportAsset struct {
	URI    string `json:"uri"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ExportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ExportResponse struct {
	SchemaVersion int                    `json:"schema_version"`
	Revision      string                 `json:"revision"`
	Plan          *exporter.Plan         `json:"plan"`
	Assets        map[string]ExportAsset `json:"assets"`
}

func (s *LookupServer) serveExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "export requires POST", 405)
		return
	}
	var request ExportRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20))
	decoder.DisallowUnknownFields()
	var trailing any
	if decoder.Decode(&request) != nil || decoder.Decode(&trailing) != io.EOF || request.Platform == "" || request.System == "" || request.Inputs == nil || len(request.Inputs) > 1000 || len(request.Profile) > 1<<20 {
		http.Error(w, "invalid export request", 400)
		return
	}
	result, err := s.export(r.Context(), request)
	if err != nil {
		code := "export_unavailable"
		if exporter.NeedsArcadeSnapshot(err) {
			code = "snapshot_required"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(ExportError{Code: code, Message: "export unavailable: " + err.Error()})
		return
	}
	if err := r.Context().Err(); err != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *LookupServer) export(ctx context.Context, request ExportRequest) (*ExportResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	if request.Revision != "" && request.Revision != s.version {
		return nil, fmt.Errorf("catalog/profile revision changed during export; retry")
	}
	profile := s.catalog.Profile
	if request.Profile != "" {
		var err error
		profile, err = exporter.ExportProfile(s.catalog.Root, request.Profile)
		if err != nil {
			return nil, err
		}
	}
	key := request.Platform + "\x00" + request.System + "\x00" + request.ArcadeSnapshot
	if s.exportResolver == nil || s.exportKey != key {
		s.exportResolver = nil
		resolver, err := s.catalog.OpenExport(request.Platform, request.System, request.ArcadeSnapshot)
		if err != nil {
			return nil, err
		}
		s.exportResolver, s.exportKey = resolver, key
	}
	plan, err := s.exportResolver.Resolve(request.Inputs, profile)
	if err != nil {
		return nil, err
	}
	if err = s.exportResolver.CheckDocuments(plan); err != nil {
		return nil, err
	}
	response := &ExportResponse{SchemaVersion: Version, Revision: s.version, Plan: plan, Assets: map[string]ExportAsset{}}
	for i := range plan.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for role, media := range plan.Records[i].Media {
			asset, exists := response.Assets[media.SourcePath]
			if !exists {
				bound, cached := s.cache[media.SourcePath]
				if cached {
					_, err = bound.verify()
				} else {
					bound, err = bindExportAsset(ctx, s.catalog.Root, media.SourcePath)
				}
				if err != nil {
					return nil, err
				}
				if bound.digest.Size != media.Size {
					return nil, fmt.Errorf("selected media changed during export")
				}
				s.cache[media.SourcePath] = bound
				digest := sha256.Sum256([]byte(media.SourcePath + "\n" + bound.digest.SHA256))
				id := hex.EncodeToString(digest[:])
				s.assets[id] = bound
				asset = ExportAsset{URI: "/v1/assets/" + id, Size: bound.digest.Size, SHA256: bound.digest.SHA256}
				response.Assets[media.SourcePath] = asset
			}
			media.SHA256 = asset.SHA256
			plan.Records[i].Media[role] = media
		}
	}
	after, err := s.signature()
	if err != nil {
		return nil, err
	}
	if after != s.version {
		return nil, fmt.Errorf("catalog changed during export")
	}
	// Only logical paths and selected metadata leave the server.
	plan.DataRoot, plan.ROMRoot = "", ""
	return response, nil
}

func bindExportAsset(ctx context.Context, root, path string) (boundFile, error) {
	name, err := contained(root, path)
	if err != nil {
		return boundFile{}, err
	}
	info, err := catalogfs.Stat(name)
	if err != nil {
		return boundFile{}, err
	}
	// No payload upload or arbitrary byte allocation: stream the selected asset.
	digest, err := fileDigestContext(ctx, name, info.Size())
	if err != nil {
		return boundFile{}, err
	}
	return boundFile{root: root, relative: path, digest: digest, info: info}, nil
}

type requestReader struct {
	ctx   context.Context
	input io.Reader
}

func (r requestReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}
