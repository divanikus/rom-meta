package igdb

import (
	"rommeta/internal/catalogcache"
	"testing"
)

func TestArcadeSearchUsesRegionalTitlesNotSetIDs(t *testing.T) {
	game := catalogGameFromCached(catalogcache.Game{Platform: "Arcade", Title: "Aero Fighters 2 Sonic Wings 2", DefaultRelease: "sonicwi2", Releases: []catalogcache.Release{{Name: "sonicwi2", MAMESet: "sonicwi2", Title: "Aero Fighters 2 / Sonic Wings 2 (NGM-075)"}}})
	terms := searchTerms(game)
	if !contains(terms, "Aero Fighters 2") || !contains(terms, "Sonic Wings 2") || contains(terms, "sonicwi2") {
		t.Fatalf("%#v", terms)
	}
	for _, platform := range []string{"Arcade", "SNES"} {
		candidate := apiGame{Name: "Aero Fighters 2", Platforms: []apiNamed{{Name: platform}}}
		score := scoreCandidate(candidate, game, terms)
		if (score >= minimumMatchScore) != (platform == "Arcade") {
			t.Fatalf("%s score=%d", platform, score)
		}
	}
}

func TestArcadeHasheousIDsCannotBypassVariantGuards(t *testing.T) {
	for _, tc := range []struct {
		name, title, family string
		tags                []string
		candidate           string
		want                bool
	}{
		{"original", "After Burner", "Arcade - Sega - X Board", nil, "After Burner", true},
		{"wrong unofficial", "After Burner", "Arcade - Sega - X Board", nil, "After Burner (bootleg)", false},
		{"port mismatch", "After Burner (Mega-Tech, SMS based)", "Arcade - Sega - Mega-Tech", nil, "After Burner", false},
		{"port confirmed", "After Burner (Mega-Tech, SMS based)", "Arcade - Sega - Mega-Tech", nil, "After Burner (Mega-Tech)", true},
		{"bootleg mismatch", "After Burner (bootleg)", "Arcade - Other", []string{"bootleg"}, "After Burner", false},
		{"bootleg confirmed", "After Burner (bootleg)", "Arcade - Other", []string{"bootleg"}, "After Burner (bootleg)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := catalogGameFromCached(catalogcache.Game{Platform: "Arcade", ArcadePlatform: tc.family, Title: tc.title, Releases: []catalogcache.Release{{Name: "set", MAMESet: "set", Title: tc.title, Tags: tc.tags}}})
			candidate := apiGame{Name: tc.candidate, Platforms: []apiNamed{{Name: "Arcade"}}}
			score := scoreHasheousCandidate(candidate, game, searchTerms(game), nil, nil)
			if (score >= minimumMatchScore) != tc.want {
				t.Fatalf("score=%d", score)
			}
		})
	}
}
