package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
	"rommeta/internal/frontend"
)

func exportPut(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
func exportFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	roms := filepath.Join(t.TempDir(), "n64")
	card := filepath.Join(root, "platforms", "n64", "Example")
	payload := "synthetic launch input"
	exportPut(t, filepath.Join(root, "platforms", "n64", "_platform.yaml"), "name: Nintendo - Nintendo 64\n")
	exportPut(t, filepath.Join(card, "game.yaml"), "title: Example\ndefault_release: Example (USA)\ndescription: Offline description\nreleases:\n  Example (USA): {regions: [USA]}\n")
	exportPut(t, filepath.Join(card, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {name: Example.z64, size: %d, sha1: %q}\n", len(payload), fmt.Sprintf("%x", sha1.Sum([]byte(payload)))))
	exportPut(t, filepath.Join(card, "media", "Example (USA)", "box-front.usa.launchbox.png"), "synthetic cover")
	exportPut(t, filepath.Join(roms, "Example.z64"), payload)
	return root, roms
}
func exportTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(name string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, name)
		if e.IsDir() {
			if e.Name() == "Backups" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func exportDestination(t *testing.T, target string) string {
	t.Helper()
	destination := t.TempDir()
	if target == "launchbox" {
		if err := os.MkdirAll(filepath.Join(destination, "Data", "Platforms"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return destination
}
func reportPlan(t *testing.T, name string) *frontend.Plan {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var p frontend.Plan
	if err = json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return &p
}
func TestUnifiedDirectExportLocalAndAPI(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			root, roms := exportFixture(t)
			profile := filepath.Join(t.TempDir(), "profile.yaml")
			exportPut(t, profile, "schema_version: 1\ntarget: "+target+"\n")
			stageParent := t.TempDir()
			reports := t.TempDir()
			local := exportDestination(t, target)
			remote := exportDestination(t, target)
			t.Setenv("TMP", stageParent)
			t.Setenv("TEMP", stageParent)
			t.Setenv("TMPDIR", stageParent)
			beforeCatalog, beforeROM := exportTree(t, root), exportTree(t, roms)
			api, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
			if err != nil {
				t.Fatal(err)
			}
			var assets atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/v1/assets/") {
					assets.Add(1)
				}
				api.ServeHTTP(w, r)
			}))
			defer server.Close()
			for _, source := range []string{"local", "api"} {
				destination := local
				sourceArgs := []string{"--data-root", root}
				if source == "api" {
					destination = remote
					sourceArgs = []string{"--api", server.URL}
				}
				args := append([]string{"--target", target, "--platform", "n64", "--rom-root", roms, "--destination", destination, "--profile", profile}, sourceArgs...)
				before := exportTree(t, destination)
				previewReport := filepath.Join(reports, source+"-preview.json")
				if err := runExport(append(append([]string{}, args...), "--report", previewReport)); err != nil {
					t.Fatal(err)
				}
				p := reportPlan(t, previewReport)
				if p.Target != target || p.Package != "" || len(p.Changes) != 1 || p.Changes[0].Action != "add" || p.Changes[0].Media != 1 {
					t.Fatal(p.Changes, p.Package)
				}
				if !reflect.DeepEqual(before, exportTree(t, destination)) {
					t.Fatal("preview wrote native destination")
				}
				if assets.Load() != 0 {
					t.Fatal("preview downloaded API assets")
				}
				if entries, err := os.ReadDir(stageParent); err != nil || len(entries) != 0 {
					t.Fatal("preview created preparation files", entries, err)
				}
				if err := runExport(append(append([]string{}, args...), "--apply")); err != nil {
					t.Fatal(err)
				}
				after := exportTree(t, destination)
				if len(after) == 0 {
					t.Fatal("direct apply wrote no native data")
				}
				assetCount := assets.Load()
				repeatReport := filepath.Join(reports, source+"-repeat.json")
				if err := runExport(append(append([]string{}, args...), "--apply", "--report", repeatReport)); err != nil {
					t.Fatal(err)
				}
				if len(reportPlan(t, repeatReport).Files) != 0 || assets.Load() != assetCount || !reflect.DeepEqual(after, exportTree(t, destination)) {
					t.Fatal("repeat rewrote destination or downloaded media")
				}
				if entries, err := os.ReadDir(stageParent); err != nil || len(entries) != 0 {
					t.Fatal("temporary package/cache left behind", entries, err)
				}
			}
			if assets.Load() != 1 {
				t.Fatal("expected one API asset transfer", assets.Load())
			}
			if !reflect.DeepEqual(exportTree(t, local), exportTree(t, remote)) {
				t.Fatal("local/API native files differ")
			}
			if !reflect.DeepEqual(beforeCatalog, exportTree(t, root)) || !reflect.DeepEqual(beforeROM, exportTree(t, roms)) {
				t.Fatal("changed source catalog or ROMs")
			}
		})
	}
}
func TestUnifiedPackageAndBatchPaths(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			root, roms := exportFixture(t)
			pkg := filepath.Join(t.TempDir(), "package")
			destination := exportDestination(t, target)
			if err := runExport([]string{"--target", target, "--platforms", "n64", "--rom-root", filepath.Dir(roms), "--data-root", root, "--out", pkg, "--apply"}); err != nil {
				t.Fatal(err)
			}
			if err := runExport([]string{"--target", target, "--package", pkg, "--destination", destination, "--apply"}); err != nil {
				t.Fatal(err)
			}
			report := filepath.Join(t.TempDir(), "repeat.json")
			if err := runExport([]string{"--target", target, "--platforms", "n64", "--rom-root", filepath.Dir(roms), "--data-root", root, "--destination", destination, "--apply", "--report", report}); err != nil {
				t.Fatal(err)
			}
			if len(reportPlan(t, report).Files) != 0 {
				t.Fatal("batch direct/package mismatch")
			}
		})
	}
}
func TestUnifiedExportArgumentValidation(t *testing.T) {
	cases := [][]string{
		{"--target", "launchbox", "--destination", "dest", "--out", "package"},
		{"--package", "package"},
		{"--package", "package", "--destination", "dest", "--rom-root", "roms"},
		{"--package", "package", "--destination", "dest", "--profile", "profile"},
		{"--platform", "n64", "--out", "package", "--report", "report"},
		{"--target", "launchbox", "--platform", "n64", "--rom-root", "roms", "--destination", "dest", "--media-root", "media"},
	}
	for _, args := range cases {
		if err := runExport(args); err == nil {
			t.Fatal("accepted incompatible flags", args)
		}
	}
}

func TestDirectAPIAssetFailureLeavesDestinationUntouched(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			root, roms := exportFixture(t)
			destination := exportDestination(t, target)
			before := exportTree(t, destination)
			api, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/v1/assets/") {
					http.Error(w, "synthetic transfer failure", http.StatusServiceUnavailable)
					return
				}
				api.ServeHTTP(w, r)
			}))
			defer server.Close()
			err = runExport([]string{"--target", target, "--platform", "n64", "--rom-root", roms, "--api", server.URL, "--destination", destination, "--apply"})
			if err == nil {
				t.Fatal("accepted failed asset transfer")
			}
			if !reflect.DeepEqual(before, exportTree(t, destination)) {
				t.Fatal("failed transfer changed native destination")
			}
		})
	}
}

func TestExportShowsProgressAfterROMObservation(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		for _, network := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/api=%t", target, network), func(t *testing.T) {
				root, roms := exportFixture(t)
				destination := exportDestination(t, target)
				args := []string{"export", "--target", target, "--platform", "n64", "--rom-root", roms, "--destination", destination}
				if network {
					api, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
					if err != nil {
						t.Fatal(err)
					}
					server := httptest.NewServer(api)
					defer server.Close()
					args = append(args, "--api", server.URL)
				} else {
					args = append(args, "--data-root", root)
				}
				code, output := commandOutput(t, args...)
				if code != 0 {
					t.Fatalf("export failed: %s", output)
				}
				stages := []string{"export scan", "export plan 0/1", "export plan 1/1", "files to write"}
				offset := 0
				for _, stage := range stages {
					at := strings.Index(output[offset:], stage)
					if at < 0 {
						t.Fatalf("missing/out-of-order stage %q after %d:\n%s", stage, offset, output)
					}
					offset += at + len(stage)
				}
			})
		}
	}
}
