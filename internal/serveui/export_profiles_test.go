package serveui

import (
	"bytes"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/exportsettings"
	"strings"
	"testing"
)

func profileResponse(t *testing.T, s *server) savedProfilesResponse {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/export/profiles", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response savedProfilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
func TestSavedExportProfilesPersistValidateAndLink(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	response := profileResponse(t, s)
	p := exportsettings.SavedProfile{Name: "Original artwork", Target: "es-de", ProfileYAML: "schema_version: 1\ntitles: {mode: release}\n"}
	w, _ := catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	response = profileResponse(t, s)
	p = response.Profiles[0]
	if p.ID == "" || !strings.Contains(p.ProfileYAML, "mode: release") {
		t.Fatal(p)
	}
	// Duplicate names and malformed YAML never alter the stored revision.
	for _, bad := range []exportsettings.SavedProfile{
		{Name: "original ARTWORK", Target: "es-de", ProfileYAML: p.ProfileYAML},
		{Name: "Bad", Target: "es-de", ProfileYAML: "schema_version: 999"},
		{Name: " ", Target: "es-de", ProfileYAML: p.ProfileYAML},
	} {
		w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: bad})
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if profileResponse(t, s).Revision != response.Revision {
		t.Fatal("invalid save changed file")
	}
	path := filepath.Join(s.root, "config", "export-profiles.yaml")
	doc, _, _, _ := exportsettings.ReadProfiles(s.root)
	doc.Extra = map[string]yaml.Node{"owner_note": {Kind: yaml.ScalarNode, Value: "preserve"}}
	doc.Profiles[0].Extra = map[string]yaml.Node{"future_field": {Kind: yaml.ScalarNode, Value: "keep"}}
	data, _ := yaml.Marshal(doc)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p.Name = "Renamed"
	w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p})
	if w.Code != 409 {
		t.Fatal("accepted stale revision")
	}
	response = profileResponse(t, s)
	w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data, _ = os.ReadFile(path)
	if !bytes.Contains(data, []byte("future_field: keep")) || !bytes.Contains(data, []byte("owner_note: preserve")) {
		t.Fatal("lost unknown fields")
	}
	h, err := HandlerWithEditing(s.root, true)
	if err != nil {
		t.Fatal(err)
	}
	response = profileResponse(t, h.(*server))
	if response.Profiles[0].Name != "Renamed" {
		t.Fatal("not persisted")
	}
	_, destinations, _, _ := s.readExportDestinations()
	d.ProfileID = p.ID
	w, _ = catalogCall(t, s, "/api/export/destinations", exportDestinationRequest{Revision: destinations.Revision, Destination: d})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, _, _, _ := exportsettings.ReadDestinations(s.root)
	if saved.Destinations[0].ProfileID != p.ID || saved.Destinations[0].ProfileYAML != "" {
		t.Fatal("expected reusable reference")
	}
	linked := saved.Destinations[0]
	if err := exportsettings.ResolveDestinationProfile(s.root, &linked); err != nil || linked.ProfileName != "Renamed" {
		t.Fatal(linked, err)
	}
	w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p, Delete: true})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "used by destination") {
		t.Fatal(w.Code, w.Body.String())
	}
	p.Target = "launchbox"
	w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p})
	if w.Code != 400 {
		t.Fatal("changed target of referenced profile", w.Code)
	}
	// Old destination schemas with inline profiles remain valid and can detach a link.
	_, destinations, _, _ = s.readExportDestinations()
	d = saved.Destinations[0]
	d.ProfileID = ""
	d.ProfileYAML = linked.ProfileYAML
	w, _ = catalogCall(t, s, "/api/export/destinations", exportDestinationRequest{Revision: destinations.Revision, Destination: d})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p, Delete: true})
	if w.Code != 200 || len(profileResponse(t, s).Profiles) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(d.Path); err != nil {
		t.Fatal("removed destination files", err)
	}
}
func TestSavedProfilesAccessAndFrozenPlan(t *testing.T) {
	s, _, d := webExportFixture(t, "es-de")
	readonly, _ := Handler(s.root)
	w := httptest.NewRecorder()
	readonly.ServeHTTP(w, httptest.NewRequest("GET", "/api/export/profiles", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	response := profileResponse(t, s)
	p := exportsettings.SavedProfile{Name: "For export", Target: "es-de", ProfileYAML: "schema_version: 1\ntitles: {mode: release}\n"}
	w, _ = catalogCall(t, s, "/api/export/profiles", savedProfileRequest{Revision: response.Revision, Profile: p})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p = profileResponse(t, s).Profiles[0]
	body, _ := json.Marshal(savedProfileRequest{Profile: p})
	request := httptest.NewRequest("POST", "/api/export/profiles", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://example.invalid")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	d.ProfileID = "missing"
	w, _ = catalogCall(t, s, "/api/export/prepare", nativeExportRequest{Destination: d})
	if w.Code != 400 {
		t.Fatal("missing reference accepted")
	}
	d.ProfileID = p.ID
	w, op := catalogCall(t, s, "/api/export/prepare", nativeExportRequest{Destination: d})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	ready := waitImport(t, s, op["id"].(string), "ready")
	if ready.Export.Destination.ProfileID != "" || ready.Export.Destination.ProfileName != p.Name || !strings.Contains(ready.Export.Destination.ProfileYAML, "mode: release") {
		t.Fatal("plan did not freeze saved policy", ready.Export.Destination)
	}
}
