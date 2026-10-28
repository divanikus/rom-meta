package hasheous

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func rateResponse(status int, body, retryAfter string) *http.Response {
	header := make(http.Header)
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func TestLookupWaitsForCloudflareAndRetriesSameRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var starts []time.Time
		var bodies []string
		var stages []string
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			bodies = append(bodies, string(body))
			starts = append(starts, time.Now())
			if len(starts) == 1 {
				return rateResponse(429, `{"error_code":1015,"retry_after":30}`, "8"), nil
			}
			return rateResponse(200, `{"id":42}`, ""), nil
		})}
		responses, err := lookupHashes(context.Background(), client, newRateLimiter(8), sourceSettings{APIBaseURL: "https://example.test"},
			[]lookupInput{{MD5: strings.Repeat("1", 32)}}, func(stage string) { stages = append(stages, stage) })
		if err != nil || len(responses) != 1 {
			t.Fatalf("rate-limited lookup should recover: responses=%v, error=%v", responses, err)
		}
		if len(starts) != 2 || starts[1].Sub(starts[0]) < 30*time.Second {
			t.Fatalf("must honor the longer JSON cooldown: %v", starts)
		}
		if bodies[0] != bodies[1] || bodies[1] == "" {
			t.Fatalf("retry must resend the original lookup: %v", bodies)
		}
		if !strings.Contains(strings.Join(stages, ", "), "server cooldown") {
			t.Fatalf("server cooldown must be visible: %v", stages)
		}
	})
}

func TestPersistentRateLimitStopsPipelineAndPreservesSavedGames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := parallelCatalog(t, "https://example.test", 3)
		blocked := true
		var starts []time.Time
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var inputs []lookupInput
			if err := json.NewDecoder(r.Body).Decode(&inputs); err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if !blocked || inputs[0].MD5 == fmt.Sprintf("%032x", 1) {
				return rateResponse(200, "null", ""), nil
			}
			starts = append(starts, time.Now())
			return rateResponse(429, `{"retry_after":30}`, "8"), nil
		})}
		options := ImportOptions{DataRoot: root, Apply: true, Client: client, Concurrency: 1}
		result, err := Import(options)
		if err == nil || !strings.Contains(err.Error(), "rate limit") {
			t.Fatalf("persistent service-wide limit must stop the job: result=%+v error=%v", result, err)
		}
		if len(starts) != 3 || starts[1].Sub(starts[0]) < 30*time.Second || starts[2].Sub(starts[1]) < 60*time.Second {
			t.Fatalf("expected three attempts with increasing waits: %v", starts)
		}
		if result.WrittenFileCount != 1 || result.FailedLookupCount != 1 || result.QueriedLookupCount != 4 {
			t.Fatalf("partial result must count actual attempts and saved games: %+v", result)
		}
		for i := range 3 {
			assertSourcePresent(t, root, i, i == 0)
		}
		blocked = false
		resumed, err := Import(options)
		if err != nil || resumed.SkippedExistingCount != 1 || resumed.WrittenFileCount != 2 || resumed.QueriedLookupCount != 2 || resumed.FailedLookupCount != 0 {
			t.Fatalf("resume must fetch only unsaved games: result=%+v error=%v", resumed, err)
		}
		for i := range 3 {
			assertSourcePresent(t, root, i, true)
		}
	})
}

func TestCooldownExtendsPendingWaitsForAllWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := newRateLimiter(4)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := limiter.wait(ctx); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		times := make(chan time.Time, 3)
		for range 3 {
			go func() {
				if err := limiter.wait(ctx); err != nil {
					t.Error(err)
					return
				}
				times <- time.Now()
			}()
		}
		synctest.Wait()
		limiter.pause(30 * time.Second)
		time.Sleep(time.Second)
		synctest.Wait()
		if len(times) != 0 {
			t.Fatal("worker bypassed the server cooldown")
		}
		limiter.pause(60 * time.Second)
		time.Sleep(59 * time.Second)
		synctest.Wait()
		if len(times) != 0 {
			t.Fatal("worker ignored the extended cooldown")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if len(times) != 3 {
			t.Fatalf("workers did not resume: %d", len(times))
		}
		var previous time.Time
		for range 3 {
			got := <-times
			if got.Sub(start) < 61*time.Second {
				t.Fatal("request resumed early")
			}
			if !previous.IsZero() && got.Sub(previous) < 250*time.Millisecond {
				t.Fatal("workers burst after cooldown")
			}
			previous = got
		}
		limiter.pause(time.Hour)
		done := make(chan error, 1)
		go func() { done <- limiter.wait(ctx) }()
		synctest.Wait()
		cancel()
		if err := <-done; err != context.Canceled {
			t.Fatalf("cooldown did not cancel: %v", err)
		}
	})
}

func TestRateLimitDelayHonorsHeaderBodyAndBackoff(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header, body string
		attempt      int
		want         time.Duration
	}{
		{"8", `{"retry_after":30}`, 0, 30 * time.Second},
		{"120", `{"retry_after":30}`, 0, 120 * time.Second},
		{now.Add(90 * time.Second).Format(http.TimeFormat), "", 0, 90 * time.Second},
		{"", `{"retry_after":"45"}`, 0, 45 * time.Second},
		{"invalid", `{"retry_after":-1}`, 0, 30 * time.Second},
		{"8", `{"retry_after":30}`, 1, 60 * time.Second},
		{"1", `{"retryAfterSeconds":120}`, 0, 120 * time.Second},
		{"", `{"retryAfterSeconds":"90"}`, 0, 90 * time.Second},
		{"1", `{"retryAfterSeconds":3600}`, 0, maxRateLimitWait + time.Second},
	} {
		if got := rateLimitDelay(httpStatusError{RetryAfter: tc.header, Body: tc.body}, tc.attempt, now); got != tc.want {
			t.Errorf("header=%q body=%q attempt=%d: delay=%s want=%s", tc.header, tc.body, tc.attempt, got, tc.want)
		}
	}
}

func TestLookupStopsInsteadOfRetryingBeforeLongServerPause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		count := 0
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			r.Body.Close()
			count++
			return rateResponse(429, `{"retry_after":3600}`, "8"), nil
		})}
		_, err := lookupHashes(context.Background(), client, newRateLimiter(8), sourceSettings{APIBaseURL: "https://example.test"}, nil, nil)
		if !isRateLimitStop(err) || count != 1 {
			t.Fatalf("must stop for a long cooldown: count=%d error=%v", count, err)
		}
	})
}

func TestRecoveredRateLimitSlowsFollowingRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var starts []time.Time
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			r.Body.Close()
			starts = append(starts, time.Now())
			if len(starts) == 1 {
				return rateResponse(429, `{"retryAfterSeconds":1}`, "1"), nil
			}
			return rateResponse(200, `{"id":42}`, ""), nil
		})}
		limiter := newRateLimiter(8)
		for range 3 {
			if _, err := lookupHashes(context.Background(), client, limiter, sourceSettings{APIBaseURL: "https://example.test"}, nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		if len(starts) != 4 {
			t.Fatalf("unexpected attempts: %v", starts)
		}
		for i := 2; i < len(starts); i++ {
			if gap := starts[i].Sub(starts[i-1]); gap < time.Second {
				t.Fatalf("recovered client resumed aggressive pacing: %v", gap)
			}
		}
	})
}

func TestDefaultHasheousRequestPacingIsConservative(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := newRateLimiter(0)
		start := time.Now()
		for range 2 {
			if err := limiter.wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if elapsed := time.Since(start); elapsed < time.Second {
			t.Fatalf("default pacing starts too quickly: %v", elapsed)
		}
	})
}
