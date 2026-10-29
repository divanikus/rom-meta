package titlematch

import "testing"

func TestIdentitySymbols(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		same        bool
	}{
		{"Street Fighter Zero 2", "Street Fighter Zero 2'", false},
		{"Zero 2' (Japan)", "Zero 2 (Japan)", false},
		{"Zero 2’", "Zero 2′", true},
		{"Game A + Game B", "Game A / Game B", true},
		{"Game +", "Game", false},
		{"Game 'Quoted Subtitle'", "Game Quoted Subtitle", true},
		{"Legacy", "Legacy+", false},
		{"Game+", "Game++", false},
		{"Game ++", "Game++", true},
		{"Game＋", "Game+", true},
		{"Ys I & II", "Ys III", false},
		{"The Game: Part 2", "The Game - Part 2", true},
		{"Warriors' Dreams", "Warriors Dreams", true},
	} {
		if got := Key(tc.left) == Key(tc.right); got != tc.same {
			t.Errorf("%q / %q: equal %v", tc.left, tc.right, got)
		}
	}
}
