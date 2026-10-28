package launchbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHomebrewReleaseTypeMatchesAftermarketAndSurvivesCache(t *testing.T) {
	for _, kind := range []string{"Homebrew", "Released", "Unreleased"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			game := filepath.Join(root, "platforms", "Atari - Atari Jaguar (J64)", "Orbit Rocks")
			writeFileForTest(t, filepath.Join(game, "game.yaml"), "title: Orbit Rocks\nreleases:\n  Orbit Rocks (World) (Proto) (Aftermarket) (Unl):\n    tags: [prototype, aftermarket, unlicensed]\n")
			snapshot := filepath.Join(t.TempDir(), "Metadata.zip")
			writeZipForTest(t, snapshot, map[string]string{"Metadata.xml": "<LaunchBox><Game><DatabaseID>123</DatabaseID><Name>Orbit Rocks</Name><Platform>Atari Jaguar</Platform><ReleaseType>" + kind + "</ReleaseType></Game></LaunchBox>"})
			for pass := 0; pass < 2; pass++ {
				result, err := Import(ImportOptions{DataRoot: root, ZipPath: snapshot, Apply: true, ForceOverwrite: true})
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if kind == "Homebrew" {
					want = 1
				}
				if result.MatchedReleaseCount != want {
					t.Fatalf("%s pass %d: %#v", kind, pass, result)
				}
				if want == 1 && !strings.Contains(readFileForTest(t, filepath.Join(game, "game.yaml")), `release_type: "Homebrew"`) {
					t.Fatal("provider classification not retained")
				}
			}
		})
	}
}
