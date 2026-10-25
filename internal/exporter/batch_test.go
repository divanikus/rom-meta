package exporter

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addPlatform(t *testing.T, options Options, system, release string, payload []byte, filename string) string {
	t.Helper()
	platform := systems[system].Platform
	card := filepath.Join(options.DataRoot, "platforms", platform, "Test Game")
	put(t, filepath.Join(filepath.Dir(card), "_platform.yaml"), []byte("name: "+platform+"\n"))
	put(t, filepath.Join(card, "game.yaml"), []byte(fmt.Sprintf("title: Test Game\ndefault_release: %q\nreleases:\n  %q:\n    regions: [USA]\n", release, release)))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: %q\n    file: {name: %q, size: %d, sha1: '%x'}\n", release, filename, len(payload), sha1.Sum(payload))))
	put(t, filepath.Join(card, "media", "box-front.png"), []byte("synthetic art"))
	return card
}

func TestBatchCartridgeDiscAndArcadeOutputPreservation(t *testing.T) {
	options, _, payload := fixture(t)
	options.Platform = ""
	put(t, filepath.Join(options.ROMRoot, "n64", "renamed.z64"), payload)
	gbBytes := []byte("synthetic Game Boy cartridge")
	addPlatform(t, options, "gb", "Pocket Game (USA)", gbBytes, "Pocket Game (USA).gb")
	putZip(t, filepath.Join(options.ROMRoot, "gb", "Pocket Game (USA).zip"), map[string][]byte{"Pocket Game (USA).gb": gbBytes})
	chdBytes := []byte("synthetic container, not a physical disc or valid CHD")
	addPlatform(t, options, "psx", "Disc Game (USA)", chdBytes, "Disc Game (USA).bin")
	put(t, filepath.Join(options.ROMRoot, "psx", "Disc Game (USA).chd"), chdBytes)
	arcadeCard := arcadeFixture(t, options)
	putZip(t, filepath.Join(options.ROMRoot, "arcade", "game1.zip"), map[string][]byte{"one.bin": []byte("component"), "two.bin": []byte("component")})
	put(t, filepath.Join(options.ROMRoot, "unsupported", "something.dat"), []byte("private"))
	beforeCatalog, beforeROM := treeHashes(t, options.DataRoot), treeHashes(t, options.ROMRoot)
	batch, err := BuildBatch(BatchOptions{Options: options, All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Plans) != 4 || len(batch.Skipped) != 1 {
		t.Fatalf("batch: %+v", batch)
	}
	for _, plan := range batch.Plans {
		if plan.System == "psx" && (plan.HashMatches != 0 || plan.NameMatches != 1 || plan.Records[0].Representation != "chd-container") {
			t.Fatal("CHD falsely verified against raw component")
		}
		if plan.System == "arcade" && (plan.HashMatches != 0 || plan.Records[0].MatchedBy != "arcade-set-name") {
			t.Fatal("arcade container falsely verified")
		}
	}
	if err := ApplyBatch(batch, nil); err != nil {
		t.Fatal(err)
	}
	for _, plan := range batch.Plans {
		body, err := os.ReadFile(filepath.Join(options.Out, "gamelists", plan.System, "gamelist.xml"))
		if err != nil {
			t.Fatal(err)
		}
		var list xmlList
		if err := xml.Unmarshal(body, &list); err != nil || len(list.Games) != 1 {
			t.Fatalf("gamelist %s: %v", plan.System, err)
		}
		if strings.HasPrefix(list.Games[0].Path, "./"+plan.System+"/") {
			t.Fatal("system prefix leaked into system-relative launch path")
		}
		preview, err := os.ReadFile(filepath.Join(options.Out, "reports", plan.System, "preview.html"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(preview, []byte("../../downloaded_media/"+plan.System+"/covers/")) {
			t.Fatalf("nested preview image URL: %s", preview)
		}
	}
	manifest, _ := os.ReadFile(filepath.Join(options.Out, "manifest.json"))
	var saved BatchPlan
	if err := json.Unmarshal(manifest, &saved); err != nil || len(saved.Plans) != 4 {
		t.Fatal("missing aggregate manifest")
	}
	first := treeHashes(t, options.Out)
	options.Out = filepath.Join(filepath.Dir(options.Out), "again")
	again, err := BuildBatch(BatchOptions{Options: options, All: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyBatch(again, nil); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(first) != fmt.Sprint(treeHashes(t, options.Out)) {
		t.Fatal("batch output is not deterministic")
	}
	if fmt.Sprint(beforeCatalog) != fmt.Sprint(treeHashes(t, options.DataRoot)) || fmt.Sprint(beforeROM) != fmt.Sprint(treeHashes(t, options.ROMRoot)) {
		t.Fatal("source trees changed")
	}
	if _, err := os.Stat(arcadeCard); err != nil {
		t.Fatal(err)
	}
}

func arcadeFixture(t *testing.T, options Options) string {
	t.Helper()
	card := filepath.Join(options.DataRoot, "platforms", "Arcade", "Arcade Game")
	put(t, filepath.Join(filepath.Dir(card), "_platform.yaml"), []byte("name: Arcade\n"))
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Arcade Game\nreleases:\n  game1:\n    authority: {source: mame, entry_name: game1}\n    mame: {snapshot: 'test-snapshot', romof: parent1}\n"))
	put(t, filepath.Join(card, "media", "box-front.png"), []byte("synthetic arcade cover"))
	put(t, filepath.Join(options.DataRoot, "bindings", "romsets", "mame", "test-snapshot", "catalog.yaml"), []byte("sets:\n  game1: {platform: Arcade, game: Arcade Game, release: game1}\n"))
	return card
}

func TestBatchFailureDoesNotPublishPartialSystems(t *testing.T) {
	options, _, payload := fixture(t)
	options.Platform = ""
	put(t, filepath.Join(options.ROMRoot, "n64", "one.z64"), payload)
	put(t, filepath.Join(options.ROMRoot, "gb", "Two.gb"), []byte("second"))
	addPlatform(t, options, "gb", "Two", []byte("second"), "Two.gb")
	batch, err := BuildBatch(BatchOptions{Options: options, Systems: []string{"n64", "gb"}})
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(options.ROMRoot, "gb", "Two.gb"), []byte("changed"))
	if err := ApplyBatch(batch, nil); err == nil {
		t.Fatal("changed later system accepted")
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("partial batch published")
	}
	staging, _ := filepath.Glob(filepath.Join(filepath.Dir(options.Out), "rommeta-export-*"))
	if len(staging) > 0 {
		t.Fatal("failed batch staging was not cleaned")
	}
}

func TestBatchSelectionAndMissingCatalogReports(t *testing.T) {
	options, _, payload := fixture(t)
	options.Platform = ""
	put(t, filepath.Join(options.ROMRoot, "n64", "one.z64"), payload)
	put(t, filepath.Join(options.ROMRoot, "snes", "game.sfc"), payload)
	batch, err := BuildBatch(BatchOptions{Options: options, Systems: []string{"n64", "snes", "gb"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Plans) != 1 || len(batch.Skipped) != 2 {
		t.Fatal("missing system/catalog hidden")
	}
	for _, selected := range [][]string{{"../n64"}, {"n64", "n64"}, {"unknown"}} {
		if _, err := BuildBatch(BatchOptions{Options: options, Systems: selected}); err == nil {
			t.Fatal("invalid batch selection accepted")
		}
	}
	options.Out = filepath.Join(options.ROMRoot, "output")
	if _, err := BuildBatch(BatchOptions{Options: options, All: true}); err == nil {
		t.Fatal("batch output overlaps ROM tree")
	}
}

func TestCHDExplicitContainerFingerprintAndRawISOMatch(t *testing.T) {
	for _, ext := range []string{".chd", ".iso"} {
		t.Run(ext, func(t *testing.T) {
			options, _, _ := fixture(t)
			payload := []byte("synthetic container or raw ISO bytes")
			addPlatform(t, options, "psx", "Disc Game (USA)", payload, "Disc Game (USA)"+ext)
			options.Platform = "psx"
			put(t, filepath.Join(options.ROMRoot, "renamed"+ext), payload)
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if plan.System != "psx" || plan.HashMatches != 1 {
				t.Fatal("explicit representation fingerprint ignored")
			}
		})
	}
}
func TestMAMEProjectionIsExactSnapshotNameAndNotGenericTitle(t *testing.T) {
	options, _, _ := fixture(t)
	card := arcadeFixture(t, options)
	options.Platform = "arcade"
	for _, name := range []string{"game1.zip", "Arcade Game.zip", "unknown.zip"} {
		putZip(t, filepath.Join(options.ROMRoot, name), map[string][]byte{"one.bin": []byte("x")})
	}
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Matched != 1 || plan.Unmatched != 2 || plan.HashMatches != 0 {
		t.Fatal("arcade title/hash guess made")
	}
	for _, record := range plan.Records {
		if record.Status == "matched" && (record.ArcadeSnapshot != "test-snapshot" || len(record.Warnings) < 2) {
			t.Fatal("missing snapshot/dependency limits")
		}
	}
	body, err := RenderGamelist(plan)
	if err != nil {
		t.Fatal(err)
	}
	var list xmlList
	if err := xml.Unmarshal(body, &list); err != nil || len(list.Games) != 1 {
		t.Fatal("unbound arcade/support archive emitted as a launch")
	}
	put(t, filepath.Join(options.DataRoot, "bindings", "romsets", "mame", "second", "catalog.yaml"), []byte("sets: {}\n"))
	if _, err := Build(options); err == nil {
		t.Fatal("multiple MAME snapshots auto-picked")
	}
	options.ArcadeSnapshot = "test-snapshot"
	raw, _ := os.ReadFile(filepath.Join(card, "game.yaml"))
	put(t, filepath.Join(card, "game.yaml"), bytes.ReplaceAll(raw, []byte("test-snapshot"), []byte("different")))
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ambiguous != 1 || plan.Matched != 0 {
		t.Fatal("stale snapshot binding accepted")
	}
}
