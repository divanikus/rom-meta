package webserver

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, token string) *Server {
	t.Helper()
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Sega - Game Gear", "Example")
	if err := os.MkdirAll(game, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{filepath.Join(filepath.Dir(game), "_platform.yaml"): "name: Sega - Game Gear\n", filepath.Join(game, "game.yaml"): "title: Example\nreleases: {Example: {}}\n"} {
		if err := os.WriteFile(name, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{DataRoot: root, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(s *Server, path, auth string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", path, nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	s.ServeHTTP(w, r)
	return w
}
func TestOneServerNeedsNeitherProfileNorToken(t *testing.T) {
	s := fixture(t, "")
	for _, path := range []string{"/", "/api", "/api/platforms", "/v1/platforms", "/v1/health", "/docs", "/openapi.json", "/docs/assets/swagger-ui-bundle.js"} {
		w := request(s, path, "")
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	w := request(s, "/api", "")
	if !strings.Contains(w.Body.String(), `"mode":"unified"`) {
		t.Fatal(w.Body.String())
	}
	w = request(s, "/docs", "")
	if strings.Contains(w.Body.String(), "supportedSubmitMethods:[]") {
		t.Fatal("shared API must be executable")
	}
	var doc map[string]any
	if err := json.Unmarshal(request(s, "/openapi.json", "").Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["security"] != nil {
		t.Fatal("token should be optional")
	}
	paths := doc["paths"].(map[string]any)
	for _, path := range []string{"/api/game", "/api/stats", "/media", "/v1/lookup", "/v1/export", "/v1/assets/{id}"} {
		if paths[path] == nil {
			t.Fatal("missing route", path)
		}
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	if schemas["GameResponse"] == nil || schemas["Selection"] == nil {
		t.Fatal("missing shared DTOs")
	}
}
func TestOptionalProtectionCoversBothUIAndAPI(t *testing.T) {
	token := strings.Repeat("s", 32)
	s := fixture(t, token)
	bearer := "Bearer " + token
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("rommeta:"+token))
	for _, path := range []string{"/", "/api", "/api/platforms", "/api/game?platform=Sega%20-%20Game%20Gear&game=Example", "/v1/platforms", "/v1/health", "/media?path=invalid"} {
		if got := request(s, path, ""); got.Code != http.StatusUnauthorized {
			t.Fatal(path, got.Code)
		}
		for _, auth := range []string{bearer, basic} {
			if got := request(s, path, auth); got.Code == http.StatusUnauthorized {
				t.Fatal("valid common credentials rejected", path, auth)
			}
		}
	}
	if got := request(s, "/api/platforms", "Bearer wrong"); got.Code != 401 {
		t.Fatal(got.Code)
	}
	for _, path := range []string{"/docs", "/openapi.json", "/docs/assets/swagger-ui.css"} {
		if got := request(s, path, ""); got.Code != 200 {
			t.Fatal(path, got.Code)
		}
	}
	var doc map[string]any
	_ = json.Unmarshal(request(s, "/openapi.json", "").Body.Bytes(), &doc)
	if len(doc["security"].([]any)) != 2 {
		t.Fatal("missing shared auth alternatives")
	}
}
func TestInvalidOptionsDoNotWeakenConfiguredProtection(t *testing.T) {
	root := t.TempDir()
	if _, err := New(Options{DataRoot: root, Token: "short"}); err == nil {
		t.Fatal("short configured token accepted")
	}
	if _, err := New(Options{DataRoot: root, ROMRoot: "roms"}); err == nil {
		t.Fatal("partial snapshot accepted")
	}
}

func TestFixedSelectionKeepsBrowserAndOneModeSpecificContract(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "catalog")
	roms := filepath.Join(root, "roms")
	game := filepath.Join(data, "platforms", "Sega - Game Gear", "Example")
	for _, dir := range []string{roms, game} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("synthetic cartridge bytes")
	files := map[string][]byte{filepath.Join(roms, "Example.gg"): payload, filepath.Join(filepath.Dir(game), "_platform.yaml"): []byte("name: Sega - Game Gear\n"), filepath.Join(game, "game.yaml"): []byte("title: Example\nreleases: {Example: {}}\n"), filepath.Join(game, "fingerprints.yaml"): []byte(fmt.Sprintf("items:\n  - release: Example\n    file: {size: %d, sha1: '%x'}\n", len(payload), sha1.Sum(payload)))}
	for path, body := range files {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{DataRoot: data, ROMRoot: roms, Platform: "gamegear"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/api/platforms", "/v1/selection", "/docs"} {
		if w := request(s, path, ""); w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	var doc map[string]any
	_ = json.Unmarshal(request(s, "/openapi.json", "").Body.Bytes(), &doc)
	paths := doc["paths"].(map[string]any)
	if paths["/v1/selection"] == nil || paths["/v1/lookup"] != nil || paths["/v1/export"] != nil || paths["/api/game"] == nil {
		t.Fatal("wrong composed mode", paths)
	}
	if strings.Contains(request(s, "/api", "").Body.String(), `"/v1/lookup"`) {
		t.Fatal("discovery advertises unsupported route")
	}
}
