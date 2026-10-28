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
