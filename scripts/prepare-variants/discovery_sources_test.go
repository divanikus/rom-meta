package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func translationXML(platform, label, body string) []byte {
	return []byte(`<datafile><header><name>` + platform + ` [` + label + `] Collection</name></header>` + body + `</datafile>`)
}
func putDATZip(t *testing.T, o options, name string, members [][2]string) {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, m := range members {
		w, err := z.Create(m[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(m[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(o.Sources, name), b.Bytes())
}
func TestAutomaticSourcesKeepDeclaredTranslationLanguages(t *testing.T) {
	o, _, _ := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
	put(t, filepath.Join(o.Sources, "english-filename.dat"), translationXML(oPlatform(), "T-Ru", outputMachine("Original [T-Ru by Russian Team v2.0]", strings.Repeat("c", 40))))
	put(t, filepath.Join(o.Sources, "nested", "other.xml"), translationXML(oPlatform(), "T-Pt-BR", outputMachine("Original [T-Pt-BR by Equipe v1.0]", strings.Repeat("d", 40))))
	put(t, filepath.Join(o.Sources, "retail.dat"), []byte(`<datafile><header><name>Nintendo original releases</name></header></datafile>`))
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 3 || r.Counts["ready"] != 3 || r.IgnoredFiles == 0 {
		t.Fatal(m, r)
	}
	want := map[string][]string{strings.Repeat("b", 40): {"en"}, strings.Repeat("c", 40): {"ru"}, strings.Repeat("d", 40): {"pt-BR"}}
	for _, e := range m.Entries {
		if !reflect.DeepEqual(e.Variant.Languages, want[e.File.SHA1]) {
			t.Fatalf("wrong language: %+v", e)
		}
		if e.File.SHA1[0] == 'c' && (e.Variant.Group != "Russian Team" || e.Variant.Version != "2.0") {
			t.Fatal(e.Variant)
		}
	}
	// A language in the collection header is sufficient when an output has no label.
	put(t, filepath.Join(o.Sources, "plain.xml"), translationXML(oPlatform(), "T-Ru", `<machine name="Original (Japan) [T-Ru by A]"><rom name="Renamed.nes" size="31" sha1="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"/></machine>`))
	m, _ = discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 4 {
		t.Fatal(m)
	}
	for _, e := range m.Entries {
		if e.File.SHA1[0] == 'e' && !reflect.DeepEqual(e.Variant.Languages, []string{"ru"}) {
			t.Fatal(e)
		}
	}
}
func TestLanguageMetadataDoesNotDefaultUnknownHeadersToEnglish(t *testing.T) {
	for _, label := range []string{"", " [T-Gibberish]"} {
		t.Run(label, func(t *testing.T) {
			o, _, _ := discoveryFixture(t, "")
			put(t, filepath.Join(o.Sources, "ten.xml"), []byte(`<datafile><header><name>`+oPlatform()+label+` Collection</name></header></datafile>`))
			if _, _, _, err := discover(o, discoveryOptions{}, nil); err == nil {
				t.Fatal("unknown language accepted")
			}
		})
	}
}
func TestDatFileAndDirectorySelectionWithAutomaticGA(t *testing.T) {
	hash := strings.Repeat("b", 40)
	o, _, _ := discoveryFixture(t, outputMachine("Renamed [T-En by Translator]", hash))
	ga := translationXML("Nintendo - Famicom [GA]", "T-En", outputMachine("Original (Japan) [T-En by Translator]", hash))
	put(t, filepath.Join(o.Sources, "helpful.xml"), ga)
	put(t, filepath.Join(o.Sources, "extra", "russian.dat"), translationXML(oPlatform(), "T-Ru", outputMachine("Original [T-Ru by A]", strings.Repeat("c", 40))))
	m, r := discoverTest(t, o, discoveryOptions{DAT: []string{"ten.xml"}}, nil)
	if len(m.Entries) != 1 || m.Entries[0].Game != "Original" || r.Rows[0].Reason != "published output-to-parent relationship" {
		t.Fatal(m, r)
	}
	if len(m.Snapshots) != 2 {
		t.Fatal("GA not pinned", m.Snapshots)
	}
	m, _ = discoverTest(t, o, discoveryOptions{DAT: []string{"extra"}}, nil)
	if len(m.Entries) != 1 || m.Entries[0].Variant.Languages[0] != "ru" {
		t.Fatal(m)
	}
}
func TestAutomaticRHAAndHackHashConnections(t *testing.T) {
	for _, provider := range []string{"rha", "hackhash"} {
		t.Run(provider, func(t *testing.T) {
			hash := strings.Repeat("b", 40)
			o, _, _ := discoveryFixture(t, outputMachine("Renamed [T-En by Team]", hash))
			if provider == "rha" {
				record := map[string]any{"info": map[string]any{"name": "Translated title"}, "rom": map[string]any{"size": 31, "sha1": hash}, "hacks": []any{map[string]any{"name": "translation", "labels": []string{"T-En"}}}}
				b, _ := json.Marshal(record)
				put(t, filepath.Join(o.Sources, "external", "database", oPlatform(), "Original (Japan).nes", "Renamed.nes", "romhack.json"), b)
			} else {
				record := map[string]any{"machines": []any{map[string]any{"romName": "Renamed.nes", "platform": "NES", "fileSize": 31, "sha1": hash, "details": map[string]any{"translationLanguages": []string{"en"}, "baseRom": map[string]any{"name": "Original (Japan)", "platform": "NES", "status": "APPROVED", "sha1": strings.Repeat("a", 40)}}}}}
				b, _ := json.Marshal(record)
				put(t, filepath.Join(o.Sources, "arbitrary-export-name.json"), b)
			}
			m, r := discoverTest(t, o, discoveryOptions{}, nil)
			if len(m.Entries) != 1 || m.Entries[0].Game != "Original" || r.Rows[0].Reason != "published output-to-parent relationship" {
				t.Fatal(m, r)
			}
			if len(m.Snapshots) != 2 {
				t.Fatal("provider not pinned", m.Snapshots)
			}
		})
	}
}
func TestZIPDatStagesWithoutExtractionAndPinsContainer(t *testing.T) {
	o, _, _ := discoveryFixture(t, "")
	if err := os.Remove(filepath.Join(o.Sources, "ten.xml")); err != nil {
		t.Fatal(err)
	}
	body := translationXML(oPlatform(), "T-Ru", outputMachine("Original [T-Ru by Team]", strings.Repeat("b", 40)))
	members := [][2]string{{"nested/translation.dat", string(body)}, {"readme.txt", "synthetic"}}
	putDATZip(t, o, "bundle.zip", members)
	before := tree(t, o.Sources)
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 1 || len(m.Snapshots) != 1 || m.Entries[0].DAT.Member != "nested/translation.dat" || r.Rows[0].Languages[0] != "ru" {
		t.Fatal(m, r)
	}
	stage := o
	stage.Links = filepath.Join(filepath.Dir(o.Catalog), "links-ready.json")
	stage.Out = filepath.Join(filepath.Dir(o.Catalog), "stage")
	stage.Apply = true
	stage.Media = "none"
	saveLinks(t, stage, *m)
	if _, err := run(stage); err != nil {
		t.Fatal(err)
	}
	if r, err := run(stage); err != nil || r.Status != "unchanged" {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(before, tree(t, o.Sources)) {
		t.Fatal("source archive was extracted or modified")
	}
	members[1][1] = "changed"
	putDATZip(t, o, "bundle.zip", members)
	if _, err := run(stage); err == nil {
		t.Fatal("changed source container accepted")
	}
}
func TestUnsafeOrDuplicateZipMetadataFails(t *testing.T) {
	for _, names := range [][]string{{"../bad.xml"}, {"same.dat", "same.dat"}} {
		t.Run(fmt.Sprint(names), func(t *testing.T) {
			o, _, _ := discoveryFixture(t, "")
			members := [][2]string{}
			for _, n := range names {
				members = append(members, [2]string{n, string(translationXML(oPlatform(), "T-Ru", ""))})
			}
			putDATZip(t, o, "unsafe.zip", members)
			if _, _, _, err := discover(o, discoveryOptions{}, nil); err == nil {
				t.Fatal("invalid ZIP metadata accepted")
			}
		})
	}
}
func TestAutomaticWorkspaceRefreshFindsNewDATWithoutQuestions(t *testing.T) {
	o, _, _ := discoveryFixture(t, outputMachine("Unknown [T-En by Team]", strings.Repeat("b", 40)))
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(work, o, discoveryOptions{}, true, strings.NewReader("b Original\n"), &out); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(o.Sources, "new-language.dat"), translationXML(oPlatform(), "T-Ru", outputMachine("Original [T-Ru by A]", strings.Repeat("c", 40))))
	if err := discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(work, "report.json"))
	var r discoveryReport
	if err := json.Unmarshal(raw, &r); err != nil || r.Counts["ready"] != 2 {
		t.Fatal(string(raw), err)
	}
	ds, _, _ := loadDecisions(work)
	if len(ds) != 1 {
		t.Fatal("lost review decision")
	}
}
func TestMissingCatalogPlatformsAreReportedWithoutBlockingOtherDATs(t *testing.T) {
	o, _, _ := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
	put(t, filepath.Join(o.Sources, "other.dat"), translationXML("Sony - PlayStation", "T-Ru", outputMachine("Unknown [T-Ru by A]", strings.Repeat("c", 40))))
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 1 || r.Counts["skipped-platform"] != 1 {
		t.Fatal(m, r)
	}
	found := false
	for _, s := range r.Sources {
		found = found || s.Status == "skipped"
	}
	if !found {
		t.Fatal("missing platform not explained")
	}
}

func TestArchivedGAConnectsRenamedOutputsWithoutDuplicateReview(t *testing.T) {
	hash := strings.Repeat("b", 40)
	o, _, _ := discoveryFixture(t, outputMachine("Renamed [T-En by Team]", hash))
	ga := translationXML("Nintendo - Famicom [GA]", "T-En", outputMachine("Original (Japan) [T-En by Team]", hash))
	putDATZip(t, o, "ga-bundle.zip", [][2]string{{"ga.xml", string(ga)}})
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 1 || m.Entries[0].Game != "Original" || r.Counts["review"] != 0 || len(m.Snapshots) != 2 {
		t.Fatal(m, r)
	}
	found := false
	for _, s := range r.Sources {
		found = found || (s.Type == "gaming-alexandria" && s.Member == "ga.xml")
	}
	if !found {
		t.Fatal("archived GA not recognized")
	}
	stage := o
	stage.Links = filepath.Join(filepath.Dir(o.Catalog), "links-ready.json")
	stage.Out = filepath.Join(filepath.Dir(o.Catalog), "stage")
	stage.Apply = true
	stage.Media = "none"
	saveLinks(t, stage, *m)
	if _, err := run(stage); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyEnglishDATSourceProvenanceIsStable(t *testing.T) {
	o, m := tenFixture(t, false)
	if err := resolveTEn(&m, o); err != nil {
		t.Fatal(err)
	}
	links := m.Entries[0].Evidence.Links
	if links[len(links)-1]["source"] != "T-En DAT" {
		t.Fatal("legacy source identity changed")
	}
}

func TestLargeUnrelatedMetadataDoesNotBlockAutomaticDiscovery(t *testing.T) {
	o, _, _ := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
	p := filepath.Join(o.Sources, "large-retail.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(33 << 20); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 1 || r.IgnoredFiles != 1 {
		t.Fatal(m, r)
	}
	if _, _, _, err = discover(o, discoveryOptions{DAT: []string{"large-retail.dat"}}, nil); err == nil {
		t.Fatal("explicit oversized source accepted")
	}
}

func TestLegacyFlagCanBeReplacedWithDatWithoutRecreatingWorkspace(t *testing.T) {
	o, d, _ := discoveryFixture(t, outputMachine("Original [T-En by Team]", strings.Repeat("b", 40)))
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(work, o, d, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if err := discoveryCLI(work, options{}, discoveryOptions{DAT: []string{"ten.xml"}}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal("legacy flag alias forced a new workspace:", err)
	}
}
