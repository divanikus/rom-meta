package sourceassets

import (
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
