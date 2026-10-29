package launchbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestForcedRefreshDistinguishesRegionalBaseAndPrime(t *testing.T) {
	root := t.TempDir()
	for _, title := range []string{"Street Fighter Zero 2", "Street Fighter Zero 2'"} {
		writeFileForTest(t, filepath.Join(root, "platforms", "Sega - Saturn", title, "game.yaml"), "title: "+title+"\nowner_note: keep\nreleases:\n  "+title+" (Japan):\n    title: "+title+"\n    regions: [Japan]\nsources:\n  launchbox: {matches: [{database_id: wrong}]}\n  igdb: {status: not_found}\n")
	}
	snapshot := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, snapshot, map[string]string{"Metadata.xml": `<LaunchBox>
 <Game><DatabaseID>base</DatabaseID><Name>Street Fighter Alpha 2</Name><Platform>Sega Saturn</Platform></Game>
 <Game><DatabaseID>prime</DatabaseID><Name>Street Fighter Zero 2'</Name><Platform>Sega Saturn</Platform></Game>
 <GameAlternateName><DatabaseID>base</DatabaseID><AlternateName>Street Fighter Zero 2</AlternateName><Region>Japan</Region></GameAlternateName>
 <GameAlternateName><DatabaseID>prime</DatabaseID><AlternateName>Street Fighter Zero 2'</AlternateName><Region>Japan</Region></GameAlternateName>
 </LaunchBox>`})
	for pass := 0; pass < 2; pass++ {
		got, err := Import(ImportOptions{DataRoot: root, ZipPath: snapshot, Apply: true, ForceOverwrite: true})
		if err != nil || got.MatchedReleaseCount != 2 || got.WrittenFileCount != 2 {
			t.Fatalf("pass %d: %#v %v", pass, got, err)
		}
		for _, tc := range []struct{ title, id string }{{"Street Fighter Zero 2", "base"}, {"Street Fighter Zero 2'", "prime"}} {
			text := readFileForTest(t, filepath.Join(root, "platforms", "Sega - Saturn", tc.title, "game.yaml"))
			if !strings.Contains(text, `database_id: "`+tc.id+`"`) || !strings.Contains(text, "owner_note: keep") || !strings.Contains(text, "igdb:") {
				t.Fatalf("wrong forced result: %s", text)
			}
		}
	}
}

func TestRegionalTitleRequiresExactRegionAndUniqueIdentity(t *testing.T) {
	for _, tc := range []struct {
		region            string
		duplicate, accept bool
	}{
		{"Japan", false, true}, {"USA", false, false}, {"", false, false}, {"World", false, false}, {"Japan", true, false},
	} {
		root := t.TempDir()
		snapshot := filepath.Join(t.TempDir(), "Metadata.zip")
		writeFileForTest(t, filepath.Join(root, "platforms", "Sega - Saturn", "Regional", "game.yaml"), "title: Regional Name\nreleases:\n  Regional Name ("+tc.region+"):\n    title: Regional Name\n    regions: ["+tc.region+"]\n")
		xml := `<Game><DatabaseID>a</DatabaseID><Name>Primary Name</Name><Platform>Sega Saturn</Platform></Game><GameAlternateName><DatabaseID>a</DatabaseID><AlternateName>Regional Name</AlternateName><Region>Japan</Region></GameAlternateName>`
		if tc.duplicate {
			xml += `<Game><DatabaseID>b</DatabaseID><Name>Other Name</Name><Platform>Sega Saturn</Platform></Game><GameAlternateName><DatabaseID>b</DatabaseID><AlternateName>Regional Name</AlternateName><Region>Japan</Region></GameAlternateName>`
		}
		writeZipForTest(t, snapshot, map[string]string{"Metadata.xml": "<LaunchBox>" + xml + "</LaunchBox>"})
		got, err := Import(ImportOptions{DataRoot: root, ZipPath: snapshot})
		if err != nil || (got.MatchedReleaseCount == 1) != tc.accept {
			t.Fatalf("%#v: %#v %v", tc, got, err)
		}
	}
}
