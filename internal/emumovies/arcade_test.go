package emumovies

import "testing"

func TestMAMEMediaKeepsExactSetRelease(t *testing.T) {
	game := catalogGame{Title: "Game", DefaultRelease: "parent", Releases: []releaseInfo{{Name: "parent", MAMESet: "parent"}, {Name: "renamed", MAMESet: "clone_a"}}}
	release, score, _ := scoreVideoAgainstGame("clone_a", game)
	if release != "renamed" || score != 200 {
		t.Fatalf("%s %d", release, score)
	}
	for _, stem := range []string{"clonea", "Game", "other"} {
		if _, score, _ := scoreVideoAgainstGame(stem, game); score != 0 {
			t.Fatalf("unsafe match %s", stem)
		}
	}
	if got := newMediaGameIndex([]catalogGame{game}).candidates("clone_a.mp4"); len(got) != 1 {
		t.Fatal("authority ID missing from media index")
	}
}
