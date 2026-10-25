package catalogfold

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeepRegionOutranksReleaseAndMetadataCounts(t *testing.T) {
	tests := []struct {
		name, preferred, other string
		priority               []string
	}{
		{"default USA", "Game (USA)", "Other (Japan)", nil},
		{"configured Japan", "Game (Japan)", "Other (USA)", []string{"japan", "usa"}},
		{"world before Europe", "Game (World)", "Other (Europe)", nil},
		{"known before unknown", "Game (Russia)", "Other (Unknown)", nil},
		{"original before demo", "Game (Japan)", "Other (USA) (Demo)", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			games := []catalogGame{
				{Folder: "Preferred [regional]", Releases: []block{{Key: test.preferred}}},
				{Folder: "Other", SourceCount: 20, Evidence: make([]Evidence, 20)},
			}
			for i := 0; i < 100; i++ {
				games[1].Releases = append(games[1].Releases, block{Key: fmt.Sprintf("%s (Rev %d)", test.other, i)})
			}
			if got := chooseKeep(games, []int{1, 0}, newRegionRules(test.priority, nil)); got != 0 {
				t.Fatalf("kept %s instead of preferred release %s", games[got].Folder, test.preferred)
			}
		})
	}
}

func TestKeepUsesMetadataOnlyWithinSameReleasePreference(t *testing.T) {
	games := []catalogGame{
		{Folder: "A", Releases: []block{{Key: "A (USA)"}}},
		{Folder: "B", SourceCount: 5, Releases: []block{{Key: "B (USA)"}}},
	}
	rules := newRegionRules(nil, nil)
	if got := chooseKeep(games, []int{0, 1}, rules); got != 1 {
		t.Fatalf("equally preferred releases should use metadata tie-break, got %d", got)
	}
	games[0].SourceCount = 5
	if got := chooseKeep(games, []int{1, 0}, rules); got != 0 {
		t.Fatalf("equal scores should use stable folder order, got %d", got)
	}
}

func TestFoldPrimaryAndDefaultReleaseStayTogether(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Dreamcast"
	writeFoldGame(t, root, platform, "Western Name", "Western Name (USA)", "Western Name", "100")
	writeFoldGame(t, root, platform, "Japanese Name", "Japanese Name (Japan)", "Japanese Name", "100")
	addRegionalTestAlias(t, root, platform, "Western Name", "Western Name", "Japanese Name", "Japan")
	japanesePath := filepath.Join(root, "platforms", platform, "Japanese Name", "game.yaml")
	original := readFoldFile(t, japanesePath)
	extra := ""
	for i := 1; i <= 3; i++ {
		extra += fmt.Sprintf("  \"Japanese Name (Japan) (Rev %d)\":\n    title: \"Japanese Name\"\n    regions: [Japan]\n", i)
	}
	writeFoldFile(t, japanesePath, strings.Replace(original, "releases:\n", "releases:\n"+extra, 1))
	result, err := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || result.Groups[0].Keep.Folder != "Western Name" {
		t.Fatalf("unexpected group: %#v", result.Groups)
	}
	merged, err := readGameYAML(catalogGame{Path: filepath.Join(root, "platforms", platform, "Western Name")})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Title != "Western Name" || len(merged.Releases) != 5 {
		t.Fatalf("title/releases = %q/%d", merged.Title, len(merged.Releases))
	}
	content := readFoldFile(t, filepath.Join(root, "platforms", platform, "Western Name", "game.yaml"))
	if !strings.Contains(content, "default_release: \"Western Name (USA)\"") {
		t.Fatalf("unexpected default release:\n%s", content)
	}
}

func TestMergedDefaultUsesKeptFolderWithinRegionTie(t *testing.T) {
	games := []catalogGame{
		{Folder: "Z Title", Title: "Z Title", Releases: []block{{Key: "Z Title (USA)", Text: "  \"Z Title (USA)\": {}\n"}}},
		{Folder: "A Title", Title: "A Title", Releases: []block{{Key: "A Title (USA)", Text: "  \"A Title (USA)\": {}\n"}}},
	}
	got := renderMergedGameYAML(games, newRegionRules(nil, nil))
	if !strings.Contains(got, "title: \"Z Title\"\ndefault_release: \"Z Title (USA)\"") {
		t.Fatalf("kept title and default release disagree:\n%s", got)
	}
}

func TestKeepEmptyBundleCannotOutrankDemo(t *testing.T) {
	games := []catalogGame{
		{Folder: "Empty", SourceCount: math.MaxInt / 100},
		{Folder: "Demo", Releases: []block{{Key: "Game (USA) (Demo)"}}},
	}
	if got := chooseKeep(games, []int{0, 1}, newRegionRules(nil, nil)); got != 1 {
		t.Fatalf("empty bundle won over a real release: %d", got)
	}
}
