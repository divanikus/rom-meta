package igdb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"rommeta/internal/catalogyaml"
	"strings"
	"testing"
)

func TestRelationshipKinds(t *testing.T) {
	for _, tc := range []struct {
		kind, want string
		fold       bool
	}{
		{"Expanded Game", "expanded_edition_of", true}, {"Port", "port_of", true},
		{"Remake", "remake_of", false}, {"Remaster", "remaster_of", false},
		{"Expansion", "expansion_of", false}, {"Main Game", "related_to", false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			game := apiGame{ID: 2, ParentGame: 1}
			game.GameType.Type = tc.kind
			got := relationshipsFromGame(game)
			if len(got) != 1 || got[0].Type != tc.want || got[0].CanFoldEdition() != tc.fold {
				t.Fatalf("%+v", got)
			}
		})
	}
	game := apiGame{ID: 2, ParentGame: 1, VersionParent: 1}
	got := relationshipsFromGame(game)
	if len(got) != 1 || got[0].Type != "edition_of" {
		t.Fatalf("%+v", got)
	}
}

func TestRefreshPreservesEveryMatchAndOtherMetadata(t *testing.T) {
	original := []byte(`title: Original
default_release: Original (USA)
custom: {keep: true}
releases:
  Original (USA): {}
  Expanded (Japan): {}
sources:
  igdb:
    note: hand edited
    matches:
      - igdb_id: 1
        release: Original (USA)
        title: Original
        media: {box_front: [{url: "https://example.test/cover.jpg"}]}
      - igdb_id: 2
        release: Expanded (Japan)
        description: curated
  launchbox:
    matches: [{database_id: 9}]
`)
	game := apiGame{ID: 2, ParentGame: 1}
	game.GameType.Type = "Expanded Game"
	records := map[int]apiGame{1: {ID: 1}, 2: game}
	updated, changed, missing, err := updateRelationships(original, records, false)
	if err != nil || !changed || missing != 0 {
		t.Fatalf("%v %v %d", err, changed, missing)
	}
	before, _ := catalogyaml.Parse(original)
	after, _ := catalogyaml.Parse(updated)
	var want, got map[string]any
	before.Root().Decode(&want)
	after.Root().Decode(&got)
	matches := got["sources"].(map[string]any)["igdb"].(map[string]any)["matches"].([]any)
	if len(matches) != 2 {
		t.Fatal("lost a folded match")
	}
	for _, m := range matches {
		delete(m.(map[string]any), "relationships")
		delete(m.(map[string]any), "game_type")
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("refresh changed existing metadata")
	}
	resumed, changed, _, err := updateRelationships(updated, records, false)
	if err != nil || changed || string(resumed) != string(updated) {
		t.Fatal("resume did not skip saved relationships")
	}
	forced, changed, _, err := updateRelationships(updated, map[int]apiGame{1: {ID: 1}, 2: {ID: 2}}, true)
	if err != nil || !changed || strings.Contains(string(forced), "expanded_edition_of") {
		t.Fatal("force did not refresh relations")
	}
	_, changed, missing, err = updateRelationships(original, map[int]apiGame{}, false)
	if err != nil || changed || missing != 2 {
		t.Fatal("missing IDs should preserve unsaved matches")
	}
}

func TestRelationshipQueryUsesIDsAndCaches(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/games" || !strings.Contains(string(body), "id = (1,2)") || !strings.Contains(string(body), "game_type.type") {
			t.Errorf("unexpected request: %s", body)
		}
		io.WriteString(w, `[{"id":2,"parent_game":1,"game_type":{"type":"Port"}},{"id":1}]`)
	}))
	defer server.Close()
	cache, err := openQueryCache(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.close()
	settings := sourceSettings{APIBaseURL: server.URL}
	limiter := newRateLimiter(1000)
	for i := 0; i < 2; i++ {
		games, cached, err := queryRelationships(server.Client(), cache, limiter, settings, accessToken{}, []int{1, 2})
		if err != nil || len(games) != 2 || cached != (i == 1) {
			t.Fatalf("%v %v %+v", err, cached, games)
		}
	}
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
}

func TestNormalImportRendersRelationships(t *testing.T) {
	var game apiGame
	if err := json.Unmarshal([]byte(`{"id":2,"name":"Edition","parent_game":1,"game_type":{"type":"Expanded Game"}}`), &game); err != nil {
		t.Fatal(err)
	}
	rendered := renderMetadata(matchedGame{Metadata: metadataFromGame(game)})
	if !strings.Contains(rendered, "expanded_edition_of") || !strings.Contains(rendered, "target_id: \"1\"") {
		t.Fatal(rendered)
	}
	if !strings.Contains(igdbGameFields, "version_parent") || !strings.Contains(igdbGameFields, "parent_game") || !strings.Contains(igdbGameFields, "game_type.type") {
		t.Fatal("normal import omits relationships")
	}
}

func TestRelationshipFailureIsNotCachedAsEmpty(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(429)
		io.WriteString(w, "retry later")
	}))
	defer server.Close()
	cache, err := openQueryCache(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.close()
	for i := 0; i < 2; i++ {
		_, _, err := queryRelationships(server.Client(), cache, newRateLimiter(1000), sourceSettings{APIBaseURL: server.URL}, accessToken{}, []int{1})
		if err == nil {
			t.Fatal("expected error")
		}
	}
	if calls != 2 {
		t.Fatal("cached a failure")
	}
}
