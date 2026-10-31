package exporter

import (
	"os"
	"path/filepath"
	launchbox "rommeta/internal/frontend"
	"strings"
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
	merged, err := launchbox.Build(launchbox.Options{Target: "launchbox", PackageRoot: plan.Out, Destination: library})
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
	again, err := launchbox.Build(launchbox.Options{Target: "launchbox", PackageRoot: plan.Out, Destination: library})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Files) != 0 {
		t.Fatal("real export package is not idempotent", again.Files)
	}
}

func TestLaunchBoxReexportRepairsFormerUnmatchedDiscs(t *testing.T) {
	o, g, data := chdExportFixture(t)
	o.Target = "launchbox"
	if err := os.Remove(filepath.Join(o.ROMRoot, "Game (USA).chd")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(g, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA) (Disc 1): {}\n  Game (USA) (Disc 2): {}\n"))
	for _, n := range []string{"I", "II"} {
		put(t, filepath.Join(o.ROMRoot, "Game (USA) (Disc "+n+").chd"), data)
	}
	old, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the prior exporter: unknown records generated independent input IDs.
	for i := range old.Records {
		r := &old.Records[i]
		r.Status = "unmatched"
		r.Game = ""
		r.GameTitle = ""
		r.Release = ""
		r.MetadataRelease = ""
		r.TitleContext = nil
		r.Fields = map[string]Value{"title": {Value: strings.TrimSuffix(filepath.Base(r.ROM), ".chd"), Source: "filename"}}
	}
	if err = ProjectTarget(old, "launchbox"); err != nil {
		t.Fatal(err)
	}
	if err = Apply(old, nil); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(t.TempDir(), "LaunchBox")
	if err = os.MkdirAll(filepath.Join(library, "Data", "Platforms"), 0755); err != nil {
		t.Fatal(err)
	}
	options := launchbox.Options{Target: "launchbox", PackageRoot: old.Out, Destination: library}
	legacy, err := launchbox.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Apply(); err != nil {
		t.Fatal(err)
	}
	o.Out = filepath.Join(t.TempDir(), "corrected")
	current, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(current, nil); err != nil {
		t.Fatal(err)
	}
	options.PackageRoot = current.Out
	repaired, err := launchbox.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(repaired.Conflicts) != 0 || len(repaired.Changes) != 1 || repaired.Changes[0].Consolidated != 2 {
		t.Fatalf("real manifest repair failed: %+v", repaired)
	}
	if _, err = repaired.Apply(); err != nil {
		t.Fatal(err)
	}
	copy := *current
	copy.Out = library
	native := readLaunchBox(t, &copy)
	if len(native.Games) != 1 || len(native.Versions) != 1 {
		t.Fatal("obsolete cards or discs retained", native)
	}
	again, err := launchbox.Build(options)
	if err != nil || len(again.Files) != 0 {
		t.Fatalf("repair not idempotent: %+v %v", again, err)
	}
}
