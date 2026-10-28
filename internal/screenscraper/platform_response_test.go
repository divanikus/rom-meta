package screenscraper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryRejectsAnExplicitlyDifferentSystem(t *testing.T) {
	for _, tc := range []struct {
		name, id, parent string
		want             bool
	}{
		{"same system", "106", "", true},
		{"declared child", "999", "106", true},
		{"different system", "3", "", false},
		{"legacy response without system", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"jeu": map[string]any{"id": "42", "nom": "Example", "systeme": map[string]any{"id": tc.id, "parentid": tc.parent}}}})
			}))
			defer server.Close()
			for _, query := range []screenScraperQuery{{Term: "Example"}, {ROMName: "Example.fds", MD5: "01234567890123456789012345678901", Size: 4}} {
				_, found, err := queryGameWithFingerprint(server.Client(), sourceSettings{APIBaseURL: server.URL}, "106", query)
				if err != nil || found != tc.want {
					t.Fatalf("found %v, want %v; %v", found, tc.want, err)
				}
			}
		})
	}
}
