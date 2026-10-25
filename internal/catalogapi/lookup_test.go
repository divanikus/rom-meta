package catalogapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"strings"
	"testing"

	"rommeta/internal/exporter"
)

func lookupFixture(t *testing.T) (*LookupServer, LookupRequest, string, string) {
	_, game, roms := fixture(t)
	root := filepath.Dir(roms)
	ps := filepath.Join(root, "catalog", "platforms", "Sony - PlayStation 2")
	if err := os.MkdirAll(ps, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ps, "_platform.yaml"), []byte("name: Sony - PlayStation 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewLookup(Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: filepath.Join(root, "catalog"), ProfilePath: filepath.Join(root, "profile.yaml")}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := exporter.ObserveInput(roms, "gamegear", "Example.gg")
	if err != nil {
		t.Fatal(err)
	}
	// The source API must remain usable without any source ROM filesystem.
	if err := os.RemoveAll(roms); err != nil {
		t.Fatal(err)
	}
	return s, LookupRequest{Platform: "Sega - Game Gear", Inputs: []exporter.ObservedInput{input}}, game, roms
}
func lookupRequest(s *LookupServer, method, path, token string, query any) *httptest.ResponseRecorder {
	content, _ := json.Marshal(query)
	r := httptest.NewRequest(method, path, bytes.NewReader(content))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestLookupNativeObservationsCapabilitiesAndNoROMAccess(t *testing.T) {
	s, q, _, _ := lookupFixture(t)
	token := strings.Repeat("s", 32)
	if got := lookupRequest(s, "POST", "/v1/lookup", "", q).Code; got != 401 {
		t.Fatal(got)
	}
	caps := lookupRequest(s, "GET", "/v1/platforms", token, nil)
	if caps.Code != 200 || !strings.Contains(caps.Body.String(), `"supported":true`) || !strings.Contains(caps.Body.String(), `"game_binding":true`) {
		t.Fatal(caps.Body.String())
	}
	w := lookupRequest(s, "POST", "/v1/lookup", token, q)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var selected Selection
	if err := json.Unmarshal(w.Body.Bytes(), &selected); err != nil {
		t.Fatal(err)
	}
	if len(selected.Inputs) != 1 || !selected.Inputs[0].Payload.Equal(q.Inputs[0].Payload) || selected.Inputs[0].Outer != q.Inputs[0].Outer {
		t.Fatal(selected)
	}
	for _, card := range selected.Cards {
		if len(card.Assets) != 1 || lookupRequest(s, "GET", card.Assets[0].URI, token, nil).Code != 200 {
			t.Fatal(card)
		}
	}
	q.Platform = "Sony - PlayStation 2"
	if got := lookupRequest(s, "POST", "/v1/lookup", token, q).Code; got != 200 {
		t.Fatal(got)
	}
	for method, path := range map[string]string{"GET": "/v1/lookup", "DELETE": "/v1/platforms", "POST": "/v1/platforms"} {
		if lookupRequest(s, method, path, token, q).Code != 405 {
			t.Fatal(method, path)
		}
	}
}
func TestLookupExactOnlyAndMoreThan1000(t *testing.T) {
	s, q, _, _ := lookupFixture(t)
	token := strings.Repeat("s", 32)
	original := q.Inputs[0]
	q.Inputs = nil
	for i := 0; i < 1005; i++ {
		item := original
		item.ROM = fmt.Sprintf("retail/Copy %04d.gg", i)
		q.Inputs = append(q.Inputs, item)
	}
	w := lookupRequest(s, "POST", "/v1/lookup", token, q)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result Selection
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Inputs) != 1005 || result.Coverage.Considered != 1005 {
		t.Fatal(result.Coverage)
	}
	// Correct-looking filename with different bytes cannot borrow metadata or verification.
	q.Inputs = []exporter.ObservedInput{original}
	q.Inputs[0].Payload.SHA1 = strings.Repeat("0", 40)
	q.Inputs[0].Payload.SHA256 = strings.Repeat("0", 64)
	q.Inputs[0].Outer.SHA256 = q.Inputs[0].Payload.SHA256
	w = lookupRequest(s, "POST", "/v1/lookup", token, q)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Inputs) != 0 || len(result.Coverage.Skipped) != 1 {
		t.Fatal(result.Coverage)
	}
	q.Inputs[0].ROM = "../Example.gg"
	if lookupRequest(s, "POST", "/v1/lookup", token, q).Code != 409 {
		t.Fatal("escape accepted")
	}
}
func TestLookupRejectsChangedIndexAndCachedMedia(t *testing.T) {
	s, q, game, _ := lookupFixture(t)
	token := strings.Repeat("s", 32)
	if w := lookupRequest(s, "POST", "/v1/lookup", token, q); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(game, "media", "box-front.usa.igdb.png"), []byte("changed image"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := lookupRequest(s, "POST", "/v1/lookup", token, q).Code; got != 409 {
		t.Fatal(got)
	}
	s, q, game, _ = lookupFixture(t)
	if w := lookupRequest(s, "POST", "/v1/lookup", token, q); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(game, "game.yaml"), []byte("title: Different\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := lookupRequest(s, "POST", "/v1/lookup", token, q).Code; got != 409 {
		t.Fatal(got)
	}
}

func TestLookupPersistentIndexPreservesSelection(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			s, q, game, _ := lookupFixture(t)
			if packed {
				catalogfstest.Pack(t, filepath.Dir(game))
			}
			token := strings.Repeat("s", 32)
			first := lookupRequest(s, "POST", "/v1/lookup", token, q)
			if first.Code != 200 {
				t.Fatal(first.Body.String())
			}
			next, err := NewLookup(Options{Token: token, Export: exporter.Options{DataRoot: s.catalog.Root, ProfilePath: filepath.Join(filepath.Dir(s.catalog.Root), "profile.yaml")}})
			if err != nil {
				t.Fatal(err)
			}
			warm := lookupRequest(next, "POST", "/v1/lookup", token, q)
			if warm.Code != 200 {
				t.Fatal(warm.Body.String())
			}
			if next.resolver.IndexStats.Cached != 1 || next.resolver.IndexStats.Rebuilt != 0 {
				t.Fatal(next.resolver.IndexStats)
			}
			var a, b Selection
			if err = json.Unmarshal(first.Body.Bytes(), &a); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(warm.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatal("index changed portable selection")
			}
			// Warm cached selections retain the same independent source-digest checks.
			if err = catalogfs.WriteFile(filepath.Join(game, "fingerprints.yaml"), []byte("items: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if got := lookupRequest(next, "POST", "/v1/lookup", token, q); got.Code != 409 {
				t.Fatal(got.Code, got.Body.String())
			}
		})
	}
}
