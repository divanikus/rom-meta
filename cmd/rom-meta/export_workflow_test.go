package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
)

func TestExportListingRejectsIgnoredOptions(t *testing.T) {
	for _, extra := range [][]string{
		{"--apply"}, {"--destination", "unused"}, {"--out", "unused"},
		{"--package", "unused"}, {"--platform", "n64"}, {"--platforms", "n64"},
		{"--all"}, {"--match", "Example"}, {"--limit", "1"},
		{"--media-root", "unused"}, {"--report", "unused.json"},
		{"--profile", "unused.yaml"}, {"--arcade-snapshot", "unused"},
	} {
		t.Run(extra[0], func(t *testing.T) {
			args := append([]string{"export", "--list-platforms", "--data-root", t.TempDir()}, extra...)
			code, output := commandOutput(t, args...)
			if code != 2 || !strings.Contains(output, "--list-platforms excludes") || !strings.Contains(output, extra[0]) {
				t.Fatalf("%v: exit %d: %s", args, code, output[:min(len(output), 800)])
			}
		})
	}
}

func TestExportSelectionValidationPrecedesSourceReads(t *testing.T) {
	for _, source := range [][]string{{"--data-root", "missing-catalog"}, {"--api", "invalid-origin"}} {
		for _, mode := range []string{"--out", "--destination"} {
			for _, test := range []struct {
				args []string
				want string
			}{
				{[]string{"--platform", "n64"}, "--rom-root is required"},
				{[]string{"--platform", "n64", "--rom-root", "unused", "--limit", "-1"}, "--limit must be non-negative"},
				{[]string{"--platforms", "n64,", "--rom-root", "unused"}, "empty system in --platforms"},
				{[]string{"--platform", "n64", "--rom-root", "unused", "--target", "launchbox", "--media-root", "unused"}, "--media-root"},
			} {
				args := append([]string{"export", mode, "unused"}, source...)
				args = append(args, test.args...)
				code, output := commandOutput(t, args...)
				if code != 2 || !strings.Contains(output, test.want) {
					t.Fatalf("%v: exit %d: %s; want %s", args, code, output, test.want)
				}
			}
		}
	}
}

func TestExportTargetNormalizationAcrossModes(t *testing.T) {
	for _, test := range []struct{ input, target string }{{"", "es-de"}, {" ES-DE ", "es-de"}, {"LAUNCHBOX", "launchbox"}} {
		for _, mode := range []string{"--destination", "--out"} {
			t.Run(test.target+mode+test.input, func(t *testing.T) {
				root, roms := exportFixture(t)
				output := exportDestination(t, test.target)
				if mode == "--out" {
					output = filepath.Join(t.TempDir(), "package")
				}
				args := []string{"export", "--target", test.input, "--platform", "n64", "--rom-root", roms, "--data-root", root, mode, output}
				code, text := commandOutput(t, args...)
				if code != 0 || !strings.Contains(text, "Preview only") {
					t.Fatalf("%v: exit %d: %s", args, code, text)
				}
				if mode == "--destination" && !strings.Contains(text, "Target: "+test.target) {
					t.Fatal(text)
				}
			})
		}
	}
}

func TestExportEmptyBatchHasConsistentOutcome(t *testing.T) {
	root, _ := exportFixture(t)
	api, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()
	for _, source := range [][]string{{"--data-root", root}, {"--api", server.URL}} {
		for _, mode := range []string{"--destination", "--out"} {
			for _, apply := range []bool{false, true} {
				roms := t.TempDir()
				output := filepath.Join(t.TempDir(), "package")
				if mode == "--destination" {
					output = exportDestination(t, "es-de")
				}
				args := append([]string{"export", "--all", "--rom-root", roms, mode, output}, source...)
				if apply {
					args = append(args, "--apply")
				}
				code, text := commandOutput(t, args...)
				if code != 2 || !strings.Contains(text, "no systems with selected inputs") || strings.Contains(text, "Repeat with --apply") {
					t.Fatalf("%v: exit %d: %s", args, code, text)
				}
				entries, err := os.ReadDir(output)
				if mode == "--out" {
					if !os.IsNotExist(err) {
						t.Fatal("empty batch created output", err)
					}
				} else if err != nil || len(entries) != 0 {
					t.Fatal(entries, err)
				}
			}
		}
	}
}

func TestExportSourceSetReadinessVisibleInPreview(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		root, roms := exportFixture(t)
		exportPut(t, filepath.Join(roms, "Example (USA).pkg"), "synthetic source package")
		code, output := commandOutput(t, "export", "--target", target, "--platform", "n64", "--rom-root", roms, "--data-root", root, "--destination", exportDestination(t, target))
		if code != 0 || !strings.Contains(output, "Source sets requiring extraction/installation and launch setup: 1") || strings.Contains(output, "playable versions") {
			t.Fatalf("exit %d: %s", code, output)
		}
	}
}
