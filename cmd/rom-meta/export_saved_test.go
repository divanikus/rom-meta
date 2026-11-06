package main

import (
	"flag"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"rommeta/internal/exporter"
	"rommeta/internal/exportsettings"
	"strings"
	"testing"
)

func savedSettingsFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root, roms := exportFixture(t)
	dest := exportDestination(t, "es-de")
	profiles := exportsettings.ProfilesDocument{SchemaVersion: 1, Profiles: []exportsettings.SavedProfile{{ID: "profile-1", Name: "Original artwork", Target: "es-de", ProfileYAML: "schema_version: 1\ntitles: {mode: release}\n"}}}
	data, _ := yaml.Marshal(profiles)
	exportPut(t, filepath.Join(root, "config", "export-profiles.yaml"), string(data))
	destinations := exportsettings.DestinationsDocument{SchemaVersion: 1, Destinations: []exportsettings.Destination{{ID: "dest-1", Name: "Living room", Target: "es-de", Path: dest, ROMRoot: roms, Platform: "n64", ProfileID: "profile-1"}}}
	data, _ = yaml.Marshal(destinations)
	exportPut(t, filepath.Join(root, "config", "export-destinations.yaml"), string(data))
	return root, roms, dest
}
func TestSavedExportChoicesCLIPlanAndOverrides(t *testing.T) {
	root, roms, dest := savedSettingsFixture(t)
	report := filepath.Join(t.TempDir(), "plan.json")
	if err := runExport([]string{"--data-root", root, "--destination-name", "living ROOM", "--report", report}); err != nil {
		t.Fatal(err)
	}
	plan := reportPlan(t, report)
	if plan.Root != dest || len(plan.Files) == 0 {
		t.Fatal(plan)
	}
	if len(exportTree(t, dest)) != 0 {
		t.Fatal("preview wrote destination")
	}
	if err := runExport([]string{"--data-root", root, "--list-profiles"}); err != nil {
		t.Fatal(err)
	}
	if err := runExport([]string{"--data-root", root, "--list-destinations"}); err != nil {
		t.Fatal(err)
	}
	override := exportDestination(t, "es-de")
	overrideReport := filepath.Join(t.TempDir(), "override.json")
	if err := runExport([]string{"--data-root", root, "--destination-name", "dest-1", "--profile-name", "profile-1", "--destination", override, "--rom-root", roms, "--platform", "n64", "--report", overrideReport}); err != nil {
		t.Fatal(err)
	}
	if reportPlan(t, overrideReport).Root != override {
		t.Fatal("explicit destination ignored")
	}
	// Standalone named profiles work without a saved destination.
	if err := runExport([]string{"--data-root", root, "--profile-name", "Original artwork", "--destination", override, "--rom-root", roms, "--platform", "n64"}); err != nil {
		t.Fatal(err)
	}
}
func TestSavedExportChoiceValidation(t *testing.T) {
	root, _, _ := savedSettingsFixture(t)
	for _, args := range [][]string{
		{"--list-profiles", "--apply"}, {"--list-profiles", "--list-destinations"},
		{"--destination-name", "missing"}, {"--profile-name", "missing"},
		{"--profile-name", "profile-1", "--profile", "anything.yaml"},
		{"--api", "http://localhost:8787", "--profile-name", "profile-1"},
		{"--destination-name", "dest-1", "--package", "somewhere"},
	} {
		if err := runExport(append([]string{"--data-root", root}, args...)); err == nil {
			t.Fatal("accepted", args)
		}
	}
	path := filepath.Join(root, "config", "export-profiles.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nprofiles: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runExport([]string{"--data-root", root, "--destination-name", "dest-1"}); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatal(err)
	}
	// Explicit YAML overrides the missing linked profile, and explicit selection replaces saved scope.
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.String("profile", "", "")
	flags.String("platforms", "", "")
	flags.String("media-root", "", "")
	flags.Parse([]string{"--profile", "explicit.yaml", "--platforms", "n64", "--media-root", ""})
	o := exporter.Options{DataRoot: root, ProfilePath: "explicit.yaml"}
	target, destination, media := "es-de", "", ""
	all := false
	if err := applySavedExportSettings(flags, &o, &target, &destination, &media, &all, "dest-1", ""); err != nil {
		t.Fatal(err)
	}
	if o.ProfileContent != "" || o.Platform != "" || all || o.ProfilePath != "explicit.yaml" {
		t.Fatal("explicit override lost", o)
	}
}
