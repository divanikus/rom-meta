package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogfstest"
	"rommeta/internal/exporter"
)

func structuredFixture(t *testing.T, provider string, translation bool) (options, string, string, string) {
	t.Helper()
	o, m, g := testFixture(t)
	roms := o.ROMRoot
	o.ROMRoot = ""
	o.Links = ""
	o.Out = ""
	o.Report = ""
	hash := m.Entries[0].File.SHA1
	size := m.Entries[0].File.Size
	var path string
	var value any
	if provider == "rha" {
		labels := []string{"Improvement"}
		if translation {
			labels = []string{"T-Ru"}
		}
		path = filepath.Join(o.Sources, "rha", "database", oPlatform(), "Original (Japan).nes", "Published Hack.nes", "romhack.json")
		value = map[string]any{"info": map[string]any{"name": "Original Remix", "translatedTitle": false}, "rom": map[string]any{"size": size, "sha1": hash}, "hacks": []any{map[string]any{"name": "Remix", "authors": []string{"Team"}, "version": "1.0", "labels": labels}}}
	} else {
		languages := []string{}
		if translation {
			languages = []string{"ru"}
		}
		path = filepath.Join(o.Sources, "published.json")
		value = map[string]any{"machines": []any{map[string]any{"romName": "Published Hack.nes", "platform": "NES", "fileSize": size, "sha1": hash, "details": map[string]any{"hackName": "Original Remix", "author": "Team", "version": "1.0", "translationLanguages": languages, "baseRom": map[string]any{"name": "Original (Japan)", "platform": "NES", "status": "APPROVED", "sha1": strings.Repeat("a", 40)}}}}}
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, b)
	return o, g, roms, path
}
func stageStructured(t *testing.T, o options, m *manifest) options {
	t.Helper()
	o.Links = filepath.Join(filepath.Dir(o.Catalog), "published-links.json")
	o.Out = filepath.Join(filepath.Dir(o.Catalog), "published-stage")
	saveLinks(t, o, *m)
	return o
}
func TestStandalonePublishedHacksStageAndExportWithoutDAT(t *testing.T) {
	for _, provider := range []string{"rha", "hackhash"} {
		for _, packed := range []bool{false, true} {
			t.Run(provider+map[bool]string{false: "-loose", true: "-packed"}[packed], func(t *testing.T) {
				o, g, roms, _ := structuredFixture(t, provider, false)
				if packed {
					catalogfstest.Pack(t, filepath.Dir(g))
				}
				before, sourceBefore := tree(t, o.Catalog), tree(t, o.Sources)
				m, r := discoverTest(t, o, discoveryOptions{}, nil)
				if len(m.Entries) != 1 || r.Counts["ready"] != 1 {
					t.Fatal(m, r)
				}
				e := m.Entries[0]
				if e.DAT != nil || e.Published == nil || len(e.Inputs) != 0 || e.Variant.Kind != "hack" || len(e.Variant.Languages) != 0 || e.Variant.BasedOn != "" || e.Variant.Title != "Original Remix" || e.Variant.Group != "Team" || e.Variant.Version != "1.0" {
					t.Fatalf("%+v", e)
				}
				staged := stageStructured(t, o, m)
				rpt, err := run(staged)
				if err != nil || rpt.Status != "preview" {
					t.Fatal(rpt, err)
				}
				if _, err = os.Stat(staged.Out); !os.IsNotExist(err) {
					t.Fatal("preview created stage")
				}
				staged.Apply = true
				if _, err = run(staged); err != nil {
					t.Fatal(err)
				}
				snapshot := tree(t, staged.Out)
				if rpt, err = run(staged); err != nil || rpt.Status != "unchanged" || !reflect.DeepEqual(snapshot, tree(t, staged.Out)) {
					t.Fatal(rpt, err)
				}
				plan, err := exporter.Build(exporter.Options{DataRoot: staged.Out, ROMRoot: filepath.Join(roms, oPlatform(), "translations"), Platform: "nes", Out: filepath.Join(filepath.Dir(o.Catalog), "export")})
				if err != nil {
					t.Fatal(err)
				}
				if plan.Matched != 1 || len(plan.Records) != 1 {
					t.Fatalf("%+v", plan)
				}
				rec := plan.Records[0]
				if rec.Release != "" || rec.MetadataRelease != "Original (USA)" || rec.Variant == "" || rec.Fields["description"].Value != "Wrong default" || rec.Fields["title"].Value != "Original Remix" {
					t.Fatalf("%+v", rec)
				}
				if !reflect.DeepEqual(before, tree(t, o.Catalog)) || !reflect.DeepEqual(sourceBefore, tree(t, o.Sources)) {
					t.Fatal("source catalog/metadata changed")
				}
			})
		}
	}
}
func TestStandaloneStructuredTranslationsKeepTheirLanguage(t *testing.T) {
	for _, provider := range []string{"rha", "hackhash"} {
		t.Run(provider, func(t *testing.T) {
			o, _, _, _ := structuredFixture(t, provider, true)
			m, _ := discoverTest(t, o, discoveryOptions{}, nil)
			if len(m.Entries) != 1 || m.Entries[0].Variant.Kind != "translation" || !reflect.DeepEqual(m.Entries[0].Variant.Languages, []string{"ru"}) {
				t.Fatal(m)
			}
		})
	}
}
func TestPublishedProofRejectsTamperingAndMissingSources(t *testing.T) {
	for _, which := range []string{"source changed", "base as output", "record as payload", "record missing", "typed CHD", "unsafe path", "unknown format", "proof removed", "DAT and published"} {
		t.Run(which, func(t *testing.T) {
			o, _, _, source := structuredFixture(t, "hackhash", false)
			m, _ := discoverTest(t, o, discoveryOptions{}, nil)
			e := &m.Entries[0]
			switch which {
			case "source changed":
				b, _ := os.ReadFile(source)
				put(t, source, append(b, '\n'))
			case "base as output":
				e.File.SHA1 = strings.Repeat("a", 40)
			case "record as payload":
				e.File.SHA256 = e.Published.Record
			case "record missing":
				e.Published.Record = strings.Repeat("e", 64)
			case "typed CHD":
				e.File.Kind = "chd-combined-sha1"
				e.File.Size = 0
			case "unsafe path":
				e.Published.Path = "../outside.json"
			case "unknown format":
				e.Published.Format = "patch-file"
			case "proof removed":
				e.Published = nil
			case "DAT and published":
				e.DAT = &datReference{Path: "published.json", Name: e.File.Name}
			}
			staged := stageStructured(t, o, m)
			staged.Apply = true
			if _, err := run(staged); err == nil {
				t.Fatal("accepted invalid published identity")
			}
			if _, err := os.Stat(staged.Out); !os.IsNotExist(err) {
				t.Fatal("invalid proof created stage")
			}
		})
	}
}
func TestStructuredReviewDoesNotRequireAnExactOriginalRelease(t *testing.T) {
	o, _, _, source := structuredFixture(t, "hackhash", false)
	b, _ := os.ReadFile(source)
	b = bytes.ReplaceAll(b, []byte(strings.Repeat("a", 40)), []byte(strings.Repeat("e", 40)))
	put(t, source, b)
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 0 || r.Counts["review"] != 1 {
		t.Fatal(m, r)
	}
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	if err := discoveryCLI(work, o, discoveryOptions{}, true, strings.NewReader("b Original\n"), &out); err != nil {
		t.Fatal(err)
	}
	stage, err := workspaceOptions(work, options{Out: filepath.Join(filepath.Dir(o.Catalog), "stage"), Media: "none", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = run(stage); err != nil {
		t.Fatal(err)
	}
	if err = discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(work, "links.json"))
	m, err = decodeManifest(raw)
	if err != nil || len(m.Entries) != 1 || m.Entries[0].Variant.BasedOn != "" {
		t.Fatal(m, err)
	}
	if rpt, err := run(stage); err != nil || rpt.Status != "unchanged" {
		t.Fatal(rpt, err)
	}
}
func TestMixedDATAndStructuredOutputsPreserveLegacySelection(t *testing.T) {
	o, _, _, source := structuredFixture(t, "hackhash", false)
	var data map[string]any
	b, _ := os.ReadFile(source)
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	rows := data["machines"].([]any)
	original := rows[0].(map[string]any)
	original["details"].(map[string]any)["translationLanguages"] = []string{"en"}
	extra := map[string]any{"romName": "Additional Hack.nes", "platform": "NES", "fileSize": 31, "sha1": strings.Repeat("d", 40), "details": map[string]any{"baseRom": map[string]any{"name": "Original (Japan)", "platform": "NES", "status": "APPROVED"}}}
	data["machines"] = append(rows, extra)
	b, _ = json.Marshal(data)
	put(t, source, b)
	writeDiscoveryDAT(t, o, outputMachine("Original [T-En by Team]", original["sha1"].(string)))
	b, _ = os.ReadFile(filepath.Join(o.Sources, "ten.xml"))
	b = bytes.ReplaceAll(b, []byte(`size="31"`), []byte(`size="32"`))
	put(t, filepath.Join(o.Sources, "ten.xml"), b)
	all, _ := discoverTest(t, o, discoveryOptions{}, nil)
	selected, _ := discoverTest(t, o, discoveryOptions{DAT: []string{"ten.xml"}}, nil)
	legacy, _ := discoverTest(t, o, discoveryOptions{TEN: []string{"ten.xml"}}, nil)
	if len(all.Entries) != 2 || len(selected.Entries) != 1 || len(legacy.Entries) != 1 {
		t.Fatal(all, selected, legacy)
	}
	a, _ := json.Marshal(selected)
	c, _ := json.Marshal(legacy)
	if !bytes.Equal(a, c) {
		t.Fatal("legacy --ten and --dat selections differ")
	}
	datOnly := selected.Entries[0]
	if datOnly.Published != nil || datOnly.DAT == nil || datOnly.Variant.Kind != "translation" || !reflect.DeepEqual(datOnly.Variant.Languages, []string{"en"}) {
		t.Fatal(datOnly)
	}
	for _, e := range all.Entries {
		if e.DAT != nil && !reflect.DeepEqual(e, datOnly) {
			t.Fatal("DAT translation definition changed in mixed mode")
		}
	}
}
func TestPublishedSourcesCannotCreateASecondFingerprintOwner(t *testing.T) {
	o, _, _, _ := structuredFixture(t, "hackhash", false)
	m, _ := discoverTest(t, o, discoveryOptions{}, nil)
	stage := stageStructured(t, o, m)
	stage.Apply = true
	if _, err := run(stage); err != nil {
		t.Fatal(err)
	}
	owned := o
	owned.Catalog = stage.Out
	m, r := discoverTest(t, owned, discoveryOptions{}, nil)
	if len(m.Entries) != 0 || r.Counts["already-owned"] != 1 {
		t.Fatal(m, r)
	}
}
func TestConflictingPublishedParentsStayForReview(t *testing.T) {
	o, g, _, _ := structuredFixture(t, "rha", false)
	hash := strings.Repeat("b", 40)
	// Reuse the RHA output fingerprint in another provider, claiming another game.
	rhaPath := filepath.Join(o.Sources, "rha", "database", oPlatform(), "Original (Japan).nes", "Published Hack.nes", "romhack.json")
	raw, _ := os.ReadFile(rhaPath)
	var rha map[string]any
	_ = json.Unmarshal(raw, &rha)
	hash = rha["rom"].(map[string]any)["sha1"].(string)
	other := filepath.Join(filepath.Dir(g), "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Other\nreleases: {Other: {title: Other}}\n"))
	put(t, filepath.Join(other, "fingerprints.yaml"), []byte("items: []\n"))
	hh := map[string]any{"machines": []any{map[string]any{"romName": "Other name.nes", "platform": "NES", "fileSize": 32, "sha1": hash, "details": map[string]any{"baseRom": map[string]any{"name": "Other", "platform": "NES", "status": "APPROVED"}}}}}
	b, _ := json.Marshal(hh)
	put(t, filepath.Join(o.Sources, "other-provider.json"), b)
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 0 || r.Counts["review"] != 1 || len(r.Rows[0].Candidates) != 2 {
		t.Fatal(m, r)
	}
}

func TestPublishedIdentityConflictsCannotBeOverriddenByChoosingAGame(t *testing.T) {
	o, _, _, source := structuredFixture(t, "hackhash", false)
	var data map[string]any
	b, _ := os.ReadFile(source)
	_ = json.Unmarshal(b, &data)
	rows := data["machines"].([]any)
	first := rows[0].(map[string]any)
	first["md5"] = strings.Repeat("a", 32)
	cloneBytes, _ := json.Marshal(first)
	var second map[string]any
	_ = json.Unmarshal(cloneBytes, &second)
	second["md5"] = strings.Repeat("b", 32)
	data["machines"] = append(rows, second)
	b, _ = json.Marshal(data)
	put(t, source, b)
	m, r := discoverTest(t, o, discoveryOptions{}, nil)
	if len(m.Entries) != 0 || r.Counts["unsupported"] != 1 {
		t.Fatal(m, r)
	}
	decisions := map[string]reviewDecision{r.Rows[0].ID: {Platform: oPlatform(), Game: "Original"}}
	m, r = discoverTest(t, o, discoveryOptions{}, decisions)
	if len(m.Entries) != 0 || r.Counts["unsupported"] != 1 {
		t.Fatal("game choice overrode conflicting output identity")
	}
}
func TestPublishedRefreshDoesNotDependOnLocalROMsOrChangeReviewDecisions(t *testing.T) {
	o, _, _, source := structuredFixture(t, "hackhash", false)
	work := filepath.Join(filepath.Dir(o.Catalog), "work")
	var out bytes.Buffer
	// A different source parent name requires review, while its output is valid.
	b, _ := os.ReadFile(source)
	b = bytes.ReplaceAll(b, []byte("Original (Japan)"), []byte("Unknown source name"))
	b = bytes.ReplaceAll(b, []byte(strings.Repeat("a", 40)), []byte(strings.Repeat("e", 40)))
	put(t, source, b)
	if err := discoveryCLI(work, o, discoveryOptions{}, true, strings.NewReader("b Original\n"), &out); err != nil {
		t.Fatal(err)
	}
	old, _, err := loadDecisions(work)
	if err != nil || len(old) != 1 {
		t.Fatal(old, err)
	}
	if err = discoveryCLI(work, options{}, discoveryOptions{}, false, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	next, _, _ := loadDecisions(work)
	if !reflect.DeepEqual(old, next) {
		t.Fatal("published source refresh lost the game choice")
	}
	raw, _ := os.ReadFile(filepath.Join(work, "links.json"))
	m, err := decodeManifest(raw)
	if err != nil || len(m.Entries) != 1 || m.Entries[0].Published == nil || m.Entries[0].DAT != nil || len(m.Entries[0].Inputs) != 0 {
		t.Fatal(m, err)
	}
}
