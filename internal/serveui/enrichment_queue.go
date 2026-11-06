package serveui

import (
	"errors"
	"fmt"
	"io/fs"
	"rommeta/internal/catalogedit"
	"time"
)

// Acceptance is independent of the current writer. One server-owned FIFO drains
// after other web/CLI writers finish; browser disconnect never cancels the queue.
func (s *server) beginEnrichment(id string, revision int) error {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[id]
	if op == nil || op.Kind != "enrichment" {
		return fmt.Errorf("enrichment plan is unavailable; prepare it again")
	}
	if op.State != "ready" {
		if op.State == "needs_review" {
			return fmt.Errorf("source settings changed; use Change selection to prepare again")
		}
		return nil
	}
	if op.Revision != revision {
		return fmt.Errorf("the plan changed; review the current operation")
	}
	for _, platform := range op.Enrichment.Platforms {
		options, _, _, err := s.enrichmentChoices(platform)
		if err != nil {
			return err
		}
		if options.Revision != op.stamp {
			return fmt.Errorf("source settings changed; use Change selection to review and prepare this task again")
		}
	}
	s.queueEnrichmentLocked(op)
	return nil
}

func (s *server) queueEnrichmentLocked(op *libraryOperation) {
	now := time.Now().UTC()
	op.State, op.Error, op.Updated, op.QueuedAt = "queued", "", now, &now
	if !s.enrichmentQueueRunning {
		s.enrichmentQueueRunning = true
		go s.drainEnrichmentQueue()
	}
}

func (s *server) drainEnrichmentQueue() {
	for {
		s.operationsMu.Lock()
		var next *libraryOperation
		for _, op := range s.operations {
			if op.Kind == "enrichment" && op.State == "queued" && (next == nil || op.QueuedAt.Before(*next.QueuedAt)) {
				next = op
			}
		}
		if next == nil {
			s.enrichmentQueueRunning = false
			s.operationsMu.Unlock()
			return
		}
		if !s.editMu.TryLock() {
			s.operationsMu.Unlock()
			time.Sleep(250 * time.Millisecond)
			continue
		}
		unlock, err := catalogedit.Lock(s.root)
		if err != nil {
			s.editMu.Unlock()
			if errors.Is(err, fs.ErrExist) {
				s.operationsMu.Unlock()
				time.Sleep(500 * time.Millisecond)
				continue
			}
			next.State, next.Error, next.Updated = "needs_review", "Cannot acquire library write access; check the server's folder permissions.", time.Now().UTC()
			s.operationsMu.Unlock()
			continue
		}
		options, config, registry, err := s.enrichmentChoices(next.Enrichment.Platforms[0])
		if err == nil && options.Revision != next.stamp {
			err = fmt.Errorf("source settings changed while queued; use Change selection to review this task again")
		}
		if err == nil {
			for _, platform := range next.Enrichment.Platforms[1:] {
				_, _, _, err = s.enrichmentChoices(platform)
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			next.State, next.Error, next.Updated = "needs_review", enrichmentText(config, err.Error()), time.Now().UTC()
			unlock()
			s.editMu.Unlock()
			s.operationsMu.Unlock()
			continue
		}
		next.State, next.Error, next.Updated = "enriching", "", time.Now().UTC()
		snapshot := cloneOperation(next)
		s.operationsMu.Unlock()
		func() { defer s.editMu.Unlock(); defer unlock(); s.runEnrichment(snapshot, config, registry) }()
	}
}
