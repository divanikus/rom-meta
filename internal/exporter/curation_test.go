package exporter

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/curation"
	"strings"
	"testing"
)

func TestNamedVariantsUseSharedExportPriorities(t *testing.T) {
	options, path, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	registry, _ := curation.Load(options.DataRoot)
	registry, a, e := curation.Save(options.DataRoot, registry.Revision, "", "Short")
	if e != nil {
		t.Fatal(e)
	}
	_, b, e := curation.Save(options.DataRoot, registry.Revision, "", "Long")
	if e != nil {
		t.Fatal(e)
	}
	card, e := catalogedit.Read(options.DataRoot, "Nintendo - Nintendo 64", "Example")
	if e != nil {
		t.Fatal(e)
	}
	_, e = catalogedit.SaveEdits(options.DataRoot, "Nintendo - Nintendo 64", "Example", card.Revision, []catalogedit.Edit{
		{Target: catalogedit.Target{Kind: "manual", Source: a.ID}, Fields: catalogedit.Patch{"title": "Short title", "developer": "Whole game", "descriptions": map[string]any{"en": "Literal &quot;short&quot;", "ru": "Кратко"}}},
		{Target: catalogedit.Target{Kind: "manual", Source: a.ID, Scope: "Example (USA)"}, Fields: catalogedit.Patch{"title": "USA title", "developer": "USA studio"}},
		{Target: catalogedit.Target{Kind: "manual", Source: b.ID}, Fields: catalogedit.Patch{"title": "Long title", "developer": "Long studio", "descriptions": map[string]any{"en": "Long prose"}}},
	})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ order, developer, description, source, title string }{
		{a.ID + ", " + b.ID + ", launchbox", "USA studio", "Literal &quot;short&quot;", a.ID, "USA title"},
		{b.ID + ", " + a.ID + ", launchbox", "Long studio", "Long prose", b.ID, "Long title"},
		{"launchbox", "First developer", "English & <safe> description", "launchbox", "Example"},
	} {
		body := "schema_version: 1\nselection:\n  description:\n    languages: [en]\n    sources: [" + tc.order + "]\n  fields:\n    title:\n      sources: [" + tc.order + "]\n    developer:\n      sources: [" + tc.order + "]\n"
		profile(t, &options, body)
		plan, e := Build(options)
		if e != nil {
			t.Fatal(e)
		}
		if len(plan.Records) != 1 {
			t.Fatal(plan.Records)
		}
		fields := plan.Records[0].Fields
		if fields["title"].Value != tc.title || fields["title"].Source != map[bool]string{true: "release", false: tc.source}[tc.source == "launchbox"] || fields["developer"].Value != tc.developer || fields["description"].Value != tc.description || fields["description"].Source != tc.source {
			t.Fatal(fields)
		}
		data, _ := os.ReadFile(filepath.Join(path, "game.yaml"))
		doc, e := catalogyaml.Parse(data)
		if e != nil {
			t.Fatal(e)
		}
		preview, e := PreviewCatalog(context.Background(), options.DataRoot, path, doc.Root(), plan.Profile, "Example (USA)", "")
		if e != nil {
			t.Fatal(e)
		}
		if preview.Fields["title"] != fields["title"] || preview.Fields["description"] != fields["description"] {
			t.Fatal(preview.Fields, fields)
		}
		for _, target := range []string{"es-de", "launchbox"} {
			raw, e := RenderCatalogPreviewXML(preview, plan.Profile, target, "Nintendo - Nintendo 64", "Example")
			if e != nil {
				t.Fatal(e)
			}
			if target == "es-de" {
				var list xmlList
				if e = xml.Unmarshal([]byte(raw.XML), &list); e != nil || list.Games[0].Description != tc.description || list.Games[0].Name != tc.title {
					t.Fatal(raw.XML, e)
				}
			} else {
				var list launchBoxList
				if e = xml.Unmarshal([]byte(raw.XML), &list); e != nil || list.Games[0].Notes != tc.description || list.Games[0].Title != map[bool]string{true: "Example & Friends", false: tc.title}[tc.source == "launchbox"] {
					t.Fatal(raw.XML, e)
				}
			}
		}
	}
}
func TestManualScopeNeverBorrowsAnotherRelease(t *testing.T) {
	text := "title: Game\nreleases:\n  USA: {}\n  Japan: {}\nsources:\n  manual.test:\n    matches:\n      - release: Japan\n        descriptions:\n          en: Japan only\n      - developer: Whole game\n      - release: USA\n        developer: USA studio\n"
	doc, e := catalogyaml.Parse([]byte(text))
	if e != nil {
		t.Fatal(e)
	}
	p, e := ExportProfile("", "selection:\n  description:\n    scope: game\n    sources: [manual.test]\n  fields:\n    developer:\n      sources: [manual.test]\n")
	if e != nil {
		t.Fatal(e)
	}
	g := &game{name: "Game", root: doc.Root()}
	f := resolveFields(g, "USA", p)
	if f["description"].Value != "" || f["developer"].Value != "USA studio" {
		t.Fatal(f)
	}
	doc, _ = catalogyaml.Parse([]byte(text + "developer: Explicit override\n"))
	f = resolveFields(&game{root: doc.Root()}, "USA", p)
	if !strings.Contains(f["developer"].Value, "Explicit") {
		t.Fatal(f)
	}
}
