package catalogapi

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/exporter"
	"strings"
	"testing"
)

func TestMetadataCartridgeLookupSelectsRenamedPayloadAndKeepsUnknownBetaUnmatched(t *testing.T) {
	for _, tc := range []struct{ platform, system, extension string }{{"Nintendo - Satellaview", "satellaview", ".bs"}, {"Sega - 32X", "sega32x", ".32x"}} {
		t.Run(tc.system, func(t *testing.T) {
			root := t.TempDir()
			catalog := filepath.Join(root, "catalog")
			roms := filepath.Join(root, "roms")
			platform := tc.platform
			game := filepath.Join(catalog, "platforms", platform, "Example")
			put := func(path, text string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			payload := "synthetic cartridge beta bytes"
			release := "Example (USA) (Beta 1)"
			put(filepath.Join(catalog, "platforms", platform, "_platform.yaml"), "name: "+platform+"\n")
			put(filepath.Join(game, "game.yaml"), "title: Example\nreleases:\n  "+release+": {title: Example}\nsources:\n  igdb:\n    matches:\n      - release: "+release+"\n        igdb_id: 123\n")
			put(filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: %s\n    file: {size: %d, sha1: '%x'}\n", release, len(payload), sha1.Sum([]byte(payload))))
			names := []string{"Different (USA) (Beta) (1994-08-22)" + tc.extension, "Example (USA) (Beta) (1994-10-02)" + tc.extension}
			put(filepath.Join(roms, names[0]), payload)
			put(filepath.Join(roms, names[1]), "unknown bytes")
			var inputs []exporter.ObservedInput
			for _, name := range names {
				observed, err := exporter.ObserveInput(roms, tc.system, name)
				if err != nil {
					t.Fatal(err)
				}
				inputs = append(inputs, observed)
			}
			token := strings.Repeat("t", 32)
			server, err := NewLookup(Options{Token: token, Export: exporter.Options{DataRoot: catalog}, LookupCacheDir: "-"})
			if err != nil {
				t.Fatal(err)
			}
			query, _ := json.Marshal(LookupRequest{Platform: platform, Inputs: inputs, GameBinding: true, Media: "none"})
			request := httptest.NewRequest("POST", "/v1/lookup", bytes.NewReader(query))
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("lookup: %d %s", response.Code, response.Body.String())
			}
			var selected Selection
			if err := json.Unmarshal(response.Body.Bytes(), &selected); err != nil {
				t.Fatal(err)
			}
			if len(selected.Inputs) != 1 {
				t.Fatalf("wrong accepted inputs: %s", response.Body.String())
			}
			record := selected.Inputs[0].Record
			if record.ROM != names[0] || record.MatchedBy != "hash" || record.Release != release || record.Payload == nil || record.Catalog != nil {
				t.Fatalf("hash proof lost: %#v", record)
			}

		})
	}
}
