package sourceassets

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
)

func TestDownloadAssetRejectsSuccessfulMissingMediaResponses(t *testing.T) {
	for _, packed := range []bool{false, true} {
		storage := "loose"
		if packed {
			storage = "packed"
		}
		for _, tc := range []struct{ name, body, want string }{{"missing", "NOMEDIA", "NOMEDIA"}, {"whitespace", " \r\nNOMEDIA\n", "NOMEDIA"}, {"empty", "", "empty"}} {
			t.Run(storage+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				writeGame(t, root, "Example", "Game", "title: Game\n")
				platform := filepath.Join(root, "platforms", "Example")
				game := filepath.Join(platform, "Game")
				if packed {
					catalogfstest.Pack(t, platform)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/jpeg")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tc.body))
				}))
				defer server.Close()
				target := filepath.Join(game, "media", "box-front.screenscraper.jpg")
				for _, existing := range []bool{false, true} {
					if existing {
						if err := catalogfs.WriteFile(target, []byte("existing media"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					raw := server.URL + "/image?ssid=private-user&sspassword=private-password"
					err := downloadAsset(raw, raw, target, nil, "image", 1, 1)
					if err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("expected rejected response, got %v", err)
					}
					if strings.Contains(err.Error(), "private-") {
						t.Fatalf("error exposed credentials: %v", err)
					}
					if existing {
						data, e := catalogfs.ReadFile(target)
						if e != nil || string(data) != "existing media" {
							t.Fatalf("existing asset changed: %q %v", data, e)
						}
					} else if _, e := catalogfs.Stat(target); !os.IsNotExist(e) {
						t.Fatalf("missing media was published: %v", e)
					}
					if _, e := os.Stat(target + ".tmp"); !os.IsNotExist(e) {
						t.Fatalf("temporary payload remains: %v", e)
					}
				}
			})
		}
	}
}

func TestDownloadAssetRedactsTransportFailureURL(t *testing.T) {
	for _, packed := range []bool{false, true} {
		name := "loose"
		if packed {
			name = "packed"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeGame(t, root, "Example", "Game", "title: Game\n")
			platform := filepath.Join(root, "platforms", "Example")
			game := filepath.Join(platform, "Game")
			if packed {
				catalogfstest.Pack(t, platform)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer server.Close()
			public := server.URL + "/image?media=box"
			signed := public + "&ssid=private-user&sspassword=private-password&devid=private-device&devpassword=private-device-password"
			target := filepath.Join(game, "media", "box-front.screenscraper.jpg")
			if err := catalogfs.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			if err := catalogfs.WriteFile(target, []byte("existing media"), 0644); err != nil {
				t.Fatal(err)
			}
			err := downloadAsset(signed, public, target, nil, "image", 1, 1)
			if err == nil || !errors.Is(err, io.EOF) {
				t.Fatalf("expected wrapped EOF, got %v", err)
			}
			if strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "sspassword") {
				t.Fatalf("transport error exposed credentials: %v", err)
			}
			manifest, e := openAssetManifestWriter(root, "screenscraper", true)
			if e != nil {
				t.Fatal(e)
			}
			d := startCandidateDownloader("screenscraper", 1, nil, manifest)
			if e = d.Enqueue(assetCandidate{Platform: "Example", Game: "Game", GamePath: game, URL: public, DownloadURL: signed, Target: target}); e != nil {
				t.Fatal(e)
			}
			downloaded, failed, failures, e := d.CloseAndWait()
			if e != nil || downloaded != 0 || failed != 1 || len(failures) != 1 {
				t.Fatalf("unexpected failure result: %d %d %v %v", downloaded, failed, failures, e)
			}
			path, _, e := manifest.Close()
			if e != nil {
				t.Fatal(e)
			}
			report, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(report), "private-") || strings.Contains(string(report), "sspassword") {
				t.Fatalf("manifest exposed credentials: %s", report)
			}
			data, e := catalogfs.ReadFile(target)
			if e != nil || string(data) != "existing media" {
				t.Fatalf("existing asset changed: %q %v", data, e)
			}
		})
	}
}
