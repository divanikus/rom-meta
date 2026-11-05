package serveui

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"net/http"
	"rommeta/internal/exporter"
)

func (s *server) exportDefaultProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	target, err := exporter.NormalizeTarget(r.URL.Query().Get("target"))
	if err != nil {
		writeError(w, 400, err)
		return
	}
	p, err := exporter.ExportProfile(s.root, "schema_version: 1\n")
	if err == nil {
		err = exporter.ValidateTargetMedia(p, target)
	}
	if err != nil {
		writeError(w, 400, fmt.Errorf("profile: %w", err))
		return
	}
	content, err := portableExportProfile(p)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"profile_yaml": string(content), "target": target, "profile": p})
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
