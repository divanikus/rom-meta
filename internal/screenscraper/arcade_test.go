package screenscraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"rommeta/internal/catalogcache"
	"testing"
)

func TestMAMEQueriesRequireExactSetAndArcadeSystem(t *testing.T) {
	for _, tc := range []struct {
		name, rom, system, parent string
		found                     bool
	}{
		{"child", "sonicwi2.zip", "142", "75", true},
		{"mame", "sonicwi2.zip", "75", "", true},
		{"wrong set", "mslug.zip", "142", "75", false},
		{"home platform", "sonicwi2.zip", "3", "", false},
		{"unproven", "", "142", "75", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				q := r.URL.Query()
				if q.Get("romnom") != "sonicwi2.zip" || q.Get("romtype") != "rom" || q.Get("md5") != "" || q.Get("systemeid") != "75" {
					t.Errorf("bad query: %v", q)
				}
				fmt.Fprintf(w, `{"response":{"jeu":{"id":"42","nom":"Aero Fighters 2","systeme":{"id":%q,"parentid":%q},"rom":{"romfilename":%q}}}}`, tc.system, tc.parent, tc.rom)
			}))
			defer server.Close()
			game := catalogGame{Title: "Aero Fighters 2", DefaultRelease: "local", MAMEReleases: []catalogcache.Release{{Name: "local", MAMESet: "sonicwi2"}}}
			match, _, found, err := searchBestMatch(server.Client(), sourceSettings{APIBaseURL: server.URL}, game, []string{"Aero Fighters 2"}, "75", newSearchPacer(1000), nil)
			if err != nil || found != tc.found || requests != 1 {
				t.Fatalf("found=%v requests=%d err=%v", found, requests, err)
			}
			if found && (match.Release != "local" || match.MatchedQuery != "sonicwi2.zip") {
				t.Fatalf("wrong provenance: %#v", match)
			}
		})
	}
}

func TestMAMEDefaultFirstAndNoCloneParent(t *testing.T) {
	game := catalogGame{DefaultRelease: "second", MAMEReleases: []catalogcache.Release{{Name: "first", MAMESet: "clone1"}, {Name: "second", MAMESet: "clone2"}}}
	q := mameQueries(game)
	if len(q) != 2 || q[0].ROMName != "clone2.zip" || q[1].ROMName != "clone1.zip" {
		t.Fatalf("%#v", q)
	}
	id, ok := screenScraperSystemID("Arcade", []screenScraperSystem{{ID: "142", Name: "Neo-Geo", Names: []string{"arcade"}}, {ID: "75", Name: "Mame", Names: []string{"arcade"}}}, nil)
	if !ok || id != "75" {
		t.Fatalf("ambiguous arcade aliases: %s", id)
	}
}
