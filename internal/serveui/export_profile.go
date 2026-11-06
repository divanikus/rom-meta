package serveui

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net/http"
	"rommeta/internal/exporter"
)

// Profile editing is a read-only round trip through the shared CLI validator.
type exportProfileRequest struct {
	Target      string            `json:"target"`
	ProfileYAML string            `json:"profile_yaml,omitempty"`
	Profile     *exporter.Profile `json:"profile,omitempty"`
}

func (s *server) exportDefaultProfile(w http.ResponseWriter, r *http.Request) {
	input := exportProfileRequest{Target: r.URL.Query().Get("target")}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, 400, err)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, 400, fmt.Errorf("one JSON document required"))
			return
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if input.Profile != nil && input.ProfileYAML != "" {
		writeError(w, 400, fmt.Errorf("use profile or profile_yaml, not both"))
		return
	}
	content := input.ProfileYAML
	if input.Profile != nil {
		b, err := portableExportProfile(*input.Profile)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		content = string(b)
	}
	p, content, target, err := s.resolveExportProfile(content, input.Target)
	if err != nil {
		writeError(w, 400, fmt.Errorf("profile: %w", err))
		return
	}
	writeJSON(w, map[string]any{"profile_yaml": content, "target": target, "profile": p})
}
func (s *server) resolveExportProfile(content, target string) (exporter.Profile, string, string, error) {
	target, err := exporter.NormalizeTarget(target)
	if err != nil {
		return exporter.Profile{}, "", "", err
	}
	if len(content) > 1<<20 {
		return exporter.Profile{}, "", "", fmt.Errorf("profile must be at most 1 MiB")
	}
	if content == "" {
		content = "schema_version: 1\n"
	}
	p, err := exporter.ExportProfile(s.root, content)
	if err != nil {
		return p, "", "", err
	}
	if err = exporter.ValidateTargetMedia(p, target); err != nil {
		return p, "", "", err
	}
	data, err := portableExportProfile(p)
	return p, string(data), target, err
}
func portableExportProfile(p exporter.Profile) ([]byte, error) {
	fields, media := map[string]any{}, map[string]any{}
	for k, r := range p.Selection.Fields {
		fields[k] = portableRule(r)
	}
	for k, r := range p.Selection.Media {
		media[k] = portableRule(r)
	}
	wire := map[string]any{"schema_version": p.SchemaVersion, "titles": p.Titles, "selection": map[string]any{"description": portableRule(p.Selection.Description), "fields": fields, "media": media}}
	if p.Target != "" {
		wire["target"] = p.Target
	}
	wire["launchbox"] = map[string]any{"kinds": p.LaunchBox.Kinds, "regions": p.LaunchBox.Regions, "languages": p.LaunchBox.Languages}
	if len(p.MediaOverrides) > 0 {
		wire["media_overrides"] = p.MediaOverrides
	}
	if len(p.DescriptionFallbacks) > 0 {
		wire["description_fallbacks"] = p.DescriptionFallbacks
	}
	return yaml.Marshal(wire)
}
