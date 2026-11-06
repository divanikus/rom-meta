package taskprogress

import (
	"sync"
	"testing"
)

func TestTransfersCountsConcurrentFilesAndUnknownSizeBytes(t *testing.T) {
	var final TransferSnapshot
	tracker := NewTransfers(func(s Snapshot) {
		if s.Transfers.Done {
			final = *s.Transfers
		}
	})
	progress := Func(tracker.Progress)
	var workers sync.WaitGroup
	for worker := 1; worker <= 3; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			tracker.Record("queued")
			progress.UpdateWorker("preparing", worker, 3, "a")
			progress.UpdateWorkerBytes("downloading", worker, 3, 4, -1, "a")
			progress.UpdateWorkerBytes("downloading", worker, 3, 7, -1, "a")
			progress.FinishWorkerBytes("downloading", worker, 3, "a")
			tracker.Record("downloaded")
			tracker.Record("queued")
			progress.UpdateWorker("preparing", worker, 3, "a")
			progress.UpdateWorkerBytes("downloading", worker, 3, 2, -1, "a")
			tracker.Record("failed")
		}(worker)
	}
	workers.Wait()
	tracker.Record("existing")
	tracker.Done()
	if final.Downloaded != 3 || final.Failed != 3 || final.Existing != 1 || final.Queued != 6 || final.Bytes != 27 || !final.Done {
		t.Fatalf("incorrect totals: %#v", final)
	}
}

func TestDiscoveryTotalStaysUnknownUntilEnumerationFinishes(t *testing.T) {
	var last Snapshot
	tracker := NewTransfers(func(s Snapshot) { last = s })
	tracker.Progress(Snapshot{Stage: "scanning", Current: 3, Total: 20})
	tracker.Record("queued")
	tracker.Record("downloaded")
	if last.Transfers.DiscoveryComplete || last.Transfers.DiscoveryCurrent != 3 || last.Transfers.DiscoveryTotal != 20 {
		t.Fatalf("lost discovery progress: %#v", last.Transfers)
	}
	tracker.Record("queued")
	tracker.FinishDiscovery()
	if !last.Transfers.DiscoveryComplete || last.Transfers.Queued != 2 || last.Transfers.Downloaded != 1 || last.Transfers.Done {
		t.Fatalf("final total unavailable while download is active: %#v", last.Transfers)
	}
	tracker.Progress(Snapshot{Stage: "extracting", Current: 1, Total: 5})
	if last.Current != 1 || last.Total != 5 || last.Stage != "extracting" {
		t.Fatalf("transfers overwrote later stage: %#v", last)
	}
}

func TestFailedDiscoveryDoesNotClaimAFinalTotal(t *testing.T) {
	var last Snapshot
	tracker := NewTransfers(func(s Snapshot) { last = s })
	tracker.Record("queued")
	tracker.Record("failed")
	tracker.Done()
	if last.Transfers.DiscoveryComplete {
		t.Fatal("an aborted scan is not a full inventory")
	}
}
