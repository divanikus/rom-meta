package exporter

import (
	"context"
	"errors"
	"os"
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

func TestLocalScanResolvesMediaBeforeNextInputAndBeforeCompletion(t *testing.T) {
	for _, removeAt := range []string{"next-input", "completion"} {
		t.Run(removeAt, func(t *testing.T) {
			options, game, payload := fixture(t)
			put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
			put(t, filepath.Join(options.ROMRoot, "two.z64"), payload)
			video := filepath.Join(game, "media", "gameplay-video.usa.emumovies.mp4")
			removed := false
			options.Progress = func(stage, name string, current, total int) {
				if stage != "ROMs" || removed || total <= 0 {
					return
				}
				if removeAt == "next-input" && name == "two.z64" || removeAt == "completion" && current == total {
					if err := os.Remove(video); err != nil {
						t.Fatal(err)
					}
					removed = true
				}
			}
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if !removed || len(plan.Records) != 2 {
				t.Fatal("test did not reach input processing")
			}
			_, first := plan.Records[0].Media["video"]
			_, second := plan.Records[1].Media["video"]
			if !first || second != (removeAt == "completion") {
				t.Fatalf("metadata/media resolved in a later pass: first=%t second=%t", first, second)
			}
		})
	}
}
