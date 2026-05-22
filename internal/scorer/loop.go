package scorer

import (
	"context"
	"log/slog"
	"time"
)

// defaultDebounce is the window the Loop waits after the first poll signal
// before scoring, so a startup burst of poller ticks coalesces into one pass.
const defaultDebounce = 5 * time.Second

// Loop drives event-driven scoring: it runs one Score pass, after a debounce
// window, each time a feeding poller (qbit/tracker/arr) signals fresh data.
// There is no fixed interval — the pollers' own cadences pace the scorer, and
// "no signal" can only mean "no new data", so there is nothing to re-score.
type Loop struct {
	Score    func(context.Context) error // one scoring pass; prod uses Scorer.ScorePass
	Signal   <-chan struct{}             // signalled by feeding pollers after each tick
	Debounce time.Duration               // coalescing window; 0 → defaultDebounce
}

// Name implements pollers.Poller.
func (l *Loop) Name() string { return "scorer" }

// Run blocks until ctx is cancelled. There is no immediate pass at t=0: the
// first score happens once a poller reports data, which avoids the cold-start
// race of scoring an empty store.
func (l *Loop) Run(ctx context.Context) error {
	debounce := l.Debounce
	if debounce <= 0 {
		debounce = defaultDebounce
	}
	slog.Info("scorer loop started", "debounce", debounce.String())
	defer slog.Info("scorer loop stopped")

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-l.Signal:
		}
		// Absorb the burst: wait out the debounce window, then drain any
		// signals that piled up so they collapse into this single pass.
		if !sleepCtx(ctx, debounce) {
			return nil
		}
		drain(l.Signal)
		if err := l.Score(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("score pass failed", "err", err)
		}
	}
}

// sleepCtx blocks for d, returning false if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// drain empties ch without blocking.
func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
