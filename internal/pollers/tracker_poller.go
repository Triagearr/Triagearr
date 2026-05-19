package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// TrackerStore is the subset of store operations the tracker poller needs.
type TrackerStore interface {
	ListTorrentHashes(ctx context.Context) ([]triagearr.Hash, error)
	ReplaceTrackers(ctx context.Context, hash triagearr.Hash, infos []triagearr.TrackerInfo) error
}

// TrackerPoller calls qBit's /api/v2/torrents/trackers for every known torrent
// and persists the result into torrent_trackers (ADR-0009). The cadence is
// deliberately slow (default 6h) — tracker state changes at day-scale.
type TrackerPoller struct {
	Client   triagearr.QbitClient
	Store    TrackerStore
	Interval time.Duration
}

// Name implements Poller.
func (p *TrackerPoller) Name() string { return "tracker" }

// Run blocks until ctx is cancelled.
func (p *TrackerPoller) Run(ctx context.Context) error {
	return tickLoop(ctx, p.Name(), p.Interval, p.tick)
}

func (p *TrackerPoller) tick(ctx context.Context) error {
	hashes, err := p.Store.ListTorrentHashes(ctx)
	if err != nil {
		return fmt.Errorf("listing torrent hashes: %w", err)
	}
	if len(hashes) == 0 {
		// qBit poll hasn't produced anything yet; nothing to do.
		return nil
	}
	ok, failed := 0, 0
	for _, h := range hashes {
		if ctx.Err() != nil {
			return nil //nolint:nilerr // tickLoop swallows context.Canceled; exit cleanly
		}
		infos, err := p.Client.ListTrackers(ctx, h)
		if err != nil {
			slog.Warn("list trackers failed", "hash", h, "err", err)
			failed++
			continue
		}
		if err := p.Store.ReplaceTrackers(ctx, h, infos); err != nil {
			slog.Warn("replace trackers failed", "hash", h, "err", err)
			failed++
			continue
		}
		ok++
	}
	slog.Info("tracker tick complete", "ok", ok, "failed", failed, "total", len(hashes))
	return nil
}
