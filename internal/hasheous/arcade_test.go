package hasheous

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"rommeta/internal/catalogcache"
	"strings"
	"testing"
)

func TestMAMEComponentRequestsAreScopedAndBounded(t *testing.T) {
	sha := strings.Repeat("a", 40)
	game := catalogGame{DefaultRelease: "local", MAMEReleases: []catalogcache.Release{{Name: "local", MAMESet: "clone"}}, Fingerprints: []catalogcache.Fingerprint{
		{Release: "other", PayloadKind: "rom-component", Name: "outside", SHA1: sha},
		{Release: "local", PayloadKind: "disk", Name: "disk", SHA1: sha},
		{Release: "local", PayloadKind: "rom-component", Name: "crc-only", CRC32: "12345678"},
		{Release: "local", PayloadKind: "rom-component", Name: "program", SHA1: sha, CRC32: "12345678"},
		{Release: "local", PayloadKind: "rom-component", Name: "duplicate", SHA1: sha},
	}}
	requests := fingerprintLookupRequests(game)
	if len(requests) != 1 || len(requests[0].Inputs) != 1 || requests[0].Inputs[0].MAMESet != "clone" || requests[0].Inputs[0].CRC != "" {
		t.Fatalf("%#v", requests)
	}
	game.MAMEReleases = nil
	if requests := fingerprintLookupRequests(game); len(requests) != 0 {
		t.Fatalf("unscoped component requests: %#v", requests)
	}
}

func TestMAMERepresentativeHashesDoNotStopAtBIOSAlternatives(t *testing.T) {
	game := catalogGame{MAMEReleases: []catalogcache.Release{{Name: "cart", MAMESet: "cart"}}}
	for _, digit := range []string{"a", "b", "c", "d", "e"} {
		game.Fingerprints = append(game.Fingerprints, catalogcache.Fingerprint{Release: "cart", PayloadKind: "rom-component", Name: "bios", Size: 131072, SHA1: strings.Repeat(digit, 40)})
	}
	game.Fingerprints = append(game.Fingerprints, catalogcache.Fingerprint{Release: "cart", PayloadKind: "rom-component", Name: "game-data", Size: 2097152, SHA1: strings.Repeat("f", 40)})
	requests := fingerprintLookupRequests(game)
	if len(requests) != 1 || len(requests[0].Inputs) != 5 || requests[0].Inputs[0].Name != "game-data" {
		t.Fatalf("%#v", requests)
	}
	if game.Fingerprints[0].Name != "bios" {
		t.Fatal("modified catalog order")
	}
}

func TestMAMESharedChipCannotSelectDifferentSet(t *testing.T) {
	sha := strings.Repeat("a", 40)
	inputs := []lookupInput{{MAMESet: "clone", Release: "local", SHA1: sha, Label: "mame:clone/program"}}
	game := catalogGame{Title: "Game", MAMEReleases: []catalogcache.Release{{Name: "local", MAMESet: "clone"}}}
	response := lookupResponse{ID: 1, Name: "Game", Signatures: map[string][]signatureResponse{"MAMEArcade": {{Game: signatureGame{Name: "Game", Description: "clone", System: "Arcade"}, ROM: signatureROM{SHA1: sha}}}}, Metadata: []metadataResponse{{Source: "IGDB", ID: "42", Status: "Mapped"}}}
	for _, tc := range []struct {
		name, set, system, hash string
		want                    bool
	}{
		{"verified", "clone", "Arcade", sha, true},
		{"shared chip", "parent", "Arcade", sha, false},
		{"home", "clone", "Home", sha, false},
		{"wrong hash", "clone", "Arcade", strings.Repeat("b", 40), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response.Signatures["MAMEArcade"][0].Game.Description = tc.set
			response.Signatures["MAMEArcade"][0].Game.System = tc.system
			response.Signatures["MAMEArcade"][0].ROM.SHA1 = tc.hash
			match := matchFromResponse(response, game, inputs)
			if (match.Score > 0) != tc.want {
				t.Fatalf("%#v", match)
			}
			if tc.want && (match.IGDBID != 42 || match.Release != "local") {
				t.Fatalf("wrong identity: %#v", match)
			}
		})
	}
}

func TestMAMELookupFiltersResponseAndSendsOnlySHA1(t *testing.T) {
	sha := strings.Repeat("a", 40)
	inputs := []lookupInput{{MAMESet: "clone", Release: "local", SHA1: sha, Label: "mame:clone/program"}}
	response := func(id int, set string) lookupResponse {
		return lookupResponse{ID: id, Name: "Game", Signatures: map[string][]signatureResponse{"MAMEArcade": {{Game: signatureGame{Name: "Game", Description: set, System: "Arcade"}, ROM: signatureROM{SHA1: sha}}}}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("returnSources") != "MAMEArcade" {
			t.Error("missing source restriction")
		}
		var body []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 1 || len(body[0]) != 1 || body[0]["SHA1"] != sha {
			t.Errorf("unexpected request: %#v", body)
		}
		json.NewEncoder(w).Encode([]lookupResponse{response(1, "parent"), response(2, "clone")})
	}))
	defer server.Close()
	game := catalogGame{Title: "Game", MAMEReleases: []catalogcache.Release{{Name: "local", MAMESet: "clone"}}}
	matches, queries, err := lookupBestMatches(context.Background(), server.Client(), newRateLimiter(1000), sourceSettings{APIBaseURL: server.URL}, game, []lookupRequest{{Release: "local", Inputs: inputs}}, nil)
	if err != nil || queries != 1 || len(matches) != 1 || matches[0].HasheousID != "2" {
		t.Fatalf("%#v %d %v", matches, queries, err)
	}
	if got := unambiguousMAMEResponses([]lookupResponse{response(1, "clone"), response(2, "clone")}, inputs); len(got) != 0 {
		t.Fatal("ambiguous IDs accepted")
	}
}
