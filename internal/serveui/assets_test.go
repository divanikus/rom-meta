package serveui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestBrowserAssetRoutesAndBundleInitialization(t *testing.T) {
	handler, err := Handler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`(?:src|href)="(/[^"?]+\.(?:js|css))"`).FindAllStringSubmatch(indexHTML, -1)
	if len(links) == 0 {
		t.Fatal("index has no browser assets")
	}
	for _, link := range links {
		path := link[1]
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			mime := "text/javascript; charset=utf-8"
			if strings.HasSuffix(path, ".css") {
				mime = "text/css; charset=utf-8"
			}
			if w.Code != 200 || w.Header().Get("Content-Type") != mime || w.Body.Len() == 0 {
				t.Fatalf("%d %s", w.Code, w.Header())
			}
			body := w.Body.String()
			if path == "/catalog-edit.js" {
				fold, editor, boot := strings.Index(body, "const catalogUI"), strings.Index(body, "const catalogInline="), strings.LastIndex(body, "catalogCapabilities();")
				if fold < 0 || editor <= fold || boot <= editor || strings.Count(body, "catalogCapabilities();") != 1 {
					t.Fatal("merge/editor bundle is missing a part or initializes out of order")
				}
			}
			if path == "/export.js" {
				setup, review, profile := strings.Index(body, "const nativeExportUI="), strings.Index(body, "function exportIssueDraft("), strings.Index(body, "const exportArtworkVariants=")
				if setup < 0 || review <= setup || profile <= review {
					t.Fatal("export bundle is missing a part or changed order")
				}
			}
			for _, method := range []string{http.MethodPost, http.MethodHead} {
				denied := httptest.NewRecorder()
				handler.ServeHTTP(denied, httptest.NewRequest(method, path, nil))
				if denied.Code != http.StatusMethodNotAllowed {
					t.Fatalf("%s status %d", method, denied.Code)
				}
			}
		})
	}
}
