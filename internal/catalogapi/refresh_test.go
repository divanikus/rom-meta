package catalogapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/exporter"
)

func automaticWrite(t *testing.T, fn func()) {
	t.Helper()
	b := catalogchanges.Begin(exporter.RefreshPlatformIndex)
	defer b.Finish()
	fn()
	if err := b.Finish(); err != nil {
		t.Fatal(err)
	}
}
func TestAutomaticMetadataIndexAndAPIRefresh(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			s, q, game, _ := lookupFixture(t)
			q.Media = "none"
			if packed {
				catalogfstest.Pack(t, filepath.Dir(game))
			}
			first := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
			if first.Code != 200 {
				t.Fatal(first.Body.String())
			}
			before := s.resolver
			automaticWrite(t, func() {
				raw, err := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				next := strings.Replace(string(raw), "Offline prose", "Fresh offline prose", 1)
				if err = atomicfile.WriteFile(filepath.Join(game, "game.yaml"), []byte(next), 0600); err != nil {
					t.Fatal(err)
				}
			})
			next := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
			if next.Code != 200 || !strings.Contains(next.Body.String(), "Fresh offline prose") {
				t.Fatal(next.Code, next.Body.String())
			}
			if s.resolver == before || s.resolver.IndexStats.Cached != 1 || s.resolver.IndexStats.Rebuilt != 0 {
				t.Fatal("disk index not refreshed before API", s.resolver.IndexStats)
			}
			if _, err := os.Stat(filepath.Join(s.catalog.Root, catalogchanges.RevisionFile)); err != nil {
				t.Fatal(err)
			}
			renamed := filepath.Join(filepath.Dir(game), "Renamed")
			automaticWrite(t, func() {
				if err := catalogfs.Rename(game, renamed); err != nil {
					t.Fatal(err)
				}
			})
			moved := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
			if moved.Code != 200 || !strings.Contains(moved.Body.String(), "Renamed") || s.resolver.IndexStats.Cached != 1 {
				t.Fatal(moved.Code, moved.Body.String())
			}
			automaticWrite(t, func() {
				if err := catalogfs.RemoveAll(renamed); err != nil {
					t.Fatal(err)
				}
			})
			removed := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
			var result Selection
			if removed.Code != 200 || json.Unmarshal(removed.Body.Bytes(), &result) != nil || result.Coverage.Included != 0 {
				t.Fatal(removed.Code, removed.Body.String())
			}
		})
	}
}
func TestNewGameAndNewPlatformAppearWithoutRestart(t *testing.T) {
	s, q, game, _ := lookupFixture(t)
	q.Media = "none"
	token := strings.Repeat("s", 32)
	if w := lookupRequest(s, "POST", "/v1/lookup", token, q); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	automaticWrite(t, func() {
		other := filepath.Join(filepath.Dir(game), "Duplicate")
		if err := catalogfs.MkdirAll(other, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
			data, err := catalogfs.ReadFile(filepath.Join(game, name))
			if err != nil {
				t.Fatal(err)
			}
			if err = catalogfs.WriteFile(filepath.Join(other, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		newp := filepath.Join(s.catalog.Root, "platforms", "Nintendo - Nintendo 64DD")
		if err := catalogfs.MkdirAll(newp, 0700); err != nil {
			t.Fatal(err)
		}
		if err := catalogfs.WriteFile(filepath.Join(newp, "_platform.yaml"), []byte("name: Nintendo - Nintendo 64DD\n"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	caps := lookupRequest(s, "GET", "/v1/platforms", token, nil)
	if caps.Code != 200 || !strings.Contains(caps.Body.String(), "Nintendo - Nintendo 64DD") {
		t.Fatal(caps.Body.String())
	}
	next := lookupRequest(s, "POST", "/v1/lookup", token, q)
	var result Selection
	if next.Code != 200 || json.Unmarshal(next.Body.Bytes(), &result) != nil || result.Coverage.Included != 0 || len(result.Coverage.Skipped) != 1 {
		t.Fatal(next.Code, next.Body.String())
	}
	if s.resolver.IndexStats.Cached != 2 || s.resolver.IndexStats.Rebuilt != 0 {
		t.Fatal(s.resolver.IndexStats)
	}
}
func TestMediaWriteRefreshesOldBoundAsset(t *testing.T) {
	s, q, game, _ := lookupFixture(t)
	token := strings.Repeat("s", 32)
	if w := lookupRequest(s, "POST", "/v1/lookup", token, q); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	automaticWrite(t, func() {
		if err := catalogfs.WriteFile(filepath.Join(game, "media", "box-front.usa.igdb.png"), []byte("new image bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	w := lookupRequest(s, "POST", "/v1/lookup", token, q)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result Selection
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, card := range result.Cards {
		for _, a := range card.Assets {
			got := lookupRequest(s, "GET", a.URI, token, nil)
			if got.Code != 200 || got.Body.String() != "new image bytes" {
				t.Fatal(got.Code, got.Body.String())
			}
		}
	}
}

func TestProfileEditRefreshesLookupWithoutCatalogWrite(t *testing.T) {
	s, q, _, _ := lookupFixture(t)
	q.Media = "none"
	token := strings.Repeat("s", 32)
	first := lookupRequest(s, "POST", "/v1/lookup", token, q)
	if first.Code != 200 || !strings.Contains(first.Body.String(), "Offline prose") {
		t.Fatal(first.Code, first.Body.String())
	}
	profile := s.options.Export.ProfilePath
	raw, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(string(raw), "selection:\n", "selection:\n  description: {languages: [ja]}\n", 1)
	if err := os.WriteFile(profile, []byte(next), 0600); err != nil {
		t.Fatal(err)
	}
	result := lookupRequest(s, "POST", "/v1/lookup", token, q)
	if result.Code != 200 || s.catalog.Profile.Selection.Description.Languages[0] != "ja" {
		t.Fatal(result.Code, result.Body.String())
	}
}
