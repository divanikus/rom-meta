package catalogapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/exporter"
	"strings"
	"testing"
)

func exportFixture(t *testing.T) (*LookupServer, ExportRequest, string) {
	t.Helper()
	_, game, roms := fixture(t)
	inputs, err := exporter.ObserveExport(exporter.Options{ROMRoot: roms, System: "gamegear"})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(filepath.Dir(roms), "catalog")
	s, err := NewLookup(Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: root, ProfilePath: filepath.Join(filepath.Dir(roms), "profile.yaml")}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(roms); err != nil {
		t.Fatal(err)
	}
	return s, ExportRequest{Platform: "Sega - Game Gear", System: "gamegear", Inputs: inputs}, game
}
func TestExportAPIValidationAndSourceDrift(t *testing.T) {
	s, q, game := exportFixture(t)
	token := strings.Repeat("s", 32)
	if got := lookupRequest(s, "POST", "/v1/export", "", q).Code; got != 401 {
		t.Fatal(got)
	}
	if got := lookupRequest(s, "GET", "/v1/export", token, q).Code; got != 405 {
		t.Fatal(got)
	}
	for _, body := range []string{`{}`, `{"platform":"x","system":"x","inputs":[],"unknown":1}`, `{} {}`} {
		r := httptest.NewRequest("POST", "/v1/export", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	bad := q
	bad.Inputs = append([]exporter.ExportInput(nil), q.Inputs...)
	bad.Inputs[0].ROM = "../secret.gg"
	if got := lookupRequest(s, "POST", "/v1/export", token, bad).Code; got != 409 {
		t.Fatal(got)
	}
	bad = q
	bad.Inputs = make([]exporter.ExportInput, 1001)
	if got := lookupRequest(s, "POST", "/v1/export", token, bad).Code; got != 400 {
		t.Fatal(got)
	}
	bad = q
	bad.Profile = "schema_version: 1\ntarget: es-de\nunknown: true\n"
	if got := lookupRequest(s, "POST", "/v1/export", token, bad).Code; got != 409 {
		t.Fatal(got)
	}
	w := lookupRequest(s, "POST", "/v1/export", token, q)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result ExportResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan.Matched != 1 || len(result.Assets) != 1 || result.Plan.DataRoot != "" || result.Plan.ROMRoot != "" {
		t.Fatal(result)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(filepath.Dir(game))) || bytes.Contains(w.Body.Bytes(), []byte("must-never")) {
		t.Fatal("private path/data leaked")
	}
	for _, asset := range result.Assets {
		if w = lookupRequest(s, "GET", asset.URI, token, nil); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if err := os.WriteFile(filepath.Join(game, "game.yaml"), []byte("title: changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if w = lookupRequest(s, "POST", "/v1/export", token, q); w.Code != 409 {
		t.Fatal("changed selected YAML accepted", w.Code, w.Body.String())
	}
}
