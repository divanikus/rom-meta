package frontend

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativePackageApplyWritesOnePassAndKeepsIndependentAssets(t *testing.T) {
	options := setup(t, "", game(a, "Example", "/roms/example.chd", ""))
	relative := "Images/" + platform + "/Box - Front/" + a + "-01.png"
	put(t, options.PackageRoot, relative, "synthetic cover")
	source := filepath.Join(options.PackageRoot, filepath.FromSlash(relative))
	target := filepath.Join(options.Destination, filepath.FromSlash(relative))
	plan := mustBuild(t, options)
	counts := []int{}
	plan.Progress = func(stage, name string, current, total int) {
		if stage == "prepare" || stage == "install" {
			t.Fatal("obsolete separate phase", stage)
		}
		if stage == "write" {
			counts = append(counts, current)
			if total != len(plan.Files) {
				t.Fatal("wrong total")
			}
		}
	}
	err := plan.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != len(plan.Files)+1 || counts[0] != 0 || counts[len(counts)-1] != len(plan.Files) {
		t.Fatal("write was not one completed-file pass", counts)
	}
	original, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(original, installed) {
		t.Fatal("package asset was linked instead of independently copied")
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
			err := plan.Apply()
			if err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatal("missing verification failure", err)
			}
			if string(get(t, options.Destination, platformFile)) != before {
				t.Fatal("failed verification replaced metadata")
			}
			if failAsset {
				if _, err = os.Stat(filepath.Join(options.Destination, filepath.FromSlash(relative))); !os.IsNotExist(err) {
					t.Fatal("partial failed asset left behind", err)
				}
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

// Check the complete output tree, including preexisting backup directories.
func TestApplyCreatesOnlyPlannedFiles(t *testing.T) {
	for _, target := range []string{"launchbox", "es-de"} {
		for _, direct := range []bool{false, true} {
			for _, legacyBackup := range []bool{false, true} {
				name := target
				if direct {
					name += "/direct"
				} else {
					name += "/package"
				}
				if legacyBackup {
					name += "/existing-backup"
				}
				t.Run(name, func(t *testing.T) {
					o := esdeSetup(t, "")
					if target == "launchbox" {
						o = setup(t, "", game(a, "Example", "/roms/example.chd", ""))
					}
					if direct {
						o.Source = virtualSource(t, o.PackageRoot)
						for name, file := range o.Source.Files {
							if file.Data == nil {
								file.Data = get(t, o.PackageRoot, name)
								o.Source.Files[name] = file
							}
						}
						o.Source.Check = func() error { return nil }
						o.PackageRoot = filepath.Join(t.TempDir(), "unmaterialized")
					}
					snapshot := func() map[string]string {
						files := map[string]string{}
						err := filepath.WalkDir(o.Destination, func(name string, entry os.DirEntry, err error) error {
							if err != nil {
								return err
							}
							if entry.IsDir() {
								return nil
							}
							rel, err := filepath.Rel(o.Destination, name)
							if err != nil {
								return err
							}
							files[filepath.ToSlash(rel)] = string(get(t, o.Destination, rel))
							return nil
						})
						if err != nil {
							t.Fatal(err)
						}
						return files
					}
					if legacyBackup {
						put(t, o.Destination, "Backups/RomMeta/previous/changes.json", "existing owner file")
					}
					before := snapshot()
					plan := mustBuild(t, o)
					expected := make(map[string]bool)
					for name := range before {
						expected[name] = true
					}
					for _, name := range plan.Files {
						if filepath.IsAbs(name) {
							name, _ = filepath.Rel(o.Destination, name)
						}
						expected[filepath.ToSlash(name)] = true
					}
					checks := 0
					plan.Progress = func(stage, name string, current, total int) {
						if stage == "check files" && current == total {
							checks++
						}
					}
					if err := plan.Apply(); err != nil {
						t.Fatal(err)
					}
					if checks != 1 {
						t.Fatal("destination validation repeated", checks)
					}
					after := snapshot()
					if len(after) != len(expected) {
						t.Fatal("unplanned output files", after)
					}
					for name := range after {
						if !expected[name] {
							t.Fatal("unplanned output", name)
						}
					}
					for name, data := range before {
						planned := false
						for _, changed := range plan.Files {
							if filepath.ToSlash(changed) == name {
								planned = true
							}
						}
						if !planned && after[name] != data {
							t.Fatal("unrelated file changed", name)
						}
					}
					if !legacyBackup {
						if _, err := os.Stat(filepath.Join(o.Destination, "Backups")); !os.IsNotExist(err) {
							t.Fatal("implicit backup directory", err)
						}
					}
					repeat := mustBuild(t, o)
					if len(repeat.Files) != 0 {
						t.Fatal("repeat has changes", repeat.Files)
					}
					if err := repeat.Apply(); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(after, snapshot()) {
						t.Fatal("unchanged export wrote files")
					}
				})
			}
		}
	}
}

func TestSourceCheckDriftRefusesBeforeAnyDestinationWrite(t *testing.T) {
	o, relative, source := directFixture(t, []byte("asset"), "")
	file := source.Files[relative]
	file.Data = []byte("asset")
	source.Files[relative] = file
	source.Check = func() error {
		put(t, o.Destination, platformFile, wrap("<!-- owner changed XML -->"))
		return nil
	}
	plan := mustBuild(t, o)
	if err := plan.Apply(); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("missed native drift", err)
	}
	if _, err := os.Stat(filepath.Join(o.Destination, "Images")); !os.IsNotExist(err) {
		t.Fatal("wrote media before validation", err)
	}
	if _, err := os.Stat(filepath.Join(o.Destination, "Backups")); !os.IsNotExist(err) {
		t.Fatal("created backup on failed apply", err)
	}
}
