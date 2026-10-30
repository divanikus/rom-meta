package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedPlatformListingAndDiscovery(t *testing.T) {
	root := t.TempDir()
	roms := t.TempDir()
	dir := filepath.Join(root, "platforms", "pocket")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_platform.yaml"), []byte("name: Nintendo - Game Boy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(roms, "Nintendo - Game Boy"), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := buildPlatformList(platformListOptions{DataRoot: root, ROMRoot: roms})
	if err != nil {
		t.Fatal(err)
	}
	rows := platformListRows(result)
	if len(rows) != 1 || rows[0].Code != "gb" || len(rows[0].ExportTargets) != 2 || len(rows[0].Catalogs) != 1 || len(result.Folders) != 1 {
		t.Fatalf("list %+v %+v", rows, result)
	}
	text := renderPlatformListText(result, false, false)
	for _, fragment := range []string{"code: gb", "export: es-de, launchbox", "catalog folders: pocket", "canonical folder name"} {
		if !strings.Contains(text, fragment) {
			t.Fatal("missing", fragment, text)
		}
	}
	empty, err := buildPlatformList(platformListOptions{DataRoot: t.TempDir()})
	if err != nil || !empty.Full || empty.Selector.CatalogKnown {
		t.Fatal("embedded fallback failed", err)
	}
	for _, flag := range []string{"--list-platforms", "--list-systems"} {
		if err := runExport([]string{flag, "--data-root", root, "--rom-root", roms}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlatformListAPIRelativeJSONOutput(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/platforms" {
			t.Error("unexpected catalog request", r.URL.Path)
		}
		fmt.Fprint(w, `{"schema_version":1,"export_schema_version":1,"mode":"catalog","platforms":[{"platform":"gb","export_systems":["gb"]}]}`)
	}))
	defer api.Close()
	t.Chdir(t.TempDir())
	if err := runPlatformsList([]string{"--api", api.URL, "--format", "json", "--out", "platforms.json"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("platforms.json")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Source    string
		Platforms []struct{ Code string }
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Source != api.URL || len(result.Platforms) != 1 || result.Platforms[0].Code != "gb" {
		t.Fatalf("invalid listing: %s %v", body, err)
	}
	if err := runPlatformsList([]string{"--api", api.URL, "--data-root", "."}); err == nil {
		t.Fatal("mixed sources accepted")
	}
}
