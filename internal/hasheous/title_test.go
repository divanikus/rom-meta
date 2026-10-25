package hasheous

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchFromResponseServiceTitles(t *testing.T) {
	for _, tc := range []struct {
		name, response, signature, wantTitle string
	}{
		{"shared bucket", "ZZZ(notgame):#NONGAME", "ZZZ(notgame):#NONGAME", ""},
		{"bare bucket", " #NoNgAmE ", " #nongame ", ""},
		{"other category", "ZZZ(notgame):##Demos##", "ZZZ(notgame):##Demos##", ""},
		{"use signature", "ZZZ(notgame):#NONGAME", "Example Game", "Example Game"},
		{"use response without trusting links", "Example Game", "ZZZ(notgame):#NONGAME", "Example Game"},
		{"no signatures", "ZZZ(notgame):#NONGAME", "", ""},
		{"no response title", "", "ZZZ(notgame):#NONGAME", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := lookupResponse{
				ID: 42, Name: tc.response,
				Signatures: map[string][]signatureResponse{
					"ExampleSource": {{Game: signatureGame{Name: tc.signature}, ROM: signatureROM{MD5: "test-hash"}}},
				},
				Metadata: []metadataResponse{
					{Source: "IGDB", ID: "123", Status: "Mapped", Link: "https://example.invalid/game"},
					{Source: "TheGamesDB", ID: "456", Status: "Mapped"},
					{Source: "RetroAchievements", ID: "789", Status: "Mapped"},
				},
			}
			match := matchFromResponse(response, catalogGame{Title: "Example Game", DefaultRelease: "Example Game (World)"},
				[]lookupInput{{Release: "Example Game (World)", MD5: "test-hash", Label: "md5:test"}})
			if match.Title != tc.wantTitle {
				t.Fatalf("title = %q, want %q", match.Title, tc.wantTitle)
			}
			if match.IGDBID != 0 || match.TheGamesDBID != "" || match.RetroAchievementsID != "" || len(match.References) != 0 || match.Score != 180 {
				t.Fatalf("service title must not promote linked metadata: %#v", match)
			}
			if match.HasheousID != "42" || match.Release != "Example Game (World)" || match.MatchedQuery != "md5:test" || !reflect.DeepEqual(match.SignatureSources, []string{"ExampleSource"}) {
				t.Fatalf("hash identity was lost: %#v", match)
			}
			rendered := renderMetadata([]matchedGame{match})
			if strings.Contains(strings.ToLower(rendered), "notgame") || strings.Contains(strings.ToLower(rendered), "#nongame") {
				t.Fatalf("service title persisted: %s", rendered)
			}
			if tc.wantTitle == "" && strings.Contains(rendered, "title:") {
				t.Fatalf("empty title should be omitted: %s", rendered)
			}
		})
	}
}

func TestMatchFromResponseStableSignatureChoice(t *testing.T) {
	response := lookupResponse{
		ID: 42, Name: "Example Game",
		Signatures: map[string][]signatureResponse{
			"AService":  {{Game: signatureGame{Name: "ZZZ(notgame):#NONGAME"}}},
			" BSource ": {{Game: signatureGame{Name: " "}}, {Game: signatureGame{Name: "Example Game"}}},
			"ZSource":   {{Game: signatureGame{Name: "Different Name"}}},
		},
		Metadata: []metadataResponse{{Source: "IGDB", ID: "123", Status: "Mapped"}},
	}
	for i := 0; i < 100; i++ {
		match := matchFromResponse(response, catalogGame{Title: "Example Game"}, nil)
		if match.Title != "Example Game" || match.IGDBID != 123 || match.Score != 220 {
			t.Fatalf("unstable title or lost valid mapping: %#v", match)
		}
	}
}

func TestMatchFromResponseKeepsNamedUtility(t *testing.T) {
	match := matchFromResponse(lookupResponse{ID: 42, Name: "GameShark"}, catalogGame{Title: "GameShark"}, nil)
	if match.Title != "GameShark" {
		t.Fatalf("ordinary utility name should remain: %#v", match)
	}
}
