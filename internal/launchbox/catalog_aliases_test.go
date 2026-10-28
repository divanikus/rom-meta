package launchbox

import (
	"path/filepath"
	"testing"
)

func TestImportUsesDeclaredCatalogAliasWithinItsPlatform(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Nintendo - Family Computer Disk System (FDS)", "Regional Adventure", "game.yaml")
	writeFileForTest(t, game, "title: Regional Adventure\naliases: [Official Adventure]\nreleases:\n  Regional Adventure (Japan): {title: Regional Adventure}\n")
	source := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, source, map[string]string{"Metadata.xml": `<LaunchBox>
 <Game><DatabaseID>42</DatabaseID><Name>Official Adventure</Name><Platform>Nintendo Famicom Disk System</Platform></Game>
 <Game><DatabaseID>43</DatabaseID><Name>Official Adventure</Name><Platform>Nintendo Entertainment System</Platform></Game>
 </LaunchBox>`})
	result, err := Import(ImportOptions{DataRoot: root, ZipPath: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedReleaseCount != 1 {
		t.Fatalf("declared alias not matched: %+v", result)
	}
}

func TestCatalogAliasesKeepAmbiguityAndUnofficialGuards(t *testing.T) {
	index := map[string][]releaseMetadata{}
	for _, m := range []releaseMetadata{{DatabaseID: "1", Title: "First Adventure"}, {DatabaseID: "2", Title: "Second Adventure"}} {
		for _, key := range titleMatchKeys(m.Title) {
			index[key] = append(index[key], m)
		}
	}
	release := catalogRelease{Name: "Regional Adventure (Japan)", Title: "Regional Adventure"}
	if _, ok := matchMetadata(index, release, "Regional Adventure", "First Adventure", "Second Adventure"); ok {
		t.Fatal("conflicting aliases resolved arbitrarily")
	}
	release.Tags = []string{"Pirate"}
	if _, ok := matchMetadata(index, release, "Regional Adventure", "First Adventure"); ok {
		t.Fatal("official alias bypassed unofficial identity")
	}
}
