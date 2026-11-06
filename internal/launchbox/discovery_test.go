package launchbox

import (
	"errors"
	"path/filepath"
	"rommeta/internal/platformnames"
	"testing"
)

func TestVerifiedDiscoveryUsesSnapshotAndPreservesUnresolvedCards(t *testing.T) {
	for _, available := range []bool{true, false} {
		root := t.TempDir()
		game := filepath.Join(root, "platforms", "Future Vendor - New System", "Alpha", "game.yaml")
		original := "title: Alpha\nreleases:\n  Alpha (USA):\n    title: Alpha\n"
		writeFileForTest(t, game, original)
		name := "Other System"
		if available {
			name = "New System"
		}
		snapshot := filepath.Join(root, "Metadata.zip")
		writeZipForTest(t, snapshot, map[string]string{"Metadata.xml": "<LaunchBox><Game><DatabaseID>1</DatabaseID><Name>Alpha</Name><Platform>" + name + "</Platform></Game></LaunchBox>"})
		result, err := Import(ImportOptions{DataRoot: root, ZipPath: snapshot, Apply: true, VerifyPlatform: true})
		if available {
			if err != nil || result.MatchedReleaseCount != 1 {
				t.Fatal(result, err)
			}
		} else {
			var unresolved *platformnames.ResolutionError
			if !errors.As(err, &unresolved) || readFileForTest(t, game) != original {
				t.Fatal("unresolved platform changed card", err)
			}
		}
	}
}
