package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// QbitStore is the subset of store operations the qbit poller needs.
type QbitStore interface {
	UpsertTorrent(ctx context.Context, t triagearr.Torrent) error
	InsertSnapshot(ctx context.Context, snap triagearr.Snapshot) error
}

// QbitPoller polls a qBittorrent instance and persists torrents + snapshots.
type QbitPoller struct {
	Client   triagearr.TorrentClient
	Store    QbitStore
	Interval time.Duration
	// Notify, when non-nil, is signalled after each successful tick so the
	// scorer can re-score against the freshly persisted torrents.
	Notify chan<- struct{}
}

// Name implements Poller.
func (p *QbitPoller) Name() string { return "qbit" }

// Run blocks until ctx is cancelled.
func (p *QbitPoller) Run(ctx context.Context) error {
	return TickLoop(ctx, p.Name(), p.Interval, p.tick, p.Notify)
}

func (p *QbitPoller) tick(ctx context.Context) error {
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
	slog.Info("qbit tick complete", "torrents", len(torrents))
	return nil
}
