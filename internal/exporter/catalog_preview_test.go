package exporter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
	"testing"
)

func TestCatalogPreviewAgreesWithRealExportSelection(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			options, path, payload := fixture(t)
			put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
			profile(t, &options, "schema_version: 1\nselection:\n  description:\n    languages: [ru, en]\n")
			if packed {
				catalogfstest.Pack(t, filepath.Dir(path))
			}
			before := treeHashes(t, options.DataRoot)
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			data, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := catalogyaml.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := PreviewCatalog(context.Background(), options.DataRoot, path, doc.Root(), plan.Profile, "", "")
			if err != nil {
				t.Fatal(err)
			}
			record := plan.Records[0]
			if !reflect.DeepEqual(preview.Fields, record.Fields) {
				t.Fatalf("browser fields differ from CLI: %+v / %+v", preview.Fields, record.Fields)
			}
			for role, asset := range preview.Media {
				if asset.SourcePath != record.Media[role].SourcePath || asset.OutputPath != "" || asset.SHA256 != "" {
					t.Fatalf("wrong media projection: %+v", asset)
				}
			}
			if preview.Fields["description"].Value != "Русское описание" || len(preview.Media) != len(record.Media) {
				t.Fatal(preview)
			}
			if !reflect.DeepEqual(before, treeHashes(t, options.DataRoot)) {
				t.Fatal("preview mutated catalog")
			}
		})
	}
}

func TestCatalogPreviewVariantContextAndCancellation(t *testing.T) {
	options, path, _ := variantFixture(t)
	data, err := os.ReadFile(filepath.Join(path, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ExportProfile(options.DataRoot, "schema_version: 1\nselection:\n  media: {}\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := PreviewCatalog(context.Background(), options.DataRoot, path, doc.Root(), p, "", "Fixed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Release != "Original (Japan)" || got.Variant != "Fixed" || got.Fields["publisher"].FromVariant != "Fixed" || got.Fields["description"].Value != "Original Japanese release description" || len(got.Media) != 0 {
		t.Fatalf("variant lost: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreviewCatalog(ctx, options.DataRoot, path, doc.Root(), p, "", ""); err != context.Canceled {
		t.Fatal(err)
	}
	for _, edition := range [][2]string{{"Missing", ""}, {"", "Missing"}, {"Original (USA)", "Fixed"}} {
		if _, err := PreviewCatalog(context.Background(), options.DataRoot, path, doc.Root(), p, edition[0], edition[1]); err == nil {
			t.Fatal("accepted invalid edition", edition)
		}
	}
}

func TestCatalogPreviewDoesNotGuessAmongMultipleReleases(t *testing.T) {
	options, path, _ := fixture(t)
	doc, err := catalogyaml.Parse([]byte("title: Card\nreleases:\n  USA:\n    developer: Wrong guess\n  Europe:\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ExportProfile(options.DataRoot, "schema_version: 1\nselection:\n  media: {}\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := PreviewCatalog(context.Background(), options.DataRoot, path, doc.Root(), p, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Release != "" || got.Fields["developer"].Value != "" || len(got.Warnings) == 0 || got.Fields["title"].Value != "Card" {
		t.Fatal(got)
	}
}
