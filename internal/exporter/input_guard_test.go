package exporter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyInputCheckUsesStampsWithoutReadingPayloadOrHeader(t *testing.T) {
	for _, chd := range []bool{false, true} {
		t.Run(map[bool]string{false: "hashed-cartridge", true: "chd-header"}[chd], func(t *testing.T) {
			options, _, payload := fixture(t)
			name := "one.z64"
			if chd {
				options, _, payload = chdExportFixture(t)
				name = "Game (USA).chd"
			} else {
				put(t, filepath.Join(options.ROMRoot, name), payload)
			}
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(options.ROMRoot, name)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Deliberately destroy the payload/header while preserving all checked facts.
			// Success here proves apply does not silently inspect these bytes again.
			changed := make([]byte, len(payload))
			for i := range changed {
				changed[i] = 0xff
			}
			put(t, path, changed)
			if err = os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err = CheckInputs(plan); err != nil {
				t.Fatal("ROM bytes reread during apply validation", err)
			}
			if err = os.Chtimes(path, before.ModTime().Add(time.Second*2), before.ModTime().Add(time.Second*2)); err != nil {
				t.Fatal(err)
			}
			if err = CheckInputs(plan); err == nil {
				t.Fatal("missed ordinary stat drift")
			}
		})
	}
}
func TestInputGuardDetectsDirectoryMembershipAndCancellation(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "Game", "nested", "one.bin"), []byte("one"))
	input := ExportInput{ROM: "Game", Kind: "catalog", Catalog: &CatalogObservation{Directory: true, Files: []CatalogFile{{Name: "nested/one.bin"}}}}
	guard, err := CaptureInputGuard(root, input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(root, "Game", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "Game", "nested", "two.bin"), []byte("two"))
	later := before.ModTime().Add(time.Second * 2)
	if err = os.Chtimes(filepath.Join(root, "Game", "nested"), later, later); err != nil {
		t.Fatal(err)
	}
	if err = guard.Check(context.Background()); err == nil {
		t.Fatal("missed membership drift")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = guard.Check(ctx); err != context.Canceled {
		t.Fatal("lost cancellation", err)
	}
}
