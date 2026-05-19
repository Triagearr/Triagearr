// Package pollers orchestrates the observation-only goroutines that feed the
// store. Each poller runs on its own cadence and reports errors through slog
// but never aborts the daemon.
package pollers

import (
	"context"
	"log/slog"
	"time"
)

// Poller is a long-running goroutine that performs a tick on a configured cadence.
type Poller interface {
	Name() string
	Run(ctx context.Context) error
}

// tickLoop drives a poller's tick function: one immediate tick, then on interval
// until ctx is cancelled. Errors are logged and swallowed so a transient failure
// in one provider never kills the daemon.
func tickLoop(ctx context.Context, name string, interval time.Duration, tick func(context.Context) error) error {
	logger := slog.With("poller", name, "interval", interval.String())
	logger.Info("poller started")
	defer logger.Info("poller stopped")

	runOnce := func() {
		t0 := time.Now()
		if err := tick(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("tick failed", "err", err, "duration", time.Since(t0).String())
			return
		}
		logger.Debug("tick ok", "duration", time.Since(t0).String())
	}

	runOnce()

	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			runOnce()
			timer.Reset(interval)
		}
	}
}
