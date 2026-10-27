package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalog"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
)

func put(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func saveLinks(t *testing.T, o options, m manifest) {
	t.Helper()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	put(t, o.Links, b)
}
func testFixture(t *testing.T) (options, manifest, string) {
	t.Helper()
	base := t.TempDir()
	o := options{Catalog: filepath.Join(base, "catalog"), ROMRoot: filepath.Join(base, "roms"), Sources: filepath.Join(base, "sources"),
		Links: filepath.Join(base, "links.json"), Out: filepath.Join(base, "stage"), Media: "artwork"}
	platform := "Nintendo - Nintendo Entertainment System"
	game := filepath.Join(o.Catalog, "platforms", platform, "Original")
	g := []byte("# retain owner comment\ntitle: Original\ndefault_release: Original (USA)\nmanual: {note: 'keep me'}\nreleases:\n  Original (USA):\n    title: Original\n    description: Wrong default\n  Original (Japan):\n    title: Japanese title\n    description: Inherited description\n    developer: Original developer\n    genres: [Adventure]\n    players: 2\n    release_date: '1991-05-06'\n")
	fp := []byte("items:\n  - release: Original (Japan)\n    file:\n      name: Original.nes\n      size: 17\n      sha1: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	put(t, filepath.Join(game, "game.yaml"), g)
	put(t, filepath.Join(game, "fingerprints.yaml"), fp)
	put(t, filepath.Join(o.Catalog, "platforms", platform, "_platform.yaml"), []byte("name: Nintendo - Nintendo Entertainment System\n"))
	put(t, filepath.Join(o.Catalog, "platforms", platform, "_catalog.yaml"), []byte("name: Synthetic\n"))
	put(t, filepath.Join(o.Catalog, "source-policies.yaml"), []byte("sources:\n  launchbox: {}\n  emumovies: {}\n"))
	put(t, filepath.Join(o.Catalog, "config", "secrets.yaml"), []byte("never copy"))
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(game, "media", "box-front.world.launchbox.png"), img.Bytes())
	put(t, filepath.Join(game, "media", "gameplay-video.japan.emumovies.mp4"), []byte("synthetic video"))
	payload := []byte("synthetic translated NES payload")
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	w, err := z.Create("Renamed inside.nes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	relative := platform + "/translations/Renamed outside.zip"
	put(t, filepath.Join(o.ROMRoot, filepath.FromSlash(relative)), archive.Bytes())
	source := []byte("pinned source assertion")
	put(t, filepath.Join(o.Sources, "source.txt"), source)
	e := entry{Platform: platform, Game: "Original",
		Variant:  variant{Key: "English 1", Title: "Translated Adventure", Kind: "translation", BasedOn: "Original (Japan)", Languages: []string{"en"}, Group: "Synthetic team", Version: "1.0"},
		File:     fingerprint{Name: "Source member.nes", Size: int64(len(payload)), SHA1: fmt.Sprintf("%x", sha1.Sum(payload))},
		Evidence: evidence{Relationship: "source-release-name", Links: []map[string]any{{"source": "Synthetic source", "entry": "Translation record", "parent": "Original (Japan)"}}},
		Expected: map[string]string{"game.yaml": digest(g), "fingerprints.yaml": digest(fp)},
		Inputs:   []input{{Path: relative, Member: "Renamed inside.nes"}},
	}
	m := manifest{SchemaVersion: 1, Snapshots: []snapshot{{Path: "source.txt", SHA256: digest(source)}}, Entries: []entry{e}, Holds: []json.RawMessage{json.RawMessage(`{"reason":"unknown base"}`)}}
	saveLinks(t, o, m)
	return o, m, game
}
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(n string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(n)
		if e != nil {
			return e
		}
		r, _ := filepath.Rel(root, n)
		result[r] = digest(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPreparedStageAndRealExport(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint("packed-", packed), func(t *testing.T) {
			o, _, game := testFixture(t)
			if packed {
				catalogfstest.Pack(t, filepath.Dir(game))
			}
			before, romBefore := tree(t, o.Catalog), tree(t, o.ROMRoot)
			r, err := run(o)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "preview" || r.Added != 1 || len(r.Holds) != 1 || r.SkippedMedia != 1 {
				t.Fatalf("%+v", r)
			}
			if _, err = os.Stat(o.Out); !os.IsNotExist(err) {
				t.Fatal("preview wrote staging")
			}
			o.Apply = true
			r, err = run(o)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "staged" || len(r.Validation) > 0 {
				t.Fatalf("%+v", r)
			}
			if _, err = os.Stat(filepath.Join(o.Out, "config", "secrets.yaml")); !os.IsNotExist(err) {
				t.Fatal("secrets copied")
			}
			if _, err = os.Stat(filepath.Join(o.Out, "platforms", oPlatform(), "catalog-storage.yaml")); !os.IsNotExist(err) {
				t.Fatal("packed marker copied into loose output")
			}
			b, err := os.ReadFile(filepath.Join(o.Out, "platforms", oPlatform(), "Original", "game.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), "retain owner comment") || !strings.Contains(string(b), "keep me") {
				t.Fatal("unknown data/comments lost")
			}
			issues, err := catalog.Validate(o.Out)
			if err != nil || len(issues) > 0 {
				t.Fatal(issues, err)
			}
			exportOptions := exporter.Options{DataRoot: o.Out, ROMRoot: filepath.Join(o.ROMRoot, oPlatform(), "translations"), Platform: "nes", Out: filepath.Join(filepath.Dir(o.Out), "export")}
			plan, err := exporter.Build(exportOptions)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Matched != 1 || len(plan.Records) != 1 {
				t.Fatalf("%+v", plan)
			}
			record := plan.Records[0]
			if record.MatchedBy != "hash" || record.Variant != "English 1" || record.MetadataRelease != "Original (Japan)" || record.Fields["description"].Value != "Inherited description" || len(record.Media) == 0 {
				t.Fatalf("%+v", record)
			}
			if err = exporter.Apply(plan, nil); err != nil {
				t.Fatal(err)
			}
			stageBefore := tree(t, o.Out)
			r, err = run(o)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "unchanged" || !reflect.DeepEqual(stageBefore, tree(t, o.Out)) {
				t.Fatal("repeat mutated stage")
			}
			if !reflect.DeepEqual(before, tree(t, o.Catalog)) || !reflect.DeepEqual(romBefore, tree(t, o.ROMRoot)) {
				t.Fatal("input mutated")
			}
		})
	}
}
func oPlatform() string { return "Nintendo - Nintendo Entertainment System" }

func TestRejectStaleConflictingAndUnsafeInputs(t *testing.T) {
	tests := map[string]func(*testing.T, *options, *manifest, string){
		"stale document": func(t *testing.T, o *options, m *manifest, g string) {
			put(t, filepath.Join(g, "game.yaml"), []byte("changed"))
		},
		"wrong payload": func(t *testing.T, o *options, m *manifest, g string) {
			m.Entries[0].File.SHA1 = strings.Repeat("0", 40)
		},
		"changed input stat": func(t *testing.T, o *options, m *manifest, g string) { m.Entries[0].Inputs[0].Size = 1 },
		"missing base":       func(t *testing.T, o *options, m *manifest, g string) { m.Entries[0].Variant.BasedOn = "Absent (Japan)" },
		"cycle": func(t *testing.T, o *options, m *manifest, g string) {
			m.Entries[0].Variant.BasedOn = m.Entries[0].Variant.Key
		},
		"snapshot changed": func(t *testing.T, o *options, m *manifest, g string) {
			put(t, filepath.Join(o.Sources, "source.txt"), []byte("changed"))
		},
		"same payload two variants": func(t *testing.T, o *options, m *manifest, g string) {
			e := m.Entries[0]
			e.Variant.Key = "Other"
			m.Entries = append(m.Entries, e)
		},
		"same variant different parent": func(t *testing.T, o *options, m *manifest, g string) {
			e := m.Entries[0]
			e.Variant.BasedOn = "Original (USA)"
			m.Entries = append(m.Entries, e)
		},
		"input traversal":       func(t *testing.T, o *options, m *manifest, g string) { m.Entries[0].Inputs[0].Path = "../escape.zip" },
		"unsafe key":            func(t *testing.T, o *options, m *manifest, g string) { m.Entries[0].Variant.Key = "CON" },
		"output source overlap": func(t *testing.T, o *options, m *manifest, g string) { o.Out = filepath.Join(o.Catalog, "stage") },
		"unsupported evidence":  func(t *testing.T, o *options, m *manifest, g string) { m.Entries[0].Evidence.Relationship = "fuzzy" },
		"unproved base hash": func(t *testing.T, o *options, m *manifest, g string) {
			m.Entries[0].Evidence.Relationship = "source-base-hash"
		},
		"duplicate yaml": func(t *testing.T, o *options, m *manifest, g string) {
			p := filepath.Join(g, "game.yaml")
			b, _ := os.ReadFile(p)
			b = append(b, []byte("manual: overwritten\n")...)
			put(t, p, b)
			m.Entries[0].Expected["game.yaml"] = digest(b)
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			o, m, g := testFixture(t)
			change(t, &o, &m, g)
			saveLinks(t, o, m)
			o.Apply = true
			_, err := run(o)
			if err == nil {
				t.Fatal("accepted invalid input")
			}
			if _, err = os.Stat(o.Out); !os.IsNotExist(err) {
				t.Fatal("invalid input wrote output")
			}
		})
	}
}
func TestRefuseModifiedOrForeignStage(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			o, _, _ := testFixture(t)
			o.Apply = true
			if foreign {
				put(t, filepath.Join(o.Out, "owner.txt"), []byte("keep"))
			} else {
				if _, err := run(o); err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(o.Out, "platforms", oPlatform(), "Original", "game.yaml"), []byte("owner edit"))
			}
			before := tree(t, o.Out)
			if _, err := run(o); err == nil {
				t.Fatal("accepted modified/foreign staging")
			}
			if !reflect.DeepEqual(before, tree(t, o.Out)) {
				t.Fatal("changed owner data")
			}
		})
	}
}
func TestStrictManifestAndSourceBaseHash(t *testing.T) {
	for _, raw := range []string{`{"schema_version":1,"schema_version":1,"entries":[]}`, `{"schema_version":1,"entries":[],"surprise":true}`, `{"schema_version":1,"entries":[]} {}`} {
		if _, err := decodeManifest([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	o, m, _ := testFixture(t)
	m.Entries[0].Evidence.Relationship = "source-base-hash"
	m.Entries[0].Evidence.Links[0]["base"] = map[string]any{"sha1": strings.Repeat("a", 40)}
	saveLinks(t, o, m)
	if _, err := run(o); err != nil {
		t.Fatal(err)
	}
	m.Entries[0].Evidence.Links[0]["base"] = map[string]any{"sha1": strings.Repeat("b", 40)}
	saveLinks(t, o, m)
	if _, err := run(o); err == nil {
		t.Fatal("wrong source base hash accepted")
	}
}
func TestPreserveLegacyFingerprintsAndExistingVariants(t *testing.T) {
	o, m, g := testFixture(t)
	b, _ := os.ReadFile(filepath.Join(g, "fingerprints.yaml"))
	b = []byte(strings.Replace(string(b), "items:", "roms:", 1))
	put(t, filepath.Join(g, "fingerprints.yaml"), b)
	m.Entries[0].Expected["fingerprints.yaml"] = digest(b)
	gb, _ := os.ReadFile(filepath.Join(g, "game.yaml"))
	gb = append(gb, []byte("variants:\n  Existing:\n    kind: fix\n    based_on: Original (USA)\n    custom: retained\n")...)
	put(t, filepath.Join(g, "game.yaml"), gb)
	m.Entries[0].Expected["game.yaml"] = digest(gb)
	saveLinks(t, o, m)
	o.Apply = true
	if _, err := run(o); err != nil {
		t.Fatal(err)
	}
	staged, _ := os.ReadFile(filepath.Join(o.Out, "platforms", oPlatform(), "Original", "game.yaml"))
	d, err := catalogyaml.Parse(staged)
	if err != nil {
		t.Fatal(err)
	}
	if catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(d.Root(), "variants"), "Existing"), "custom") == nil {
		t.Fatal("existing override lost")
	}
}
func TestReportCannotOverwriteInput(t *testing.T) {
	o, _, _ := testFixture(t)
	o.Report = o.Links
	if _, err := run(o); err == nil {
		t.Fatal("report could overwrite input")
	}
	o.Report = filepath.Join(o.Catalog, "report.json")
	if _, err := run(o); err == nil {
		t.Fatal("report could write into source")
	}
}
func TestGlobalModifiedFingerprintConflict(t *testing.T) {
	o, m, g := testFixture(t)
	other := filepath.Join(filepath.Dir(g), "Other")
	for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
		b, err := catalogfs.ReadFile(filepath.Join(g, name))
		if err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(other, name), b)
	}
	e := m.Entries[0]
	e.Game = "Other"
	m.Entries = append(m.Entries, e)
	saveLinks(t, o, m)
	if _, err := run(o); err == nil {
		t.Fatal("cross-bundle modified collision accepted")
	}
}

func TestRawAndLegacyZIPInputs(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			o, m, _ := testFixture(t)
			payload := []byte("synthetic translated NES payload")
			in := &m.Entries[0].Inputs[0]
			p := filepath.Join(o.ROMRoot, filepath.FromSlash(in.Path))
			if raw {
				in.Member = ""
				in.Path = strings.TrimSuffix(in.Path, ".zip") + ".nes"
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(o.ROMRoot, filepath.FromSlash(in.Path)), payload)
			} else {
				var b bytes.Buffer
				z := zip.NewWriter(&b)
				w, err := z.CreateHeader(&zip.FileHeader{Name: "M" + string([]byte{0x82}) + "rida.nes", Method: zip.Deflate, NonUTF8: true})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Write(payload); err != nil {
					t.Fatal(err)
				}
				if err = z.Close(); err != nil {
					t.Fatal(err)
				}
				put(t, p, b.Bytes())
				in.Member = "Mérida.nes"
			}
			saveLinks(t, o, m)
			o.Apply = true
			if _, err := run(o); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExplicitTitleAliasForMetadataOnlyLookup(t *testing.T) {
	o, m, _ := testFixture(t)
	m.Entries[0].Variant.Title = "Renamed outside"
	saveLinks(t, o, m)
	o.Apply = true
	if _, err := run(o); err != nil {
		t.Fatal(err)
	}
	c, err := exporter.NewLookupCatalog(o.Out, "")
	if err != nil {
		t.Fatal(err)
	}
	c.CacheDir = "-"
	r, err := c.Open(oPlatform())
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(m.Entries[0].Inputs[0].Path, oPlatform()+"/")
	observed, err := exporter.ObserveCatalogInput(filepath.Join(o.ROMRoot, oPlatform()), name)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := r.ResolveBindings([]exporter.ObservedInput{observed}, true)
	if err != nil {
		t.Fatal(err)
	}
	record := plan.Records[0]
	if record.Variant != "English 1" || record.MatchedBy != "catalog-name" || record.MetadataRelease != "Original (Japan)" {
		t.Fatalf("%+v", record)
	}
}
func TestEvidenceConflictsAndPins(t *testing.T) {
	for _, which := range []string{"unpinned", "conflicting base hashes"} {
		t.Run(which, func(t *testing.T) {
			o, m, _ := testFixture(t)
			e := &m.Entries[0]
			if which == "unpinned" {
				e.Evidence.Links[0]["snapshot"] = "absent.txt"
				e.Evidence.Links[0]["snapshot_sha256"] = strings.Repeat("a", 64)
			} else {
				e.Evidence.Relationship = "source-base-hash"
				e.Evidence.Links[0]["base"] = map[string]any{"sha1": strings.Repeat("a", 40)}
				e.Evidence.Links = append(e.Evidence.Links, map[string]any{"source": "Conflicting source", "entry": "Other assertion", "parent": e.Variant.BasedOn, "base": map[string]any{"sha1": strings.Repeat("b", 40)}})
			}
			saveLinks(t, o, m)
			o.Apply = true
			if _, err := run(o); err == nil {
				t.Fatal("conflicting evidence accepted")
			}
		})
	}
}
func TestMediaPolicies(t *testing.T) {
	for _, mode := range []string{"none", "all"} {
		t.Run(mode, func(t *testing.T) {
			o, _, _ := testFixture(t)
			o.Media = mode
			o.Apply = true
			r, err := run(o)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, f := range r.Files {
				if strings.Contains(f.Path, "/media/") {
					found++
				}
			}
			want := 0
			if mode == "all" {
				want = 2
			}
			if found != want {
				t.Fatalf("copied %d media, expected %d", found, want)
			}
		})
	}
}
