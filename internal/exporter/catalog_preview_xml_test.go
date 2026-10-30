package exporter

import (
	"context"
	"encoding/xml"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

func TestCatalogRawUsesNativeRenderersWithoutChangingSelection(t *testing.T) {
	options, path, _ := fixture(t)
	data, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ExportProfile(options.DataRoot, "schema_version: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCatalog(context.Background(), options.DataRoot, path, doc.Root(), p, "", "")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]Value{}
	for key, value := range preview.Fields {
		fields[key] = value
	}
	before := treeHashes(t, options.DataRoot)
	for _, target := range []string{"es-de", "launchbox"} {
		raw, err := RenderCatalogPreviewXML(preview, p, target, "Nintendo - Nintendo 64", "Example")
		if err != nil {
			t.Fatal(err)
		}
		if target == "es-de" {
			var list xmlList
			if err := xml.Unmarshal([]byte(raw.XML), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Games) != 1 || list.Games[0].Description != fields["description"].Value || list.Games[0].Path != "./ROM_FILE" {
				t.Fatal(raw.XML)
			}
		} else {
			var list launchBoxList
			if err := xml.Unmarshal([]byte(raw.XML), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Games) != 1 || len(list.Versions) != 1 || list.Games[0].Notes != fields["description"].Value || !strings.Contains(list.Games[0].ApplicationPath, "ROM_ROOT") || strings.Contains(raw.XML, "<Image") {
				t.Fatal(raw.XML)
			}
			if !strings.HasPrefix(raw.MediaPaths["cover"], "Images/Nintendo 64/Box - Front/") {
				t.Fatal(raw.MediaPaths)
			}
		}
		if !reflect.DeepEqual(fields, preview.Fields) || !reflect.DeepEqual(before, treeHashes(t, options.DataRoot)) {
			t.Fatal("Raw changed selection or catalog")
		}
	}
}

func TestLaunchBoxExtendedMediaSelectionsAndAdapterBoundary(t *testing.T) {
	options, card, payload := fixture(t)
	options.Target = "launchbox"
	put(t, filepath.Join(options.ROMRoot, "Example.z64"), payload)
	var body strings.Builder
	body.WriteString("selection:\n  media:\n")
	for _, role := range ExportMediaRoles("launchbox") {
		body.WriteString("    " + role.ID + ": {}\n")
		mapping := roles[role.ID]
		put(t, filepath.Join(card, "media", mapping.Kind+".world.launchbox."+roleFormat(role.ID)), []byte("synthetic"))
	}
	profile(t, &options, body.String())
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records[0].Media) != 16 {
		t.Fatal(plan.Records[0].Media)
	}
	for _, role := range []string{"cart-front", "cart-back", "cart-3d", "disc", "box-spine", "banner", "icon"} {
		if !strings.Contains(plan.Records[0].Media[role].OutputPath, "/"+launchBoxImages[role]+"/") {
			t.Fatal(role, plan.Records[0].Media[role])
		}
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	if err := ProjectTarget(plan, "es-de"); err == nil {
		t.Fatal("ES-DE accepted LaunchBox-only media roles")
	}
}

func TestLaunchBoxHumanVariantNamesKeepNativeIdentity(t *testing.T) {
	plan := &Plan{System: "n64", ROMRoot: "/roms", Profile: Profile{}, Records: []Record{{ROM: "variant.z64", Status: "matched", Game: "platforms/Nintendo/Example", Variant: "translation-0123456789abcdef0123456789abcdef01234567", VariantInfo: &VariantInfo{Kind: "translation", Title: "Translated title", Languages: []string{"en"}, Group: "Test team", Version: "1.2"}, Fields: map[string]Value{"title": {Value: "Example"}}}}}
	id := launchBoxVersionID(plan, plan.Records[0])
	projection, err := buildLaunchBoxProjection(plan)
	if err != nil {
		t.Fatal(err)
	}
	version := projection.Groups[0].Versions[0]
	if version.ID != id || !strings.Contains(version.Name, "Translated title [en] / Test team / v1.2") || strings.Contains(version.Name, "012345") {
		t.Fatal(version)
	}
}
