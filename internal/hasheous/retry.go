package hasheous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	rateLimitAttempts = 3
	maxRateLimitWait  = 5 * time.Minute
)

type rateLimitStopError struct {
	cause  error
	reason string
}

func (e *rateLimitStopError) Error() string {
	return fmt.Sprintf("Hasheous rate limit: %s; saved results are kept, resume later: %v", e.reason, e.cause)
}

func (e *rateLimitStopError) Unwrap() error { return e.cause }

func isRateLimitStop(err error) bool {
	var stopped *rateLimitStopError
	return errors.As(err, &stopped)
}

// Retry the same read-only hash lookup after a service-wide cooldown. All
// workers share the limiter, including requests already waiting for a slot.
func lookupHashes(ctx context.Context, client *http.Client, limiter *rateLimiter, settings sourceSettings, inputs []lookupInput, progress func(string)) ([]lookupResponse, error) {
	if limiter == nil {
		limiter = newRateLimiter(settings.RateLimitPerSecond)
	}
	for attempt := 0; ; attempt++ {
		responses, err := lookupHashesOnce(ctx, client, limiter, settings, inputs, progress)
		var status httpStatusError
		if !errors.As(err, &status) || status.StatusCode != http.StatusTooManyRequests {
			return responses, err
		}
		if attempt+1 >= rateLimitAttempts {
			return nil, &rateLimitStopError{cause: err, reason: fmt.Sprintf("still blocked after %d attempts", rateLimitAttempts)}
		}
		delay := rateLimitDelay(status, attempt, time.Now())
		if delay > maxRateLimitWait {
			return nil, &rateLimitStopError{cause: err, reason: "server requests a pause longer than 5 minutes"}
		}
		limiter.pause(delay)
	}
}

func rateLimitDelay(status httpStatusError, attempt int, now time.Time) time.Duration {
	delay := 30 * time.Second * time.Duration(1<<attempt)
	delay = max(delay, retryAfterDelay(status.RetryAfter, now))
	// Cloudflare may provide a longer pause in JSON than in Retry-After.
	var body struct {
		RetryAfter json.Number `json:"retry_after"`
	}
	if json.Unmarshal([]byte(status.Body), &body) == nil {
		delay = max(delay, retryAfterDelay(body.RetryAfter.String(), now))
	}
	return delay
}

func retryAfterDelay(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(maxRateLimitWait/time.Second) {
			return maxRateLimitWait + time.Second
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, date.Sub(now))
	}
	return 0
}
