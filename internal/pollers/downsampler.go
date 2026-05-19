package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// MaintenanceStore is the subset of store operations the maintenance job needs.
type MaintenanceStore interface {
	DownsampleRange(ctx context.Context, before time.Time) (dailyWritten, rawDeleted int, err error)
	EnforceRetention(ctx context.Context, rawHorizon, dailyHorizon time.Duration) (rawDeleted, dailyDeleted int, err error)
	PruneStaleTorrents(ctx context.Context, olderThan time.Duration) (int, error)
	Vacuum(ctx context.Context, minReclaimBytes int64) (ran bool, reclaimable int64, err error)
}

// MaintenanceConfig groups the storage-maintenance knobs. Defaults are applied
// by the caller (cmd/triagearr) so this struct stays free of magic numbers.
type MaintenanceConfig struct {
	// Schedule is a cron expression evaluated in UTC (default "0 3 * * *").
	Schedule string
	// DownsampleAge bounds the cutoff: snapshots_raw older than this become
	// snapshots_daily rows (default 48h — keep two days of raw for ad-hoc debug).
	DownsampleAge time.Duration
	// RawRetention drops snapshots_raw rows older than this even if they have
	// been downsampled (default 7d).
	RawRetention time.Duration
	// DailyRetention drops snapshots_daily rows older than this (default 365d).
	DailyRetention time.Duration
	// TorrentRetention drops torrents (and their snapshots/trackers) whose
	// last_seen is older than this. Default 7d — large enough to absorb qBit
	// flaps, small enough that the M3 scorer never evaluates a ghost. arr_imports
	// is kept (it's *arr-side history, independent of qBit lifecycle).
	TorrentRetention time.Duration
	// VacuumEnabled gates the post-cleanup VACUUM.
	VacuumEnabled bool
	// VacuumMinReclaimBytes skips VACUUM when freelist*page_size is smaller.
	VacuumMinReclaimBytes int64
}

// Maintenance runs the daily storage-maintenance job (downsample + retention + VACUUM)
// on a cron schedule. It satisfies Poller so it can sit in the same Manager
// as the interval-based pollers.
type Maintenance struct {
	Store  MaintenanceStore
	Config MaintenanceConfig
}

// Name implements Poller.
func (m *Maintenance) Name() string { return "maintenance" }

// Run blocks until ctx is cancelled. Errors from individual runs are logged
// and swallowed — the next scheduled tick retries.
func (m *Maintenance) Run(ctx context.Context) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(m.Config.Schedule)
	if err != nil {
		return fmt.Errorf("parsing downsample_cron %q: %w", m.Config.Schedule, err)
	}
	logger := slog.With("poller", m.Name(), "cron", m.Config.Schedule)
	logger.Info("maintenance scheduler started")
	defer logger.Info("maintenance scheduler stopped")

	for {
		now := time.Now().UTC()
		next := schedule.Next(now)
		wait := time.Until(next)
		logger.Debug("next maintenance run", "at", next.Format(time.RFC3339), "in", wait.String())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
			m.runOnce(ctx)
		}
	}
}

func (m *Maintenance) runOnce(ctx context.Context) {
	logger := slog.With("poller", m.Name())
	t0 := time.Now()

	cutoff := time.Now().UTC().Add(-m.Config.DownsampleAge)
	daily, raw, err := m.Store.DownsampleRange(ctx, cutoff)
	if err != nil {
		logger.Error("downsample failed", "err", err)
	} else {
		logger.Info("downsample complete", "daily_rows", daily, "raw_deleted", raw, "cutoff", cutoff.Format(time.RFC3339))
	}

	rawRet, dailyRet, err := m.Store.EnforceRetention(ctx, m.Config.RawRetention, m.Config.DailyRetention)
	if err != nil {
		logger.Error("retention failed", "err", err)
	} else {
		logger.Info("retention complete", "raw_deleted", rawRet, "daily_deleted", dailyRet)
	}

	if m.Config.TorrentRetention > 0 {
		pruned, err := m.Store.PruneStaleTorrents(ctx, m.Config.TorrentRetention)
		if err != nil {
			logger.Error("torrent prune failed", "err", err)
		} else if pruned > 0 {
			logger.Info("torrent prune complete", "pruned", pruned, "grace", m.Config.TorrentRetention.String())
		}
	}

	if m.Config.VacuumEnabled {
		ran, reclaimable, err := m.Store.Vacuum(ctx, m.Config.VacuumMinReclaimBytes)
		switch {
		case err != nil:
			logger.Error("vacuum failed", "err", err)
		case ran:
			logger.Info("vacuum ran", "reclaimable_bytes", reclaimable)
		default:
			logger.Debug("vacuum skipped", "reclaimable_bytes", reclaimable, "threshold_bytes", m.Config.VacuumMinReclaimBytes)
		}
	}

	logger.Debug("maintenance run complete", "duration", time.Since(t0).String())
}
