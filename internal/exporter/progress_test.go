package exporter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolutionDoesNotCompleteProgressBeforeWorkSucceeds(t *testing.T) {
	var counts []int
	resolver := &ExportResolver{System: "saturn", Progress: func(stage, name string, current, total int) {
		if stage == "metadata/media" {
			counts = append(counts, current)
		}
	}}
	_, err := resolver.Resolve([]ExportInput{{ROM: "one.chd", Kind: "unsupported", Reason: "unsupported"}, {ROM: "one.chd", Kind: "unsupported", Reason: "duplicate"}}, Profile{})
	if err == nil || len(counts) != 1 || counts[0] != 0 {
		t.Fatal("failed work reported completion", counts, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver.Context = ctx
	if _, err = resolver.Resolve([]ExportInput{{ROM: "unread.chd"}}, Profile{}); !errors.Is(err, context.Canceled) {
		t.Fatal("resolution ignores cancellation", err)
	}
}

func TestInputCountingReportsScanBeforeDirectoryWalk(t *testing.T) {
	root := t.TempDir()
	observed := false
	options := Options{System: "n64", ROMRoot: root, Progress: func(stage, name string, current, total int) {
		if stage == "ROMs" && name == "Counting input files" {
			observed = true
			// The synthetic input appears only after startup is reported, so the
			// returned input demonstrates that both counting and observation follow it.
			put(t, filepath.Join(root, "Example.z64"), []byte("synthetic payload"))
		}
	}}
	inputs, err := ObserveExport(options)
	if err != nil || !observed || len(inputs) != 1 || !strings.HasSuffix(inputs[0].ROM, "Example.z64") {
		t.Fatalf("scan was silent before counting: %v %+v", err, inputs)
	}
}
