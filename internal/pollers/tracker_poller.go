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
	// Notify, when non-nil, is signalled after each successful tick so the
	// scorer can re-score against the freshly persisted tracker state.
	Notify chan<- struct{}
}

// Name implements Poller.
func (p *TrackerPoller) Name() string { return "tracker" }

// Run blocks until ctx is cancelled.
func (p *TrackerPoller) Run(ctx context.Context) error {
	return TickLoop(ctx, p.Name(), p.Interval, p.tick, p.Notify)
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
			return nil //nolint:nilerr // TickLoop swallows context.Canceled; exit cleanly
		}
		// Per-hash timeout so one stuck tracker call doesn't stall the rest of
		// the run. 30s is generous for a qBit local call but well under the
		// 6h cadence.
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		infos, err := p.Client.ListTrackers(callCtx, h)
		cancel()
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
