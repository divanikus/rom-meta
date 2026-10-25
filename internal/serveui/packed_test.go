package serveui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"testing"
)

func TestPackedBrowserRefreshAndMedia(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Arcade", "Game")
	writeTestFile(t, filepath.Join(game, "game.yaml"), "title: Before\n")
	writeTestFile(t, filepath.Join(game, "media", "box-front.launchbox.png"), "picture")
	catalogfstest.Pack(t, filepath.Dir(game))
	handler, e := Handler(root)
	if e != nil {
		t.Fatal(e)
	}
	var batch gamesResponse
	requestJSON(t, handler, "/api/games?platform=Arcade&game=Game", &batch)
	if len(batch.Games) != 1 || batch.Games[0].Title != "Before" {
		t.Fatal(batch)
	}
	if e = catalogfs.WriteFile(filepath.Join(game, "game.yaml"), []byte("title: Latest\n"), 0644); e != nil {
		t.Fatal(e)
	}
	requestJSON(t, handler, "/api/games?platform=Arcade&game=Game", &batch)
	if batch.Games[0].Title != "Latest" {
		t.Fatal(batch)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/media?path="+url.QueryEscape("platforms/Arcade/Game/media/box-front.launchbox.png"), nil))
	if w.Code != 200 || w.Body.String() != "picture" {
		t.Fatal(w.Code, w.Body.String())
	}
}
