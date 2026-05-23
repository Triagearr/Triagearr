package pollers

import (
	"context"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// DiskStore is the subset of store operations the disk poller needs.
type DiskStore interface {
	InsertDiskUsage(ctx context.Context, d triagearr.DiskUsage) error
}

// Sampler returns one disk-usage snapshot. The default sampler in this
// package wraps statfs(2); dev fixtures inject an HTTP-backed sampler so the
// pressure trigger can be exercised against fake volumes.
type Sampler func(ctx context.Context) (triagearr.DiskUsage, error)

// Volume is the watched filesystem mount. When Sample is nil, the poller falls
// back to statfs(Path) — the production code path.
type Volume struct {
	Path   string
	Sample Sampler
}

// DiskPoller polls disk usage on the watched volume (ADR-0024).
type DiskPoller struct {
	Volume   Volume
	Store    DiskStore
	Interval time.Duration
}

// Name implements Poller.
func (p *DiskPoller) Name() string { return "disk" }

// Run blocks until ctx is cancelled.
func (p *DiskPoller) Run(ctx context.Context) error {
	return TickLoop(ctx, p.Name(), p.Interval, p.tick, nil)
}

func (p *DiskPoller) tick(ctx context.Context) error {
	var (
		usage triagearr.DiskUsage
		err   error
	)
	if p.Volume.Sample != nil {
		usage, err = p.Volume.Sample(ctx)
	} else {
		usage, err = Statfs(p.Volume.Path)
	}
	if err != nil {
		slog.Warn("disk sample failed", "path", p.Volume.Path, "err", err)
		return nil
	}
	usage.Path = p.Volume.Path
	usage.Timestamp = time.Now().UTC()
	if err := p.Store.InsertDiskUsage(ctx, usage); err != nil {
		slog.Warn("insert disk_pressure failed", "err", err)
		return nil
	}
	slog.Info("disk tick complete", "path", p.Volume.Path)
	return nil
}
