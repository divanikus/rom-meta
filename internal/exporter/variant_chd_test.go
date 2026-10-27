package exporter

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVariantCombinedCHDExportAndLookup(t *testing.T) {
	for _, gameOnly := range []bool{false, true} {
		t.Run(fmt.Sprint("game-only=", gameOnly), func(t *testing.T) {
			options, old, _ := variantFixture(t)
			platform := "Sony - PlayStation"
			path := filepath.Join(options.DataRoot, "platforms", platform, "Game")
			raw, err := os.ReadFile(filepath.Join(old, "game.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			wantBasis, wantDescription := "Original (Japan)", "Original Japanese release description"
			if gameOnly {
				raw = []byte(strings.Replace(string(raw), "    based_on: Original (Japan)\n", "", 1))
				wantBasis, wantDescription = "Original (USA)", "Wrong default description"
			}
			put(t, filepath.Join(path, "game.yaml"), raw)
			put(t, filepath.Join(filepath.Dir(path), "_platform.yaml"), []byte("name: Sony - PlayStation\n"))
			chd := syntheticCHD()
			chd[84] = 2
			put(t, filepath.Join(path, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - variant: Fixed\n    file:\n      kind: chd-combined-sha1\n      name: Translated.chd\n      sha1: %x\n", chd[84:104])))
			rom := "translations/Completely renamed.chd"
			put(t, filepath.Join(options.ROMRoot, filepath.FromSlash(rom)), chd)
			options.Platform, options.System = "psx", "psx"
			result, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != 1 {
				t.Fatalf("%+v", result.Records)
			}
			r := result.Records[0]
			if r.Representation != "chd-container" || r.Variant != "Fixed" || r.MatchedBy != "variant-chd-header" || r.Release != "" || r.MetadataRelease != wantBasis || r.Disc == nil ||
				r.Fields["description"].Value != wantDescription || r.Payload == nil || r.Payload.SHA1 == fmt.Sprintf("%x", chd[84:104]) {
				t.Fatalf("CHD identity/proof conflated: %+v", r)
			}
			for i := 0; i < 2; i++ {
				catalog, err := NewLookupCatalog(options.DataRoot, "")
				if err != nil {
					t.Fatal(err)
				}
				resolver, err := catalog.Open(platform)
				if err != nil {
					t.Fatal(err)
				}
				input, err := ObserveInput(options.ROMRoot, "psx", rom)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
				if err != nil {
					t.Fatal(err)
				}
				if got := plan.Records[0]; got.Variant != "Fixed" || got.MatchedBy != "variant-chd-header" || got.Payload != nil || got.Release != "" || got.MetadataRelease != wantBasis {
					t.Fatalf("native CHD variant: %+v", got)
				}
			}
		})
	}
}

func TestVariantCHDContainerAndCombinedConflict(t *testing.T) {
	options, _, _ := variantFixture(t)
	platform := "Sony - PlayStation"
	path := filepath.Join(options.DataRoot, "platforms", platform, "Game")
	put(t, filepath.Join(filepath.Dir(path), "_platform.yaml"), []byte("name: Sony - PlayStation\n"))
	put(t, filepath.Join(path, "game.yaml"), []byte("title: Game\nreleases:\n  Original: {}\nvariants:\n  English:\n    kind: translation\n    based_on: Original\n"))
	chd := syntheticCHD()
	chd[84] = 5
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - variant: English\n    file:\n      kind: chd-combined-sha1\n      sha1: %x\n  - release: Original\n    file:\n      name: Original.chd\n      size: %d\n      sha1: %x\n", chd[84:104], len(chd), sha1.Sum(chd))))
	put(t, filepath.Join(options.ROMRoot, "Renamed.chd"), chd)
	options.Platform, options.System = "psx", "psx"
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Status != "ambiguous" {
		t.Fatalf("conflicting representations selected a target: %+v", plan.Records[0])
	}
}

func TestVariantCHDPlaylistPreservesDiscIdentities(t *testing.T) {
	options, _, _ := fixture(t)
	game := filepath.Join(options.DataRoot, "platforms", "Sony - PlayStation", "Game")
	put(t, filepath.Join(filepath.Dir(game), "_platform.yaml"), []byte("name: Sony - PlayStation\n"))
	data := "title: Game\nreleases:\n  Base (Disc 1): {description: First disc prose}\n  Base (Disc 2): {}\nvariants:\n"
	fps := "items:\n"
	folder := filepath.Join(options.ROMRoot, "Game.m3u")
	for i := 1; i <= 2; i++ {
		data += fmt.Sprintf("  English (Disc %d):\n    title: English Adventure\n    kind: translation\n    based_on: Base (Disc %d)\n    languages: [en]\n    version: '1.0'\n", i, i)
		chd := syntheticCHD()
		chd[84] = byte(10 + i)
		fps += fmt.Sprintf("  - variant: English (Disc %d)\n    file:\n      kind: chd-combined-sha1\n      sha1: %x\n", i, chd[84:104])
		put(t, filepath.Join(folder, fmt.Sprintf("renamed-%d.chd", i)), chd)
	}
	put(t, filepath.Join(game, "game.yaml"), []byte(data))
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte(fps))
	put(t, filepath.Join(folder, "Game.m3u"), []byte("renamed-1.chd\nrenamed-2.chd\n"))
	options.Platform, options.System = "psx", "psx"
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	r := plan.Records[0]
	if r.Status != "matched" || r.Variant != "English (Disc 1)" || len(r.Playlist) != 2 || r.Playlist[1].Variant != "English (Disc 2)" ||
		r.Fields["description"].Value != "First disc prose" || r.MatchedBy != "playlist-name" {
		t.Fatalf("lost CHD playlist identity/evidence: %+v", r)
	}
	// Same game's other patch version is not silently merged into this variant.
	data = strings.Replace(data, "version: '1.0'", "version: '2.0'", 1)
	put(t, filepath.Join(game, "game.yaml"), []byte(data))
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Status != "unsupported" {
		t.Fatalf("mixed patch versions formed one edition: %+v", plan.Records[0])
	}
}

func TestVariantCHDGroupRequiresEveryDeclaredDiscInOneGame(t *testing.T) {
	options, _, _ := variantFixture(t)
	platform := "Sony - PlayStation"
	folder := filepath.Join(options.DataRoot, "platforms", platform)
	game := filepath.Join(folder, "Game")
	put(t, filepath.Join(folder, "_platform.yaml"), []byte("name: Sony - PlayStation\n"))
	data := "title: Original\ndefault_release: USA\nreleases:\n  USA: {description: Default prose}\n  Japan: {description: First disc prose}\nvariants:\n  First: {kind: translation, based_on: Japan, group: First team, version: '1.0'}\n  Second: {kind: translation, group: Other team, version: '2.0'}\n"
	put(t, filepath.Join(game, "game.yaml"), []byte(data))
	fps := "items:\n"
	for i := 1; i <= 2; i++ {
		h := syntheticCHD()
		h[84] = byte(10 + i)
		variant := "First"
		if i == 2 {
			variant = "Second"
		}
		fps += fmt.Sprintf("  - variant: %s\n    file: {kind: chd-combined-sha1, sha1: '%x'}\n", variant, h[84:104])
		put(t, filepath.Join(options.ROMRoot, "translations", "Renamed", fmt.Sprintf("Unknown (Disc %d).chd", i)), h)
	}
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte(fps))
	input, err := ObserveInput(options.ROMRoot, "psx", "translations/Renamed")
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
	held, err := resolver.ResolveBindings([]ObservedInput{input}, false)
	if err != nil || held.Records[0].Status == "matched" {
		t.Fatal("missing game-binding consent", err)
	}
	for i := 0; i < 2; i++ {
		plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
		if err != nil {
			t.Fatal(err)
		}
		r := plan.Records[0]
		if r.Status != "matched" || r.Variant != "First" || r.MetadataRelease != "Japan" || r.Release != "" || len(r.DiscVariants) != 2 || r.DiscVariants[1].Variant != "Second" || r.DiscVariants[1].MetadataRelease != "USA" || r.Fields["description"].Value != "First disc prose" || r.Payload != nil {
			t.Fatalf("lost group identity: %+v", r)
		}
	}
	for _, missing := range []int{0, 1} {
		changed := input
		disc := *input.Disc
		disc.Files = append([]DiscFile(nil), disc.Files...)
		h := syntheticCHD()
		h[84] = 99
		disc.Files[missing].Header = fmt.Sprintf("%x", h[:124])
		changed.Disc = &disc
		plan, err := resolver.ResolveBindings([]ObservedInput{changed}, true)
		if err != nil || plan.Records[0].Status == "matched" {
			t.Fatal("partial declaration accepted", err)
		}
	}
	// The same filenames cannot join two independently identified games.
	h := syntheticCHD()
	h[84] = 12
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte(strings.Split(fps, "  - variant: Second")[0]))
	other := filepath.Join(folder, "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Other\nreleases:\n  Other: {}\nvariants:\n  Second: {kind: translation}\n"))
	put(t, filepath.Join(other, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - variant: Second\n    file: {kind: chd-combined-sha1, sha1: '%x'}\n", h[84:104])))
	catalog, err = NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err = catalog.Open(platform)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.ResolveBindings([]ObservedInput{input}, true)
	if err != nil || plan.Records[0].Status != "ambiguous" {
		t.Fatal("mixed games accepted", err, plan.Records)
	}
}
