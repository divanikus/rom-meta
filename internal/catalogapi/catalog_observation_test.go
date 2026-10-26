package catalogapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/exporter"
	"rommeta/internal/romm"
)

func TestCatalogNamesAcrossRepresentationsRemainUnverified(t *testing.T) {
	_, game, files := fixture(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(game)))
	// This platform intentionally has no ES-DE launch reader.
	header := filepath.Join(filepath.Dir(game), "_platform.yaml")
	if err := os.WriteFile(header, []byte("name: Synthetic Catalog Platform\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("s", 32)
	s, err := NewLookup(Options{Token: token, Export: exporter.Options{DataRoot: root}})
	if err != nil {
		t.Fatal(err)
	}
	caps := s.catalog.Platforms
	if len(caps) != 1 || !caps[0].Supported || !caps[0].NameBinding || len(caps[0].ExportSystems) != 0 {
		t.Fatal(caps)
	}
	for _, ext := range []string{".rvz", ".wux", ".xiso", ".zip"} {
		name := "Example (USA)" + ext
		if err := os.WriteFile(filepath.Join(files, name), []byte("unverified bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		obs, err := exporter.ObserveCatalogInput(files, name)
		if err != nil {
			t.Fatal(err)
		}
		query := LookupRequest{Platform: caps[0].Platform, Inputs: []exporter.ObservedInput{obs}, Media: "none"}
		w := lookupRequest(s, "POST", "/v1/lookup", token, query)
		var result Selection
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Coverage.Included != 0 {
			t.Fatal(w.Body.String())
		}
		query.GameBinding = true
		w = lookupRequest(s, "POST", "/v1/lookup", token, query)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Coverage.Included != 1 {
			t.Fatal(w.Body.String())
		}
		input := result.Inputs[0]
		card := result.Cards[input.Record.Game]
		if input.Record.Release != "Example (USA)" || input.Record.MatchedBy != "catalog-name" || input.Catalog == nil || input.Outer.SHA256 != "" {
			t.Fatal(input)
		}
		evidence, form, err := romm.ProjectSnapshot(input.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), romm.VerificationProof{}, nil)
		if err != nil || form["igdb_id"] != "123" || len(evidence.Verification.Flags) != 0 || evidence.Verification.SHA1 != "" || evidence.Verification.Authority != "" {
			t.Fatal(evidence, form, err)
		}
	}
	// Regions/labels are never silently discarded in the complete-name mode.
	obs, _ := exporter.ObserveCatalogInput(files, "Example (USA).zip")
	for _, name := range []string{"Example (Europe).zip", "Example (USA) [Translation].zip", "translations/Example (USA).zip"} {
		obs.ROM = name
		obs.Catalog.Files[0].Name = filepath.Base(name)
		q := LookupRequest{Platform: caps[0].Platform, Inputs: []exporter.ObservedInput{obs}, GameBinding: true, Media: "none"}
		w := lookupRequest(s, "POST", "/v1/lookup", token, q)
		var result Selection
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Coverage.Included != 0 {
			t.Fatal(name, w.Body.String())
		}
	}
}
