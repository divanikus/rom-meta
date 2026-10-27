package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
)

func tenFixture(t *testing.T, chd bool) (options, manifest) {
	t.Helper()
	o, m, game := testFixture(t)
	e := &m.Entries[0]
	e.Variant.BasedOn = ""
	e.Evidence.Relationship = "reviewed"
	e.Evidence.Links[0]["parent"] = "Original"
	e.Inputs = nil
	row := fmt.Sprintf(`<rom name="Translated.nes" size="%d" sha1="%s"/>`, e.File.Size, e.File.SHA1)
	name := "Translated.nes"
	if chd {
		e.Platform = "Sony - PlayStation"
		target := filepath.Join(o.Catalog, "platforms", e.Platform)
		for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
			data, err := os.ReadFile(filepath.Join(game, name))
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(target, "Original", name), data)
		}
		put(t, filepath.Join(target, "_platform.yaml"), []byte("name: Sony - PlayStation\n"))
		put(t, filepath.Join(target, "_catalog.yaml"), []byte("name: Synthetic\n"))
		name = "Renamed English Game [T-En by Team v1.2]"
		h := make([]byte, 256)
		copy(h, "MComprHD")
		binary.BigEndian.PutUint32(h[8:12], 124)
		binary.BigEndian.PutUint32(h[12:16], 5)
		binary.BigEndian.PutUint64(h[32:40], 23520)
		binary.BigEndian.PutUint64(h[40:48], 124)
		binary.BigEndian.PutUint32(h[56:60], 9408)
		binary.BigEndian.PutUint32(h[60:64], 2352)
		h[64] = 1
		h[84] = 2
		put(t, filepath.Join(o.ROMRoot, e.Platform, "translations", "Renamed.chd"), h)
		row = fmt.Sprintf(`<disk name="%s" sha1="%s"/>`, name, hex.EncodeToString(h[84:104]))
	}
	e.DAT = &datReference{Path: "ten.xml", Name: name}
	e.File = fingerprint{}
	put(t, filepath.Join(o.Sources, "ten.xml"), []byte(fmt.Sprintf(`<datafile><header><name>%s [T-En] Collection</name></header><machine name="Outputs">%s</machine></datafile>`, e.Platform, row)))
	saveLinks(t, o, m)
	return o, m
}

func TestTEnGameOnlyStageDoesNotRequireLocalROMs(t *testing.T) {
	for _, chd := range []bool{false, true} {
		t.Run(fmt.Sprint("chd=", chd), func(t *testing.T) {
			o, m := tenFixture(t, chd)
			romRoot := o.ROMRoot
			o.ROMRoot = ""
			before := tree(t, o.Catalog)
			sources := tree(t, o.Sources)
			r, err := run(o)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "preview" || r.Entries != 1 {
				t.Fatalf("%+v", r)
			}
			if _, err = os.Stat(o.Out); !os.IsNotExist(err) {
				t.Fatal("preview wrote stage")
			}
			o.Apply = true
			r, err = run(o)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(o.Out, "platforms", m.Entries[0].Platform, "Original", "game.yaml")
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := catalogyaml.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			node := catalogyaml.Value(catalogyaml.Value(doc.Root(), "variants"), m.Entries[0].Variant.Key)
			if catalogyaml.Value(node, "based_on") != nil || !strings.Contains(string(raw), "modified-output") {
				t.Fatalf("game-only provenance: %s", raw)
			}
			fp, err := os.ReadFile(filepath.Join(filepath.Dir(name), "fingerprints.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if chd && (!strings.Contains(string(fp), "chd-combined-sha1") || strings.Contains(string(fp), "size: 256")) {
				t.Fatal(string(fp))
			}
			staged := tree(t, o.Out)
			r, err = run(o)
			if err != nil || r.Status != "unchanged" || !reflect.DeepEqual(staged, tree(t, o.Out)) {
				t.Fatalf("repeat %v %+v", err, r)
			}
			system, roms := "nes", filepath.Join(romRoot, oPlatform())
			if chd {
				system, roms = "psx", filepath.Join(romRoot, m.Entries[0].Platform)
			}
			plan, err := exporter.Build(exporter.Options{DataRoot: o.Out, ROMRoot: roms, Platform: system, System: system, Out: filepath.Join(filepath.Dir(o.Out), "export")})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, record := range plan.Records {
				if record.Variant != m.Entries[0].Variant.Key {
					continue
				}
				found = true
				if record.Release != "" || record.MetadataRelease != "Original (USA)" || record.VariantInfo.BasedOn != "" || record.Fields["description"].Value != "Wrong default" {
					t.Fatalf("%+v", record)
				}
				if chd && record.MatchedBy != "variant-chd-header" {
					t.Fatal("CHD digest reinterpreted as payload")
				}
			}
			if !found {
				t.Fatal("prepared translation not recognized")
			}
			if !reflect.DeepEqual(before, tree(t, o.Catalog)) || !reflect.DeepEqual(sources, tree(t, o.Sources)) {
				t.Fatal("input data changed")
			}
		})
	}
}

func TestTEnRejectsMissingAmbiguousOrConflictingEvidence(t *testing.T) {
	for _, which := range []string{"missing output", "duplicate name", "wrong platform", "wrong digest", "no game source", "no DAT or input", "changed pin", "partial group", "documentation"} {
		t.Run(which, func(t *testing.T) {
			o, m := tenFixture(t, false)
			o.ROMRoot = ""
			raw, _ := os.ReadFile(filepath.Join(o.Sources, "ten.xml"))
			switch which {
			case "missing output":
				m.Entries[0].DAT.Name = "Absent.nes"
			case "duplicate name":
				raw = []byte(strings.Replace(string(raw), "</machine>", strings.Split(strings.Split(string(raw), "<machine name=\"Outputs\">")[1], "</machine>")[0]+"</machine>", 1))
			case "wrong platform":
				m.Entries[0].Platform = "Sony - PlayStation"
			case "wrong digest":
				m.Entries[0].File.SHA1 = strings.Repeat("b", 40)
			case "no game source":
				m.Entries[0].Evidence.Links = nil
			case "no DAT or input":
				m.Entries[0].DAT = nil
			case "changed pin":
				m.Snapshots = append(m.Snapshots, snapshot{Path: "ten.xml", SHA256: strings.Repeat("b", 64)})
			case "partial group":
				raw = []byte(strings.Replace(string(raw), "</machine>", `<rom name="Second.nes" size="1" sha1="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"/></machine>`, 1))
			case "documentation":
				raw = []byte(strings.Replace(string(raw), `name="Outputs"`, `name="_Docs"`, 1))
			}
			put(t, filepath.Join(o.Sources, "ten.xml"), raw)
			saveLinks(t, o, m)
			o.Apply = true
			if _, err := run(o); err == nil {
				t.Fatal("accepted " + which)
			}
			if _, err := os.Stat(o.Out); !os.IsNotExist(err) {
				t.Fatal("invalid mapping wrote stage")
			}
		})
	}
}

func TestTEnOptionalLocalCHDBindingChecksHeader(t *testing.T) {
	o, m := tenFixture(t, true)
	m.Entries[0].Inputs = []input{{Path: m.Entries[0].Platform + "/translations/Renamed.chd"}}
	saveLinks(t, o, m)
	if _, err := run(o); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.ROMRoot, filepath.FromSlash(m.Entries[0].Inputs[0].Path))
	h, _ := os.ReadFile(path)
	h[84] = 3
	put(t, path, h)
	if _, err := run(o); err == nil {
		t.Fatal("accepted changed combined SHA1")
	}
}

func TestReviewedGameOnlyLinkWithExistingLocalPayload(t *testing.T) {
	o, m, _ := testFixture(t)
	m.Entries[0].Variant.BasedOn = ""
	m.Entries[0].Evidence.Relationship = "reviewed"
	m.Entries[0].Evidence.Links[0]["parent"] = "Original"
	saveLinks(t, o, m)
	o.Apply = true
	r, err := run(o)
	if err != nil || r.Status != "staged" {
		t.Fatalf("%+v %v", r, err)
	}
	raw, err := os.ReadFile(filepath.Join(o.Out, "platforms", oPlatform(), "Original", "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalogyaml.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, base, err := catalogyaml.VariantChain(doc.Root(), m.Entries[0].Variant.Key)
	if err != nil || base != "" {
		t.Fatalf("invented exact parent: %q %v", base, err)
	}
}
