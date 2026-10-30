package exporter

import (
	"os"
	"path/filepath"
	"rommeta/internal/launchbox"
	"testing"
)

func TestLaunchBoxExportPackageMergesIntoLibrary(t *testing.T) {
	options, _, payload := fixture(t)
	options.Target = "launchbox"
	put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(t.TempDir(), "LaunchBox")
	if err = os.MkdirAll(filepath.Join(library, "Data", "Platforms"), 0o755); err != nil {
		t.Fatal(err)
	}
	merged, err := launchbox.Build(launchbox.Options{PackageRoot: plan.Out, LaunchBoxRoot: library})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Changes) != 1 || merged.Changes[0].Action != "add" || merged.Changes[0].Media != 4 {
		t.Fatalf("%+v", merged.Changes)
	}
	if _, err = merged.Apply(); err != nil {
		t.Fatal(err)
	}
	copied := *plan
	copied.Out = library
	native := readLaunchBox(t, &copied)
	if len(native.Games) != 1 || len(native.Versions) != 1 {
		t.Fatal(native)
	}
	again, err := launchbox.Build(launchbox.Options{PackageRoot: plan.Out, LaunchBoxRoot: library})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Files) != 0 {
		t.Fatal("real export package is not idempotent", again.Files)
	}
}
