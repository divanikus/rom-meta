package screenscraper

import (
	"net/http"
	"strings"
	"testing"

	"rommeta/internal/catalogcache"
	"rommeta/internal/sourceyaml"
)

func TestNamedNonGameFingerprintMatching(t *testing.T) {
	for _, tc := range []struct {
		name, title, release string
		hash, accept         bool
	}{
		{"named utility", "GameShark", "GameShark (USA)", true, true},
		{"punctuation", "Tool: Editor", "Tool - Editor (USA)", true, true},
		{"wrong utility", "Other Tool", "Other Tool (USA)", true, false},
		{"wrong issue", "GameShark 2", "GameShark 2 (USA)", true, false},
		{"wrong hash", "GameShark", "GameShark (USA)", false, false},
		{"missing ROM evidence", "GameShark", "GameShark (USA)", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serviceTitle := "GameShark"
			if tc.name == "punctuation" {
				serviceTitle = "Tool - Editor"
			}
			returnedHash := strings.Repeat("1", 32)
			if !tc.hash {
				returnedHash = strings.Repeat("2", 32)
			}
			game := catalogGame{Title: tc.title, DefaultRelease: tc.release, Fingerprints: []catalogcache.Fingerprint{{Release: tc.release, Name: tc.release + ".gb", MD5: strings.Repeat("1", 32), Size: 1024}}}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Query().Get("md5") == "" {
					t.Fatal("unexpected title fallback")
				}
				if tc.name == "missing ROM evidence" {
					return testResponse(200, `{"response":{"jeu":{"id":42,"noms":[{"region":"wor","text":"ZZZ(notgame):GameShark"}]}}}`), nil
				}
				return testResponse(200, `{"response":{"jeu":{"id":42,"noms":[{"region":"wor","text":"ZZZ(notgame):`+serviceTitle+`"}],"roms":[{"rommd5":"`+returnedHash+`","romsize":1024}]}}}`), nil
			})}
			match, _, found, err := searchBestMatch(client, sourceSettings{APIBaseURL: "https://example.test", RateLimitPerSecond: 1000}, game, []string{tc.title}, "3", newSearchPacer(1000), nil)
			if err != nil || found != tc.accept || calls != 1 {
				t.Fatalf("found=%t err=%v calls=%d", found, err, calls)
			}
			if found {
				match.Metadata.MediaURLs = map[string][]sourceyaml.MediaCandidate{"box_front": {{URL: "https://example.test/cover.png"}}}
				rendered := renderMetadata(match)
				if !strings.Contains(rendered, "ZZZ(notgame):") || !strings.Contains(rendered, "cover.png") {
					t.Fatalf("lost service marker or media: %s", rendered)
				}
			}
		})
	}
}

func TestNamedNonGameCannotAcceptGenericAlias(t *testing.T) {
	game := catalogGame{Title: "Example Game"}
	for _, title := range []string{"#NONGAME", "ZZZ(notgame):#PROGRAMS", "ZZZ(notgame):Demo Disc", "ZZZ(notgame):pas un jeu"} {
		metadata := gameMetadata{Title: "ZZZ(notgame):Example Game", AlternateTitles: []string{title}}
		if namedNonGameMatchesCatalog(metadata, game, "") {
			t.Fatalf("generic alias accepted: %q", title)
		}
	}
}

func TestNamedNonGameTitleOnlyRejected(t *testing.T) {
	game := catalogGame{Title: "GameShark", DefaultRelease: "GameShark (USA)"}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return testResponse(200, `{"response":{"jeu":{"id":42,"noms":[{"region":"wor","text":"ZZZ(notgame):GameShark"}]}}}`), nil
	})}
	_, _, found, err := searchBestMatch(client, sourceSettings{APIBaseURL: "https://example.test", RateLimitPerSecond: 1000}, game, []string{"GameShark"}, "3", newSearchPacer(1000), nil)
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}
