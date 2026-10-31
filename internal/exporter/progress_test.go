package exporter

import (
	"context"
	"errors"
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
