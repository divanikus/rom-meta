package catalogyaml

import "testing"

func TestMatchIncludesExactSingleAndListReleaseScopes(t *testing.T) {
	for _, test := range []struct {
		text string
		want bool
	}{
		{"title: Game", true}, {"release: A", true}, {"release: B", false},
		{"releases: [B, A]", true}, {"releases: [B]", false}, {"releases: []", false},
		{"release: B\nreleases: [A]", true},
	} {
		doc, err := Parse([]byte(test.text))
		if err != nil {
			t.Fatal(err)
		}
		if got := MatchIncludesRelease(doc.Root(), "A"); got != test.want {
			t.Fatal(test.text, got)
		}
	}
}
