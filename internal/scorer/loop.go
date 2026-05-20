package scorer

import (
	"context"
	"log/slog"
	"time"
)

// Loop drives the periodic ScoreAll pass. Shape mirrors internal/pollers so
// the daemon manages scorer + pollers under the same lifecycle.
type Loop struct {
	Scorer   *Scorer
	Interval time.Duration
}

// Name implements pollers.Poller.
func (l *Loop) Name() string { return "scorer" }

// Run blocks until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) error {
	logger := slog.With("poller", l.Name(), "interval", l.Interval.String())
	logger.Info("scorer loop started")
	defer logger.Info("scorer loop stopped")

	runOnce := func() {
		stats, err := l.Scorer.ScoreAll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("score pass failed", "err", err)
			return
		}
		logger.Info("score pass complete",
			"scored", stats.Scored,
			"excluded", stats.Excluded,
			"errors", stats.Errors,
			"duration", stats.Duration.String(),
		)
	}

	runOnce()
	timer := time.NewTimer(l.Interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			runOnce()
			timer.Reset(l.Interval)
		}
	}
}
