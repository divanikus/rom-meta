package taskprogress

import (
	"sync"
	"time"
)

// TransferSnapshot counts files independently of catalog discovery progress.
type TransferSnapshot struct {
	Queued, Downloaded, Existing, Failed int
	Bytes                                int64
	Elapsed                              time.Duration
	Done                                 bool
}
type Transfers struct {
	mu      sync.Mutex
	started time.Time
	state   TransferSnapshot
	workers map[int]transferBytes
	output  Func
}
type transferBytes struct {
	item    string
	current int64
}

func NewTransfers(output Func) *Transfers {
	return &Transfers{started: time.Now(), workers: map[int]transferBytes{}, output: output}
}
func (t *Transfers) Progress(s Snapshot) {
	t.mu.Lock()
	if s.Stage == "preparing" {
		delete(t.workers, s.Worker)
	}
	if s.Stage == "downloading" && s.BytesCurrent >= 0 {
		prev := t.workers[s.Worker]
		if prev.item != s.Item || s.BytesCurrent < prev.current {
			prev = transferBytes{item: s.Item}
		}
		t.state.Bytes += s.BytesCurrent - prev.current
		t.workers[s.Worker] = transferBytes{item: s.Item, current: s.BytesCurrent}
	}
	state := t.state
	state.Elapsed = time.Since(t.started)
	s.Transfers = &state
	t.mu.Unlock()
	if t.output != nil {
		t.output(s)
	}
}
func (t *Transfers) Record(status string) {
	t.mu.Lock()
	switch status {
	case "queued":
		t.state.Queued++
	case "downloaded":
		t.state.Downloaded++
	case "existing":
		t.state.Existing++
	case "failed":
		t.state.Failed++
	}
	t.mu.Unlock()
	t.Progress(Snapshot{Stage: "files"})
}
func (t *Transfers) Done() {
	t.mu.Lock()
	t.state.Done = true
	t.mu.Unlock()
	t.Progress(Snapshot{Stage: "files"})
}
