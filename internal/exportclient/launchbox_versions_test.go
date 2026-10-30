package exportclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
)

func nativeLaunchBoxFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		prefix := strings.Split(filepath.ToSlash(rel), "/")[0]
		if prefix != "Data" && prefix != "Images" && prefix != "Videos" && prefix != "Manuals" && prefix != "VersionMedia" && prefix != "Playlists" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLaunchBoxGroupedVersionNetworkParityAndLegacyProfile(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	card(t, root, "n64", "Game", "Canonical card", "Game (USA)", "unused.z64", []byte("unused"))
	path := filepath.Join(root, "platforms", "n64", "Game")
	put(t, filepath.Join(path, "game.yaml"), []byte("title: Canonical card\ndefault_release: Game (USA)\nreleases:\n  Game (USA): {regions: [USA], languages: [en]}\n  Game (Japan): {regions: [Japan], languages: [ja]}\nvariants:\n  Russian:\n    kind: translation\n    based_on: Game (Japan)\n    languages: [ru]\n    group: Test team\n    version: '2.0'\n  Hard:\n    kind: hack\n    based_on: Game (USA)\n"))
	var fingerprints strings.Builder
	fingerprints.WriteString("items:\n")
	for _, entry := range []struct{ name, key, kind string }{{"us", "Game (USA)", "release"}, {"jp", "Game (Japan)", "release"}, {"ru", "Russian", "variant"}, {"hack", "Hard", "variant"}} {
		data := []byte("unique synthetic " + entry.name)
		put(t, filepath.Join(roms, entry.name+".z64"), data)
		fmt.Fprintf(&fingerprints, "  - %s: %q\n    file: {size: %d, sha256: '%x'}\n", entry.kind, entry.key, len(data), sha256.Sum256(data))
	}
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte(fingerprints.String()))
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	put(t, profile, []byte("schema_version: 1\ntarget: launchbox\nlaunchbox:\n  kinds: [translation, original, hack]\n  languages: [ru, en]\n  regions: [USA, Japan]\n"))
	requests := 0
	api := server(t, root, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/export" {
				var input catalogapi.ExportRequest
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if err = json.Unmarshal(data, &input); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(input.Profile, "launchbox:") || !strings.Contains(input.Profile, "target: es-de") {
					t.Error("incompatible profile sent to legacy server:", input.Profile)
				}
				r.Body = io.NopCloser(strings.NewReader(string(data)))
				requests++
			}
			next.ServeHTTP(w, r)
		})
	})
	options := exporter.Options{DataRoot: root, ROMRoot: roms, Platform: "n64", Target: "launchbox", ProfilePath: profile, Out: filepath.Join(t.TempDir(), "local")}
	local, err := exporter.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if err = exporter.Apply(local, nil); err != nil {
		t.Fatal(err)
	}
	options.DataRoot, options.Out = "", filepath.Join(t.TempDir(), "remote")
	remote, err := Build(context.Background(), Options{API: api.URL, BatchOptions: exporter.BatchOptions{Options: options}})
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := remote.Batch.Plans[0]
	if requests != 1 || len(got.LaunchBox.Groups) != 1 || got.LaunchBox.VersionCount != 4 || !strings.Contains(got.LaunchBox.Groups[0].Versions[0].Name, "translation: Russian") {
		t.Fatal(got.LaunchBox, requests)
	}
	if !reflect.DeepEqual(local.LaunchBox, got.LaunchBox) || !reflect.DeepEqual(nativeLaunchBoxFiles(t, local.Out), nativeLaunchBoxFiles(t, remote.Batch.Out)) {
		t.Fatal("grouped native output differs")
	}
}

func TestLaunchBoxGeneratedDiscNetworkParity(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	card(t, root, "psx", "Game", "Game", "Game (USA) (Disc 1)", "none.iso", []byte("none"))
	path := filepath.Join(root, "platforms", "psx", "Game")
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte("items: []\n"))
	var yaml strings.Builder
	yaml.WriteString("title: Game\nreleases:\n")
	for _, region := range []string{"USA", "Europe"} {
		for disc := 1; disc <= 2; disc++ {
			name := fmt.Sprintf("Game (%s) (Disc %d)", region, disc)
			fmt.Fprintf(&yaml, "  %q: {regions: [%s]}\n", name, region)
			put(t, filepath.Join(roms, name+".iso"), []byte("synthetic "+name))
		}
	}
	put(t, filepath.Join(path, "game.yaml"), []byte(yaml.String()))
	api := server(t, root, nil)
	options := exporter.Options{DataRoot: root, ROMRoot: roms, Platform: "psx", Target: "launchbox", Out: filepath.Join(t.TempDir(), "local")}
	local, err := exporter.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if err = exporter.Apply(local, nil); err != nil {
		t.Fatal(err)
	}
	options.DataRoot, options.Out = "", filepath.Join(t.TempDir(), "remote")
	remote, err := Build(context.Background(), Options{API: api.URL, BatchOptions: exporter.BatchOptions{Options: options}})
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if local.LaunchBox.VersionCount != 2 || !reflect.DeepEqual(local.LaunchBox, remote.Batch.Plans[0].LaunchBox) || !reflect.DeepEqual(nativeLaunchBoxFiles(t, local.Out), nativeLaunchBoxFiles(t, remote.Batch.Out)) {
		t.Fatal("regional M3U/API parity")
	}
}
