package serveui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerListsPlatformsGamesAndGameDetails(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"
default_release: "Star Fox 64 (USA)"
releases:
  Star Fox 64 (USA):
sources:
  launchbox:
    matches:
      - release: "Star Fox 64 (USA)"
        title: "Star Fox 64"
        release_date: "1997-07-01"
        developer: "Nintendo EAD"
        genres: ["Rail Shooter"]
        max_players: "1-4"
        community_rating: "4.5"
        titles:
          - title: "Starwing"
            region: Europe
        descriptions:
          en: |-
            A readable source description.
          de: |-
            Eine lesbare Beschreibung.
        media:
          box_front:
            - url: https://example.test/starfox.jpg
`)
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front.launchbox.jpg"), "image")
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Checked Missing", "game.yaml"), `title: "Checked Missing"
sources:
  openvgdb:
    status: not_found
`)
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Not Checked", "game.yaml"), `title: "Not Checked"
`)

	handler, err := Handler(root)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	var platforms []platformItem
	requestJSON(t, handler, "/api/platforms", &platforms)
	if len(platforms) != 1 {
		t.Fatalf("platform count = %d, want 1", len(platforms))
	}
	if platforms[0].GameCount != 3 || platforms[0].ReleaseCount != 0 {
		t.Fatalf("unexpected fast platform counts: %#v", platforms[0])
	}

	var games gamesResponse
	requestJSON(t, handler, "/api/games?platform=n64", &games)
	if games.Platform != "Nintendo - Nintendo 64" || len(games.Games) != 3 {
		t.Fatalf("unexpected games response: %#v", games)
	}
	checked := findGameListItem(games.Games, "Checked Missing")
	if checked == nil || !checked.MissingMetadata || !checked.CheckedNoMatch {
		t.Fatalf("checked missing game not marked for UI highlighting: %#v", checked)
	}
	matched := findGameListItem(games.Games, "Star Fox 64")
	if matched == nil || matched.MissingMetadata || matched.SourceMediaURLCount != 1 {
		t.Fatalf("matched game not summarized correctly: %#v", matched)
	}

	var detail gameResponse
	requestJSON(t, handler, "/api/game?platform=Nintendo%20-%20Nintendo%2064&game=Star%20Fox%2064", &detail)
	if !detail.HasMetadata || detail.MissingMetadata {
		t.Fatalf("detail metadata flags are wrong: %#v", detail)
	}
	if detail.Profile.Description != "A readable source description." || detail.Profile.ReleaseDate != "1997-07-01" || detail.Profile.Developer != "Nintendo EAD" || len(detail.Profile.Genres) != 1 || detail.Profile.Genres[0] != "Rail Shooter" {
		t.Fatalf("detail profile was not built from source metadata: %#v", detail.Profile)
	}
	if len(detail.Profile.Descriptions) != 2 || detail.Profile.Descriptions[0].Language != "en" || detail.Profile.Descriptions[1].Language != "de" {
		t.Fatalf("detail profile did not expose all descriptions: %#v", detail.Profile.Descriptions)
	}
	if len(detail.SourceBlocks) != 1 || detail.SourceBlocks[0].Source != "launchbox" || !strings.Contains(detail.SourceBlocks[0].Raw, "Starwing") {
		t.Fatalf("source block was not exposed for tabs: %#v", detail.SourceBlocks)
	}
	if len(detail.SourceBlocks[0].Matches) != 1 || detail.SourceBlocks[0].Matches[0].Title != "Star Fox 64" || len(detail.SourceBlocks[0].Matches[0].Titles) != 1 {
		t.Fatalf("source match was not parsed for readable tabs: %#v", detail.SourceBlocks[0].Matches)
	}
	if len(detail.LocalMedia) != 1 || detail.LocalMedia[0].Type != "image" || detail.LocalMedia[0].URL == "" {
		t.Fatalf("local media was not exposed: %#v", detail.LocalMedia)
	}
}

func TestHandlerServesMediaOnlyInsideDataRoot(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"
`)
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front.launchbox.jpg"), "image")
	writeTestFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "notes.txt"), "not media")
	writeTestFile(t, filepath.Join(root, "config", "secrets.yaml"), "secret")
	writeTestFile(t, filepath.Join(root, ".rommeta", "cache", "cache.sqlite"), "cache")

	handler, err := Handler(root)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}

	ok := httptest.NewRecorder()
	handler.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/media?path=platforms/Nintendo%20-%20Nintendo%2064/Star%20Fox%2064/media/box-front.launchbox.jpg", nil))
	if ok.Code != http.StatusOK {
		t.Fatalf("media status = %d, want 200: %s", ok.Code, ok.Body.String())
	}

	for _, target := range []string{
		"/media?path=../secret.txt",
		"/media?path=config/secrets.yaml",
		"/media?path=.rommeta/cache/cache.sqlite",
		"/media?path=platforms/Nintendo%20-%20Nintendo%2064/Star%20Fox%2064/game.yaml",
		"/media?path=platforms/Nintendo%20-%20Nintendo%2064/Star%20Fox%2064/media/notes.txt",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", target, response.Code)
		}
	}
}

func TestHandlerRejectsMediaSymlinkOutsideDataRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	writeTestFile(t, outside, "secret image")
	mediaPath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front.jpg")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(outside, mediaPath); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	handler, err := Handler(root)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/media?path=platforms/Nintendo%20-%20Nintendo%2064/Star%20Fox%2064/media/box-front.jpg", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("symlink status = %d, want 400", response.Code)
	}
}

func TestParseSourceMatchesReadsReadableFields(t *testing.T) {
	matches := parseSourceMatches(`launchbox:
  matches:
    - release: "Star Fox 64 (USA)"
      title: "Star Fox 64"
      release_date: "1997-07-01"
      developer: "Nintendo EAD"
      genres: ["Rail Shooter"]
      titles:
        - title: "Starwing"
          region: Europe
      descriptions:
        en: |-
          A readable source description.
        de: |-
          Eine lesbare Beschreibung.
      media:
        box_front:
          - url: https://example.test/starfox.jpg
      releases:
        - release: "Star Fox 64 (Europe)"
          media:
            screenshot:
              - url: https://example.test/starfox-eu.png
`)
	if len(matches) != 1 {
		t.Fatalf("match count = %d", len(matches))
	}
	if matches[0].Descriptions["en"] != "A readable source description." || matches[0].Descriptions["de"] != "Eine lesbare Beschreibung." || len(matches[0].Titles) != 1 || matches[0].MediaCounts["box_front"] != 1 || matches[0].MediaCounts["screenshot"] != 1 || len(matches[0].Releases) != 1 || matches[0].Releases[0] != "Star Fox 64 (Europe)" {
		t.Fatalf("unexpected parsed match: %#v", matches[0])
	}
}

func TestHandlerHighlightsIdentityOnlyGameAsMissingMetadata(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "platforms", "NEC - PC Engine - TurboGrafx-16", "Identity Only", "game.yaml"), `title: "Identity Only"
releases:
  Identity Only (Japan):
sources:
  hasheous:
    matches:
      - release: "Identity Only (Japan)"
        hasheous_id: "42"
`)
	handler, err := Handler(root)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	var games gamesResponse
	requestJSON(t, handler, "/api/games?platform=NEC%20-%20PC%20Engine%20-%20TurboGrafx-16", &games)
	if len(games.Games) != 1 || games.Games[0].HasMetadata || !games.Games[0].MissingMetadata {
		t.Fatalf("identity-only game flags = %#v", games.Games)
	}
}

func requestJSON(t *testing.T, handler http.Handler, target string, out any) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned status %d: %s", target, response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), out); err != nil {
		t.Fatalf("json decode failed: %v\n%s", err, response.Body.String())
	}
}

func findGameListItem(games []gameListItem, folder string) *gameListItem {
	for index := range games {
		if games[index].Folder == folder {
			return &games[index]
		}
	}
	return nil
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func TestHandlerListsAndServesFoldMediaVariants(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Game")
	writeTestFile(t, filepath.Join(game, "game.yaml"), "title: Game\n")
	writeTestFile(t, filepath.Join(game, "media", "box-front.launchbox.jpg"), "original")
	writeTestFile(t, filepath.Join(game, "media", "_fold", "Japan", "box-front.launchbox.jpg"), "regional")
	handler, err := Handler(root)
	if err != nil {
		t.Fatal(err)
	}
	var detail gameResponse
	requestJSON(t, handler, "/api/game?platform=n64&game=Game", &detail)
	if len(detail.LocalMedia) != 2 {
		t.Fatalf("missing preserved variant: %#v", detail.LocalMedia)
	}
	found := map[string]bool{}
	for _, item := range detail.LocalMedia {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, item.URL, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("variant not accessible: %d %s", response.Code, item.URL)
		}
		found[response.Body.String()] = true
	}
	if !found["original"] || !found["regional"] {
		t.Fatalf("variant bytes lost: %v", found)
	}
}
