package gamelist_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
	"rommeta/internal/gamelist"
)

func put(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, packed bool) (gamelist.Options, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "catalog")
	platform := filepath.Join(root, "platforms", "Nintendo - Nintendo 64")
	game := filepath.Join(platform, "Example")
	put(t, filepath.Join(platform, "_catalog.yaml"), "name: Example\n")
	put(t, filepath.Join(platform, "_platform.yaml"), "name: Nintendo - Nintendo 64\nrom_layout: BigEndian\n")
	put(t, filepath.Join(game, "game.yaml"), `title: Owner title
default_release: Example (USA)
unknown: {owner: keep}
releases:
  Example (USA):
    title: Example
    aliases: [Different label (USA)]
    regions: [USA]
    authority: {source: no-intro, entry_name: Example (USA)}
sources:
  launchbox:
    matches:
      - release: Example (USA)
        descriptions: {en: Keep provider text}
`)
	put(t, filepath.Join(game, "fingerprints.yaml"), "roms: []\n")
	if packed {
		catalogfstest.Pack(t, platform)
	}
	in := filepath.Join(base, "input")
	put(t, filepath.Join(in, "gamelist.xml"), `<gameList><provider><system>n64</system></provider>
<game id="unverified-id"><path>./Different label (USA).zip</path><name>Imported title</name><desc>РЎРёРЅС‚РµС‚РёС‡РµСЃРєРѕРµ РѕРїРёСЃР°РЅРёРµ &amp; С‚РµРєСЃС‚</desc><lang>jp</lang><md5>unverified</md5><releasedate>19981231T000000</releasedate><image>./media/art/screen.png</image><thumbnail>./media/boxart/front.jpg</thumbnail><video>./media/video/clip.mp4</video><playcount>42</playcount></game>
<game><path>./Unresolved Hack.zip</path><desc>Must stay unresolved</desc></game></gameList>`)
	put(t, filepath.Join(in, "media/art/screen.png"), "synthetic screenshot")
	put(t, filepath.Join(in, "media/boxart/front.jpg"), "synthetic front")
	put(t, filepath.Join(in, "media/video/clip.mp4"), "synthetic video")
	return gamelist.Options{Input: filepath.Join(in, "gamelist.xml"), DataRoot: root, Collection: "sample", Language: "ru"}, game
}
func TestPrivateImportDryRunApplyRepeatAndConsumers(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			o, game := fixture(t, packed)
			fingerprint, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
			original, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			plan, err := gamelist.Import(o)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Matched != 1 || plan.Unmatched != 1 || plan.Updated != 1 || plan.MediaAdded != 3 {
				t.Fatalf("%+v", plan)
			}
			if _, err = os.Stat(filepath.Join(o.DataRoot, ".rommeta")); !os.IsNotExist(err) {
				t.Fatal("dry run wrote runtime state")
			}
			o.Apply = true
			result, err := gamelist.Import(o)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			doc, err := catalogyaml.Parse(body)
			if err != nil {
				t.Fatal(err)
			}
			if catalogyaml.Scalar(catalogyaml.Value(doc.Root(), "title")) != "Owner title" || !bytes.Contains(body, []byte("Keep provider text")) || !bytes.Contains(body, []byte("owner: keep")) {
				t.Fatal(string(body))
			}
			if !bytes.Contains(body, []byte("gamelist.sample:")) || !bytes.Contains(body, []byte("РЎРёРЅС‚РµС‚РёС‡РµСЃРєРѕРµ РѕРїРёСЃР°РЅРёРµ & С‚РµРєСЃС‚")) || bytes.Contains(body, []byte("playcount")) {
				t.Fatal(string(body))
			}
			after, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
			if !bytes.Equal(after, fingerprint) {
				t.Fatal("fingerprints changed")
			}
			backup, _ := os.ReadFile(filepath.Join(result.Backup, "backups", "platforms", "Nintendo - Nintendo 64", "Example", "game.yaml"))
			if !bytes.Equal(backup, original) {
				t.Fatal("backup mismatch")
			}
			if packed {
				if _, err = os.Stat(filepath.Join(game, "artwork.gamelist.sample.zip")); err != nil {
					t.Fatal(err)
				}
			}
			issues, err := catalog.Validate(o.DataRoot)
			if err != nil || len(issues) != 0 {
				t.Fatalf("validate: %v %+v", err, issues)
			}
			repeat, err := gamelist.Import(o)
			if err != nil || repeat.Updated != 0 || repeat.MediaAdded != 0 || repeat.Backup != "" {
				t.Fatalf("repeat: %+v %v", repeat, err)
			}
			again, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			if !bytes.Equal(body, again) {
				t.Fatal("no-op changed YAML")
			}
			assets, err := exporter.InventoryMedia(o.DataRoot, []exporter.Record{{Game: "platforms/Nintendo - Nintendo 64/Example"}})
			if err != nil || len(assets) != 3 {
				t.Fatalf("inventory: %v %+v", err, assets)
			}
			profile := filepath.Join(t.TempDir(), "profile.yaml")
			put(t, profile, "schema_version: 1\ntarget: es-de\nselection:\n  description: {languages: [ru], sources: [gamelist.sample]}\n  media:\n    cover: {sources: [gamelist.sample]}\n    video: {sources: [gamelist.sample]}\n")
			lookup, e := exporter.NewLookupCatalog(o.DataRoot, profile)
			if e != nil {
				t.Fatal(e)
			}
			lookup.CacheDir = "-"
			resolver, e := lookup.Open("Nintendo - Nintendo 64")
			if e != nil {
				t.Fatal(e)
			}
			observation := exporter.ObservedInput{ROM: "Example (USA).z64", Outer: exporter.FileDigest{Size: 1}, Catalog: &exporter.CatalogObservation{Kind: "name-stat-v1", Files: []exporter.CatalogFile{{Name: "Example (USA).z64", Size: 1, MtimeNS: 1}}}}
			selected, e := resolver.ResolveBindings([]exporter.ObservedInput{observation}, true)
			if e != nil || len(selected.Records) != 1 {
				t.Fatalf("lookup %v %+v", e, selected)
			}
			record := selected.Records[0]
			if record.Fields["description"].Source != "gamelist.sample" || record.Fields["description"].Language != "ru" || record.Media["cover"].Source != "gamelist.sample" || record.Media["video"].Source != "gamelist.sample" {
				t.Fatalf("source selection: %+v", record)
			}
			for _, a := range assets {
				if a.Source != "gamelist.sample" {
					t.Fatalf("lost source: %+v", a)
				}
			}
		})
	}
}
func TestZIPSelectionNoROMAndMultipleLists(t *testing.T) {
	o, _ := fixture(t, false)
	archive := filepath.Join(t.TempDir(), "pack.zip")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, p := range []string{"n64/gamelist.xml", "alternate/gamelist.xml", "n64/media/boxart/front.jpg", "n64/forbidden.rom"} {
		f, _ := z.Create(p)
		if strings.HasSuffix(p, ".xml") {
			f.Write([]byte(`<gameList><provider><system>n64</system></provider><game><path>./Example (USA).zip</path><thumbnail>./media/boxart/front.jpg</thumbnail></game></gameList>`))
		} else {
			f.Write([]byte("synthetic content"))
		}
	}
	z.Close()
	os.WriteFile(archive, b.Bytes(), 0644)
	o.Input = archive
	if _, err := gamelist.Import(o); err == nil {
		t.Fatal("multiple gamelists accepted without --member")
	}
	o.Member = "n64/gamelist.xml"
	o.Apply = true
	r, err := gamelist.Import(o)
	if err != nil || r.Matched != 1 || r.MediaAdded != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	found := false
	filepath.WalkDir(o.DataRoot, func(n string, d os.DirEntry, e error) error {
		if strings.HasSuffix(n, ".rom") {
			found = true
		}
		return nil
	})
	if found {
		t.Fatal("ROM extracted")
	}
}
func TestCollisionAndUnsafeMediaDoNotMutate(t *testing.T) {
	o, game := fixture(t, false)
	body, _ := os.ReadFile(filepath.Join(game, "game.yaml"))
	put(t, filepath.Join(filepath.Dir(game), "Other", "game.yaml"), string(body))
	r, err := gamelist.Import(o)
	if err != nil || r.Ambiguous != 1 || r.Matched != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	os.RemoveAll(filepath.Join(filepath.Dir(game), "Other"))
	put(t, o.Input, `<gameList><provider><system>n64</system></provider><game><path>./Example (USA).zip</path><image>../outside.png</image></game></gameList>`)
	o.Apply = true
	if _, err = gamelist.Import(o); err == nil {
		t.Fatal("traversal accepted")
	}
	after, _ := os.ReadFile(filepath.Join(game, "game.yaml"))
	if !bytes.Equal(body, after) {
		t.Fatal("unsafe input mutated catalog")
	}
}
func TestDescriptionLanguageSeparateFromROMLanguageAndCollection(t *testing.T) {
	o, game := fixture(t, false)
	o.Collection = "../bad"
	if _, err := gamelist.Import(o); err == nil {
		t.Fatal("unsafe collection accepted")
	}
	o.Collection = "sample"
	o.Language = ""
	if _, err := gamelist.Import(o); err == nil {
		t.Fatal("description language guessed")
	}
	o.Language = "ru"
	o.Apply = true
	gamelist.Import(o)
	body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
	if !bytes.Contains(body, []byte("rom_languages: jp")) || !bytes.Contains(body, []byte("ru:")) {
		t.Fatal(string(body))
	}
}
