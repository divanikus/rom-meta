package frontend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeApplyWritesOnePassAndOnlyReusesOwnedAssets(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "public-package-copy", true: "owned-package-reuse"}[reuse], func(t *testing.T) {
			options := setup(t, "", game(a, "Example", "/roms/example.chd", ""))
			relative := "Images/" + platform + "/Box - Front/" + a + "-01.png"
			put(t, options.PackageRoot, relative, "synthetic cover")
			source := filepath.Join(options.PackageRoot, filepath.FromSlash(relative))
			target := filepath.Join(options.Destination, filepath.FromSlash(relative))
			if reuse {
				probe := filepath.Join(options.Destination, "link-probe")
				if err := os.Link(source, probe); err != nil {
					t.Skip("test filesystem does not support same-volume hard links")
				}
				if err := os.Remove(probe); err != nil {
					t.Fatal(err)
				}
			}
			options.ReuseAssets = reuse
			plan := mustBuild(t, options)
			var counts []int
			plan.Progress = func(stage, name string, current, total int) {
				if stage == "prepare" || stage == "install" {
					t.Fatal("obsolete separate write phase", stage)
				}
				if stage == "write" {
					counts = append(counts, current)
					if total != len(plan.Files) {
						t.Fatal("wrong write total")
					}
				}
			}
			backup, err := plan.Apply()
			if err != nil {
				t.Fatal(err)
			}
			if backup == "" {
				t.Fatal("missing ordinary recovery receipt")
			}
			if len(counts) != len(plan.Files)+1 || counts[0] != 0 || counts[len(counts)-1] != len(plan.Files) {
				t.Fatal("write is not one completed-file pass", counts)
			}
			original, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}
			installed, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(original, installed) != reuse {
				t.Fatal("public package was consumed/aliased or owned asset was copied twice")
			}
			if _, err = os.Stat(filepath.Join(backup, "changes.json")); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(options.Destination)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if len(entry.Name()) >= 15 && entry.Name()[:15] == ".rommeta-merge-" {
					t.Fatal("aggregate prepare directory remains")
				}
			}
		})
	}
}

func TestSinglePassHashFailurePreservesMetadataAndCleansPartialFile(t *testing.T) {
	for _, failAsset := range []bool{true, false} {
		t.Run(map[bool]string{true: "asset", false: "metadata"}[failAsset], func(t *testing.T) {
			options := setup(t, "", game(a, "Example", "/roms/example.chd", ""))
			relative := "Images/" + platform + "/Box - Front/" + a + "-01.png"
			put(t, options.PackageRoot, relative, "synthetic cover")
			before := string(get(t, options.Destination, platformFile))
			plan := mustBuild(t, options)
			for i := range plan.files {
				if (failAsset && plan.files[i].source != "") || (!failAsset && plan.files[i].path == platformFile) {
					plan.files[i].hash = strings.Repeat("a", 64)
				}
			}
			backup, err := plan.Apply()
			if err == nil || backup == "" || !strings.Contains(err.Error(), "changed") {
				t.Fatal("missing recovery on failed verification", backup, err)
			}
			if string(get(t, options.Destination, platformFile)) != before {
				t.Fatal("failed verification replaced metadata")
			}
			if failAsset {
				if _, err = os.Stat(filepath.Join(options.Destination, filepath.FromSlash(relative))); !os.IsNotExist(err) {
					t.Fatal("partial failed asset left behind", err)
				}
			}
			if _, err = os.Stat(filepath.Join(backup, "changes.json")); err != nil {
				t.Fatal(err)
			}
			if err = filepath.WalkDir(options.Destination, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if strings.HasPrefix(entry.Name(), ".rommeta-write-") {
					t.Fatal("temporary replacement leaked", path)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
