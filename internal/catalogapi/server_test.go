package catalogapi

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/exporter"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "catalog")
	roms := filepath.Join(root, "roms")
	game := filepath.Join(data, "platforms", "Sega - Game Gear", "Example")
	put := func(name, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	payload := "synthetic Game Gear bytes"
	put(filepath.Join(roms, "Example.gg"), payload)
	put(filepath.Join(data, "platforms", "Sega - Game Gear", "_platform.yaml"), "name: Sega - Game Gear\n")
	put(filepath.Join(game, "game.yaml"), "title: Example\nreleases:\n  Example (USA):\n    regions: [USA]\nsources:\n  igdb:\n    matches:\n      - release: Example (USA)\n        igdb_id: 123\n        release_date: 1993-07-02\n        descriptions: {en: Offline prose}\n")
	put(filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    file:\n      size: %d\n      sha1: %x\n", len(payload), sha1.Sum([]byte(payload))))
	put(filepath.Join(game, "media", "box-front.usa.igdb.png"), "synthetic image")
	put(filepath.Join(data, "config", "secrets.yaml"), "must-never-be-served")
	profile := filepath.Join(root, "profile.yaml")
	put(profile, "schema_version: 1\ntarget: es-de\ntitle_mode: catalog\nselection:\n  media:\n    cover: {scope: game, regions: ['*'], sources: [igdb]}\n")
	server, err := New(Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: data, ROMRoot: roms, Platform: "gamegear", ProfilePath: profile, Limit: 20}})
	if err != nil {
		t.Fatal(err)
	}
	return server, game, roms
}
func request(s *Server, method, route, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, route, nil)
	if auth != "" {
		r.Header.Set("Authorization", "Bearer "+auth)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestPortableSnapshotAuthenticationAndAssetBinding(t *testing.T) {
	s, _, _ := fixture(t)
	token := strings.Repeat("s", 32)
	if got := request(s, "GET", "/v1/selection", "").Code; got != 401 {
		t.Fatal(got)
	}
	if got := request(s, "POST", "/v1/selection", token).Code; got != 405 {
		t.Fatal(got)
	}
	w := request(s, "GET", "/v1/selection", token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var snapshot Selection
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Inputs) != 1 || len(snapshot.Cards) != 1 || snapshot.Inputs[0].Payload.SHA1 == "" || snapshot.Inputs[0].Outer.SHA256 == "" {
		t.Fatal(snapshot)
	}
	if strings.Contains(w.Body.String(), "must-never") || strings.Contains(w.Body.String(), "raw_igdb_metadata") || strings.Contains(w.Body.String(), "downloaded_media") {
		t.Fatal("non-portable/secret content exposed")
	}
	for _, card := range snapshot.Cards {
		if len(card.Assets) != 1 {
			t.Fatal(card)
		}
		a := card.Assets[0]
		if got := request(s, "GET", a.URI, token); got.Code != 200 || got.Body.String() != "synthetic image" {
			t.Fatal(got.Code, got.Body.String())
		}
	}
	for _, route := range []string{"/config/secrets.yaml", "/v1/assets/../../config/secrets.yaml", "/v1/assets/unknown"} {
		if got := request(s, "GET", route, token).Code; got != 404 {
			t.Fatal(route, got)
		}
	}
	if got := request(s, "GET", "/v1/selection?path=../../config", token).Code; got != 400 {
		t.Fatal(got)
	}
}
func TestSourceAndAssetDriftFailClosed(t *testing.T) {
	s, game, roms := fixture(t)
	token := strings.Repeat("s", 32)
	var asset Asset
	for _, card := range s.selection.Cards {
		asset = card.Assets[0]
	}
	if err := os.WriteFile(filepath.Join(game, "media", "box-front.usa.igdb.png"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := request(s, "GET", asset.URI, token).Code; got != 409 {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(roms, "Example.gg"), []byte("changed ROM"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := request(s, "GET", "/v1/selection", token).Code; got != 409 {
		t.Fatal(got)
	}
}
func TestContainmentAndTimestampPreservation(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"../secret", "/secret", "x/../secret", "C:/secret", "x\\secret", "x//secret"} {
		if _, err := contained(root, relative); err == nil {
			t.Fatal(relative)
		}
	}
	s, _, _ := fixture(t)
	for _, card := range s.selection.Cards {
		if !strings.Contains(string(card.Game.Data), `"release_date":"1993-07-02"`) {
			t.Fatal(string(card.Game.Data))
		}
	}
}

func TestExplicitSkipCoverageKeepsOnlyExactInputs(t *testing.T) {
	_, game, roms := fixture(t)
	for name, content := range map[string]string{"translations/en/Fan Title.gg": "unregistered translation", "aftermarket/Unknown.7z": "unsupported archive"} {
		path := filepath.Join(roms, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: filepath.Join(filepath.Dir(roms), "catalog"), ROMRoot: roms, Platform: "gamegear", ProfilePath: filepath.Join(filepath.Dir(roms), "profile.yaml"), Limit: 1000}}
	if _, err := New(opts); err == nil || !strings.Contains(err.Error(), "--skip-unmatched") {
		t.Fatal("strict default accepted non-exact inputs", err)
	}
	opts.SkipUnmatched = true
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	got := s.selection.Coverage
	if got.Considered != 3 || got.Included != 1 || len(got.Skipped) != 2 || len(s.selection.Cards) != 1 {
		t.Fatal(got)
	}
	if s.selection.Inputs[0].Record.ROM != "Example.gg" || len(s.assets) != 1 {
		t.Fatal("unmatched input inherited retail media")
	}
	for _, skipped := range got.Skipped {
		if skipped.Reason == "" || skipped.Status == "matched" {
			t.Fatal(skipped)
		}
	}
	originalDigest := s.selection.Digest
	// A newly known fingerprint changes both coverage and its snapshot digest.
	fingerprints := filepath.Join(game, "fingerprints.yaml")
	f, err := os.OpenFile(fingerprints, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(f, "  - release: Example (USA)\n    file:\n      size: %d\n      sha1: %x\n", len("unregistered translation"), sha1.Sum([]byte("unregistered translation")))
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := New(opts)
	if err != nil || changed.selection.Coverage.Included != 2 || changed.selection.Digest == originalDigest {
		t.Fatal("coverage not digest-bound", err)
	}
}

func TestWholeSystemSelectionHasNoDefaultOrPositiveCountCeiling(t *testing.T) {
	_, _, roms := fixture(t)
	for i := 0; i < 1004; i++ {
		if err := os.WriteFile(filepath.Join(roms, fmt.Sprintf("Copy %04d.gg", i)), []byte("synthetic Game Gear bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: filepath.Join(filepath.Dir(roms), "catalog"), ROMRoot: roms, Platform: "gamegear", ProfilePath: filepath.Join(filepath.Dir(roms), "profile.yaml")}}
	for _, limit := range []int{0, 1005, 2000, 20} {
		opts.Export.Limit = limit
		server, err := New(opts)
		if err != nil {
			t.Fatal(limit, err)
		}
		want := 1005
		if limit == 20 {
			want = 20
		}
		response := request(server, "GET", "/v1/selection", opts.Token)
		var selection Selection
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &selection) != nil || len(selection.Inputs) != want || selection.Coverage.Considered != want || selection.Coverage.Included != want || len(selection.Cards) != 1 {
			t.Fatalf("limit %d lost coverage: %+v", limit, selection.Coverage)
		}
	}
	opts.Export.Limit = -1
	if _, err := New(opts); err == nil {
		t.Fatal("negative limit accepted")
	}
}
