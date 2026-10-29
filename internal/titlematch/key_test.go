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
		{"Legacy", "Legacy+", false},
		{"Game+", "Game++", false},
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
