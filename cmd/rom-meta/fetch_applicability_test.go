package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/platformnames"
	"rommeta/internal/store"
)

func TestAutomaticFetchScummVMSelectionAndNoWrites(t *testing.T) {
	root := t.TempDir()
	if _, err := store.Init(root, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "platforms", "ScummVM", "Monkey", "game.yaml")
	original := "title: Monkey\ndefault_release: Monkey\nreleases:\n  Monkey:\n"
	writeFetchFixture(t, path, original)
	config, err := store.LoadSources(root)
	if err != nil {
		t.Fatal(err)
	}
	config.Fetch = map[string][]string{
		"metadata": {"launchbox", "openvgdb", "hasheous", "igdb", "screenscraper"},
		"artwork":  {"launchbox", "igdb", "screenscraper", "emumovies"},
		"video":    {"emumovies"},
	}
	registry := platformnames.DefaultRegistry()
	options := fetchOptions{DataRoot: root, Platform: "scummvm", PlatformRegistry: &registry, Apply: true, ForceOverwrite: true}
	jobs, err := selectFetchJobs(buildFetchJobs(options, config), options, config)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	var ran []string
	err = runFetchJobs(&out, jobs, options, config, func(job fetchJob, scoped fetchOptions, _ store.SourcesConfig) error {
		if scoped.Platform != "ScummVM" || !scoped.Apply || !scoped.ForceOverwrite {
			t.Fatalf("lost scope/options: %#v", scoped)
		}
		ran = append(ran, job.Source+"/"+job.Type)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"launchbox/metadata", "screenscraper/metadata", "launchbox/artwork", "screenscraper/artwork", "emumovies/artwork", "emumovies/video"}
	if !reflect.DeepEqual(ran, want) {
		t.Fatalf("ran %v, want %v; output:\n%s", ran, want, out.String())
	}
	for _, reason := range []string{"no igdb platform mapping", "no openvgdb platform mapping", "no pending games with usable ROM hashes", "Result: skipped;"} {
		if !strings.Contains(out.String(), reason) {
			t.Fatalf("missing reason %q:\n%s", reason, out.String())
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != original {
		t.Fatalf("skipped fetch changed card: %s; %v", got, err)
	}
}

func TestAutomaticFetchMixedPlatformsAndLocalMapping(t *testing.T) {
	root := t.TempDir()
	for _, platform := range []string{"ScummVM", "Nintendo - Nintendo 64", "Custom Platform"} {
		writeFetchFixture(t, filepath.Join(root, "platforms", platform, "Game", "game.yaml"), "title: Game\n")
	}
	registry := platformnames.DefaultRegistry()
	local, err := platformnames.ParseRegistryYAML("\"Custom Platform\":\n  sources:\n    igdb: Custom System\n")
	if err != nil {
		t.Fatal(err)
	}
	registry.Merge(local)
	registry.MergeAliases(map[string]string{"my-scumm": "ScummVM"})
	config := store.SourcesConfig{
		Sources: []store.Source{{Name: "igdb", Enabled: true}, {Name: "launchbox", Enabled: true}},
		Fetch:   map[string][]string{"metadata": {"igdb", "launchbox"}},
	}
	options := fetchOptions{DataRoot: root, PlatformRegistry: &registry}
	jobs, err := selectFetchJobs(buildFetchJobs(options, config), options, config)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	var ran []string
	if err := runFetchJobs(&out, jobs, options, config, func(job fetchJob, scoped fetchOptions, _ store.SourcesConfig) error {
		ran = append(ran, job.Source+":"+scoped.Platform)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"igdb:Custom Platform", "igdb:Nintendo - Nintendo 64", "launchbox:Nintendo - Nintendo 64", "launchbox:ScummVM"}
	if !reflect.DeepEqual(ran, want) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	options.Platform = "my-scumm"
	platforms, err := automaticFetchPlatforms(options)
	if err != nil || !reflect.DeepEqual(platforms, []string{"ScummVM"}) {
		t.Fatalf("local alias: %v, %v", platforms, err)
	}
}

func TestAutomaticFetchPlatformDirectories(t *testing.T) {
	root := t.TempDir()
	registry, err := platformnames.ParseRegistryYAML("Canonical:\n  aliases: [First, Second, short]\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"First", "Second", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, "platforms", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ input, want, errorText string }{
		{input: "first", want: "First"},
		{input: "short", errorText: "multiple directories"},
		{input: "absent", errorText: "no catalog platform directory"},
		{input: "", want: "First,Second"},
	} {
		got, err := automaticFetchPlatforms(fetchOptions{DataRoot: root, Platform: tc.input, PlatformRegistry: &registry})
		if tc.errorText != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("%q: %v, %v", tc.input, got, err)
			}
		} else if err != nil || strings.Join(got, ",") != tc.want {
			t.Fatalf("%q: %v, %v; want %s", tc.input, got, err, tc.want)
		}
	}
}

func TestAutomaticFetchAllSkippedAndExplicitOverride(t *testing.T) {
	root := t.TempDir()
	writeFetchFixture(t, filepath.Join(root, "platforms", "Unknown", "Game", "game.yaml"), "title: Game\n")
	config := store.SourcesConfig{Sources: []store.Source{{Name: "igdb", Enabled: true}, {Name: "launchbox"}}, Fetch: map[string][]string{"metadata": {"igdb", "launchbox", "openvgdb"}}}
	options := fetchOptions{DataRoot: root}
	jobs, err := selectFetchJobs(buildFetchJobs(options, config), options, config)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runFetchJobs(&out, jobs, options, config, func(fetchJob, fetchOptions, store.SourcesConfig) error {
		t.Fatal("all automatic sources should be skipped")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"no igdb platform mapping", "no launchbox platform mapping", "source is not configured"} {
		if !strings.Contains(out.String(), reason) {
			t.Fatalf("missing %q: %s", reason, out.String())
		}
	}
	options.Sources = []string{"igdb"}
	explicit := buildFetchJobs(options, config)
	got, err := selectFetchJobs(explicit, options, config)
	if err != nil || !reflect.DeepEqual(got, explicit) || got[0].Automatic || got[0].SkipReason != "" {
		t.Fatalf("explicit source selection changed: %v, %v", got, err)
	}
}

func TestAutomaticFetchEvidenceProbeFindsPendingHashes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "platforms", "ScummVM", "Has Hash", "game.yaml")
	writeFetchFixture(t, path, "title: Game\n")
	writeFetchFixture(t, filepath.Join(filepath.Dir(path), "fingerprints.yaml"), "items:\n  - file:\n      name: Game.rom\n      md5: 11111111111111111111111111111111\n")
	options := fetchOptions{DataRoot: root, Platform: "ScummVM"}
	job := fetchJob{Source: "hasheous", Type: "metadata", Automatic: true}
	var out bytes.Buffer
	if reason, err := automaticFetchEvidenceReason(&out, job, options); err != nil || reason != "" {
		t.Fatalf("usable hash rejected: %s, %v", reason, err)
	}
	writeFetchFixture(t, path, "title: Game\nsources:\n  hasheous:\n    status: not_found\n")
	if reason, err := automaticFetchEvidenceReason(&out, job, options); err != nil || reason == "" {
		t.Fatalf("existing source not skipped: %s, %v", reason, err)
	}
	options.ForceOverwrite = true
	if reason, err := automaticFetchEvidenceReason(&out, job, options); err != nil || reason != "" {
		t.Fatalf("force ignored: %s, %v", reason, err)
	}
}

func TestFetchApplyWithOnlyInapplicableDefaults(t *testing.T) {
	root := t.TempDir()
	if _, err := store.Init(root, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	writeFetchFixture(t, filepath.Join(root, "config", "sources.yaml"), "sources:\n  igdb:\n    enabled: true\n  openvgdb:\n    enabled: true\n  hasheous:\n    enabled: true\nfetch:\n  metadata:\n    - igdb\n    - openvgdb\n    - hasheous\n")
	path := filepath.Join(root, "platforms", "ScummVM", "Game", "game.yaml")
	original := "title: Game\n"
	writeFetchFixture(t, path, original)
	if err := runFetch([]string{"--data-root", root, "--platform", "scummvm", "--type", "metadata", "--apply"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != original {
		t.Fatalf("skipped defaults changed card: %s; %v", got, err)
	}
}

func writeFetchFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticFetchRetainsKnownConsoleAndArcadeMappings(t *testing.T) {
	registry := platformnames.DefaultRegistry()
	for _, platform := range []string{"Arcade", "snes", "n64"} {
		for _, source := range []string{"launchbox", "openvgdb", "igdb", "screenscraper", "emumovies"} {
			config := store.SourcesConfig{Sources: []store.Source{{Name: source}}}
			job := fetchJob{Source: source, Type: "metadata"}
			if reason := automaticFetchMappingReason(job, platform, registry, config); reason != "" {
				t.Errorf("%s/%s unexpectedly skipped: %s", platform, source, reason)
			}
		}
	}
}

func TestAutomaticFetchEmptyLibrary(t *testing.T) {
	root := t.TempDir()
	if _, err := store.Init(root, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runFetch([]string{"--data-root", root, "--apply"}); err != nil {
		t.Fatalf("empty library should finish without provider calls: %v", err)
	}
}
