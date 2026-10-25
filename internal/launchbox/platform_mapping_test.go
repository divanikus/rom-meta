package launchbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFMTownsImportUsesProviderPlatformWithoutBorrowingFM7(t *testing.T) {
	for _, platform := range []string{"Fujitsu - FM Towns series", "Fujitsu - FM-Towns"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			gamePath := filepath.Join(root, "platforms", platform, "Example Game", "game.yaml")
			writeFileForTest(t, gamePath, `title: "Example Game"
releases:
  "Example Game (Japan)":
    title: "Example Game"
`)
			zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
			writeZipForTest(t, zipPath, map[string]string{
				"Metadata.xml": `<LaunchBox>
  <Game><DatabaseID>7</DatabaseID><Name>Example Game</Name><Platform>Fujitsu FM-7</Platform></Game>
  <Game><DatabaseID>253</DatabaseID><Name>Example Game</Name><Platform>Fujitsu FM Towns Marty</Platform></Game>
</LaunchBox>`,
			})
			result, err := Import(ImportOptions{DataRoot: root, ZipPath: zipPath, Apply: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.MatchedReleaseCount != 1 || result.WrittenFileCount != 1 {
				t.Fatalf("expected one matched release: %+v", result)
			}
			contents := readFileForTest(t, gamePath)
			if !strings.Contains(contents, `id: "253"`) || strings.Contains(contents, `id: "7"`) {
				t.Fatalf("wrong provider platform selected:\n%s", contents)
			}
		})
	}
}
