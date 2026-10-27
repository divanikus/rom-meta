package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyZIPMemberNameRemainsPortable(t *testing.T) {
	options, _, payload := variantFixture(t)
	name := filepath.Join(options.ROMRoot, "legacy.zip")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	raw := "M" + string([]byte{0x82}) + "rida.z64"
	w, err := z.CreateHeader(&zip.FileHeader{Name: raw, Method: zip.Deflate, NonUTF8: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, b.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	observed, err := ObserveInput(options.ROMRoot, "n64", "legacy.zip")
	if err != nil {
		t.Fatal(err)
	}
	if observed.Payload.Member != "Mérida.z64" {
		t.Fatalf("%q", observed.Payload.Member)
	}
	encoded, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip ObservedInput
	if err = json.Unmarshal(encoded, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !roundtrip.Payload.Equal(observed.Payload) {
		t.Fatal("JSON changed member proof")
	}
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan.Records {
		if r.ROM == "legacy.zip" {
			if r.Member != "Mérida.z64" || r.Variant != "Fixed" || r.MatchedBy != "hash" {
				t.Fatalf("%+v", r)
			}
			return
		}
	}
	t.Fatal("legacy ZIP not exported")
}
