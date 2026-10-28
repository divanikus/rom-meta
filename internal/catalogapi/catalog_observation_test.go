package catalogapi

import (
	"encoding/binary"
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

func TestMAMEQuotedFingerprintSizesSurviveAPIProjection(t *testing.T) {
	root := t.TempDir()
	data, roms := filepath.Join(root, "catalog"), filepath.Join(root, "roms")
	game := filepath.Join(data, "platforms", "Arcade", "Example")
	put := func(path, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(filepath.Dir(game), "_platform.yaml"), "name: Arcade\nkind: arcade\nauthority: {source: mame}\n")
	put(filepath.Join(game, "game.yaml"), "title: Example\nreleases:\n  testset:\n    authority: {source: mame, entry_name: testset}\n    mame: {snapshot: synthetic}\nsources:\n  igdb:\n    matches:\n      - {release: testset, igdb_id: 123}\n")
	fingerprints := "items:\n  - id: mame.synthetic.testset.rom-component.1\n    release: testset\n    source: {kind: dat, name: MAME, entry_name: testset}\n    file: {name: component.bin, kind: rom-component, size: '2048', crc32: '01234567'}\n"
	put(filepath.Join(game, "fingerprints.yaml"), fingerprints)
	put(filepath.Join(roms, "testset.zip"), "synthetic unverified archive")
	other := filepath.Join(data, "platforms", "Arcade", "Other")
	put(filepath.Join(other, "game.yaml"), "title: Other\nreleases:\n  other:\n    authority: {source: mame, entry_name: other}\n    mame: {snapshot: synthetic}\n")
	put(filepath.Join(other, "fingerprints.yaml"), "items:\n  - release: other\n    file: {name: testset.bin, size: 1}\n")
	token := strings.Repeat("s", 32)
	s, err := NewLookup(Options{Token: token, Export: exporter.Options{DataRoot: data}})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := exporter.ObserveCatalogInput(roms, "testset.zip")
	if err != nil {
		t.Fatal(err)
	}
	w := lookupRequest(s, "POST", "/v1/lookup", token, LookupRequest{Platform: "Arcade", Inputs: []exporter.ObservedInput{observation}, GameBinding: true, Media: "none"})
	var selection Selection
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &selection) != nil || selection.Coverage.Included != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	input := selection.Inputs[0]
	card := selection.Cards[input.Record.Game]
	if card.Fingerprints.YAML != fingerprints || input.Record.ArcadeSnapshot != "synthetic" {
		t.Fatal("source YAML or MAME binding changed", input.Record, card.Fingerprints)
	}
	evidence, form, err := romm.ProjectSnapshot(input.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), romm.VerificationProof{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if form["igdb_id"] != "123" || len(evidence.Verification.Flags) != 0 || evidence.Verification.Authority != "" || evidence.Verification.SHA1 != "" {
		t.Fatal("set-name metadata gained payload verification", form, evidence.Verification)
	}
}

func TestPSPUMDHeaderLookupAndNativeProjectionRemainUnverified(t *testing.T) {
	root := t.TempDir()
	data, roms := filepath.Join(root, "catalog"), filepath.Join(root, "roms")
	game := filepath.Join(data, "platforms", "Sony - PlayStation Portable", "Example")
	put := func(path, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(filepath.Dir(game), "_platform.yaml"), "name: Sony - PlayStation Portable\n")
	put(filepath.Join(game, "game.yaml"), "title: Example\nreleases:\n  Example (USA) (En,Fr):\n    authority: {source: redump, entry_name: 'Example (USA) (En,Fr)'}\nsources:\n  igdb:\n    matches:\n      - {release: 'Example (USA) (En,Fr)', igdb_id: 123}\n")
	put(filepath.Join(game, "fingerprints.yaml"), "items:\n  - release: Example (USA) (En,Fr)\n    file: {name: game.iso, size: 204800, sha1: '0100000000000000000000000000000000000000'}\n")
	header := make([]byte, 512)
	copy(header, "MComprHD")
	binary.BigEndian.PutUint32(header[8:12], 124)
	binary.BigEndian.PutUint32(header[12:16], 5)
	binary.BigEndian.PutUint64(header[32:40], 204800)
	binary.BigEndian.PutUint64(header[40:48], 124)
	binary.BigEndian.PutUint32(header[56:60], 16384)
	binary.BigEndian.PutUint32(header[60:64], 2048)
	header[64] = 1
	put(filepath.Join(roms, "Renamed.chd"), string(header))
	observed, err := exporter.ObserveInput(roms, "psp", "Renamed.chd")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("s", 32)
	server, err := NewLookup(Options{Token: token, Export: exporter.Options{DataRoot: data}})
	if err != nil {
		t.Fatal(err)
	}
	query := LookupRequest{Platform: "Sony - PlayStation Portable", Inputs: []exporter.ObservedInput{observed}, GameBinding: true, Media: "none"}
	w := lookupRequest(server, "POST", "/v1/lookup", token, query)
	var result Selection
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Coverage.Included != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	input := result.Inputs[0]
	card := result.Cards[input.Record.Game]
	evidence, form, err := romm.ProjectSnapshot(input.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), romm.VerificationProof{}, nil)
	if err != nil || form["igdb_id"] != "123" || input.Record.Release != "" || input.Record.MetadataRelease != "Example (USA) (En,Fr)" || input.Disc == nil || input.Outer.SHA256 != "" || evidence.Verification.SHA1 != "" || len(evidence.Verification.Flags) != 0 || evidence.Verification.Authority != "" {
		t.Fatal(evidence, form, err)
	}
}

func TestDeclaredReleaseAliasWorksThroughCatalogAPI(t *testing.T) {
	_, game, files := fixture(t)
	path := filepath.Join(game, "game.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "  Example (USA):\n", "  Example (USA):\n    aliases: ['Local Example (USA)']\n", 1))
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	name := "Local Example (USA).zip"
	if err = os.WriteFile(filepath.Join(files, name), []byte("unverified bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	obs, err := exporter.ObserveCatalogInput(files, name)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("s", 32)
	s, err := NewLookup(Options{Token: token, Export: exporter.Options{DataRoot: filepath.Dir(filepath.Dir(filepath.Dir(game)))}})
	if err != nil {
		t.Fatal(err)
	}
	w := lookupRequest(s, "POST", "/v1/lookup", token, LookupRequest{Platform: "Sega - Game Gear", Inputs: []exporter.ObservedInput{obs}, GameBinding: true, Media: "none"})
	var result Selection
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Coverage.Included != 1 {
		t.Fatal(w.Body.String())
	}
	item := result.Inputs[0]
	if item.Record.Release != "Example (USA)" || item.Record.MatchedBy != "catalog-name" || item.Outer.SHA256 != "" {
		t.Fatal(item)
	}
	card := result.Cards[item.Record.Game]
	ev, form, err := romm.ProjectSnapshot(item.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), romm.VerificationProof{}, nil)
	if err != nil || form["igdb_id"] != "123" || ev.Verification.SHA1 != "" || ev.Verification.Authority != "" {
		t.Fatal(ev, form, err)
	}
}
