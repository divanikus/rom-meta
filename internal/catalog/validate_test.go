package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateGeneratedCatalogFixture(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), `schema_version: 0
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), `name: "Nintendo - Nintendo 64"
authority: no-intro
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"
default_release: "Star Fox 64 (USA)"
releases:
  "Star Fox 64 (USA)":
    region: USA
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "fingerprints.yaml"), `roms:
  - release: "Star Fox 64 (USA)"
    crc32: b1fcaa9c
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "Star Fox 64 (USA)", "box-front.openvgdb.jpg"), "\n")
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "Star Fox 64 (USA)", "cart-top.launchbox.png"), "\n")
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front-02.europe.launchbox.jpg"), "\n")
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "gameplay-video.usa.emumovies.mp4"), "\n")
	writeFile(t, filepath.Join(root, "source-policies.yaml"), `sources:
  launchbox:
    media:
      redistributable: false
  openvgdb:
    media:
      redistributable: false
  emumovies:
    media:
      redistributable: false
`)

	issues, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got %#v", issues)
	}
}

func TestMediaSourceNameUsesFinalSourceSuffixAfterAttributes(t *testing.T) {
	tests := map[string]string{
		"box-front-02.europe.launchbox.jpg":     "launchbox",
		"gameplay-video.usa.emumovies.mp4":      "emumovies",
		"box-front.north-america.launchbox.png": "launchbox",
	}
	for name, want := range tests {
		if got := mediaSourceName(name); got != want {
			t.Errorf("mediaSourceName(%q) = %q, want %q", name, got, want)
		}
		if !isAllowedMediaName(name) {
			t.Errorf("isAllowedMediaName(%q) = false", name)
		}
	}
}

func TestValidateReportsMalformedGameYAML(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "platforms", "Sega - 32X", "_catalog.yaml"), "schema_version: 0\n")
	writeFile(t, filepath.Join(root, "platforms", "Sega - 32X", "_platform.yaml"), "name: Sega - 32X\n")
	writeFile(t, filepath.Join(root, "platforms", "Sega - 32X", "Doom", "game.yaml"), "title: [broken\n")

	issues, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Path != "platforms/Sega - 32X/Doom/game.yaml" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateReportsUnknownSourceReleaseReferences(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "platforms", "Sega - 32X", "_catalog.yaml"), "schema_version: 0\n")
	writeFile(t, filepath.Join(root, "platforms", "Sega - 32X", "_platform.yaml"), "name: Sega - 32X\n")
	writeFile(t, filepath.Join(root, "platforms", "Sega - 32X", "Doom", "game.yaml"), `title: Doom
default_release: Doom (USA)
releases:
  Doom (USA): {}
sources:
  launchbox:
    matches:
      - release: Doom (Europe)
  screenscraper:
    matches:
      - releases:
          - release: Doom (Japan)
`)

	issues, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || !strings.Contains(issues[0].Message, "unknown release") || !strings.Contains(issues[1].Message, "unknown release") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateAcceptsCurrentFetcherSourceShapes(t *testing.T) {
	root := t.TempDir()
	platform := filepath.Join(root, "platforms", "Sega - 32X")
	writeFile(t, filepath.Join(platform, "_catalog.yaml"), "schema_version: 0\n")
	writeFile(t, filepath.Join(platform, "_platform.yaml"), "name: Sega - 32X\n")
	writeFile(t, filepath.Join(platform, "Doom", "game.yaml"), `title: Doom
default_release: Doom (USA)
releases:
  Doom (USA): {}
  Doom (Europe): {}
sources:
  hasheous:
    matches:
      - release: Doom (USA)
        hasheous_id: "1"
  launchbox:
    matches:
      - releases: [Doom (USA), Doom (Europe)]
        database_id: "2"
  openvgdb:
    matches:
      - release: Doom (USA)
        release_id: "3"
  igdb:
    matches:
      - release: Doom (USA)
        igdb_id: "4"
  screenscraper:
    matches:
      - release: Doom (USA)
        screenscraper_id: "5"
  emumovies:
    matches:
      - releases:
          - release: Doom (USA)
            media:
              gameplay_video:
                - url: https://example.test/doom-us.mp4
          - release: Doom (Europe)
            media:
              gameplay_video:
                - url: https://example.test/doom-eu.mp4
`)

	issues, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRequiresRootSourcePolicyForSourceMedia(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), `schema_version: 0
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), `name: "Nintendo - Nintendo 64"
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front.launchbox.jpg"), "\n")

	issues, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %#v", issues)
	}
	if issues[0].Path != "source-policies.yaml" {
		t.Fatalf("expected source-policies.yaml issue, got %#v", issues)
	}
}

func TestValidateReportsProgress(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), `schema_version: 0
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), `name: "Nintendo - Nintendo 64"
`)
	writeFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
`)

	var snapshots []ValidateProgress
	issues, err := ValidateWithOptions(root, ValidateOptions{
		Progress: func(snapshot ValidateProgress) {
			snapshots = append(snapshots, snapshot)
		},
	})
	if err != nil {
		t.Fatalf("ValidateWithOptions returned error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got %#v", issues)
	}
	if len(snapshots) == 0 {
		t.Fatalf("expected progress snapshots")
	}
	last := snapshots[len(snapshots)-1]
	if last.Stage != "validating" || last.Current != 1 || last.Total != 1 || last.Game != "Star Fox 64" {
		t.Fatalf("unexpected final progress snapshot: %#v", last)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func TestValidateFoldMediaRetainsNormalChecks(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{"_fold/Japan/box-front.launchbox.jpg", ""},
		{"_fold/Japan~2/Game (Japan)/box-front.launchbox.jpg", ""},
		{"_fold/Previous/_fold/Japan/logo.launchbox.png", ""},
		{"_fold/image.jpg", "must contain original-card folders"},
		{"_fold/Japan/notes.txt", "unknown game-wide media filename"},
		{"_fold/Japan/Unknown Release/box-front.launchbox.jpg", "does not match a release key"},
		{"_fold/Japan/Game (Japan)/extra/box-front.launchbox.jpg", "nested media directories"},
		{"Unknown Release/box-front.launchbox.jpg", "does not match a release key"},
		{"_fold/Japan/box-front.unconfigured.jpg", "missing source policy"},
	} {
		t.Run(test.path, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "source-policies.yaml"), "sources:\n  launchbox:\n    media:\n      redistributable: false\n")
			media := filepath.Join(root, "media")
			writeFile(t, filepath.Join(media, filepath.FromSlash(test.path)), "variant")
			var issues []Issue
			validateMedia(root, media, map[string]bool{"Game (Japan)": true}, loadSourcePolicies(root), &issues)
			if test.want == "" {
				if len(issues) != 0 {
					t.Fatalf("valid folded media rejected: %#v", issues)
				}
			} else {
				for _, issue := range issues {
					if strings.Contains(issue.Message, test.want) {
						return
					}
				}
				t.Fatalf("expected %q, got %#v", test.want, issues)
			}
		})
	}
}
