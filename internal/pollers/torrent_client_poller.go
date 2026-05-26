package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// TorrentClientStore is the subset of store operations the torrent client poller needs.
type TorrentClientStore interface {
	UpsertTorrent(ctx context.Context, t triagearr.Torrent) error
	InsertSnapshot(ctx context.Context, snap triagearr.Snapshot) error
}

// TorrentClientPoller polls a torrent client instance and persists torrents + snapshots.
type TorrentClientPoller struct {
	Client   triagearr.TorrentClient
	Store    TorrentClientStore
	Interval time.Duration
	// Notify, when non-nil, is signalled after each successful tick so the
	// scorer can re-score against the freshly persisted torrents.
	Notify chan<- struct{}
	// TrackerCatchup, when non-nil, is signalled after each successful tick
	// so the tracker poller fetches trackers for freshly-seen hashes without
	// waiting for its 6h periodic sweep.
	TrackerCatchup chan<- struct{}
}

// Name implements Poller.
func (p *TorrentClientPoller) Name() string { return "torrent-client" }

// Run blocks until ctx is cancelled.
func (p *TorrentClientPoller) Run(ctx context.Context) error {
	// Notify is delivered by tick() so we can fan out to multiple subscribers
	// (scorer + tracker catchup) without spawning a router goroutine.
	return TickLoop(ctx, p.Name(), p.Interval, p.tick, nil)
}

func (p *TorrentClientPoller) tick(ctx context.Context) error {
	torrents, err := p.Client.ListTorrents(ctx)
	if err != nil {
		return fmt.Errorf("listing torrents: %w", err)
	}
	now := time.Now().UTC()
	for _, t := range torrents {
		if err := p.Store.UpsertTorrent(ctx, t); err != nil {
			slog.Warn("upsert torrent failed", "hash", t.Hash, "err", err)
			continue
		}
		snap := triagearr.Snapshot{
			Hash:         t.Hash,
			Timestamp:    now,
			Ratio:        t.Ratio,
			Uploaded:     t.Uploaded,
			Seeders:      t.Seeders,
			Leechers:     t.Leechers,
			State:        t.State,
			LastActivity: t.LastActivity,
		}
		if err := p.Store.InsertSnapshot(ctx, snap); err != nil {
			slog.Warn("insert snapshot failed", "hash", t.Hash, "err", err)
		}
	}
	slog.Info("torrent-client tick complete", "torrents", len(torrents))
	notifyNonBlocking(p.Notify)
	notifyNonBlocking(p.TrackerCatchup)
	return nil
}

func notifyNonBlocking(c chan<- struct{}) {
	if c == nil {
		return
	}
	select {
	case c <- struct{}{}:
	default:
	}
}
