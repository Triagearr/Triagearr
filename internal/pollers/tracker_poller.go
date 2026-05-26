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
	HashesWithoutTrackers(ctx context.Context) ([]triagearr.Hash, error)
	ReplaceTrackers(ctx context.Context, hash triagearr.Hash, infos []triagearr.TrackerInfo) error
}

// catchupDebounce coalesces bursts of signals from the qBit poller into one
// catchup pass. Short enough that a freshly-seen torrent gets its trackers
// well before the operator can refresh the dashboard.
const catchupDebounce = 2 * time.Second

// TrackerPoller calls qBit's /api/v2/torrents/trackers and persists the result
// into torrent_trackers (ADR-0009). Two cadences run side by side:
//
//   - Periodic full sweep (Interval, default 6h) re-fetches every known hash so
//     transitions (working → not-working, Factor 7) are caught.
//   - Event-driven catchup (Signal) reacts to qBit ingestion and fetches only
//     hashes that have no row in torrent_trackers yet. Resolves the cold-start
//     race where a freshly-seen torrent would otherwise wait one full Interval.
type TrackerPoller struct {
	Client   triagearr.TorrentClient
	Store    TrackerStore
	Interval time.Duration
	// Signal, when non-nil, triggers a catchup pass (fetch hashes missing
	// trackers) on every receive. Buffered upstream (qBit poller) so a tick
	// in flight doesn't block the producer.
	Signal <-chan struct{}
	// Notify, when non-nil, is signalled after each successful pass so the
	// scorer can re-score against the freshly persisted tracker state.
	Notify chan<- struct{}
}

// Name implements Poller.
func (p *TrackerPoller) Name() string { return "tracker" }

// Run blocks until ctx is cancelled.
func (p *TrackerPoller) Run(ctx context.Context) error {
	logger := slog.With("poller", p.Name(), "interval", p.Interval.String())
	logger.Info("poller started")
	defer logger.Info("poller stopped")

	// Immediate first sweep mirrors TickLoop's runOnce-then-wait semantics.
	p.runPass(ctx, "initial", p.fullSweep)

	full := time.NewTimer(p.Interval)
	defer full.Stop()

	// Debounce timer is created stopped; (re)armed when Signal fires.
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	debouncing := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-full.C:
			p.runPass(ctx, "sweep", p.fullSweep)
			full.Reset(p.Interval)
		case <-p.Signal:
			if !debouncing {
				debounce.Reset(catchupDebounce)
				debouncing = true
			}
			// Drain extra signals that arrived while debouncing — the next
			// debounce.C fire covers them.
		case <-debounce.C:
			debouncing = false
			p.runPass(ctx, "catchup", p.catchup)
		}
	}
}

func (p *TrackerPoller) runPass(ctx context.Context, mode string, pass func(context.Context) error) {
	t0 := time.Now()
	if err := pass(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Error("tracker pass failed", "mode", mode, "err", err, "duration", time.Since(t0).String())
		return
	}
	slog.Debug("tracker pass ok", "mode", mode, "duration", time.Since(t0).String())
	if p.Notify != nil {
		select {
		case p.Notify <- struct{}{}:
		default:
		}
	}
}

func (p *TrackerPoller) fullSweep(ctx context.Context) error {
	hashes, err := p.Store.ListTorrentHashes(ctx)
	if err != nil {
		return fmt.Errorf("listing torrent hashes: %w", err)
	}
	return p.fetchAll(ctx, "sweep", hashes)
}

func (p *TrackerPoller) catchup(ctx context.Context) error {
	hashes, err := p.Store.HashesWithoutTrackers(ctx)
	if err != nil {
		return fmt.Errorf("listing hashes without trackers: %w", err)
	}
	return p.fetchAll(ctx, "catchup", hashes)
}

func (p *TrackerPoller) fetchAll(ctx context.Context, mode string, hashes []triagearr.Hash) error {
	if len(hashes) == 0 {
		return nil
	}
	ok, failed := 0, 0
	for _, h := range hashes {
		if ctx.Err() != nil {
			return nil //nolint:nilerr // exit cleanly on cancel, no error to report
		}
		if err := p.fetchOne(ctx, h); err != nil {
			slog.Warn("tracker fetch failed", "mode", mode, "hash", h, "err", err)
			failed++
			continue
		}
		ok++
	}
	slog.Info("tracker pass complete", "mode", mode, "ok", ok, "failed", failed, "total", len(hashes))
	return nil
}

func (p *TrackerPoller) fetchOne(ctx context.Context, hash triagearr.Hash) error {
	// Per-hash timeout so one stuck tracker call doesn't stall the rest.
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	infos, err := p.Client.ListTrackers(callCtx, hash)
	if err != nil {
		return fmt.Errorf("list trackers: %w", err)
	}
	if err := p.Store.ReplaceTrackers(ctx, hash, infos); err != nil {
		return fmt.Errorf("replace trackers: %w", err)
	}
	return nil
}
