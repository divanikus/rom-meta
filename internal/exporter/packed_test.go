package exporter

import (
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/catalogfstest"
	"testing"
)

func TestPackedExportEqualsLooseWithoutEmuMovies(t *testing.T) {
	options, game, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example.z64"), payload)
	profile(t, &options, "schema_version: 1\ntarget: es-de\nselection:\n  media:\n    cover: {sources: [launchbox]}\n    video: {sources: [launchbox]}\n")
	a, e := Build(options)
	if e != nil {
		t.Fatal(e)
	}
	if e = Apply(a, nil); e != nil {
		t.Fatal(e)
	}
	before := treeHashes(t, options.Out)
	catalogfstest.Pack(t, filepath.Dir(game))
	options.Out = filepath.Join(filepath.Dir(options.Out), "packed-output")
	b, e := Build(options)
	if e != nil {
		t.Fatal(e)
	}
	if e = Apply(b, nil); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(a.Records, b.Records) {
		t.Fatal("portable selection changed")
	}
	after := treeHashes(t, options.Out)
	// Manifest records the intentionally different package destination.
	delete(before, "manifest.json")
	delete(after, "manifest.json")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("frontend payloads differ", before, after)
	}
	if _, e = os.Stat(filepath.Join(options.Out, "media", "video")); e == nil {
		t.Fatal("restricted video unexpectedly exported")
	}
}
