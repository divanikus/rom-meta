package hasheous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rommeta/internal/catalogcache"
	"rommeta/internal/store"
)

func TestImportRequestTimeout(t *testing.T) {
	for _, test := range []struct {
		name   string
		custom bool
		want   time.Duration
	}{
		{name: "default", want: 120 * time.Second},
		{name: "custom client", custom: true, want: 10 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			var remaining time.Duration
			var hasDeadline bool
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				deadline, ok := request.Context().Deadline()
				hasDeadline = ok
				remaining = time.Until(deadline)
				request.Body.Close()
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("not found")),
				}, nil
			})
			originalTransport := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			options := ImportOptions{
				DataRoot: parallelCatalog(t, "http://example.test", 1),
				Apply:    true, Concurrency: 1,
			}
			if test.custom {
				options.Client = &http.Client{Transport: transport, Timeout: test.want}
			}
			result, err := Import(options)
			if err != nil || result.QueriedLookupCount != 1 || result.NotFoundGameCount != 1 {
				t.Fatalf("import result=%+v, error=%v", result, err)
			}
			if !hasDeadline || remaining > test.want || remaining < test.want-time.Second {
				t.Fatalf("request deadline present=%v, remaining=%s, want approximately %s", hasDeadline, remaining, test.want)
			}
		})
	}
}

func TestImportParallelWritesBeforeSlowGameAndResumes(t *testing.T) {
	slow := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(slow) })
	var active, peak, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		var inputs []lookupInput
		if err := json.NewDecoder(r.Body).Decode(&inputs); err != nil || len(inputs) != 1 {
			t.Errorf("invalid lookup body: %v, %#v", err, inputs)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		hash := inputs[0].MD5
		if hash == fmt.Sprintf("%032x", 1) {
			select {
			case <-slow:
			case <-r.Context().Done():
				return
			}
		}
		if hash == fmt.Sprintf("%032x", 2) {
			fmt.Fprintf(w, `{"id":42,"name":"Game B","signatures":{"NoIntros":[{"game":{"name":"Game B"},"rom":{"md5":%q}}]}}`, hash)
			return
		}
		fmt.Fprint(w, "null")
	}))
	defer server.Close()
	// Release blocked handlers before Server.Close, including on assertion failure.
	defer release.Do(func() { close(slow) })
	root := parallelCatalog(t, server.URL, 3)
	completed := make(chan ImportProgress, 3)
	var progress []ImportProgress
	options := ImportOptions{DataRoot: root, Apply: true, Concurrency: 2, Progress: func(s ImportProgress) {
		progress = append(progress, s)
		if s.Stage == "" {
			completed <- s
		}
	}}
	type outcome struct {
		result ImportResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Import(options)
		done <- outcome{result, err}
	}()
	for range 2 {
		s := receive(t, completed)
		if s.Current >= s.Total {
			t.Errorf("unfinished slow game must keep progress below 100%%: %#v", s)
		}
	}
	assertSourcePresent(t, root, 0, false)
	assertSourcePresent(t, root, 1, true)
	assertSourcePresent(t, root, 2, true)
	release.Do(func() { close(slow) })
	got := receive(t, done)
	if got.err != nil || got.result.WrittenFileCount != 3 || got.result.GameFileCount != 1 || got.result.NotFoundGameCount != 2 {
		t.Fatalf("unexpected result: %#v, %v", got.result, got.err)
	}
	if peak.Load() != 2 || active.Load() != 0 {
		t.Fatalf("expected exactly two concurrent requests and no remaining work: peak=%d active=%d", peak.Load(), active.Load())
	}
	last := 0
	for _, s := range progress {
		if s.Total != 3 {
			continue
		}
		if s.Current < last {
			t.Errorf("progress went backwards: %d -> %d", last, s.Current)
		}
		last = s.Current
	}
	if last != 3 {
		t.Errorf("final progress = %d, want 3", last)
	}
	options.Progress = nil
	resumed, err := Import(options)
	if err != nil || resumed.SkippedExistingCount != 3 || resumed.QueriedLookupCount != 0 || calls.Load() != 3 {
		t.Fatalf("resume should skip saved matches and not-found results: %#v, %v, calls=%d", resumed, err, calls.Load())
	}
	options.ForceOverwrite = true
	refreshed, err := Import(options)
	if err != nil || refreshed.WrittenFileCount != 3 || refreshed.QueriedLookupCount != 3 {
		t.Fatalf("overwrite should query all games again: %#v, %v", refreshed, err)
	}
	for i := range 3 {
		assertSourcePresent(t, root, i, true)
	}
}

func TestImportParallelDryRunLimitAndLookupFailure(t *testing.T) {
	var calls atomic.Int32
	var unavailable atomic.Bool
	unavailable.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if unavailable.Load() && strings.Contains(string(body), fmt.Sprintf("%032x", 2)) {
			http.Error(w, "lookup pending", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "null")
	}))
	defer server.Close()
	root := parallelCatalog(t, server.URL, 5)
	options := ImportOptions{DataRoot: root, Concurrency: 3, Limit: 3}
	plan, err := Import(options)
	if err != nil || plan.SelectedGameCount != 3 || plan.SkippedByLimitCount != 2 || plan.PlannedLookupCount != 3 || calls.Load() != 0 {
		t.Fatalf("unexpected dry run: %#v, %v", plan, err)
	}
	for i := range 5 {
		assertSourcePresent(t, root, i, false)
	}
	options.Apply = true
	got, err := Import(options)
	if err != nil || got.SelectedGameCount != 3 || got.FailedLookupCount != 1 || got.WrittenFileCount != 2 || calls.Load() != 3 {
		t.Fatalf("lookup failure should not stop other games: %#v, %v", got, err)
	}
	assertSourcePresent(t, root, 0, true)
	assertSourcePresent(t, root, 1, false)
	assertSourcePresent(t, root, 2, true)
	assertSourcePresent(t, root, 3, false)
	if len(got.Failures) != 1 || got.Failures[0].Game != "Game B" {
		t.Fatalf("missing failure details: %#v", got.Failures)
	}
	unavailable.Store(false)
	options.Limit = 1
	retried, err := Import(options)
	if err != nil || retried.WrittenFileCount != 1 || retried.SkippedExistingCount != 2 || calls.Load() != 4 {
		t.Fatalf("retry should select the unsaved failure: %#v, %v", retried, err)
	}
	assertSourcePresent(t, root, 1, true)
}

func TestImportPipelineBoundsQueueAndCancelsOnScanFailure(t *testing.T) {
	started := make(chan struct{}, 2)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	p := newImportPipeline(ImportOptions{Apply: true}, sourceSettings{Concurrency: 2, RateLimitPerSecond: 1000, APIBaseURL: "http://example.test"}, client, ImportResult{})
	var admitted atomic.Int32
	producer := make(chan error, 1)
	go func() {
		for range 100 {
			if err := p.submit(testLookupGame("", "Game")); err != nil {
				producer <- err
				return
			}
			admitted.Add(1)
		}
		producer <- nil
	}()
	defer func() {
		p.cancel()
		receive(t, producer)
		scanErr := errors.New("catalog read failed")
		if _, err := p.finish(scanErr); !errors.Is(err, scanErr) {
			t.Errorf("lost scan failure: %v", err)
		}
	}()
	receive(t, started)
	receive(t, started)
	time.Sleep(50 * time.Millisecond)
	if got := admitted.Load(); got != 4 {
		t.Fatalf("expected 2 active + 2 queued games, got %d", got)
	}
}

func TestImportPipelineWriteFailureCancelsOtherRequests(t *testing.T) {
	blocked := make(chan struct{})
	finished := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), fmt.Sprintf("%032x", 2)) {
			close(blocked)
			<-r.Context().Done()
			close(finished)
			return nil, r.Context().Err()
		}
		select {
		case <-blocked:
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("null")), Header: http.Header{}}, nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	p := newImportPipeline(ImportOptions{Apply: true}, sourceSettings{Concurrency: 2, RateLimitPerSecond: 1000, APIBaseURL: "http://example.test"}, client, ImportResult{})
	defer p.cancel()
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "game.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := testLookupGame(path, "Bad")
	other := testLookupGame(t.TempDir(), "Other")
	other.Fingerprints[0].MD5 = fmt.Sprintf("%032x", 2)
	for _, game := range []catalogGame{bad, other} {
		if err := p.submit(game); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.finish(nil)
		done <- err
	}()
	if err := receive(t, done); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("expected actionable write error, got %v", err)
	}
	receive(t, finished)
	if p.result.FailedLookupCount != 0 || p.result.WrittenFileCount != 0 {
		t.Fatalf("cancelled requests are not remote failures or saved results: %#v", p.result)
	}
}

func TestRateLimiterIsSharedAndCancellable(t *testing.T) {
	limiter := newRateLimiter(20)
	times := make(chan time.Time, 6)
	for range cap(times) {
		go func() {
			if err := limiter.wait(context.Background()); err != nil {
				t.Error(err)
			}
			times <- time.Now()
		}()
	}
	var observed []time.Time
	for range cap(times) {
		observed = append(observed, receive(t, times))
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].Before(observed[j]) })
	if elapsed := observed[len(observed)-1].Sub(observed[0]); elapsed < 225*time.Millisecond {
		t.Errorf("workers bypassed shared rate limit: six starts in %s", elapsed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	limiter = &rateLimiter{delay: time.Hour, last: time.Now()}
	go func() { done <- limiter.wait(ctx) }()
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("rate-limit wait did not cancel: %v", err)
	}
}

func TestHasheousConcurrencySettings(t *testing.T) {
	config := store.SourcesConfig{}
	if got := hasheousSettings(config).Concurrency; got != 1 {
		t.Fatalf("unconfigured concurrency = %d, want 1", got)
	}
	config.FetchConcurrency = 4
	if got := hasheousSettings(config).Concurrency; got != 4 {
		t.Fatalf("global concurrency = %d, want 4", got)
	}
	config.Sources = []store.Source{{Name: "hasheous", FetchConcurrency: 2}}
	if got := hasheousSettings(config).Concurrency; got != 2 {
		t.Fatalf("source concurrency = %d, want 2", got)
	}
}

func TestImportRefreshesWorkerProgressWhileHTTPIsBlocked(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(gate) })
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		select {
		case <-gate:
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("null")), Header: http.Header{}}, nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	root := parallelCatalog(t, "http://example.test", 2)
	snapshots := make(chan ImportProgress, 128)
	var callbacks atomic.Int32
	done := make(chan error, 1)
	go func() {
		result, err := Import(ImportOptions{DataRoot: root, Apply: true, Concurrency: 2, Client: client, Progress: func(s ImportProgress) {
			callbacks.Add(1)
			snapshots <- s
		}})
		if err == nil && (result.QueriedLookupCount != 2 || result.WrittenFileCount != 2) {
			err = fmt.Errorf("unexpected completed result: %#v", result)
		}
		done <- err
	}()
	queries := map[int]int{}
	activeSummary := false
	for queries[1] < 2 || queries[2] < 2 || !activeSummary {
		s := receive(t, snapshots)
		if s.Worker > 0 && s.Stage == "querying" {
			queries[s.Worker]++
			if s.Request != 1 || s.Requests != 1 || s.Workers != 2 {
				t.Errorf("wrong active request: %#v", s)
			}
		}
		if s.Worker == 0 && s.Stage == "matching" && s.Game == "2 active, 0 queued" && s.Queries == 2 {
			activeSummary = true
			if s.Current != 0 || s.Total != 2 {
				t.Errorf("queries should be counted before HTTP completes: %#v", s)
			}
		}
		if s.Worker == 0 && s.Total == 2 && s.Current == 2 {
			t.Fatal("blocked requests were counted as completed")
		}
	}
	release.Do(func() { close(gate) })
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	count := callbacks.Load()
	time.Sleep(1100 * time.Millisecond)
	if callbacks.Load() != count {
		t.Fatal("progress goroutine outlived Import")
	}
}

func TestLookupProgressReportsReleaseAttempts(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		status := http.StatusNotFound
		if calls == 2 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("null")), Header: http.Header{}}, nil
	})}
	var events []string
	_, queries, err := lookupBestMatches(context.Background(), client, newRateLimiter(1000), sourceSettings{APIBaseURL: "http://example.test"}, catalogGame{Title: "Game"}, []lookupRequest{
		{Release: "Game (USA)", Inputs: []lookupInput{{MD5: fmt.Sprintf("%032x", 1)}}},
		{Release: "Game (Europe)", Inputs: []lookupInput{{MD5: fmt.Sprintf("%032x", 2)}}},
	}, func(stage string, current, total int) {
		events = append(events, fmt.Sprintf("%s %d/%d", stage, current, total))
	})
	if err != nil || queries != 2 {
		t.Fatalf("lookup failed: queries=%d, err=%v", queries, err)
	}
	if got := strings.Join(events, ", "); got != "rate limit 1/2, querying 1/2, rate limit 2/2, querying 2/2" {
		t.Errorf("request progress = %s", got)
	}
}

func parallelCatalog(t *testing.T, endpoint string, count int) string {
	t.Helper()
	root := t.TempDir()
	writeText(t, filepath.Join(root, "config", "sources.yaml"), fmt.Sprintf("fetch:\n  concurrency: 4\nsources:\n  hasheous:\n    concurrency: 2\n    api:\n      base_url: %s\n      rate_limit_per_second: 1000\n", endpoint))
	for i := range count {
		name := fmt.Sprintf("Game %c", 'A'+i)
		path := filepath.Join(root, "platforms", "Sega - 32X", name)
		writeText(t, filepath.Join(path, "game.yaml"), fmt.Sprintf("title: %q\ndefault_release: %q\nreleases:\n  %q:\n    regions: [USA]\nsources:\n  launchbox:\n    status: not_found\n", name, name+" (USA)", name+" (USA)"))
		writeText(t, filepath.Join(path, "fingerprints.yaml"), fmt.Sprintf("items:\n  - id: test.%d\n    release: %q\n    file:\n      name: %q\n      md5: %q\n    dump:\n      format: 32x\n", i+1, name+" (USA)", name+".32x", fmt.Sprintf("%032x", i+1)))
	}
	return root
}

func assertSourcePresent(t *testing.T, root string, index int, present bool) {
	t.Helper()
	name := fmt.Sprintf("Game %c", 'A'+index)
	data, err := os.ReadFile(filepath.Join(root, "platforms", "Sega - 32X", name, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Contains(string(data), "  hasheous:"); got != present {
		t.Errorf("%s: source present=%v, want %v", name, got, present)
	}
	for _, preserved := range []string{name, "default_release:", "releases:", "  launchbox:"} {
		if !strings.Contains(string(data), preserved) {
			t.Errorf("%s lost %q: %s", name, preserved, data)
		}
	}
}

func testLookupGame(path, title string) catalogGame {
	return catalogGame{Path: path, Title: title, DefaultRelease: title, Fingerprints: []catalogcache.Fingerprint{
		{Release: title, Name: title + ".32x", Format: "32x", MD5: fmt.Sprintf("%032x", 1)},
	}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for worker")
		var zero T
		return zero
	}
}
