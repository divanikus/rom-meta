package catalogapi

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"rommeta/internal/exporter"
	"rommeta/internal/romm"
	"strings"
	"testing"
)

func TestLookupDiscObservationTransportAndUnverifiedProjection(t *testing.T) {
	_, game, roms := fixture(t)
	data := filepath.Dir(filepath.Dir(filepath.Dir(game)))
	platform := filepath.Join(data, "platforms", "3DO Interactive Multiplayer")
	if err := os.MkdirAll(platform, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Dir(game), platform+"-old"); err != nil {
		t.Fatal(err)
	}
	// Retain the same small game/media fixture under its optical platform identity.
	if err := os.Rename(platform+"-old/Example", filepath.Join(platform, "Example")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(platform + "-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(platform, "_platform.yaml"), []byte("name: 3DO Interactive Multiplayer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 512)
	copy(header, "MComprHD")
	binary.BigEndian.PutUint32(header[8:12], 124)
	binary.BigEndian.PutUint32(header[12:16], 5)
	binary.BigEndian.PutUint64(header[32:40], 2448*100)
	binary.BigEndian.PutUint64(header[40:48], 124)
	binary.BigEndian.PutUint32(header[56:60], 2448*8)
	binary.BigEndian.PutUint32(header[60:64], 2448)
	header[64] = 1
	if err := os.WriteFile(filepath.Join(roms, "Example (USA).chd"), header, 0600); err != nil {
		t.Fatal(err)
	}
	observed, err := exporter.ObserveInput(roms, "3do", "Example (USA).chd")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewLookup(Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: data}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(roms); err != nil {
		t.Fatal(err)
	}
	q := LookupRequest{Platform: "3DO Interactive Multiplayer", Inputs: []exporter.ObservedInput{observed}, Media: "none"}
	w := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
	var selection Selection
	if err = json.Unmarshal(w.Body.Bytes(), &selection); err != nil || selection.Coverage.Included != 0 {
		t.Fatal(w.Body.String(), err)
	}
	q.GameBinding = true
	w = lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &selection); err != nil || len(selection.Inputs) != 1 {
		t.Fatal(w.Body.String(), err)
	}
	var wire map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	wireRecord := wire["inputs"].([]any)[0].(map[string]any)["record"].(map[string]any)
	if _, ok := wireRecord["release"]; ok || wireRecord["metadata_release"] == nil {
		t.Fatal("game-only JSON must omit empty exact release", wireRecord)
	}
	input := selection.Inputs[0]
	if input.Outer.SHA256 != "" || !input.Payload.Equal(exporter.PayloadProof{}) || input.Disc == nil || input.Record.Release != "" {
		t.Fatal(input)
	}
	card := selection.Cards[input.Record.Game]
	evidence, form, err := romm.ProjectSnapshot(input.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), romm.VerificationProof{}, nil)
	if err != nil || form["igdb_id"] != "123" {
		t.Fatal(evidence, form, err)
	}
	if len(evidence.Verification.Flags) != 0 || evidence.Verification.SHA1 != "" || evidence.Verification.SHA256 != "" || evidence.Verification.Authority != "" {
		t.Fatal("CHD borrowed donor dump verification", evidence.Verification)
	}
	forged := romm.VerificationProof{Size: input.Record.Size, SHA1: strings.Repeat("a", 40), MD5: strings.Repeat("b", 32), SHA256: strings.Repeat("c", 64), CRC32: "01234567"}
	if _, _, err = romm.ProjectSnapshot(input.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), forged, nil); err == nil {
		t.Fatal("fabricated optical payload proof accepted")
	}
}
