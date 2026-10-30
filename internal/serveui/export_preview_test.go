package serveui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/exporter"
	"strings"
	"testing"
)

func postPreview(t *testing.T, h http.Handler, input exportPreviewRequest) exportPreviewResponse {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response exportPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestBrowserLibraryOverridesAndSharedExportProfile(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "platforms", "Test platform", "Card")
			writeTestFile(t, filepath.Join(path, "game.yaml"), `title: Owner title
description: Explicit owner description
developer: Owner developer
unknown_owner_field: Keep this evidence
default_release: Edition
releases:
  Edition:
    title: Regional title
    publisher: Release publisher
sources:
  launchbox:
    matches:
      - developer: Provider developer
        descriptions: {en: English prose, ru: Russian prose}
`)
			writeTestFile(t, filepath.Join(path, "media", "box-front.world.launchbox.png"), "synthetic image")
			if packed {
				catalogfstest.Pack(t, filepath.Dir(path))
			}
			h, err := Handler(root)
			if err != nil {
				t.Fatal(err)
			}
			var detail gameResponse
			requestJSON(t, h, "/api/game?platform=Test%20platform&game=Card", &detail)
			if detail.Profile.Title != "Owner title" || detail.Profile.Developer != "Owner developer" || detail.Profile.Description != "Explicit owner description" || !strings.Contains(detail.CatalogYAML, "unknown_owner_field") || detail.SourceBlocks[0].Profile.Developer != "Provider developer" {
				t.Fatal(detail)
			}
			for _, target := range []string{"es-de", "launchbox"} {
				settings := exportSettings{Target: target, Language: "ru", Media: "cover"}
				response := postPreview(t, h, exportPreviewRequest{Platform: "Test platform", Settings: settings, Games: []previewGameRequest{{Folder: "Card"}, {Folder: "Missing"}}})
				if response.Target != target || len(response.Games) != 2 || response.Games[1].Error == "" {
					t.Fatal(response)
				}
				preview := response.Games[0].Preview
				if preview == nil || preview.Fields["title"].Value != "Regional title" || preview.Fields["publisher"].Value != "Release publisher" || preview.Fields["description"].Source != "local" || len(preview.Media) != 1 {
					t.Fatal(preview, response.Games[0].Error)
				}
				if err := exporter.ValidateExportProfile(response.ProfileYAML); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(response.ProfileYAML, "target:") || strings.Contains(response.ProfileYAML, root) {
					t.Fatal("profile contains machine/target context")
				}
				settings.Media = "none"
				empty := postPreview(t, h, exportPreviewRequest{Settings: settings})
				p, err := exporter.ExportProfile(root, empty.ProfileYAML)
				if err != nil || len(p.Selection.Media) != 0 {
					t.Fatal(p, err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "catalog.sqlite")); !os.IsNotExist(err) {
				t.Fatal("browser created acquisition database", err)
			}
		})
	}
}

func TestExportPreviewRequestBoundsAndValidation(t *testing.T) {
	h, err := Handler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{}`, `{"settings":{"target":"bad","language":"en","media":"all"}}`,
		`{"settings":{"language":"xx","media":"all"}}`,
		`{"settings":{"language":"en","media":"bad"}}`,
		`{"settings":{"language":"en","media":"all","description_scope":"bad"}}`,
		`{"settings":{"language":"en","media":"all"},"platform":"../bad","games":[{"folder":"Card"}]}`,
		`{"settings":{"language":"en","media":"all"},"platform":"Test","games":[{"folder":"../bad"}]}`,
		`{"settings":{"language":"en","media":"all"},"profile_path":"private.yaml"}`,
		`{"settings":{"language":"en","media":"all"}} {}`, strings.Repeat(" ", 64<<10) + `{}`,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("accepted %q: %d %s", body[:min(len(body), 150)], w.Code, w.Body.String())
		}
	}
	input := exportPreviewRequest{Platform: "Test", Settings: exportSettings{Language: "en", Media: "all"}, Games: make([]previewGameRequest, 33)}
	body, _ := json.Marshal(input)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", bytes.NewReader(body)))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/export-preview", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}

func TestCompactProfileControlsRoundTripCatalogDefaults(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "config", "sources.yaml"), "priority:\n  metadata: [screenscraper, launchbox]\n")
	server := &server{root: root}
	for _, language := range []string{"en", "ru", "ja"} {
		for _, media := range []string{"all", "images", "cover", "none"} {
			for _, scope := range []string{"game", "release"} {
				for _, title := range []string{"regional", "release"} {
					settings := exportSettings{Target: "launchbox", Language: language, Media: media, DescriptionScope: scope, MediaScope: scope, TitleMode: title}
					p, content, _, err := server.exportProfile(settings)
					if err != nil {
						t.Fatalf("%+v: %v", settings, err)
					}
					loaded, err := exporter.ExportProfile(root, content)
					if err != nil || loaded.Selection.Description.Sources[0] != "screenscraper" || loaded.Selection.Description.Languages[0] != language || loaded.Titles.Mode != title || loaded.Selection.Description.Scope != scope || len(loaded.Selection.Media) != len(p.Selection.Media) {
						t.Fatal(settings, loaded, err)
					}
				}
			}
		}
	}
	_, content, _, err := server.exportProfile(exportSettings{Target: "es-de", Language: "en", Media: "all", MetadataSources: []string{"igdb"}, ArtworkSources: []string{"screenscraper"}, VideoSources: []string{"emumovies"}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := exporter.ExportProfile(root, content)
	if err != nil || loaded.Selection.Fields["developer"].Sources[0] != "igdb" || loaded.Selection.Media["cover"].Sources[0] != "screenscraper" || loaded.Selection.Media["video"].Sources[0] != "emumovies" {
		t.Fatal(loaded, err)
	}
}

func TestBrowserDoesNotPresentImplicitReleaseAsStoredDefault(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "platforms", "Test", "Card", "game.yaml"), "title: Card\nreleases:\n  USA:\n  Europe:\n")
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	var detail gameResponse
	requestJSON(t, h, "/api/game?platform=Test&game=Card", &detail)
	var list gamesResponse
	requestJSON(t, h, "/api/games?platform=Test&game=Card", &list)
	if detail.Game.DefaultRelease != "" || list.Games[0].DefaultRelease != "" {
		t.Fatal("invented stored default", detail.Game, list)
	}
	response := postPreview(t, h, exportPreviewRequest{Platform: "Test", Settings: exportSettings{Language: "en", Media: "none"}, Games: []previewGameRequest{{Folder: "Card"}}})
	if response.Games[0].Preview.Release != "" {
		t.Fatal(response)
	}
}

func TestExportSourceOptionsAndExplicitEmptySources(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "config", "sources.yaml"), "priority:\n  metadata: [screenscraper, launchbox]\n  artwork: [launchbox]\n  video: [emumovies]\n")
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	var options exportSourceOptions
	requestJSON(t, h, "/api/export-options", &options)
	if len(options.Metadata) != 2 || options.Metadata[0] != "screenscraper" || len(options.Artwork) != 1 || options.Video[0] != "emumovies" {
		t.Fatal(options)
	}
	body := `{"settings":{"language":"en","media":"all","metadata_sources":[],"artwork_sources":[],"video_sources":[]}}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response exportPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	p, err := exporter.ExportProfile(root, response.ProfileYAML)
	if err != nil {
		t.Fatal(err)
	}
	if p.Selection.Description.Sources == nil || len(p.Selection.Description.Sources) != 0 || len(p.Selection.Fields["developer"].Sources) != 0 || len(p.Selection.Media["cover"].Sources) != 0 || len(p.Selection.Media["video"].Sources) != 0 {
		t.Fatal("empty source lists reverted to defaults", p)
	}
}

func TestExportRegistryDiscoveryCustomMediaAndRaw(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "source-policies.yaml"), "schema_version: 0\nsources:\n  gamelist.synthetic: {redistribution: private}\n  launchbox: {media: {scope: personal}}\n")
	card := filepath.Join(root, "platforms", "Test", "Card")
	writeTestFile(t, filepath.Join(card, "game.yaml"), "title: Example & safe\nsources:\n  gamelist.synthetic:\n    matches:\n      - descriptions: {en: Selected imported prose}\n        media:\n          cart_front:\n            - {url: media/cart-front.world.gamelist.synthetic.png}\n")
	writeTestFile(t, filepath.Join(card, "media", "cart-front.world.gamelist.synthetic.png"), "synthetic")
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	var options exportSourceOptions
	requestJSON(t, h, "/api/export-options", &options)
	if len(options.Available) != 2 || options.Available[0] != "gamelist.synthetic" || len(options.MediaRoles["launchbox"]) != 16 || strings.Contains(strings.Join(options.Metadata, ","), "gamelist.synthetic") {
		t.Fatal(options)
	}
	settings := exportSettings{Target: "launchbox", Language: "en", Media: "custom", MediaRoles: []string{"cart-front"}, MetadataSources: []string{"gamelist.synthetic"}, ArtworkSources: []string{"gamelist.synthetic"}}
	input := exportPreviewRequest{Platform: "Test", Settings: settings, Games: []previewGameRequest{{Folder: "Card"}}, IncludeRaw: true}
	response := postPreview(t, h, input)
	game := response.Games[0]
	if game.Error != "" || game.Raw == nil || game.Preview.Fields["description"].Value != "Selected imported prose" || !strings.Contains(game.Raw.XML, "Selected imported prose") || !strings.Contains(game.Raw.MediaPaths["cart-front"], "/Cart - Front/") {
		t.Fatalf("error=%s preview=%+v raw=%+v", game.Error, game.Preview, game.Raw)
	}
	p, err := exporter.ExportProfile(root, response.ProfileYAML)
	if err != nil || len(p.Selection.Media) != 1 {
		t.Fatal(p, err)
	}
	input.IncludeRaw = false
	response = postPreview(t, h, input)
	if response.Games[0].Raw != nil {
		t.Fatal("list preview includes Raw")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", strings.NewReader(`{"settings":{"target":"launchbox","language":"en","media":"custom","media_roles":[]}}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	p, err = exporter.ExportProfile(root, response.ProfileYAML)
	if err != nil || len(p.Selection.Media) != 0 {
		t.Fatal("explicit empty media roles restored defaults", p, err)
	}

	for _, bad := range []string{
		`{"settings":{"target":"es-de","language":"en","media":"custom","media_roles":["disc"]}}`,
		`{"settings":{"target":"launchbox","language":"en","media":"custom","media_roles":["unknown"]}}`,
		`{"settings":{"target":"launchbox","language":"en","media":"custom","media_roles":["disc","disc"]}}`,
		`{"settings":{"language":"en","media":"custom"}}`,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", strings.NewReader(bad)))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
