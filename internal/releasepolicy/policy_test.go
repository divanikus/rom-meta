package releasepolicy

import "testing"

func TestChoosePrefersOriginalOverHigherPriorityDigitalReissue(t *testing.T) {
	names := []string{"Final Soldier (Japan)", "Final Soldier (World) (Wii U Virtual Console)"}
	if got := Choose(names, nil, nil); got != "Final Soldier (Japan)" {
		t.Fatalf("Choose() = %q", got)
	}
}

func TestChooseUsesConfiguredRegionPriority(t *testing.T) {
	names := []string{"Castlevania III (USA)", "Akumajou Densetsu (Japan)"}
	if got := Choose(names, []string{"japan", "usa"}, nil); got != "Akumajou Densetsu (Japan)" {
		t.Fatalf("Choose() = %q", got)
	}
}

func TestChooseRecognizesConfiguredRegionAlias(t *testing.T) {
	names := []string{"Game (US)", "Game (JAP)"}
	groups := map[string][]string{"japan": {"JAP"}, "usa": {"US"}}
	if got := Choose(names, []string{"japan", "usa"}, groups); got != "Game (JAP)" {
		t.Fatalf("Choose() = %q", got)
	}
}

func TestChooseKeepsFullOriginalAheadOfExplicitSupplementAndReissue(t *testing.T) {
	tests := []struct {
		name, original, other string
	}{
		{"special disc", "Game - Full Title (Japan) (Disc 1)", "Game (Japan) (Special Disc)"},
		{"bonus disc", "Game (Europe)", "Game (USA) (Bonus Disc)"},
		{"compilation extraction", "Game (Japan)", "Game (USA, Europe) (Atari Anthology)"},
		{"Japanese demo qualifier", "Game (Europe)", "Game (USA) (Taikenban)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Choose([]string{test.other, test.original}, nil, nil); got != test.original {
				t.Fatalf("Choose() = %q, want original %q", got, test.original)
			}
			if Preference(test.original, nil, nil) <= Preference(test.other, nil, nil) {
				t.Fatal("fold preference disagrees with default selection")
			}
		})
	}
	// A title word alone does not identify a supplementary release.
	if got := Choose([]string{"Special Disc (USA)", "Other (Europe)"}, nil, nil); got != "Special Disc (USA)" {
		t.Fatalf("game title was mistaken for a qualifier: %q", got)
	}
}

func TestChooseDoesNotTreatLanguagesAsRegions(t *testing.T) {
	japan := "A Japanese Title (Japan) (En,Ja,Fr,De,Es,It,Ko)"
	europe := "Western Title (Europe, Australia) (En,Ja,Fr,De,Es,It,Ko)"
	groups := map[string][]string{"europe": {"France", "Germany", "Spain", "Italy", "Australia"}, "japan": {"JA"}}
	if got := Choose([]string{japan, europe}, nil, groups); got != europe {
		t.Fatalf("languages overrode release region: %q", got)
	}
	if Preference(japan, nil, groups) >= Preference(europe, nil, groups) {
		t.Fatal("multilingual Japanese release acquired a European rank")
	}
	if got := Choose([]string{"A (Unknown) (Fr,De)", "B (Japan)"}, nil, groups); got != "B (Japan)" {
		t.Fatalf("unknown region acquired a language-derived rank: %q", got)
	}
}

func TestRetailDefaultOutranksAllRecognizedDevelopmentStages(t *testing.T) {
	for _, marker := range []string{"Trial", "Tentou-you Taikenban", "Cheheompan", "Trade Demo", "Beta 2", "Prototype", "Sample", "Preview", "Debug", "Promo", "Promotional"} {
		names := []string{"Game (USA) (" + marker + ")", "Game (Japan)"}
		if got := Choose(names, []string{"usa", "japan"}, nil); got != names[1] {
			t.Fatal(marker, got)
		}
	}
	if got := Choose([]string{"Demo Racer (USA)", "Demo Racer (Japan)"}, nil, nil); got != "Demo Racer (USA)" {
		t.Fatal(got)
	}
}

func TestOfficialReleaseOutranksHigherRegionPirateDefault(t *testing.T) {
	for _, marker := range []string{"Pirate", "Aftermarket", "Unl", "Homebrew"} {
		original := "Game (Japan)"
		clone := "Game (USA) (" + marker + ")"
		if got := Choose([]string{clone, original}, nil, nil); got != original {
			t.Fatalf("%s: %q", marker, got)
		}
	}
	a, b := "Game (USA) (Pirate)", "Game (Japan) (Pirate)"
	if got := Choose([]string{b, a}, nil, nil); got != a {
		t.Fatal("all-unofficial region priority changed", got)
	}
	if got := Choose([]string{"Pirate Adventure (USA)", "Pirate Adventure (Japan)"}, nil, nil); got != "Pirate Adventure (USA)" {
		t.Fatal("title word treated as unofficial tag", got)
	}
}
