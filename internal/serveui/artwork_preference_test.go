package serveui

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/exporter"
)

func TestArtworkPreferenceExplainsAndSavesActualSelection(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo 64"
	card := filepath.Join(root, "platforms", platform, "Card")
	writeTestFile(t, filepath.Join(root, "platforms", platform, "_platform.yaml"), "name: Nintendo - Nintendo 64\nrom_layout: BigEndian\n")
	payload := "synthetic cartridge fixture"
	romRoot := t.TempDir()
	writeTestFile(t, filepath.Join(romRoot, "Example (USA).z64"), payload)
	writeTestFile(t, filepath.Join(card, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    source: {entry_name: Example (USA)}\n    file: {name: Example (USA).z64, size: %d, sha1: %x}\n", len(payload), sha1.Sum([]byte(payload))))
	writeTestFile(t, filepath.Join(card, "game.yaml"), "title: Example\ndefault_release: Example (USA)\nreleases:\n  Example (USA): {regions: [USA]}\n")
	for _, name := range []string{"box-front-01.usa.variant-original.launchbox.jpg", "box-front-04.usa.variant-reconstructed.launchbox.png", "box-front-00.japan.variant-original.launchbox.png", "box-3d-01.japan.launchbox.png", "box-3d-02.usa.launchbox.png", "logo.usa.launchbox.png"} {
		writeTestFile(t, filepath.Join(card, "media", name), "synthetic artwork")
	}
	h, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"es-de", "launchbox"} {
		for _, tc := range []struct{ preference, file, why string }{
			{"auto", "box-front-04.usa.variant-reconstructed.launchbox.png", "PNG is preferred to JPG"},
			{"original", "box-front-01.usa.variant-original.launchbox.jpg", "Original artwork is preferred to reconstructed artwork"},
			{"reconstructed", "box-front-04.usa.variant-reconstructed.launchbox.png", "Reconstructed artwork is preferred to original artwork"},
		} {
			t.Run(target+"/"+tc.preference, func(t *testing.T) {
				settings := exportSettings{Target: target, Language: "en", Media: "expanded", ArtworkPreference: tc.preference}
				response := postPreview(t, h, exportPreviewRequest{Platform: platform, Settings: settings, Games: []previewGameRequest{{Folder: "Card"}}, IncludeRaw: true})
				got := response.Games[0]
				if got.Error != "" || got.Preview == nil || got.Raw == nil {
					t.Fatal(got.Error)
				}
				if filepath.Base(got.Preview.Media["cover"].SourcePath) != tc.file {
					t.Fatal(got.Preview.Media)
				}
				reason := got.Preview.MediaReasons["cover"]
				if !strings.Contains(reason.Summary, tc.why) || reason.ComparedWith == "" || !strings.Contains(strings.Join(reason.Details, " "), "Region matches the selected release: USA") {
					t.Fatal(reason)
				}
				if !strings.HasSuffix(got.Preview.Media["box-3d"].SourcePath, "box-3d-02.usa.launchbox.png") {
					t.Fatal("artwork preference defeated release region")
				}
				if got.Preview.MediaReasons["box-3d"].Summary != "No other usable candidate under this profile." {
					t.Fatal(got.Preview.MediaReasons)
				}
				p, err := exporter.ExportProfile(root, response.ProfileYAML)
				if err != nil {
					t.Fatal(err)
				}
				if tc.preference == "auto" && p.Selection.Media["cover"].Variants != nil {
					t.Fatal("Auto changed existing ranking")
				}
				if tc.preference != "auto" && (p.Selection.Media["cover"].Order[1] != "variant" || p.Selection.Media["cover"].Variants[0] != tc.preference || p.Selection.Media["video"].Variants != nil || p.Selection.Media["manual"].Variants != nil) {
					t.Fatal(p.Selection.Media)
				}
				profilePath := filepath.Join(t.TempDir(), "profile.yaml")
				writeTestFile(t, profilePath, response.ProfileYAML)
				native, err := exporter.Build(exporter.Options{DataRoot: root, ROMRoot: romRoot, Platform: "n64", Out: filepath.Join(t.TempDir(), "out"), ProfilePath: profilePath, Target: target})
				if err != nil {
					t.Fatal(err)
				}
				if native.Matched != 1 || native.Records[0].Media["cover"].SourcePath != got.Preview.Media["cover"].SourcePath {
					t.Fatal("saved profile disagrees with native export", native.Records)
				}
				wire, err := json.Marshal(native)
				if err != nil || strings.Contains(string(wire), "media_reasons") {
					t.Fatal("presentation reasons changed native wire", err)
				}

				encoded, err := json.Marshal(got.Preview)
				if err != nil || !strings.Contains(string(encoded), "media_reasons") {
					t.Fatal(err, string(encoded))
				}
			})
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/export-preview", strings.NewReader(`{"settings":{"language":"en","media":"none","artwork_preference":"bad"}}`)))
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
