// Package retry runs an operation with exponential backoff and jitter, retrying
// only errors that wrap triagearr.ErrTransient. Hard failures (4xx, decode
// errors, anything not marked transient) return on the first attempt so a run
// never burns its budget on a request that can't succeed.
//
// It is the shared primitive behind the *arr/qBit clients' read retries and the
// Actor's write retries (ADR-0027). The sleep hook is injectable so tests drive
// the backoff schedule without real waits.
package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type config struct {
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	sleep       func(time.Duration)
}

// Option tunes a single Do call.
type Option func(*config)

// WithMaxAttempts caps the total number of attempts (including the first).
func WithMaxAttempts(n int) Option { return func(c *config) { c.maxAttempts = n } }

// WithBaseDelay sets the first backoff interval; it doubles each retry.
func WithBaseDelay(d time.Duration) Option { return func(c *config) { c.baseDelay = d } }

// WithMaxDelay caps the per-retry backoff after doubling.
func WithMaxDelay(d time.Duration) Option { return func(c *config) { c.maxDelay = d } }

// WithSleep replaces the wait function (tests inject a no-op or recorder).
func WithSleep(fn func(time.Duration)) Option { return func(c *config) { c.sleep = fn } }

// Do calls op until it succeeds, returns a non-transient error, or exhausts the
// attempt budget. The defaults (3 attempts, 500ms base, 4s cap) keep one bad
// upstream from stalling a run for more than ~10s.
func Do(ctx context.Context, op func() error, opts ...Option) error {
	cfg := config{
		maxAttempts: 3,
		baseDelay:   500 * time.Millisecond,
		maxDelay:    4 * time.Second,
		sleep:       time.Sleep,
	}
	for _, o := range opts {
		o(&cfg)
	}

	var lastErr error
	delay := cfg.baseDelay
	for attempt := 0; attempt < cfg.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, triagearr.ErrTransient) {
			return lastErr
		}
		if attempt == cfg.maxAttempts-1 {
			break
		}
		cfg.sleep(delay + jitter(delay/2))
		delay *= 2
		if delay > cfg.maxDelay {
			delay = cfg.maxDelay
		}
	}
	return lastErr
}

func jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max))) //nolint:gosec // G404: jitter is timing noise, not security-sensitive
}
