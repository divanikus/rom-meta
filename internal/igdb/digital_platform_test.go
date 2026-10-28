package igdb

import "testing"

func TestDigitalCatalogCandidatesUseIGDBHardwarePlatform(t *testing.T) {
	for _, tc := range []struct{ platform, title, release, source string }{
		{"Nintendo - Wii (Digital) (CDN)", "Bonsai Barber (WiiWare)", "Bonsai Barber (USA) (WiiWare)", "Wii"},
		{"Nintendo - Wii U (Digital) (CDN)", "Example Adventure", "Example Adventure (USA)", "Wii U"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			game := catalogGame{Platform: tc.platform, Title: tc.title, DefaultRelease: tc.release}
			title := tc.title
			if tc.source == "Wii" {
				title = "Bonsai Barber"
			}
			candidate := apiGame{Name: title, Platforms: []apiNamed{{Name: tc.source}}}
			available := []string{"Wii", "Wii U", "PlayStation 3"}
			if got := scoreCandidateWithPlatforms(candidate, game, searchTerms(game), available, nil); got < minimumMatchScore {
				t.Fatalf("valid digital candidate rejected: score=%d", got)
			}
			candidate.Platforms = []apiNamed{{Name: "PlayStation 3"}}
			if got := scoreCandidateWithPlatforms(candidate, game, searchTerms(game), available, nil); got != 0 {
				t.Fatalf("foreign hardware candidate accepted: score=%d", got)
			}
		})
	}
}
