package scorer

import (
	"context"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/pollers"
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
	return pollers.TickLoop(ctx, l.Name(), l.Interval, func(ctx context.Context) error {
		stats, err := l.Scorer.ScoreAll(ctx)
		if err != nil {
			return err
		}
		slog.Info("score pass complete",
			"scored", stats.Scored,
			"excluded", stats.Excluded,
			"errors", stats.Errors,
			"duration", stats.Duration.String(),
		)
		return nil
	})
}
