package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func esdeSetup(t *testing.T, existing string) Options {
	t.Helper()
	pkg, destination, roms := t.TempDir(), t.TempDir(), t.TempDir()
	manifest := fmt.Sprintf(`{"schema_version":1,"target":"es-de","system":"nes","rom_root":%q,"records":[{"rom":"one.zip","media":{"cover":{"output_path":"downloaded_media/nes/covers/one.png"},"video":{"output_path":"downloaded_media/nes/videos/one.mp4"}}},{"rom":"two.zip"}]}`, filepath.ToSlash(roms))
	put(t, pkg, "manifest.json", manifest)
	put(t, pkg, "gamelists/nes/gamelist.xml", `<?xml version="1.0"?><gameList><game><path>./one.zip</path><name>Catalog title</name><desc>Prepared description</desc><genre>Action</genre></game><game><path>./two.zip</path><name>New game</name></game></gameList>`)
	put(t, pkg, "downloaded_media/nes/covers/one.png", "catalog cover")
	put(t, pkg, "downloaded_media/nes/videos/one.mp4", "video")
	if existing != "" {
		put(t, destination, "gamelists/nes/gamelist.xml", existing)
	}
	return Options{Target: "es-de", PackageRoot: pkg, Destination: destination}
}
func TestESDEAdditivePreservationExternalMediaAndRepeat(t *testing.T) {
	existing := `<?xml version="1.0"?>
<alternativeEmulator><label>My emulator</label></alternativeEmulator>
<!-- owner comment -->
<gameList><game><path>one.zip</path><name>Owner title</name><desc></desc><favorite>true</favorite><playcount>23</playcount><hidden>true</hidden><altemulator>Owner core</altemulator><custom attr="keep"><nested /></custom></game><folder><path>./folder</path><name>Folder</name></folder><game><path>./unrelated.zip</path><name>Unrelated</name><playtime>55</playtime></game></gameList>
`
	o := esdeSetup(t, existing)
	media := t.TempDir()
	put(t, o.Destination, "settings/es_settings.xml", `<?xml version="1.0"?><string name="MediaDirectory" value="`+media+`"/><bool name="LegacyGamelistFileLocation" value="false"/>`)
	put(t, media, "nes/covers/one.jpg", "owner cover")
	p := mustBuild(t, o)
	if p.MediaRoot != media || len(p.Changes) != 2 || p.Changes[0].Action != "enrich" || p.Changes[0].Media != 1 || p.Changes[1].Action != "add" {
		t.Fatal(p.Changes, p.MediaRoot)
	}
	if string(get(t, o.Destination, "gamelists/nes/gamelist.xml")) != existing {
		t.Fatal("preview changed native XML")
	}
	backup, err := p.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if string(get(t, backup, "gamelists/nes/gamelist.xml")) != existing {
		t.Fatal("backup changed XML")
	}
	after := get(t, o.Destination, "gamelists/nes/gamelist.xml")
	for _, fragment := range []string{"<alternativeEmulator><label>My emulator</label></alternativeEmulator>", "<!-- owner comment -->", "<name>Owner title</name>", "<favorite>true</favorite>", "<playcount>23</playcount>", "<hidden>true</hidden>", "<altemulator>Owner core</altemulator>", "<custom attr=\"keep\"><nested /></custom>", "<desc>Prepared description</desc>", "<name>Unrelated</name>"} {
		if !bytes.Contains(after, []byte(fragment)) {
			t.Fatal("lost native field", fragment)
		}
	}
	if string(get(t, media, "nes/covers/one.jpg")) != "owner cover" {
		t.Fatal("changed owner art")
	}
	if _, err := os.Stat(filepath.Join(media, "nes/covers/one.png")); !os.IsNotExist(err) {
		t.Fatal("installed competing image format")
	}
	_ = get(t, media, "nes/videos/one.mp4")
	repeat := mustBuild(t, o)
	if len(repeat.Files) != 0 {
		t.Fatal("repeat changed library", repeat.Files)
	}
	if backup, err = repeat.Apply(); err != nil || backup != "" {
		t.Fatal(backup, err)
	}
}
func virtualSource(t *testing.T, pkg string) *Source {
	t.Helper()
	source := &Source{Files: map[string]SourceFile{}}
	err := filepath.WalkDir(pkg, func(name string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(pkg, name)
		file := SourceFile{Size: int64(len(data)), SHA256: hashBytes(data)}
		if strings.HasSuffix(relative, ".json") || strings.HasSuffix(relative, ".xml") || strings.HasSuffix(relative, ".m3u") {
			file.Data = data
		}
		source.Files[filepath.ToSlash(relative)] = file
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}
func TestBothTargetsVirtualAndDiskPlanParity(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			var o Options
			if target == "es-de" {
				o = esdeSetup(t, "")
			} else {
				o = setup(t, "", game(c, "New", "ROMs/new.zip", "")+version(v1, c, "ROMs/new.zip", ""))
				put(t, o.PackageRoot, "Images/Test Console/Box - Front/"+c+"-01.png", "cover")
			}
			disk := mustBuild(t, o)
			virtual := o
			virtual.Source = virtualSource(t, o.PackageRoot)
			virtual.PackageRoot = filepath.Join(t.TempDir(), "not-created")
			preview := mustBuild(t, virtual)
			if !reflect.DeepEqual(disk.Changes, preview.Changes) || !reflect.DeepEqual(disk.Files, preview.Files) {
				t.Fatal("preview differs from staged package", disk.Changes, preview.Changes)
			}
			if _, err := os.Stat(virtual.PackageRoot); !os.IsNotExist(err) {
				t.Fatal("preview created staging directory")
			}
			if _, err := preview.Apply(); err == nil {
				t.Fatal("applied virtual asset sources")
			}
		})
	}
}
func TestESDEConfiguredROMRootsAndMediaTokens(t *testing.T) {
	o := esdeSetup(t, "")
	var manifest esdeManifest
	if err := json.Unmarshal(get(t, o.PackageRoot, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(manifest.ROMRoot)
	put(t, o.Destination, "settings/es_settings.xml", `<string name="ROMDirectory" value="`+base+`"/><string name="MediaDirectory" value="%ROMPATH%/media"/>`)
	put(t, o.Destination, "custom_systems/es_systems.xml", `<systemList><system><name>nes</name><path>`+manifest.ROMRoot+`</path><command>one</command><command>two</command></system></systemList>`)
	p := mustBuild(t, o)
	if p.MediaRoot != filepath.Join(base, "media") {
		t.Fatal(p.MediaRoot)
	}
	put(t, o.Destination, "custom_systems/es_systems.xml", `<systemList><system><name>nes</name><path>%ROMPATH%/wrong</path></system></systemList>`)
	if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatal(err)
	}
	put(t, o.Destination, "settings/es_settings.xml", `<bool name="LegacyGamelistFileLocation" value="true"/>`)
	if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatal(err)
	}
}
func TestESDEConflictsDriftAndSourceProtection(t *testing.T) {
	t.Run("folder conflict", func(t *testing.T) {
		o := esdeSetup(t, `<gameList><folder><path>./one.zip</path><name>Owner folder</name></folder></gameList>`)
		p := mustBuild(t, o)
		if len(p.Conflicts) == 0 {
			t.Fatal("missing conflict")
		}
		if _, err := p.Apply(); err == nil {
			t.Fatal("applied path conflict")
		}
	})
	t.Run("duplicate path", func(t *testing.T) {
		o := esdeSetup(t, `<gameList><game><path>./one.zip</path></game><game><path>one.zip</path></game></gameList>`)
		if _, err := Build(o); err == nil {
			t.Fatal("accepted duplicate paths")
		}
	})
	t.Run("external-media drift", func(t *testing.T) {
		o := esdeSetup(t, "")
		o.MediaRoot = filepath.Join(t.TempDir(), "new-media")
		p := mustBuild(t, o)
		put(t, o.MediaRoot, "nes/covers/one.png", "owner new cover")
		if _, err := p.Apply(); err == nil || !strings.Contains(err.Error(), "changed") {
			t.Fatal(err)
		}
	})
	t.Run("protect ROM tree", func(t *testing.T) {
		o := esdeSetup(t, "")
		var m esdeManifest
		_ = json.Unmarshal(get(t, o.PackageRoot, "manifest.json"), &m)
		o.MediaRoot = m.ROMRoot
		if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatal(err)
		}
	})
	t.Run("default settings and existing nonempty", func(t *testing.T) {
		o := esdeSetup(t, `<gameList><game><path>./one.zip</path><name>Owner</name><desc>My description</desc><genre>My genre</genre></game></gameList>`)
		p := mustBuild(t, o)
		if len(p.Changes[0].Fields) != 0 || p.MediaRoot != filepath.Join(o.Destination, "downloaded_media") {
			t.Fatal(p.Changes, p.MediaRoot)
		}
	})
}

func TestDeferredLocalMediaPreservesExistingVideoAndFillsLink(t *testing.T) {
	incoming := game(c, "Catalog", "ROMs/one.zip", "<VideoPath>Videos/Test Console/"+c+"-01.mp4</VideoPath>") + version(v1, c, "ROMs/one.zip", "")
	o := setup(t, game(a, "Owner", "ROMs/one.zip", "<VideoPath />"), incoming)
	put(t, o.PackageRoot, "Videos/Test Console/"+c+"-01.mp4", "catalog video")
	put(t, o.Destination, "Videos/Test Console/"+a+"-01.mp4", "owner video")
	disk := mustBuild(t, o)
	o.Source = virtualSource(t, o.PackageRoot)
	file := o.Source.Files["Videos/Test Console/"+c+"-01.mp4"]
	file.SHA256 = ""
	o.Source.Files["Videos/Test Console/"+c+"-01.mp4"] = file
	preview := mustBuild(t, o)
	if !reflect.DeepEqual(disk.Changes, preview.Changes) || len(preview.Changes[0].Fields) != 1 {
		t.Fatal(disk.Changes, preview.Changes)
	}
}

func TestGeneratedM3UVirtualPlanMatchesDisk(t *testing.T) {
	playlist := "Playlists/Test Console/" + v1 + ".m3u"
	old := filepath.Join(t.TempDir(), "working.m3u")
	put(t, filepath.Dir(old), filepath.Base(old), "C:/ROMs/disc1.chd\nC:/ROMs/disc2.chd\n")
	o := setup(t, game(a, "Owner", old, ""), game(c, "Catalog", playlist, "")+version(v1, c, playlist, ""))
	put(t, o.PackageRoot, playlist, "C:/ROMs/disc1.chd\nC:/ROMs/disc2.chd\n")
	disk := mustBuild(t, o)
	virtual := o
	virtual.Source = virtualSource(t, o.PackageRoot)
	virtual.PackageRoot = filepath.Join(t.TempDir(), "not-created")
	preview := mustBuild(t, virtual)
	if !reflect.DeepEqual(disk.Changes, preview.Changes) || !reflect.DeepEqual(disk.Files, preview.Files) || preview.Changes[0].ID != a || preview.Changes[0].Playlists != 0 {
		t.Fatal(disk.Changes, preview.Changes)
	}
}
func TestESDEMultipleSystemsUseSeparateROMAndMediaBindings(t *testing.T) {
	o := esdeSetup(t, "")
	var first esdeManifest
	_ = json.Unmarshal(get(t, o.PackageRoot, "manifest.json"), &first)
	var second esdeManifest
	// Roundtrip gives the second system its own media records before rewriting paths.
	data, _ := json.Marshal(first)
	data = bytes.ReplaceAll(data, []byte("downloaded_media/nes/"), []byte("downloaded_media/snes/"))
	_ = json.Unmarshal(data, &second)
	second.System = "snes"
	second.ROMRoot = t.TempDir()
	manifest := map[string]any{"schema_version": 1, "target": "es-de", "systems": []esdeManifest{first, second}}
	body, _ := json.Marshal(manifest)
	put(t, o.PackageRoot, "manifest.json", string(body))
	put(t, o.PackageRoot, "gamelists/snes/gamelist.xml", string(get(t, o.PackageRoot, "gamelists/nes/gamelist.xml")))
	put(t, o.PackageRoot, "downloaded_media/snes/covers/one.png", "SNES cover")
	put(t, o.PackageRoot, "downloaded_media/snes/videos/one.mp4", "SNES video")
	p := mustBuild(t, o)
	if len(p.Changes) != 4 {
		t.Fatal(p.Changes)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if string(get(t, p.MediaRoot, "nes/covers/one.png")) != "catalog cover" || string(get(t, p.MediaRoot, "snes/covers/one.png")) != "SNES cover" {
		t.Fatal("mixed system media")
	}
	if len(mustBuild(t, o).Files) != 0 {
		t.Fatal("multi-system repeat changed destination")
	}
}
