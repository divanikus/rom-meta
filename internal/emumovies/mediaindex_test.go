package emumovies

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func mediaIndexFixtures() []catalogGame {
	return []catalogGame{
		{Folder: "Example", Path: "/games/example", Title: "The Example", DefaultRelease: "Example (USA)", Aliases: []string{"Regional Name", "Another Alias"}, Releases: []releaseInfo{{Name: "Example (USA)", Title: "The Example", Regions: []string{"USA"}}, {Name: "Example (Japan)", Title: "Regional Name", Regions: []string{"Japan"}}}},
		{Folder: "Punctuation", Path: "/games/punctuation", Title: "A & B", Releases: []releaseInfo{{Name: "A & B (World)", Title: "A & B"}}},
		{Folder: "Version", Path: "/games/version", Title: "Example 2", Releases: []releaseInfo{{Name: "Example 2 (USA) (Demo)", Title: "Example 2", Regions: []string{"USA"}}}},
		{Folder: "Tie A", Path: "/games/tie-a", Title: "Shared", Releases: []releaseInfo{{Name: "Shared (World)", Title: "Shared"}}},
		{Folder: "Tie B", Path: "/games/tie-b", Title: "Shared", Releases: []releaseInfo{{Name: "Shared (World)", Title: "Shared"}}},
	}
}

func TestMediaIndexPreservesMatchesAndAmbiguity(t *testing.T) {
	games := mediaIndexFixtures()
	index := newMediaGameIndex(games)
	for _, tc := range []struct {
		name  string
		found bool
	}{
		{"Example (USA)", true}, {"Example (Japan)", true}, {"Example, The", true},
		{"Regional Name (Japan)", true}, {"Another Alias", true}, {"A and B (World)", true},
		{"Example 2 (USA) (Demo)", true}, {"Shared (World)", false}, {"Unknown Game", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := FTPEntry{Name: tc.name + ".mp4", Size: 123}
			before, beforeOK := matchVideoEntry("Example Platform", "videos", "HQ", "ftps", "example.test", entry, games)
			after, afterOK := matchVideoEntry("Example Platform", "videos", "HQ", "ftps", "example.test", entry, index.candidates(entry.Name))
			if beforeOK != tc.found || afterOK != beforeOK || !reflect.DeepEqual(before, after) {
				t.Fatalf("video match changed: before=%#v/%v after=%#v/%v", before, beforeOK, after, afterOK)
			}
			path := filepath.Join("art", tc.name+".png")
			pkg := artworkPackage{Platform: "Example Platform", MediaType: "box-front"}
			a, aOK := matchArtworkFile(pkg, "art", path, 123, games)
			b, bOK := matchArtworkFile(pkg, "art", path, 123, index.candidates(path))
			if aOK != tc.found || aOK != bOK || !reflect.DeepEqual(a, b) {
				t.Fatalf("artwork match changed: %#v/%v %#v/%v", a, aOK, b, bOK)
			}
		})
	}
}

func BenchmarkMediaCandidateSelection(b *testing.B) {
	games := mediaIndexFixtures()
	for i := 0; i < 500; i++ {
		title := fmt.Sprintf("Unrelated %d", i)
		games = append(games, catalogGame{Title: title, Path: title, Releases: []releaseInfo{{Name: title + " (USA)", Title: title}}})
	}
	index := newMediaGameIndex(games)
	entry := FTPEntry{Name: "Another Alias (USA).mp4", Size: 123}
	b.Run("full-scan", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchVideoEntry("Platform", "videos", "HQ", "ftps", "example.test", entry, games)
		}
	})
	b.Run("indexed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchVideoEntry("Platform", "videos", "HQ", "ftps", "example.test", entry, index.candidates(entry.Name))
		}
	})
}
