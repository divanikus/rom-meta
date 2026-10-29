package hasheous

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"rommeta/internal/catalogcache"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/taskprogress"
)

type lookupTask struct {
	game     catalogGame
	requests []lookupRequest
}

// Only the scanner touches the catalog cache. The bounded queue holds at most
// one waiting game per worker; writes, counters and callbacks are serialized.
type importPipeline struct {
	options        ImportOptions
	settings       sourceSettings
	client         *http.Client
	limiter        *rateLimiter
	ctx            context.Context
	cancel         context.CancelFunc
	tasks          chan lookupTask
	workers        sync.WaitGroup
	workerProgress map[int]ImportProgress
	progressDone   chan struct{}
	mu             sync.Mutex
	result         ImportResult
	err            error
	completed      int
	total          int
}

func newImportPipeline(options ImportOptions, settings sourceSettings, client *http.Client, result ImportResult) *importPipeline {
	ctx, cancel := context.WithCancel(context.Background())
	p := &importPipeline{
		options: options, settings: settings, client: client,
		limiter: newRateLimiter(settings.RateLimitPerSecond),
		ctx:     ctx, cancel: cancel, result: result,
		workerProgress: make(map[int]ImportProgress),
	}
	if options.Apply {
		workers := settings.Concurrency
		if options.Limit > 0 && workers > options.Limit {
			workers = options.Limit
		}
		p.result.Concurrency = workers
		p.tasks = make(chan lookupTask, workers)
		p.workers.Add(workers)
		for worker := range workers {
			go p.work(worker + 1)
		}
		if options.Progress != nil {
			p.progressDone = make(chan struct{})
			go p.refreshProgress()
		}
	}
	return p
}

func (p *importPipeline) catalogProgress(snapshot taskprogress.Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if snapshot.Stage == "reading catalog" {
		p.total = snapshot.Total
		// Reading ahead is not completion: pending HTTP requests and writes
		// must finish before the progress bar can reach 100%.
		snapshot.Current = p.completed
		if p.pending() > 0 {
			snapshot.Stage = "matching"
			snapshot.Item = p.activity()
		}
	}
	if p.options.Apply && p.options.Progress != nil {
		p.options.Progress(ImportProgress{
			Stage: snapshot.Stage, Current: snapshot.Current, Total: snapshot.Total,
			Game: snapshot.Item, Queries: p.result.QueriedLookupCount, Matches: p.result.MatchedGameCount,
		})
	}
}

func (p *importPipeline) submit(game catalogGame) error {
	var requests []lookupRequest
	if p.options.SkipWithoutEvidence && (p.options.ForceOverwrite || !catalogcache.HasSource(game.Sources, "hasheous")) {
		requests = fingerprintLookupRequests(game)
	}
	p.mu.Lock()
	if p.err != nil {
		err := p.err
		p.mu.Unlock()
		return err
	}
	if err := p.ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	skipped := false
	if !p.options.ForceOverwrite && catalogcache.HasSource(game.Sources, "hasheous") {
		p.result.SkippedExistingCount++
		skipped = true
	} else if p.options.SkipWithoutEvidence && len(requests) == 0 {
		p.result.SkippedWithoutEvidenceCount++
		skipped = true
	} else if p.options.Limit > 0 && p.result.SelectedGameCount >= p.options.Limit {
		p.result.SkippedByLimitCount++
		skipped = true
	}
	if skipped {
		p.completed++
		if p.pending() > 0 {
			p.reportActivity()
		} else {
			reportStage(p.options.Progress, p.options.Apply, "reading catalog", p.completed, p.total, game.Platform+" / "+game.Title)
		}
		p.mu.Unlock()
		return nil
	}
	p.result.SelectedGameCount++
	p.mu.Unlock()

	if !p.options.SkipWithoutEvidence {
		requests = fingerprintLookupRequests(game)
	}
	p.mu.Lock()
	p.result.PlannedLookupCount += len(requests)
	p.mu.Unlock()
	if !p.options.Apply {
		return nil
	}
	game.Fingerprints = nil
	game.Sources = nil
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case p.tasks <- lookupTask{game: game, requests: requests}:
		return nil
	}
}

func (p *importPipeline) work(worker int) {
	defer p.workers.Done()
	for task := range p.tasks {
		if p.ctx.Err() != nil {
			return
		}
		matches, _, err := lookupBestMatches(p.ctx, p.client, p.limiter, p.settings, task.game, task.requests, func(stage string, current, total int) {
			p.updateWorker(worker, task.game, stage, current, total)
		})
		if err == nil {
			p.updateWorker(worker, task.game, "writing metadata", 0, 0)
		}
		p.complete(worker, task, matches, err)
	}
}

func (p *importPipeline) complete(worker int, task lookupTask, matches []matchedGame, lookupErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil {
		return
	}
	if lookupErr != nil {
		if isRateLimitStop(lookupErr) {
			p.err = lookupErr
			p.cancel()
		}
		p.result.FailedLookupCount++
		if len(p.result.Failures) < 10 {
			p.result.Failures = append(p.result.Failures, ImportFailure{
				Game: task.game.Title, Query: requestLabels(task.requests), Error: lookupErr.Error(),
			})
		}
	} else {
		metadata := renderMetadata(matches)
		if len(matches) == 0 {
			reason := "no hash match"
			if len(task.requests) == 0 {
				reason = "no usable fingerprints"
			}
			metadata = renderNotFoundMetadata(reason)
		}
		path := filepath.Join(task.game.Path, "game.yaml")
		if err := sourceyaml.WriteSourceMetadata(path, "hasheous", metadata); err != nil {
			p.err = fmt.Errorf("write Hasheous metadata for %s: %w", path, err)
			p.cancel()
			return
		}
		p.result.WrittenFileCount++
		if len(matches) == 0 {
			p.result.NotFoundGameCount++
		} else {
			p.result.MatchedGameCount++
			p.result.GameFileCount++
		}
	}
	p.completed++
	p.workerProgress[worker] = ImportProgress{Worker: worker, Workers: p.result.Concurrency, Stage: "waiting"}
	if p.options.Progress != nil {
		p.options.Progress(p.workerProgress[worker])
	}
	reportProgress(p.options.Progress, p.options.Apply, p.completed, p.total, task.game.Platform+" / "+task.game.Title, p.result.QueriedLookupCount, p.result.MatchedGameCount)
}

// These helpers run with mu held; callbacks never overlap the scanner or writer.
func (p *importPipeline) pending() int {
	return p.result.SelectedGameCount - p.result.WrittenFileCount - p.result.FailedLookupCount
}

func (p *importPipeline) activity() string {
	active := 0
	for _, snapshot := range p.workerProgress {
		if snapshot.Stage != "waiting" {
			active++
		}
	}
	return fmt.Sprintf("%d active, %d queued", active, max(0, p.pending()-active))
}

func (p *importPipeline) reportActivity() {
	if p.options.Apply && p.options.Progress != nil {
		p.options.Progress(ImportProgress{
			Stage: "matching", Current: p.completed, Total: p.total, Game: p.activity(),
			Queries: p.result.QueriedLookupCount, Matches: p.result.MatchedGameCount,
		})
	}
}

func (p *importPipeline) updateWorker(worker int, game catalogGame, stage string, current, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil {
		return
	}
	if stage == "querying" {
		p.result.QueriedLookupCount++
	}
	snapshot := ImportProgress{
		Worker: worker, Workers: p.result.Concurrency, Stage: stage,
		Game: game.Platform + " / " + game.Title, Request: current, Requests: total,
	}
	p.workerProgress[worker] = snapshot
	if p.options.Progress != nil {
		p.options.Progress(snapshot)
		p.reportActivity()
	}
}

func (p *importPipeline) refreshProgress() {
	defer close(p.progressDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			if p.ctx.Err() == nil {
				for worker := 1; worker <= p.result.Concurrency; worker++ {
					if snapshot, ok := p.workerProgress[worker]; ok && snapshot.Stage != "waiting" {
						p.options.Progress(snapshot)
					}
				}
			}
			p.mu.Unlock()
		}
	}
}

func (p *importPipeline) finish(scanErr error) (ImportResult, error) {
	defer p.cancel()
	if scanErr != nil {
		p.cancel()
	}
	if p.tasks != nil {
		close(p.tasks)
		p.workers.Wait()
	}
	p.cancel()
	if p.progressDone != nil {
		<-p.progressDone
	}
	if p.err != nil {
		return p.result, p.err
	}
	return p.result, scanErr
}
