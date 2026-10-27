package catalogapi

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/exporter"
)

func TestVariantExportAPIEqualsLocalAndKeepsModifiedProof(t *testing.T) {
	for _, gameOnly := range []bool{false, true} {
		t.Run(fmt.Sprint("game-only=", gameOnly), func(t *testing.T) {
			_, game, roms := fixture(t)
			root := filepath.Dir(roms)
			appendFile := func(path, extra string) {
				old, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, append(old, []byte(extra)...), 0600); err != nil {
					t.Fatal(err)
				}
			}
			appendFile(filepath.Join(game, "game.yaml"), "variants:\n  English:\n    title: New English Title\n    kind: translation\n    based_on: Example (USA)\n    languages: [en]\n    group: Example team\n    version: '1.0'\n")
			if gameOnly {
				name := filepath.Join(game, "game.yaml")
				raw, _ := os.ReadFile(name)
				if err := os.WriteFile(name, []byte(strings.Replace(string(raw), "    based_on: Example (USA)\n", "", 1)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			payload := []byte("synthetic translated Game Gear bytes")
			appendFile(filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("  - variant: English\n    file:\n      name: Source.gg\n      size: %d\n      sha1: %x\n", len(payload), sha1.Sum(payload)))
			if err := os.WriteFile(filepath.Join(roms, "Renamed.gg"), payload, 0600); err != nil {
				t.Fatal(err)
			}
			options := exporter.Options{DataRoot: filepath.Join(root, "catalog"), ROMRoot: roms, Platform: "gamegear", System: "gamegear", ProfilePath: filepath.Join(root, "profile.yaml"), Out: filepath.Join(root, "output")}
			local, err := exporter.Build(options)
			if err != nil {
				t.Fatal(err)
			}
			inputs, err := exporter.ObserveExport(options)
			if err != nil {
				t.Fatal(err)
			}
			server, err := NewLookup(Options{Token: strings.Repeat("s", 32), Export: options})
			if err != nil {
				t.Fatal(err)
			}
			// HTTP resolver receives observations only, and cannot open client ROMs.
			if err = os.RemoveAll(roms); err != nil {
				t.Fatal(err)
			}
			response := lookupRequest(server, "POST", "/v1/export", strings.Repeat("s", 32), ExportRequest{Platform: "Sega - Game Gear", System: "gamegear", Inputs: inputs})
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			var remote ExportResponse
			if err = json.Unmarshal(response.Body.Bytes(), &remote); err != nil {
				t.Fatal(err)
			}
			for i, r := range local.Records {
				if r.Variant == "" {
					continue
				}
				got := remote.Plan.Records[i]
				if !reflect.DeepEqual(r.Fields, got.Fields) || got.Variant != "English" || got.Release != "" || got.MetadataRelease != "Example (USA)" ||
					got.VariantInfo.Group != "Example team" || got.Payload.SHA1 != fmt.Sprintf("%x", sha1.Sum(payload)) {
					t.Fatalf("local/API variant mismatch: %+v / %+v", r, got)
				}
				if gameOnly && (got.VariantInfo.BasedOn != "" || got.Fields["description"].Selection != "game") {
					t.Fatalf("game-only link lost: %+v", got)
				}
				if got.Fields["description"].Value != "Offline prose" || !strings.Contains(got.Fields["title"].Value, "New English Title") {
					t.Fatal(got.Fields)
				}
				return
			}
			t.Fatal("no variant exported")
		})
	}
}
