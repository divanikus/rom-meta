package igdb

import "testing"

func TestCandidateRequiresKnownMatchingPlatform(t *testing.T) {
	game := catalogGame{Platform: "Nintendo - Game Boy Advance", Title: "Example Game"}
	for _, tc := range []struct {
		name      string
		platforms []apiNamed
		wantMatch bool
	}{
		{"missing", nil, false},
		{"wrong", []apiNamed{{Name: "PlayStation 2"}}, false},
		{"matching", []apiNamed{{Name: "Game Boy Advance"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := apiGame{ID: 42, Name: "Example Game", Platforms: tc.platforms}
			for name, score := range map[string]int{
				"search":   scoreCandidate(candidate, game, []string{"Example Game"}),
				"hasheous": scoreHasheousCandidate(candidate, game, []string{"Example Game"}, nil, nil),
			} {
				if (score >= minimumMatchScore) != tc.wantMatch {
					t.Fatalf("%s score=%d, want match=%t", name, score, tc.wantMatch)
				}
			}
		})
	}
}
