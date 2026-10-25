package catalogfold

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/dat"
	"strings"
	"testing"
)

func TestPackedFoldAndDATRefreshPreservePacks(t *testing.T) {
	root := t.TempDir()
	platform := "Sony - PlayStation 2"
	path := contentDAT(t, "Orbit (USA)", "Tsuki (Japan)")
	if _, e := dat.Import(path, dat.ImportOptions{DataRoot: root, Apply: true}); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "platforms", platform)
	for _, folder := range []string{"Orbit", "Tsuki"} {
		region := "USA"
		if folder == "Tsuki" {
			region = "Japan"
		}
		writeFoldGame(t, root, platform, folder, folder+" ("+region+")", folder, "42")
		writeFoldFile(t, filepath.Join(dir, folder, "media", "box-front.launchbox.jpg"), folder)
	}
	addRegionalTestAlias(t, root, platform, "Orbit", "Orbit", "Tsuki", "Japan")
	catalogfstest.Pack(t, dir)
	r, e := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if e != nil || r.AppliedGroupCount != 1 {
		t.Fatal(r, e)
	}
	kept := filepath.Join(dir, r.Groups[0].Keep.Folder)
	b, e := catalogfs.ReadFile(filepath.Join(kept, "game.yaml"))
	if e != nil || !strings.Contains(string(b), "42") {
		t.Fatal(string(b), e)
	}
	if _, e = os.Stat(filepath.Join(kept, "metadata.zip")); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(kept, "game.yaml")); !os.IsNotExist(e) {
		t.Fatal("loose YAML created", e)
	}
	// A separate exact DAT reimport must keep the packed format and media.
	fresh := t.TempDir()
	p := contentDAT(t, "Orbit (USA)")
	if _, e = dat.Import(p, dat.ImportOptions{DataRoot: fresh, Apply: true}); e != nil {
		t.Fatal(e)
	}
	pd := filepath.Join(fresh, "platforms", platform)
	catalogfstest.Pack(t, pd)
	if _, e = dat.Import(p, dat.ImportOptions{DataRoot: fresh, Apply: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(pd, "Orbit", "metadata.zip")); e != nil {
		t.Fatal(e)
	}
	if _, e = catalogfs.ReadFile(filepath.Join(pd, "Orbit", "game.yaml")); e != nil {
		t.Fatal(e)
	}
}
