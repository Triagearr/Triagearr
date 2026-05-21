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

// Volume is a watched filesystem mount.
type Volume struct {
	Name string
	Path string
}

// DiskPoller polls disk usage on every configured volume.
type DiskPoller struct {
	Volumes  []Volume
	Store    DiskStore
	Interval time.Duration
}

// Name implements Poller.
func (p *DiskPoller) Name() string { return "disk" }

// Run blocks until ctx is cancelled.
func (p *DiskPoller) Run(ctx context.Context) error {
	return TickLoop(ctx, p.Name(), p.Interval, p.tick)
}

func (p *DiskPoller) tick(ctx context.Context) error {
	now := time.Now().UTC()
	for _, v := range p.Volumes {
		usage, err := statfs(v.Path)
		if err != nil {
			slog.Warn("statfs failed", "volume", v.Name, "path", v.Path, "err", err)
			continue
		}
		usage.VolumeName = v.Name
		usage.Path = v.Path
		usage.Timestamp = now
		if err := p.Store.InsertDiskUsage(ctx, usage); err != nil {
			slog.Warn("insert disk_pressure failed", "volume", v.Name, "err", err)
		}
	}
	slog.Info("disk tick complete", "volumes", len(p.Volumes))
	return nil
}
