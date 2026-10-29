package exporter

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readLaunchBox(t *testing.T, plan *Plan) launchBoxList {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(plan.Out, filepath.FromSlash(outputXMLPath(plan))))
	if err != nil {
		t.Fatal(err)
	}
	var list launchBoxList
	if err := xml.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestLaunchBoxCartridgesMediaAndStableIDs(t *testing.T) {
	options, _, payload := fixture(t)
	options.Target = "launchbox"
	profile(t, &options, "schema_version: 1\ntarget: es-de\ntitles: {variants: none}\n")
	put(t, filepath.Join(options.ROMRoot, "first.z64"), payload)
	putZip(t, filepath.Join(options.ROMRoot, "nested", "second.zip"), map[string][]byte{"payload.z64": payload})
	beforeCatalog, beforeROM := treeHashes(t, options.DataRoot), treeHashes(t, options.ROMRoot)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != "launchbox" || plan.Matched != 2 {
		t.Fatalf("%+v", plan)
	}
	if _, err := os.Stat(plan.Out); !os.IsNotExist(err) {
		t.Fatal("preview wrote package")
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	list := readLaunchBox(t, plan)
	if len(list.Games) != 2 {
		t.Fatal(list)
	}
	for i, g := range list.Games {
		if g.Title != "Example" || g.Notes != "English & <safe> description" || g.Platform != "Nintendo 64" || !g.Emulation || !g.Installed || g.MaxPlayers != 4 || g.Genre != "Action; Adventure" {
			t.Fatalf("%+v", g)
		}
		if g.ApplicationPath != filepath.Join(options.ROMRoot, filepath.FromSlash(plan.Records[i].ROM)) {
			t.Fatal(g.ApplicationPath)
		}
		if !strings.HasPrefix(g.VideoPath, "Videos/Nintendo 64/") {
			t.Fatal(g.VideoPath)
		}
		for role, m := range plan.Records[i].Media {
			if !strings.Contains(m.OutputPath, g.ID+"-01.") {
				t.Fatalf("%s: %+v", role, m)
			}
			if _, err := os.Stat(filepath.Join(plan.Out, filepath.FromSlash(m.OutputPath))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if list.Games[0].ID == list.Games[1].ID {
		t.Fatal("same-title media identity collision")
	}
	again := *plan
	again.Out = filepath.Join(filepath.Dir(plan.Out), "other")
	again.Records = append([]Record(nil), plan.Records...)
	again.Records[0].Fields = map[string]Value{"title": {Value: "Changed display title"}}
	if launchBoxID(&again, again.Records[0]) != list.Games[0].ID {
		t.Fatal("ID depends on output or metadata")
	}
	again.ROMRoot = filepath.Join(options.ROMRoot, "moved")
	if launchBoxID(&again, again.Records[0]) == list.Games[0].ID {
		t.Fatal("ID ignores actual launch path")
	}
	if !reflect.DeepEqual(beforeCatalog, treeHashes(t, options.DataRoot)) || !reflect.DeepEqual(beforeROM, treeHashes(t, options.ROMRoot)) {
		t.Fatal("modified inputs")
	}
	if err := Apply(plan, nil); err == nil {
		t.Fatal("replaced existing package")
	}
	preview, _ := os.ReadFile(filepath.Join(plan.Out, "preview.html"))
	if bytes.Contains(preview, []byte("ES-DE gamelist")) || !bytes.Contains(preview, []byte("Data/Platforms/Nintendo%2064.xml")) {
		t.Fatal("wrong preview target")
	}
}

func TestLaunchBoxDiscPlaylistAndUnsupported(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform, options.Target = "psx", "launchbox"
	card := addPlatform(t, options, "psx", "Disc Game (USA) (Disc 1)", []byte("one"), "unused.chd")
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Disc Game\nreleases:\n  Disc Game (USA) (Disc 1): {regions: [USA]}\n  Disc Game (USA) (Disc 2): {regions: [USA]}\n  Standalone (USA): {regions: [USA]}\n"))
	dir := filepath.Join(options.ROMRoot, "Disc Game.m3u")
	put(t, filepath.Join(dir, "Disc Game.m3u"), []byte("Disc Game (USA) (Disc 2).chd\nDisc Game (USA) (Disc 1).chd\n"))
	put(t, filepath.Join(dir, "Disc Game (USA) (Disc 1).chd"), []byte("one"))
	put(t, filepath.Join(dir, "Disc Game (USA) (Disc 2).chd"), []byte("two"))
	put(t, filepath.Join(options.ROMRoot, "Standalone (USA).iso"), []byte("iso"))
	put(t, filepath.Join(options.ROMRoot, "unsupported.cue"), []byte("FILE missing.bin BINARY"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 3 || plan.Matched != 2 || plan.Unsupported != 1 {
		t.Fatalf("%+v", plan)
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	list := readLaunchBox(t, plan)
	if len(list.Games) != 2 {
		t.Fatalf("%+v", list)
	}
	if list.Games[0].ApplicationPath != filepath.Join(dir, "Disc Game.m3u") {
		t.Fatal("did not launch actual inner M3U", list.Games[0])
	}
	if plan.Records[0].Playlist[0].ROM != "Disc Game.m3u/Disc Game (USA) (Disc 2).chd" {
		t.Fatal("changed playlist order")
	}
	options.Out = filepath.Join(filepath.Dir(options.Out), "changed")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "Disc Game (USA) (Disc 1).chd"), []byte("changed disc"))
	if err := Apply(plan, nil); err == nil {
		t.Fatal("accepted changed disc member")
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("published failed package")
	}
}

func TestLaunchBoxBatchAndDuplicatePlatform(t *testing.T) {
	options, _, payload := fixture(t)
	options.Platform, options.Target = "", "launchbox"
	put(t, filepath.Join(options.ROMRoot, "n64", "cart.z64"), payload)
	addPlatform(t, options, "psx", "Disc (USA)", []byte("disc"), "Disc (USA).iso")
	put(t, filepath.Join(options.ROMRoot, "psx", "Disc (USA).iso"), []byte("disc"))
	batch, err := BuildBatch(BatchOptions{Options: options, Systems: []string{"n64", "psx"}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Target != "launchbox" || len(batch.Plans) != 2 {
		t.Fatal(batch)
	}
	if err := ApplyBatch(batch, nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range batch.Plans {
		if len(readLaunchBox(t, p).Games) != 1 {
			t.Fatal(p)
		}
	}
	batch.Out = filepath.Join(filepath.Dir(batch.Out), "collision")
	batch.Plans = append(batch.Plans, batch.Plans[0])
	if err := ApplyBatch(batch, nil); err == nil || !strings.Contains(err.Error(), "duplicate target platform") {
		t.Fatal(err)
	}
	if _, err := os.Stat(batch.Out); !os.IsNotExist(err) {
		t.Fatal("published duplicate platform")
	}
}

func TestLaunchBoxRenderOmissionAndPathValidation(t *testing.T) {
	plan := &Plan{System: "n64", ROMRoot: t.TempDir(), Records: []Record{
		{ROM: "one.z64", Status: "unsupported"},
		{ROM: "two.z64", Status: "matched", Hidden: true},
		{ROM: "three.z64", Status: "unmatched", Fields: map[string]Value{"title": {Value: "Filename only"}}},
	}}
	body, err := RenderLaunchBox(plan)
	if err != nil {
		t.Fatal(err)
	}
	var list launchBoxList
	if err := xml.Unmarshal(body, &list); err != nil || len(list.Games) != 1 || list.Games[0].Title != "Filename only" {
		t.Fatal(string(body), err)
	}
	plan.Records[2].LaunchFile = "../escape.z64"
	if err := ProjectTarget(plan, "launchbox"); err == nil {
		t.Fatal("accepted path escape")
	}
	if _, err := NormalizeTarget("missing"); err == nil {
		t.Fatal("accepted unknown target")
	}
	a := &Plan{System: "genesis", ROMRoot: "roms"}
	b := &Plan{System: "megadrive", ROMRoot: "roms"}
	if launchBoxID(a, Record{ROM: "game.md"}) != launchBoxID(b, Record{ROM: "game.md"}) {
		t.Fatal("alias changes ID")
	}
}
