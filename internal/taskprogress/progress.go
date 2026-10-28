package taskprogress

import "io"

// Snapshot describes progress for a long-running task stage.
type Snapshot struct {
	Transfers    *TransferSnapshot
	Stage        string
	Current      int
	Total        int
	Item         string
	BytesCurrent int64
	BytesTotal   int64
	Worker       int
	Workers      int
}

// Func receives progress snapshots.
type Func func(Snapshot)

// Update emits a progress snapshot when a callback is configured.
func (fn Func) Update(stage string, current, total int, item string) {
	if fn == nil {
		return
	}
	fn(Snapshot{Stage: stage, Current: current, Total: total, Item: item})
}

// UpdateBytes emits byte-level progress when a callback is configured.
func (fn Func) UpdateBytes(stage string, current, total int64, item string) {
	if fn == nil {
		return
	}
	fn(Snapshot{Stage: stage, BytesCurrent: current, BytesTotal: total, Item: item})
}

// UpdateWorker emits status for one worker in a parallel task.
func (fn Func) UpdateWorker(stage string, worker, workers int, item string) {
	if fn == nil {
		return
	}
	fn(Snapshot{Stage: stage, Worker: worker, Workers: workers, Item: item})
}

// UpdateWorkerBytes emits byte-level progress for one worker in a parallel task.
func (fn Func) UpdateWorkerBytes(stage string, worker, workers int, current, total int64, item string) {
	if fn == nil {
		return
	}
	fn(Snapshot{Stage: stage, Worker: worker, Workers: workers, BytesCurrent: current, BytesTotal: total, Item: item})
}

// FinishBytes marks an unknown-size byte progress item as complete.
func (fn Func) FinishBytes(stage, item string) {
	if fn == nil {
		return
	}
	fn(Snapshot{Stage: stage, BytesCurrent: -1, BytesTotal: -1, Item: item})
}

// FinishWorkerBytes marks an unknown-size worker byte progress item as complete.
func (fn Func) FinishWorkerBytes(stage string, worker, workers int, item string) {
	if fn == nil {
		return
	}
	fn(Snapshot{Stage: stage, Worker: worker, Workers: workers, BytesCurrent: -1, BytesTotal: -1, Item: item})
}

// NewReader wraps an io.Reader and reports byte-level read progress.
func NewReader(reader io.Reader, progress Func, stage string, total int64, item string) io.Reader {
	if progress == nil || reader == nil {
		return reader
	}
	return &readerProgress{
		reader:   reader,
		progress: progress,
		stage:    stage,
		total:    total,
		item:     item,
	}
}

// NewWorkerReader wraps an io.Reader and reports byte-level read progress for one worker.
func NewWorkerReader(reader io.Reader, progress Func, stage string, worker, workers int, total int64, item string) io.Reader {
	if progress == nil || reader == nil {
		return reader
	}
	return &readerProgress{
		reader:   reader,
		progress: progress,
		stage:    stage,
		worker:   worker,
		workers:  workers,
		total:    total,
		item:     item,
	}
}

type readerProgress struct {
	reader   io.Reader
	progress Func
	stage    string
	worker   int
	workers  int
	total    int64
	item     string
	current  int64
}

func (reader *readerProgress) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.current += int64(count)
		if reader.worker > 0 {
			reader.progress.UpdateWorkerBytes(reader.stage, reader.worker, reader.workers, reader.current, reader.total, reader.item)
		} else {
			reader.progress.UpdateBytes(reader.stage, reader.current, reader.total, reader.item)
		}
	}
	return count, err
}
