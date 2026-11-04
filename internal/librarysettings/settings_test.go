package librarysettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/exporter"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
	"runtime"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := store.Init(root, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	return root
}
func section(t *testing.T, root, id string) Section {
	t.Helper()
	sections, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sections {
		if s.ID == id {
			return s
		}
	}
	t.Fatal(id)
	return Section{}
}
func save(t *testing.T, root, id string, values map[string]any) {
	t.Helper()
	s := section(t, root, id)
	raw := map[string]json.RawMessage{}
	for k, v := range values {
		data, _ := json.Marshal(v)
		raw[k] = data
	}
	if _, err := Save(root, Request{Section: id, Revision: s.Revision, Values: raw}); err != nil {
		t.Fatal(err)
	}
}
func TestSettingsRoundtripUsesCLIPolicyAndPreservesPrivateData(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, "config", "sources.yaml")
	original, _ := os.ReadFile(path)
	original = append(original, []byte("\n# owner note\ncustom_owner: {unchanged: true}\n")...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	secret := []byte("sources:\n  igdb:\n    client_id: PRIVATE_ID\n    client_secret: PRIVATE_SECRET\n")
	if err := os.WriteFile(filepath.Join(root, "config", "secrets.yaml"), secret, 0600); err != nil {
		t.Fatal(err)
	}
	beforeSources, _ := store.LoadSources(root)
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(loaded)
	if strings.Contains(string(wire), "PRIVATE_") || strings.Contains(string(wire), "client_secret") {
		t.Fatal("private credential payload in settings")
	}
	if len(loaded) < 10 {
		t.Fatal("missing configuration sections", len(loaded))
	}
	stale := section(t, root, "priorities")
	save(t, root, "priorities", map[string]any{"metadata": []string{"igdb", "gamelist.synthetic"}, "video": []string{}})
	profile, err := exporter.ExportProfile(root, "schema_version: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.Selection.Description.Sources, []string{"igdb", "gamelist.synthetic"}) || len(profile.Selection.Media["video"].Sources) != 0 {
		t.Fatal("export did not use saved priorities")
	}
	current, _ := os.ReadFile(path)
	if !strings.Contains(string(current), "# owner note") || !strings.Contains(string(current), "custom_owner") {
		t.Fatal("unknown fields/comments lost")
	}
	afterSources, _ := store.LoadSources(root)
	if !reflect.DeepEqual(beforeSources.Sources, afterSources.Sources) {
		t.Fatal("provider configuration changed")
	}
	actualSecret, _ := os.ReadFile(filepath.Join(root, "config", "secrets.yaml"))
	if string(actualSecret) != string(secret) {
		t.Fatal("secrets changed")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("mode changed")
		}
	}
	if _, err := Save(root, Request{Section: "priorities", Revision: stale.Revision, Values: map[string]json.RawMessage{"metadata": json.RawMessage("[]")}}); err == nil {
		t.Fatal("stale write allowed")
	}
}
func TestSettingsAcquisitionFoldAndAliasesRoundtrip(t *testing.T) {
	root := fixture(t)
	save(t, root, "fetch", map[string]any{"metadata": []string{"igdb", "launchbox"}, "concurrency": 3})
	save(t, root, "source:igdb", map[string]any{"rate": 2, "concurrency": 1})
	config, err := store.LoadSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if config.FetchConcurrency != 3 || !reflect.DeepEqual(config.Fetch["metadata"], []string{"igdb", "launchbox"}) {
		t.Fatal(config.Fetch)
	}
	for _, s := range config.Sources {
		if s.Name == "igdb" && (s.RateLimitPerSecond != 2 || s.FetchConcurrency != 1) {
			t.Fatal(s)
		}
	}
	save(t, root, "fold", map[string]any{"regions": []string{"japan", "usa"}, "groups": "usa: [USA, US]\njapan: [Japan, JP]\n", "platforms": "segacd: {region_priority: [usa, japan]}\n"})
	fold, err := store.LoadCatalogConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fold.Fold.RegionPriority, []string{"japan", "usa"}) || !reflect.DeepEqual(fold.Fold.Platforms["segacd"].RegionPriority, []string{"usa", "japan"}) {
		t.Fatal(fold.Fold)
	}
	save(t, root, "platforms", map[string]any{"document": "Synthetic Console:\n  aliases: [synthetic]\n  sources:\n    screenscraper:\n      id: 999\n      name: Synthetic system\n"})
	registry, _, err := platformnames.LoadRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if registry.ResolveCLI("synthetic") != "Synthetic Console" {
		t.Fatal("alias lost")
	}
	if id, ok := registry.SourceID("synthetic", "screenscraper"); !ok || id != "999" {
		t.Fatal(id, ok)
	}
	save(t, root, "legacy-aliases", map[string]any{"aliases": "old-synthetic: Synthetic Console\n"})
	config, err = store.LoadSources(root)
	if err != nil || config.PlatformAliases["old-synthetic"] != "Synthetic Console" {
		t.Fatal(err, config.PlatformAliases)
	}
	save(t, root, "platforms", map[string]any{"document": "{}"})
	_ = section(t, root, "platforms")
}
func TestInvalidSettingsAndSymlinkDoNotWrite(t *testing.T) {
	root := fixture(t)
	s := section(t, root, "fetch")
	path := filepath.Join(root, "config", "sources.yaml")
	before, _ := os.ReadFile(path)
	for _, values := range []map[string]json.RawMessage{
		{"concurrency": json.RawMessage("-1")}, {"concurrency": json.RawMessage("1.5")}, {"credentials": json.RawMessage("{}")}, {"metadata": json.RawMessage("[\"igdb\",\"igdb\"]")},
	} {
		if _, err := Save(root, Request{Section: s.ID, Revision: s.Revision, Values: values}); err == nil {
			t.Fatal("invalid value accepted", values)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid save changed file")
	}
	for _, id := range []string{"fold", "platforms"} {
		current := section(t, root, id)
		field := "document"
		if id == "fold" {
			field = "platforms"
		}
		raw, _ := json.Marshal("bad: [")
		if _, err := Save(root, Request{Section: id, Revision: current.Revision, Values: map[string]json.RawMessage{field: raw}}); err == nil {
			t.Fatal("malformed YAML accepted")
		}
	}
	target := filepath.Join(t.TempDir(), "outside.yaml")
	os.WriteFile(target, []byte("priority: {}\n"), 0600)
	link := filepath.Join(root, "config", "platforms.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Log("symlink test unavailable", err)
		return
	}
	if _, err := Load(root); err == nil {
		t.Fatal("configuration symlink accepted")
	}
}
