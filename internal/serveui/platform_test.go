package serveui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
)

func TestBrowsePlatformFoldersTakePrecedenceOverAliases(t *testing.T) {
	for _, test := range []struct {
		name     string
		query    string
		platform string
		other    string
	}{
		{"Wii U folder", "Nintendo - Wii U", "Nintendo - Wii U", ""},
		{"separate digital catalog", "Nintendo - Wii U", "Nintendo - Wii U", "Nintendo - Wii U (Digital) (CDN)"},
		{"literal short name", "n64", "n64", "Nintendo - Nintendo 64"},
		{"alias fallback", "n64", "Nintendo - Nintendo 64", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for _, platform := range []string{test.platform, test.other} {
				if platform == "" {
					continue
				}
				gameDir := filepath.Join(root, "platforms", platform, "Example")
				writeTestFile(t, filepath.Join(gameDir, "game.yaml"), fmt.Sprintf("title: %q\n", platform))
				writeTestFile(t, filepath.Join(gameDir, "media", "box-front.jpg"), "image")
			}
			handler, err := Handler(root)
			if err != nil {
				t.Fatal(err)
			}
			var platforms []platformItem
			requestJSON(t, handler, "/api/platforms", &platforms)
			found := false
			for _, item := range platforms {
				found = found || item.Platform == test.platform
			}
			if !found {
				t.Fatalf("catalog folder missing from platform list: %#v", platforms)
			}
			query := url.QueryEscape(test.query)
			for _, suffix := range []string{"&names=1", "&game=Example", ""} {
				var games gamesResponse
				requestJSON(t, handler, "/api/games?platform="+query+suffix, &games)
				if games.Platform != test.platform || len(games.Games) != 1 || games.Games[0].Folder != "Example" {
					t.Fatalf("wrong catalog for %q: %#v", suffix, games)
				}
				if suffix != "&names=1" && (!games.Games[0].SummaryLoaded || games.Games[0].Title != test.platform) {
					t.Fatalf("wrong summary for %q: %#v", suffix, games.Games[0])
				}
			}
			var detail gameResponse
			requestJSON(t, handler, "/api/game?platform="+query+"&game=Example", &detail)
			if detail.Game.Platform != test.platform || detail.Game.Title != test.platform {
				t.Fatalf("wrong detail: %#v", detail.Game)
			}
			wantMedia := "platforms/" + test.platform + "/Example/media/box-front.jpg"
			if len(detail.LocalMedia) != 1 || detail.LocalMedia[0].Path != wantMedia {
				t.Fatalf("wrong media: %#v", detail.LocalMedia)
			}
		})
	}
}
