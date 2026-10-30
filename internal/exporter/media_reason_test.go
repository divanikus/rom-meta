package exporter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
)

func TestMediaReasonsFollowActualRankingWithoutReadingPayloads(t *testing.T) {
	for _, packed := range []bool{false, true} {
		for _, tc := range []struct {
			name      string
			paths     []string
			want, why string
		}{
			{"source", []string{"box-front-01.usa.launchbox.jpg", "box-front-01.usa.screenscraper.png"}, "box-front-01.usa.launchbox.jpg", "LaunchBox is preferred to ScreenScraper"},
			{"format", []string{"box-front-01.usa.launchbox.jpg", "box-front-04.usa.launchbox.png"}, "box-front-04.usa.launchbox.png", "PNG is preferred to JPG"},
			{"region", []string{"box-front-01.usa.launchbox.jpg", "box-front-01.world.launchbox.png"}, "box-front-01.usa.launchbox.jpg", "higher regional priority"},
			{"number", []string{"box-front-01.usa.launchbox.jpg", "box-front-02.usa.launchbox.jpg"}, "box-front-01.usa.launchbox.jpg", "File number 1 comes before 2"},
			{"filename", []string{"box-front-01.usa.variant-original.launchbox.jpg", "box-front-01.usa.variant-fanart.launchbox.jpg"}, "box-front-01.usa.variant-fanart.launchbox.jpg", "Alphabetically first filename"},
			{"scope", []string{"Example (USA)/box-front-09.usa.launchbox.jpg", "box-front-01.usa.launchbox.png"}, "box-front-09.usa.launchbox.jpg", "Edition-specific artwork"},
		} {
			t.Run(tc.name+map[bool]string{false: "/loose", true: "/packed"}[packed], func(t *testing.T) {
				options, card, _ := fixture(t)
				if err := os.RemoveAll(filepath.Join(card, "media")); err != nil {
					t.Fatal(err)
				}
				for _, name := range tc.paths {
					put(t, filepath.Join(card, "media", filepath.FromSlash(name)), []byte("synthetic; no image decoding is possible"))
				}
				// Zero-byte candidates must not be cited as usable alternatives.
				put(t, filepath.Join(card, "media", "box-front-00.usa.launchbox.png"), nil)
				data, err := os.ReadFile(filepath.Join(card, "game.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				doc, err := catalogyaml.Parse(data)
				if err != nil {
					t.Fatal(err)
				}
				p, err := ExportProfile(options.DataRoot, "selection:\n  media:\n    cover: {}\n")
				if err != nil {
					t.Fatal(err)
				}
				if packed {
					catalogfstest.Pack(t, filepath.Dir(card))
				}
				before := treeHashes(t, options.DataRoot)
				got, err := PreviewCatalog(context.Background(), options.DataRoot, card, doc.Root(), p, "", "")
				if err != nil {
					t.Fatal(err)
				}
				why := got.MediaReasons["cover"]
				if filepath.Base(got.Media["cover"].SourcePath) != tc.want || !strings.Contains(why.Summary, tc.why) || why.ComparedWith == "box-front-00.usa.launchbox.png" {
					t.Fatal(got.Media, why)
				}
				if !reflect.DeepEqual(before, treeHashes(t, options.DataRoot)) {
					t.Fatal("reason trace wrote catalog")
				}
			})
		}
	}
}

func TestMediaReasonRetainsSelectedVariantAndExplicitOverride(t *testing.T) {
	options, card, _ := variantFixture(t)
	put(t, filepath.Join(card, "media", "Original (Japan)", "box-front.japan.launchbox.png"), []byte("base"))
	put(t, filepath.Join(card, "media", "Fixed", "box-front.japan.variant-original.launchbox.jpg"), []byte("variant"))
	data, err := os.ReadFile(filepath.Join(card, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ExportProfile(options.DataRoot, "selection:\n  media:\n    cover: {}\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := PreviewCatalog(context.Background(), options.DataRoot, card, doc.Root(), p, "", "Fixed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Media["cover"].FromVariant != "Fixed" || !strings.Contains(strings.Join(got.MediaReasons["cover"].Details, " "), "edition: Fixed") {
		t.Fatal(got.Media, got.MediaReasons)
	}
	game, _ := filepath.Rel(options.DataRoot, card)
	p.MediaOverrides = []MediaOverride{{Game: filepath.ToSlash(game), Release: "Original (Japan)", Role: "cover", Path: "media/Fixed/box-front.japan.variant-original.launchbox.jpg"}}
	got, err = PreviewCatalog(context.Background(), options.DataRoot, card, doc.Root(), p, "Original (Japan)", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Media["cover"].Selection != "override" || got.MediaReasons["cover"].Summary != "Explicit file choice from the profile." {
		t.Fatal(got.Media, got.MediaReasons)
	}
}
