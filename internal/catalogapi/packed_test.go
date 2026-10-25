package catalogapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/exporter"
	"strings"
	"testing"
)

func TestPackedSnapshotMatchesLooseAndServesRanges(t *testing.T) {
	loose, game, roms := fixture(t)
	root := filepath.Dir(roms)
	data := filepath.Join(root, "catalog")
	catalogfstest.Pack(t, filepath.Dir(game))
	s, e := New(Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: data, ROMRoot: roms, Platform: "gamegear", ProfilePath: filepath.Join(root, "profile.yaml"), Limit: 20}})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(loose.selection, s.selection) {
		t.Fatal("physical layout changed portable selection")
	}
	for _, card := range s.selection.Cards {
		for _, asset := range card.Assets {
			r := httptest.NewRequest("GET", asset.URI, nil)
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 32))
			r.Header.Set("Range", "bytes=2-7")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 206 || w.Body.String() != "ntheti" {
				t.Fatal(w.Code, w.Body.String())
			}
			head := request(s, "HEAD", asset.URI, strings.Repeat("s", 32))
			if head.Code != 200 || head.Body.Len() != 0 {
				t.Fatal(head.Code, head.Body.String())
			}
		}
	}
	if e = catalogfs.WriteFile(filepath.Join(game, "game.yaml"), []byte("title: changed\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if w := request(s, "GET", "/v1/selection", strings.Repeat("s", 32)); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestPackedLookupAndBackupPlatformExclusion(t *testing.T) {
	_, q, game, roms := lookupFixture(t)
	root := filepath.Dir(roms)
	data := filepath.Join(root, "catalog")
	catalogfstest.Pack(t, filepath.Dir(game))
	backup := filepath.Join(data, "platforms", "Sega - Game Gear.old")
	if e := os.MkdirAll(backup, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(backup, "_platform.yaml"), []byte("name: Sega - Game Gear\n"), 0644); e != nil {
		t.Fatal(e)
	}
	busy := filepath.Join(data, "platforms", "Busy")
	if e := os.MkdirAll(busy, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(busy, "_platform.yaml"), []byte("name: Nintendo - Nintendo 64\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(busy, "catalog-conversion.pending"), []byte("incomplete"), 0644); e != nil {
		t.Fatal(e)
	}
	s, e := NewLookup(Options{Token: strings.Repeat("s", 32), Export: exporter.Options{DataRoot: data, ProfilePath: filepath.Join(root, "profile.yaml")}})
	if e != nil {
		t.Fatal(e)
	}
	w := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Offline prose") {
		t.Fatal(w.Code, w.Body.String())
	}
	caps := lookupRequest(s, "GET", "/v1/platforms", strings.Repeat("s", 32), nil)
	if caps.Code != 200 || strings.Contains(caps.Body.String(), ".old") || strings.Contains(caps.Body.String(), "Busy") {
		t.Fatal(caps.Code, caps.Body.String())
	}
}
