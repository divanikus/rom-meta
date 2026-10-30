package serveui

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserOpenAPIRoutesAndWireSchemas(t *testing.T) {
	h, err := Handler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	for _, path := range []string{"/api", "/api/platforms", "/api/games", "/api/game", "/api/fingerprints", "/api/export-preview", "/api/export-options", "/api/stats", "/media"} {
		if paths[path] == nil {
			t.Fatal("undocumented route", path)
		}
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["GameResponse"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"game", "profile", "source_blocks", "local_media"} {
		if props[key] == nil {
			t.Fatal("missing wire field", key)
		}
	}
	var walk func(any)
	walk = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, "#/components/schemas/")
				if schemas[name] == nil {
					t.Fatal("unresolved schema", ref)
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(doc)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/docs/assets/swagger-ui-bundle.js", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatal(w.Code, w.Header())
	}
}
