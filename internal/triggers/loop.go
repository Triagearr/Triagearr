package triggers

import (
	"context"
	"log/slog"
	"time"
)

// tickLoop mirrors internal/pollers tickLoop with the same semantics: one
// immediate tick, then on Interval until ctx is cancelled. Errors are logged
// and swallowed.
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
