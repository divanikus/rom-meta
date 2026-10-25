package catalogapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIAndSwaggerArePublicButDataRemainsAuthenticated(t *testing.T) {
	lookup, _, _, _ := lookupFixture(t)
	snapshot, _, _ := fixture(t)
	for _, test := range []struct {
		name    string
		handler http.Handler
		lookup  bool
	}{{"catalog", lookup, true}, {"snapshot", snapshot, false}} {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			for _, path := range []string{"/openapi.json", "/docs", "/docs/assets/swagger-ui.css", "/docs/assets/swagger-ui-bundle.js", "/docs/assets/LICENSE"} {
				w := httptest.NewRecorder()
				test.handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 200 {
					t.Fatal(path, w.Code)
				}
				if path == "/openapi.json" {
					if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
						t.Fatal(err)
					}
				}
				if path == "/docs" && !strings.Contains(w.Body.String(), "SwaggerUIBundle") {
					t.Fatal("missing interactive UI")
				}
			}
			if document["openapi"] != "3.0.3" {
				t.Fatal(document["openapi"])
			}
			paths := document["paths"].(map[string]any)
			_, hasLookup := paths["/v1/lookup"]
			_, hasSelection := paths["/v1/selection"]
			if hasLookup != test.lookup || hasSelection == test.lookup {
				t.Fatal("wrong mode paths", paths)
			}
			if len(document["security"].([]any)) != 1 {
				t.Fatal("missing bearer requirement")
			}
			component := document["components"].(map[string]any)
			scheme := component["securitySchemes"].(map[string]any)["bearerAuth"].(map[string]any)
			if scheme["scheme"] != "bearer" {
				t.Fatal(scheme)
			}
			for _, path := range []string{"/v1/health", "/v1/assets/unknown"} {
				w := httptest.NewRecorder()
				test.handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 401 {
					t.Fatal("data authorization bypass", path, w.Code)
				}
			}
			w := httptest.NewRecorder()
			test.handler.ServeHTTP(w, httptest.NewRequest("POST", "/docs", nil))
			if w.Code != 405 {
				t.Fatal(w.Code)
			}
			w = httptest.NewRecorder()
			test.handler.ServeHTTP(w, httptest.NewRequest("HEAD", "/openapi.json", nil))
			if w.Code != 200 || w.Body.Len() != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			test.handler.ServeHTTP(w, httptest.NewRequest("GET", "/docs/assets/../../config/secrets.yaml", nil))
			if w.Code != 404 {
				t.Fatal(w.Code)
			}
		})
	}
}
