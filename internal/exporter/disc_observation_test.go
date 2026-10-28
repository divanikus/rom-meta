package exporter

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticCHD() []byte {
	h := make([]byte, 512)
	copy(h, "MComprHD")
	binary.BigEndian.PutUint32(h[8:12], 124)
	binary.BigEndian.PutUint32(h[12:16], 5)
	binary.BigEndian.PutUint64(h[32:40], 2448*100)
	binary.BigEndian.PutUint64(h[40:48], 124)
	binary.BigEndian.PutUint32(h[56:60], 2448*8)
	binary.BigEndian.PutUint32(h[60:64], 2448)
	h[64] = 1
	return h
}

func TestObserveCHDSinglesAndDiscFolders(t *testing.T) {
	options, _, _ := fixture(t)
	root := options.ROMRoot
	put(t, filepath.Join(root, "Single (USA).chd"), syntheticCHD())
	single, err := ObserveInput(root, "3do", "Single (USA).chd")
	if err != nil || !ValidObservation(single) || single.Outer.SHA256 != "" || !single.Payload.Equal(PayloadProof{}) || single.Disc.Directory || len(single.Disc.Files[0].Header) != 248 {
		t.Fatal(single, err)
	}
	for _, count := range []int{2, 3, 4} {
		folder := fmt.Sprintf("Group%d [compact]", count)
		for number := count; number >= 1; number-- {
			put(t, filepath.Join(root, folder, fmt.Sprintf("Game (USA) (Disc %d).chd", number)), syntheticCHD())
		}
		input, e := ObserveInput(root, "3do", folder)
		if e != nil || !ValidObservation(input) || len(input.Disc.Files) != count || input.Outer.Size != int64(count*512) || input.Outer.SHA256 != "" {
			t.Fatal(input, e)
		}
		for i, file := range input.Disc.Files {
			if !strings.Contains(file.Name, fmt.Sprintf("(Disc %d)", i+1)) {
				t.Fatal("disc ordering", input)
			}
		}
	}
	if key, number, _ := discEdition("Game (USA) (Disc 3) (v1.0)"); key != "Game (USA) (v1.0)" || number != 3 {
		t.Fatal("release version treated as extension", key, number)
	}
	for _, name := range []string{"../Single (USA).chd", "/Single (USA).chd", "Single.iso"} {
		if _, e := ObserveInput(root, "3do", name); e == nil {
			t.Fatal("invalid path accepted", name)
		}
	}
	bad := syntheticCHD()
	bad[104] = 1
	put(t, filepath.Join(root, "Parent.chd"), bad)
	if _, e := ObserveInput(root, "3do", "Parent.chd"); e == nil {
		t.Fatal("parent CHD accepted")
	}
	bad = syntheticCHD()
	bad[12] = 1
	put(t, filepath.Join(root, "Bad.chd"), bad)
	if _, e := ObserveInput(root, "3do", "Bad.chd"); e == nil {
		t.Fatal("invalid header accepted")
	}
	for _, files := range [][]string{{"Game (USA) (Disc 1).chd", "Game (USA) (Disc 3).chd"}, {"Game (USA) (Disc 1).chd", "Other (USA) (Disc 2).chd"}, {"Game (USA) (Disc 1 of 3).chd", "Game (USA) (Disc 2 of 3).chd"}} {
		folder := t.TempDir()
		for _, name := range files {
			put(t, filepath.Join(folder, name), syntheticCHD())
		}
		if _, e := ObserveInput(filepath.Dir(folder), "3do", filepath.Base(folder)); e == nil {
			t.Fatal("mixed/missing discs accepted", files)
		}
	}
}

func TestDiscGameBindingOptInAndCatalogMissingDisc(t *testing.T) {
	options, _, _ := fixture(t)
	card := addPlatform(t, options, "3do", "Game (USA) (Disc 1)", []byte("raw track"), "Game.bin")
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA) (Disc 1): {}\n  Game (USA) (Disc 2): {}\n"))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte("items:\n  - release: Game (USA) (Disc 1)\n    file: {name: Game.bin, size: 1, sha1: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}\n  - release: Game (USA) (Disc 2)\n    file: {name: Game.bin, size: 2, sha1: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}\n"))
	for i := 1; i <= 2; i++ {
		put(t, filepath.Join(options.ROMRoot, "Compact [abc]", fmt.Sprintf("Game (USA) (Disc %d).chd", i)), syntheticCHD())
	}
	input, err := ObserveInput(options.ROMRoot, "3do", "Compact [abc]")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open("3DO Interactive Multiplayer")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.Resolve([]ObservedInput{input})
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal("default fabricated CHD proof", err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status != "matched" || plan.Records[0].MatchedBy != "game-name" || plan.Records[0].Release != "" || plan.Records[0].SHA1 != "" || plan.Records[0].Payload != nil {
		t.Fatal(plan, err)
	}
	// A new listed third disc makes the same contiguous two-disc folder incomplete.
	f, err := os.OpenFile(filepath.Join(card, "fingerprints.yaml"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(f, "  - release: Game (USA) (Disc 3)\n    file: {name: third.bin, size: 3, sha1: cccccccccccccccccccccccccccccccccccccccc}\n")
	f.Close()
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA) (Disc 1): {}\n  Game (USA) (Disc 2): {}\n  Game (USA) (Disc 3): {}\n"))
	resolver, err = catalog.Open("3DO Interactive Multiplayer")
	if err != nil {
		t.Fatal(err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal("catalog missing disc accepted", plan, err)
	}
}

func TestPSPMetadataUsesDeclaredUMDHeaderWithoutLaunchReader(t *testing.T) {
	options, _, _ := fixture(t)
	platform := filepath.Join(options.DataRoot, "platforms", "Sony - PlayStation Portable")
	put(t, filepath.Join(platform, "_platform.yaml"), []byte("name: Sony - PlayStation Portable\n"))
	card := filepath.Join(platform, "UMD Game")
	put(t, filepath.Join(card, "game.yaml"), []byte("title: UMD Game\nreleases:\n  UMD Game (USA) (En,Fr):\n    authority: {source: redump, entry_name: 'UMD Game (USA) (En,Fr)'}\n"))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte("items:\n  - release: UMD Game (USA) (En,Fr)\n    file: {name: game.iso, size: 204800, sha1: '0100000000000000000000000000000000000000'}\n"))
	h := syntheticCHD()
	binary.BigEndian.PutUint64(h[32:40], 204800)
	binary.BigEndian.PutUint32(h[56:60], 2048*8)
	binary.BigEndian.PutUint32(h[60:64], 2048)
	for _, name := range []string{"Renamed.chd", "Translated [T-En].chd"} {
		put(t, filepath.Join(options.ROMRoot, name), h)
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open("Sony - PlayStation Portable")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.Platform.System != "psp" || len(resolver.Platform.ExportSystems) != 0 || !resolver.Platform.GameBinding {
		t.Fatal(resolver.Platform)
	}
	input, err := ObserveInput(options.ROMRoot, "psp", "Renamed.chd")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.ResolveBindings([]ObservedInput{input}, false)
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal(plan, err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil {
		t.Fatal(err)
	}
	record := plan.Records[0]
	if record.Status != "matched" || record.MatchedBy != "game-name" || record.Release != "" || record.MetadataRelease != "UMD Game (USA) (En,Fr)" || record.SHA1 != "" || record.Payload != nil || record.Disc == nil {
		t.Fatalf("%+v", record)
	}
	translated, err := ObserveInput(options.ROMRoot, "psp", "Translated [T-En].chd")
	if err != nil {
		t.Fatal(err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{translated}, true)
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal(plan, err)
	}
	// An unknown declaration and a conflicting Redump target cannot select a card.
	input.Disc.Files[0].Header = strings.Replace(input.Disc.Files[0].Header, "0100000000000000000000000000000000000000", "0200000000000000000000000000000000000000", 1)
	plan, err = resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal(plan, err)
	}
	duplicate := filepath.Join(platform, "Another UMD Game")
	put(t, filepath.Join(duplicate, "game.yaml"), []byte("title: Another UMD Game\nreleases:\n  Other (USA):\n    authority: {source: redump, entry_name: 'Other (USA)'}\n"))
	put(t, filepath.Join(duplicate, "fingerprints.yaml"), []byte("items:\n  - release: Other (USA)\n    file: {name: other.iso, size: 204800, sha1: '0100000000000000000000000000000000000000'}\n"))
	resolver, err = catalog.Open("Sony - PlayStation Portable")
	if err != nil {
		t.Fatal(err)
	}
	input, err = ObserveInput(options.ROMRoot, "psp", "Renamed.chd")
	if err != nil {
		t.Fatal(err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status != "ambiguous" || plan.Records[0].Game != "" {
		t.Fatal(plan, err)
	}
}

func TestCHDCompleteReleaseNamePrecedesSharedGameTitle(t *testing.T) {
	options, _, _ := fixture(t)
	platform := "Sony - PlayStation 2"
	familyCard(t, options.DataRoot, platform, "Adventure", "Adventure (USA)")
	familyCard(t, options.DataRoot, platform, "Adventure (Demo)", "Adventure (USA) (Demo)")
	put(t, filepath.Join(options.ROMRoot, "Adventure (USA).chd"), syntheticCHD())
	observed, err := ObserveInput(options.ROMRoot, "ps2", "Adventure (USA).chd")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open(platform)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.ResolveBindings([]ObservedInput{observed}, true)
	if err != nil {
		t.Fatal(err)
	}
	record := plan.Records[0]
	if record.Status != "matched" || record.Release != "" || record.MetadataRelease != "Adventure (USA)" || record.MatchedBy != "game-name" || record.Payload != nil || record.SHA1 != "" {
		t.Fatalf("complete release lost to shared title: %+v", record)
	}
	// A duplicate complete release cannot select either card.
	familyCard(t, options.DataRoot, platform, "Conflict", "Adventure (USA)")
	catalog, err = NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err = catalog.Open(platform)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{observed}, true)
	if err != nil || plan.Records[0].Status != "ambiguous" {
		t.Fatalf("complete-name collision: %+v %v", plan, err)
	}
}
