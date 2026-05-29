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
	UpsertTorrents(ctx context.Context, torrents []triagearr.Torrent) error
	InsertSnapshots(ctx context.Context, snaps []triagearr.Snapshot) error
	UpsertTorrentClientInstance(ctx context.Context, kind, url string, healthy bool, lastErr string) error
}

// TorrentClientPoller polls a torrent client instance and persists torrents + snapshots.
type TorrentClientPoller struct {
	Client triagearr.TorrentClient
	// Kind and URL identify the client for the torrent_client_instances health
	// row (mirror of arr_instances). When Kind is empty the health probe is
	// skipped — used by tests that only exercise the torrent ingestion path.
	Kind     string
	URL      string
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
	p.recordHealth(ctx)
	torrents, err := p.Client.ListTorrents(ctx)
	if err != nil {
		return fmt.Errorf("listing torrents: %w", err)
	}
	now := time.Now().UTC()
	snaps := make([]triagearr.Snapshot, len(torrents))
	for i, t := range torrents {
		snaps[i] = triagearr.Snapshot{
			Hash:         t.Hash,
			Timestamp:    now,
			Ratio:        t.Ratio,
			Uploaded:     t.Uploaded,
			Seeders:      t.Seeders,
			Leechers:     t.Leechers,
			State:        t.State,
			LastActivity: t.LastActivity,
		}
	}
	if err := p.Store.UpsertTorrents(ctx, torrents); err != nil {
		return fmt.Errorf("batch upserting torrents: %w", err)
	}
	if err := p.Store.InsertSnapshots(ctx, snaps); err != nil {
		slog.Warn("batch insert snapshots failed", "err", err)
	}
	slog.Info("torrent-client tick complete", "torrents", len(torrents))
	notifyNonBlocking(p.Notify)
	notifyNonBlocking(p.TrackerCatchup)
	return nil
}

// recordHealth probes the client and persists the result into
// torrent_client_instances, mirroring arr_poller.pollOne. Skipped when Kind is
// unset (test wiring) so the ingestion-only path needs no health row.
func (p *TorrentClientPoller) recordHealth(ctx context.Context) {
	if p.Kind == "" {
		return
	}
	healthErr := p.Client.HealthCheck(ctx)
	healthy := healthErr == nil
	lastErr := ""
	if healthErr != nil {
		lastErr = healthErr.Error()
	}
	if err := p.Store.UpsertTorrentClientInstance(ctx, p.Kind, p.URL, healthy, lastErr); err != nil {
		slog.Warn("upsert torrent_client_instance failed", "err", err)
	}
	if !healthy {
		slog.Info("torrent client unhealthy", "kind", p.Kind, "err", healthErr)
	}
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
