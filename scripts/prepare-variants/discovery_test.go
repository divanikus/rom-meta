package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
)

func discoveryFixture(t *testing.T, body string) (options, discoveryOptions, string) {
	t.Helper()
	o, _, g := testFixture(t)
	o.ROMRoot = ""
	o.Links = ""
	o.Out = ""
	o.Report = ""
	writeDiscoveryDAT(t, o, body)
	return o, discoveryOptions{TEN: []string{"ten.xml"}}, g
}
func writeDiscoveryDAT(t *testing.T, o options, body string) {
	t.Helper()
	put(t, filepath.Join(o.Sources, "ten.xml"), []byte(`<datafile><header><name>`+oPlatform()+` [T-En] Synthetic</name></header>`+body+`</datafile>`))
}
func outputMachine(name, hash string) string {
	return fmt.Sprintf(`<machine name="%s"><rom name="%s.nes" size="31" sha1="%s"/></machine>`, name, name, hash)
}
func discoverTest(t *testing.T, o options, d discoveryOptions, ds map[string]reviewDecision) (*manifest, *discoveryReport) {
	t.Helper()
	m, r, _, e := discover(o, d, ds)
	if e != nil {
		t.Fatal(e)
	}
	return m, r
}
func TestDiscoverySourceFirstGameOnlyAndExport(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			o, orig, g := testFixture(t)
			roms := o.ROMRoot
			o.ROMRoot = ""
			o.Links = ""
			o.Out = ""
			o.Report = ""
			if packed {
				catalogfstest.Pack(t, filepath.Dir(g))
			}
			writeDiscoveryDAT(t, o, strings.Replace(outputMachine("Original (Japan) [T-En by Team v1.0]", orig.Entries[0].File.SHA1), `size="31"`, `size="32"`, 1)+outputMachine("Unidentified [T-En by Other]", strings.Repeat("c", 40)))
			before := tree(t, o.Catalog)
			src := tree(t, o.Sources)
			m, r := discoverTest(t, o, discoveryOptions{TEN: []string{"ten.xml"}}, nil)
			if len(m.Entries) != 1 || r.Counts["review"] != 1 || m.Entries[0].Game != "Original" || m.Entries[0].Variant.BasedOn != "" || len(m.Entries[0].Inputs) != 0 {
				t.Fatalf("%+v %+v", m, r)
			}
			stage := o
			stage.Links = filepath.Join(filepath.Dir(o.Catalog), "prepared.json")
			stage.Out = filepath.Join(filepath.Dir(o.Catalog), "stage")
			stage.Apply = true
			saveLinks(t, stage, *m)
			if _, err := run(stage); err != nil {
				t.Fatal(err)
			}
			plan, err := exporter.Build(exporter.Options{DataRoot: stage.Out, ROMRoot: filepath.Join(roms, oPlatform(), "translations"), Platform: "nes", Out: filepath.Join(filepath.Dir(stage.Out), "export")})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Matched != 1 || len(plan.Records) != 1 || plan.Records[0].Release != "" || plan.Records[0].Fields["description"].Value != "Wrong default" || plan.Records[0].Variant == "" {
				t.Fatalf("%+v", plan)
			}
			if !reflect.DeepEqual(before, tree(t, o.Catalog)) || !reflect.DeepEqual(src, tree(t, o.Sources)) {
				t.Fatal("modified inputs")
			}
		})
	}
}
func TestDiscoveryDoesNotGuessAmbiguousTitlesOrSequels(t *testing.T) {
	o, d, g := discoveryFixture(t, outputMachine("Original [T-En by A]", strings.Repeat("b", 40))+outputMachine("Original II [T-En by B]", strings.Repeat("c", 40))+outputMachine("Original (Japan) [T-En by C]", strings.Repeat("d", 40)))
	put(t, filepath.Join(filepath.Dir(g), "Other Original", "game.yaml"), []byte("title: Original\nreleases: {Other edition: {title: Original}}\n"))
	put(t, filepath.Join(filepath.Dir(g), "Other Original", "fingerprints.yaml"), []byte("items: []\n"))
	m, r := discoverTest(t, o, d, nil)
	if len(m.Entries) != 1 || r.Counts["review"] != 2 || m.Entries[0].Game != "Original" {
		t.Fatalf("%+v %+v", m, r)
	}
	for _, row := range r.Rows {
		if strings.Contains(row.Name, "Original II") && len(row.Candidates) > 0 {
			t.Fatal("fuzzy sequel match")
		}
	}
}
func TestDiscoveryRenamedOutputUsesPublishedParentAndConflictsHold(t *testing.T) {
	hash := strings.Repeat("b", 40)
	o, d, g := discoveryFixture(t, outputMachine("Renamed Title [T-En by Team]", hash))
	ga := `<datafile><header><name>Famicom translations</name></header><machine name="Original (Japan) [T-En by Team]"><rom name="Published.nes" size="31" sha1="` + hash + `"/></machine></datafile>`
	put(t, filepath.Join(o.Sources, "ga.xml"), []byte(ga))
	d.GA = "ga.xml"
	m, r := discoverTest(t, o, d, nil)
	if len(m.Entries) != 1 || m.Entries[0].Game != "Original" || len(m.Snapshots) != 2 || r.Rows[0].Reason != "published output-to-parent relationship" {
		t.Fatalf("%+v %+v", m, r)
	}
	put(t, filepath.Join(filepath.Dir(g), "Elsewhere", "game.yaml"), []byte("title: Elsewhere\nreleases: {Elsewhere: {}}\n"))
	put(t, filepath.Join(filepath.Dir(g), "Elsewhere", "fingerprints.yaml"), []byte("items: []\n"))
	ga = strings.Replace(ga, "</datafile>", `<machine name="Elsewhere [T-En by Team]"><rom name="Other.nes" size="31" sha1="`+hash+`"/></machine></datafile>`, 1)
	put(t, filepath.Join(o.Sources, "ga.xml"), []byte(ga))
	m, r = discoverTest(t, o, d, nil)
	if len(m.Entries) != 0 || r.Counts["review"] != 1 {
		t.Fatal("conflicting explicit parents accepted")
	}
}
func TestDiscoveryHashRolesAndProviderPlatformScope(t *testing.T) {
	o, d, g := discoveryFixture(t, outputMachine("Foreign Alias [T-En by Team]", strings.Repeat("b", 40)))
	b, _ := os.ReadFile(filepath.Join(g, "game.yaml"))
	b = append(b, []byte("sources:\n  launchbox:\n    matches:\n      - title: Foreign Alias\n        platform: Sony Playstation\n")...)
	put(t, filepath.Join(g, "game.yaml"), b)
	m, _ := discoverTest(t, o, d, nil)
	if len(m.Entries) != 0 {
		t.Fatal("foreign provider alias accepted")
	}
	b = bytes.ReplaceAll(b, []byte("Sony Playstation"), []byte(oPlatform()))
	put(t, filepath.Join(g, "game.yaml"), b)
	m, _ = discoverTest(t, o, d, nil)
	if len(m.Entries) != 1 {
		t.Fatal("same-platform alias missed")
	}
	c, err := openDiscoveryCatalog(o)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.load(oPlatform()); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{strings.Repeat("b", 40), strings.Repeat("a", 40)} {
		got, reason := sourceBaseTargets(c, oPlatform(), sourceRecord{Parent: "Original (Japan)", Base: map[string]any{"sha1": hash}})
		if hash[0] == 'b' && (len(got) != 0 || reason == "") {
			t.Fatal("modified hash treated as base hash")
		}
		if hash[0] == 'a' && (len(got) != 1 || got[0].Game != "Original") {
			t.Fatal("base did not resolve to game")
		}
	}
}
func TestDiscoveryCHDAndWholeGroups(t *testing.T) {
	o, d, _ := discoveryFixture(t, `<machine name="Original (Japan) [T-En by Team]"><disk name="Original (Japan) [T-En by Team]" sha1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"/></machine><machine name="Set [T-En by Team]"><rom name="a.bin" size="31" sha1="cccccccccccccccccccccccccccccccccccccccc"/><rom name="b.bin" size="31" sha1="dddddddddddddddddddddddddddddddddddddddd"/></machine><machine name="_BIOS"><rom name="bios.bin" size="31" sha1="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"/></machine>`)
	m, r := discoverTest(t, o, d, nil)
	if len(m.Entries) != 1 || m.Entries[0].File.Kind != "chd-combined-sha1" || m.Entries[0].File.Size != 0 || r.Counts["unsupported"] != 1 || r.Counts["skipped-group"] != 1 {
		t.Fatalf("%+v %+v", m, r)
	}
}
func TestDiscoveryReviewPersistsAcrossDATRevisionAndStagesRepeatedly(t *testing.T) {
	hash := strings.Repeat("b", 40)
	o, d, _ := discoveryFixture(t, outputMachine("Renamed [T-En by Team]", hash))
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(work, o, d, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if err := discoveryCLI(work, options{}, discoveryOptions{}, true, strings.NewReader("f original\n1\nq\n"), &out); err != nil {
		t.Fatal(err)
	}
	ds, _, err := loadDecisions(work)
	if err != nil || len(ds) != 1 {
		t.Fatal(err, ds)
	}
	staged, err := workspaceOptions(work, options{Out: filepath.Join(filepath.Dir(o.Catalog), "stage"), Media: "none"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := run(staged)
	if err != nil || r.Status != "preview" {
		t.Fatal(r, err)
	}
	staged.Apply = true
	r, err = run(staged)
	if err != nil {
		t.Fatal(err)
	}
	before := tree(t, staged.Out)
	r, err = run(staged)
	if err != nil || r.Status != "unchanged" || !reflect.DeepEqual(before, tree(t, staged.Out)) {
		t.Fatal(r, err)
	}
	writeDiscoveryDAT(t, o, outputMachine("Another Filename [T-En by Team]", hash))
	if err = discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(work, "report.json"))
	var report discoveryReport
	if err = json.Unmarshal(raw, &report); err != nil || report.Counts["ready"] != 1 || report.Rows[0].Reason != "saved reviewer choice" {
		t.Fatal(string(raw), err)
	}
	writeDiscoveryDAT(t, o, outputMachine("Another Filename [T-En by Team]", strings.Repeat("c", 40)))
	if err = discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err = workspaceOptions(work, options{}); err == nil {
		t.Fatal("staged empty/unknown output")
	}
}
func TestDiscoveryNewOriginalIsAnUnspecifiedMetadataAnchor(t *testing.T) {
	o, d, _ := discoveryFixture(t, outputMachine("Renamed [T-En by Team]", strings.Repeat("b", 40)))
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(work, o, d, true, strings.NewReader("n Missing Original\n"), &out); err != nil {
		t.Fatal(err)
	}
	stage, err := workspaceOptions(work, options{Out: filepath.Join(filepath.Dir(o.Catalog), "stage"), Media: "none", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := run(stage)
	if err != nil {
		t.Fatal(err, r)
	}
	path := filepath.Join(stage.Out, "platforms", oPlatform(), "Missing Original")
	g, _ := os.ReadFile(filepath.Join(path, "game.yaml"))
	fp, _ := os.ReadFile(filepath.Join(path, "fingerprints.yaml"))
	doc, err := catalogyaml.Parse(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(g), "edition_precision: unspecified") || strings.Contains(string(fp), "release:") || catalogyaml.Value(doc.Root(), "variants") == nil {
		t.Fatalf("%s\n%s", g, fp)
	}
	if _, err = run(stage); err != nil {
		t.Fatal("new-original repeat:", err)
	}
	put(t, filepath.Join(o.Catalog, "platforms", oPlatform(), "missing original", "game.yaml"), []byte("title: Existing\n"))
	if _, err = run(stage); err == nil {
		t.Fatal("case-only new-game collision accepted")
	}
}
func TestDiscoveryOwnedOutputsAndUntouchedBundleConflict(t *testing.T) {
	o, d, g := discoveryFixture(t, outputMachine("Original (Japan) [T-En by Team]", strings.Repeat("b", 40)))
	m, _ := discoverTest(t, o, d, nil)
	// A different, untouched game acquires this identity after discovery.
	other := filepath.Join(filepath.Dir(g), "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Other\nreleases: {Other: {}}\n"))
	put(t, filepath.Join(other, "fingerprints.yaml"), []byte("items:\n- release: Other\n  file: {name: same.nes, size: 31, sha1: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}\n"))
	stage := o
	stage.Links = filepath.Join(filepath.Dir(o.Catalog), "links-ready.json")
	stage.Out = filepath.Join(filepath.Dir(o.Catalog), "stage")
	stage.Apply = true
	saveLinks(t, stage, *m)
	if _, err := run(stage); err == nil {
		t.Fatal("untouched conflicting bundle ignored")
	}
	m, r := discoverTest(t, o, d, nil)
	if len(m.Entries) != 0 || r.Counts["review"] != 1 {
		t.Fatal("existing owner conflict accepted")
	}
	writeDiscoveryDAT(t, o, outputMachine("Unidentified [T-En by Team]", strings.Repeat("b", 40)))
	m, r = discoverTest(t, o, d, nil)
	if len(m.Entries) != 0 || r.Counts["already-owned"] != 1 {
		t.Fatal("existing owner not recognized")
	}
}
func TestDiscoveryWorkspaceGuards(t *testing.T) {
	o, d, _ := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(filepath.Join(o.Catalog, "work"), o, d, false, strings.NewReader(""), &out); err == nil {
		t.Fatal("workspace overlaps catalog")
	}
	if err := discoveryCLI(work, o, d, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceOptions(work, options{Out: filepath.Join(work, "stage")}); err == nil {
		t.Fatal("stage inside workspace")
	}
	ds, _, _ := loadDecisions(work)
	for id := range ds {
		delete(ds, id)
	}
	ds[strings.Repeat("e", 64)] = reviewDecision{Exclude: true}
	if err := saveDecisions(work, ds); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceOptions(work, options{}); err == nil {
		t.Fatal("stale generated links after decision edit")
	}
	if err := discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(work, "links.json"), []byte("edited"))
	if err := discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err == nil {
		t.Fatal("overwrote manual edits")
	}
}

func TestDiscoveryPlatformAliasesAndExplicitCrossPlatformReview(t *testing.T) {
	o, d, g := discoveryFixture(t, outputMachine("Unknown [T-En by Team]", strings.Repeat("b", 40)))
	c, err := openDiscoveryCatalog(o)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.platform("Nintendo - Famicom")
	if err != nil || p != oPlatform() {
		t.Fatal(p, err)
	}
	target := filepath.Join(o.Catalog, "platforms", "Sony - PlayStation")
	for _, n := range []string{"game.yaml", "fingerprints.yaml"} {
		b, _ := os.ReadFile(filepath.Join(g, n))
		put(t, filepath.Join(target, "Original", n), b)
	}
	put(t, filepath.Join(target, "_platform.yaml"), []byte("name: Sony - PlayStation\n"))
	put(t, filepath.Join(target, "_catalog.yaml"), []byte("name: Synthetic\n"))
	work := filepath.Join(filepath.Dir(o.Catalog), "cross")
	var out bytes.Buffer
	if err = discoveryCLI(work, o, d, true, strings.NewReader("b psx/Original\n"), &out); err != nil {
		t.Fatal(err)
	}
	stage, err := workspaceOptions(work, options{Out: filepath.Join(filepath.Dir(o.Catalog), "stage"), Media: "none", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = run(stage); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoveryReviewAllCanCorrectAutomaticChoice(t *testing.T) {
	o, d, _ := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(work, o, d, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if err := discoveryCLI(work, options{ReviewAll: true}, discoveryOptions{}, true, strings.NewReader("x\n"), &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(work, "report.json"))
	var r discoveryReport
	_ = json.Unmarshal(raw, &r)
	if r.Counts["excluded"] != 1 {
		t.Fatal(string(raw))
	}
}

func TestDiscoveryUsesNamedContainerForRenamedPayload(t *testing.T) {
	o, d, _ := discoveryFixture(t, `<machine name="Original (Japan) [T-En by Team]"><rom name="Renamed title [T-En by Team].nes" size="31" sha1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"/></machine>`)
	m, r := discoverTest(t, o, d, nil)
	if len(m.Entries) != 1 || m.Entries[0].Game != "Original" || r.Counts["ready"] != 1 {
		t.Fatal(m, r)
	}
}
func TestDiscoveryRejectsStalePinsAndChangedDocumentAtStaging(t *testing.T) {
	for _, change := range []string{"source", "catalog"} {
		t.Run(change, func(t *testing.T) {
			o, d, g := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
			work := filepath.Join(filepath.Dir(o.Catalog), "work")
			var out bytes.Buffer
			if err := discoveryCLI(work, o, d, false, strings.NewReader(""), &out); err != nil {
				t.Fatal(err)
			}
			stage, err := workspaceOptions(work, options{Out: filepath.Join(filepath.Dir(o.Catalog), "stage"), Media: "none", Apply: true})
			if err != nil {
				t.Fatal(err)
			}
			if change == "source" {
				writeDiscoveryDAT(t, o, outputMachine("Original [T-En by Team]", strings.Repeat("c", 40)))
			} else {
				b, _ := os.ReadFile(filepath.Join(g, "game.yaml"))
				put(t, filepath.Join(g, "game.yaml"), append(b, []byte("# changed\n")...))
			}
			if _, err = run(stage); err == nil {
				t.Fatal("stale selection was staged")
			}
			if _, err = os.Stat(stage.Out); !os.IsNotExist(err) {
				t.Fatal("published stale stage")
			}
		})
	}
}
