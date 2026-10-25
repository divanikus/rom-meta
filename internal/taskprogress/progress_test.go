package taskprogress

import (
	"io"
	"strings"
	"testing"
)

func TestNewReaderReportsByteProgress(t *testing.T) {
	var snapshots []Snapshot
	progress := Func(func(snapshot Snapshot) {
		snapshots = append(snapshots, snapshot)
	})

	reader := NewReader(strings.NewReader("download"), progress, "download", 8, "file.zip")
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if string(data) != "download" {
		t.Fatalf("unexpected data: %q", data)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected byte progress snapshots")
	}
	last := snapshots[len(snapshots)-1]
	if last.Stage != "download" || last.BytesCurrent != 8 || last.BytesTotal != 8 || last.Item != "file.zip" {
		t.Fatalf("unexpected final snapshot: %#v", last)
	}
}

func TestFinishBytesUsesNegativeSentinel(t *testing.T) {
	var got Snapshot
	progress := Func(func(snapshot Snapshot) {
		got = snapshot
	})

	progress.FinishBytes("download", "file.zip")

	if got.Stage != "download" || got.BytesCurrent != -1 || got.BytesTotal != -1 || got.Item != "file.zip" {
		t.Fatalf("unexpected finish snapshot: %#v", got)
	}
}

func TestNewWorkerReaderReportsWorkerByteProgress(t *testing.T) {
	var snapshots []Snapshot
	progress := Func(func(snapshot Snapshot) {
		snapshots = append(snapshots, snapshot)
	})

	reader := NewWorkerReader(strings.NewReader("download"), progress, "download", 2, 4, 8, "file.zip")
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if string(data) != "download" {
		t.Fatalf("unexpected data: %q", data)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected worker byte progress snapshots")
	}
	last := snapshots[len(snapshots)-1]
	if last.Stage != "download" || last.Worker != 2 || last.Workers != 4 || last.BytesCurrent != 8 || last.BytesTotal != 8 || last.Item != "file.zip" {
		t.Fatalf("unexpected final snapshot: %#v", last)
	}
}

func TestFinishWorkerBytesUsesNegativeSentinel(t *testing.T) {
	var got Snapshot
	progress := Func(func(snapshot Snapshot) {
		got = snapshot
	})

	progress.FinishWorkerBytes("download", 2, 4, "file.zip")

	if got.Stage != "download" || got.Worker != 2 || got.Workers != 4 || got.BytesCurrent != -1 || got.BytesTotal != -1 || got.Item != "file.zip" {
		t.Fatalf("unexpected finish snapshot: %#v", got)
	}
}
