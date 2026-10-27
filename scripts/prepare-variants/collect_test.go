package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func collectFixture(t *testing.T) (options, collectOptions, manifest) {
	t.Helper()
	o, m, _ := testFixture(t)
	o.Links = ""
	o.Out = ""
	c := collectOptions{Out: filepath.Join(filepath.Dir(o.Catalog), "collected.json"), GA: "ga.dat", Platforms: "nes", Subdir: "translations"}
	return o, c, m
}
func gaSource(t *testing.T, o options, m manifest, parent string) {
	t.Helper()
	f := m.Entries[0].File
	raw := fmt.Sprintf("<datafile><header><name>Nintendo - Famicom T-En</name></header><machine name=%q><rom name=\"Source.nes\" size=\"%d\" sha1=\"%s\"/></machine></datafile>", parent+" [T-En by Source Team v1.2]", f.Size, f.SHA1)
	put(t, filepath.Join(o.Sources, "ga.dat"), []byte(raw))
}
func TestCollectGASurvivesRenamedArchiveAndStagesOrdinaryYAML(t *testing.T) {
	o, c, original := collectFixture(t)
	gaSource(t, o, original, "Original (Japan)")
	before := tree(t, o.Catalog)
	roms := tree(t, o.ROMRoot)
	m, r, err := collectLinks(o, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prepared != 1 || r.Held != 0 || len(m.Entries) != 1 || m.Entries[0].Variant.BasedOn != "Original (Japan)" || m.Entries[0].Evidence.Relationship != "source-release-name" || m.Entries[0].Variant.Version != "1.2" {
		t.Fatalf("%+v / %+v", r, m)
	}
	if _, err = os.Stat(c.Out); !os.IsNotExist(err) {
		t.Fatal("collection preview wrote mapping")
	}
	o.Apply = true
	if _, _, err = collectLinks(o, c); err != nil {
		t.Fatal(err)
	}
	o.Links = c.Out
	o.Out = filepath.Join(filepath.Dir(o.Catalog), "compiled-stage")
	if _, err = run(o); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(before) != fmt.Sprint(tree(t, o.Catalog)) || fmt.Sprint(roms) != fmt.Sprint(tree(t, o.ROMRoot)) {
		t.Fatal("collector mutated input")
	}
}
func TestCollectRHAUsesHierarchyAndStructuredCredits(t *testing.T) {
	o, c, m := collectFixture(t)
	c.GA = ""
	c.RHA = "rha"
	f := m.Entries[0].File
	record := map[string]any{"info": map[string]any{"name": "English source title", "translatedTitle": true}, "rom": map[string]any{"size": f.Size, "sha1": f.SHA1}, "hacks": []any{map[string]any{"name": "English translation", "authors": []string{"Alice", "Bob"}, "version": "2.0", "labels": []string{"T-En"}}}}
	raw, _ := json.Marshal(record)
	put(t, filepath.Join(o.Sources, "rha", "database", oPlatform(), "Original (Japan).nes", "Translated source.nes", "romhack.json"), raw)
	got, r, err := collectLinks(o, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prepared != 1 || got.Entries[0].Variant.Title != "English source title" || got.Entries[0].Variant.Group != "Alice, Bob" || got.Entries[0].Variant.BasedOn != "Original (Japan)" {
		t.Fatalf("%+v", got)
	}
}
func hackHashSource(t *testing.T, o options, m manifest, parent, sha string) {
	t.Helper()
	f := m.Entries[0].File
	record := map[string]any{"machines": []any{map[string]any{"romName": "Source name.nes", "platform": "NES", "fileSize": fmt.Sprint(f.Size), "sha1": f.SHA1, "details": map[string]any{"hackName": "English source title", "author": "Team", "version": "1.0", "translationLanguages": []string{"en"}, "baseRom": map[string]any{"name": parent, "platform": "NES", "status": "APPROVED", "sha1": sha}}}}}
	raw, _ := json.Marshal(record)
	put(t, filepath.Join(o.Sources, "hh.json"), raw)
}
func TestCollectHackHashBaseHashesResolveWithoutGuessingName(t *testing.T) {
	o, c, m := collectFixture(t)
	c.GA = ""
	c.HackHash = "hh.json"
	hackHashSource(t, o, m, "Legacy source name", strings.Repeat("a", 40))
	got, r, err := collectLinks(o, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prepared != 1 || got.Entries[0].Variant.BasedOn != "Original (Japan)" || got.Entries[0].Evidence.Relationship != "source-base-hash" {
		t.Fatalf("%+v", got)
	}
	hackHashSource(t, o, m, "Original (USA)", strings.Repeat("a", 40))
	_, r, err = collectLinks(o, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prepared != 0 || r.Held != 1 {
		t.Fatal("accepted conflicting name/base hash")
	}
}
func TestCollectPreservesRomanNumberBoundaries(t *testing.T) {
	o, c, m := collectFixture(t)
	game := filepath.Join(o.Catalog, "platforms", oPlatform(), "Original")
	put(t, filepath.Join(game, "game.yaml"), []byte("title: Original\nreleases:\n  Final Fantasy I, II (Japan): {}\n  Final Fantasy III (Japan): {}\n"))
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte("items: []\n"))
	gaSource(t, o, m, "Final Fantasy I-II (Japan)")
	got, r, err := collectLinks(o, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prepared != 1 || got.Entries[0].Variant.BasedOn != "Final Fantasy I, II (Japan)" || got.Entries[0].Evidence.Normalization[0] != "token-preserving" {
		t.Fatalf("%+v", got)
	}
}
func TestCollectHoldsMissingMalformedOwnedAndUnsupported(t *testing.T) {
	for _, which := range []string{"missing parent", "malformed label", "already owned", "unsupported CHD"} {
		t.Run(which, func(t *testing.T) {
			o, c, m := collectFixture(t)
			parent := "Original (Japan)"
			if which == "missing parent" {
				parent = "Absent (Japan)"
			}
			gaSource(t, o, m, parent)
			switch which {
			case "malformed label":
				name := filepath.Join(o.Sources, "ga.dat")
				raw, _ := os.ReadFile(name)
				put(t, name, []byte(strings.Replace(string(raw), "Original (Japan) [T-En", "Original (Japan) [Bad [T-En", 1)))
			case "already owned":
				name := filepath.Join(o.Catalog, "platforms", oPlatform(), "Original", "fingerprints.yaml")
				f := m.Entries[0].File
				put(t, name, []byte(fmt.Sprintf("items:\n - release: Original (Japan)\n   file: {size: %d, sha1: %s}\n", f.Size, f.SHA1)))
			case "unsupported CHD":
				path := filepath.Join(o.ROMRoot, filepath.FromSlash(m.Entries[0].Inputs[0].Path))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(filepath.Dir(path), "Translated.chd"), []byte("bounded placeholder"))
			}
			got, r, err := collectLinks(o, c)
			if err != nil {
				t.Fatal(err)
			}
			if r.Prepared != 0 || r.Held != 1 || len(got.Holds) != 1 {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestCollectRejectsSourceDriftAndOutputOverlap(t *testing.T) {
	o, c, m := collectFixture(t)
	gaSource(t, o, m, "Original (Japan)")
	c.Out = filepath.Join(o.Catalog, "generated.json")
	if _, _, err := collectLinks(o, c); err == nil {
		t.Fatal("output can modify source")
	}
	c.Out = filepath.Join(filepath.Dir(o.Catalog), "fresh.json")
	put(t, filepath.Join(o.Sources, "ga.dat"), []byte("<new-format/>"))
	if _, _, err := collectLinks(o, c); err == nil {
		t.Fatal("source format drift accepted")
	}
}
