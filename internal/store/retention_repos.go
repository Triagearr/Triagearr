package store

import (
	"context"
	"fmt"
	"time"
)

// DownsampleRange aggregates snapshots_raw rows older than `before` into
// snapshots_daily (one row per torrent per day), then deletes the consumed
// raw rows. Returns the count of (daily rows written, raw rows deleted).
//
// Safe to call repeatedly; idempotent on the upsert side, and the delete is
// bounded by the same `before` cutoff. AVG is statistically sound because
// snapshots_raw is sampled at a regular interval per torrent (project memory
// "snapshots_pk_design").
func (s *Store) DownsampleRange(ctx context.Context, before time.Time) (dailyWritten, rawDeleted int, err error) {
	cutoff := ts(before)

	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin downsample tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ins, err := tx.ExecContext(ctx, `
		INSERT INTO snapshots_daily(torrent_hash, day, ratio_avg, ratio_min, ratio_max, seeders_avg, seeders_min, seeders_max, uploaded_max, samples)
		SELECT torrent_hash, date(ts) AS day,
		       AVG(ratio), MIN(ratio), MAX(ratio),
		       AVG(seeders), MIN(seeders), MAX(seeders),
		       MAX(uploaded), COUNT(*)
		FROM snapshots_raw
		WHERE ts < ?
		GROUP BY torrent_hash, date(ts)
		ON CONFLICT(torrent_hash, day) DO UPDATE SET
			ratio_avg=excluded.ratio_avg,
			ratio_min=excluded.ratio_min,
			ratio_max=excluded.ratio_max,
			seeders_avg=excluded.seeders_avg,
			seeders_min=excluded.seeders_min,
			seeders_max=excluded.seeders_max,
			uploaded_max=excluded.uploaded_max,
			samples=excluded.samples
	`, cutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("aggregating snapshots_raw: %w", err)
	}
	written, _ := ins.RowsAffected()

	res, err := tx.ExecContext(ctx, `DELETE FROM snapshots_raw WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("deleting raw rows: %w", err)
	}
	deleted, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit downsample: %w", err)
	}
	return int(written), int(deleted), nil
}

// PruneStaleTorrents deletes torrents whose last_seen is older than `olderThan`
// ago, plus the dependent rows in snapshots_raw, snapshots_daily, and
// torrent_trackers. Returns the count of torrent rows removed.
//
// arr_imports is intentionally NOT pruned: it records *arr-side history (which
// download_id brought in which file_id) and remains valid even when the qBit
// torrent is gone — the linker joins it through media_files, not torrents.
//
// last_seen is refreshed on every qbit tick (UpsertTorrent), so a torrent
// absent from qBit keeps its frozen last_seen and ages out after the grace.
func (s *Store) PruneStaleTorrents(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	cutoff := ts(time.Now().UTC().Add(-olderThan))

	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin prune tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Stage the stale hash set in a temp table so each dependent DELETE shares
	// one scan of torrents.last_seen rather than re-running the subquery. The
	// table lives on the writer connection, which is shared across calls, so
	// recreate it from scratch every invocation.
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS stale_hashes`); err != nil {
		return 0, fmt.Errorf("dropping stale_hashes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE stale_hashes AS SELECT hash FROM torrents WHERE last_seen < ?`, cutoff); err != nil {
		return 0, fmt.Errorf("staging stale hashes: %w", err)
	}
	defer func() { _, _ = s.writer.ExecContext(ctx, `DROP TABLE IF EXISTS stale_hashes`) }()

	hashFilter := `torrent_hash IN (SELECT hash FROM stale_hashes)`
	if _, err := tx.ExecContext(ctx, `DELETE FROM snapshots_raw    WHERE `+hashFilter); err != nil {
		return 0, fmt.Errorf("prune snapshots_raw: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM snapshots_daily  WHERE `+hashFilter); err != nil {
		return 0, fmt.Errorf("prune snapshots_daily: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_trackers WHERE `+hashFilter); err != nil {
		return 0, fmt.Errorf("prune torrent_trackers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM scores WHERE `+hashFilter); err != nil {
		return 0, fmt.Errorf("prune scores: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_files WHERE `+hashFilter); err != nil {
		return 0, fmt.Errorf("prune torrent_files: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM torrents WHERE hash IN (SELECT hash FROM stale_hashes)`)
	if err != nil {
		return 0, fmt.Errorf("prune torrents: %w", err)
	}
	n, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit prune: %w", err)
	}
	return int(n), nil
}

// EnforceRetention drops snapshots_raw and snapshots_daily rows older than
// their respective horizons. Returns the count of rows deleted per table.
func (s *Store) EnforceRetention(ctx context.Context, rawHorizon, dailyHorizon time.Duration) (rawDeleted, dailyDeleted int, err error) {
	now := time.Now().UTC()
	if rawHorizon > 0 {
		res, err := s.writer.ExecContext(ctx, `DELETE FROM snapshots_raw WHERE ts < ?`, ts(now.Add(-rawHorizon)))
		if err != nil {
			return 0, 0, fmt.Errorf("retention on snapshots_raw: %w", err)
		}
		n, _ := res.RowsAffected()
		rawDeleted = int(n)
	}
	if dailyHorizon > 0 {
		res, err := s.writer.ExecContext(ctx, `DELETE FROM snapshots_daily WHERE day < ?`, now.Add(-dailyHorizon).Format("2006-01-02"))
		if err != nil {
			return 0, 0, fmt.Errorf("retention on snapshots_daily: %w", err)
		}
		n, _ := res.RowsAffected()
		dailyDeleted = int(n)
	}
	return rawDeleted, dailyDeleted, nil
}

// Vacuum runs PRAGMA-gated VACUUM. The caller passes a minimum reclaim
// threshold in bytes; if the freelist holds less than that, VACUUM is skipped
// (it rewrites the whole DB and is expensive). Returns whether VACUUM ran.
func (s *Store) Vacuum(ctx context.Context, minReclaimBytes int64) (ran bool, reclaimable int64, err error) {
	var freelist, pageSize int64
	if err := s.reader.GetContext(ctx, &freelist, `PRAGMA freelist_count`); err != nil {
		return false, 0, fmt.Errorf("reading freelist_count: %w", err)
	}
	if err := s.reader.GetContext(ctx, &pageSize, `PRAGMA page_size`); err != nil {
		return false, 0, fmt.Errorf("reading page_size: %w", err)
	}
	reclaimable = freelist * pageSize
	if reclaimable < minReclaimBytes {
		return false, reclaimable, nil
	}
	if _, err := s.writer.ExecContext(ctx, `VACUUM`); err != nil {
		return false, reclaimable, fmt.Errorf("vacuum: %w", err)
	}
	return true, reclaimable, nil
}

// Optimize runs `PRAGMA optimize`, refreshing the planner's sqlite_stat1
// statistics so the JOIN-heavy reads (dashboard list, scoring prefetch, the
// snapshot window queries) keep choosing good plans as table sizes drift.
// analysis_limit caps per-index sampling so the call stays cheap even on the
// large snapshots tables. Both run on the single writer connection. Cheap and
// safe to call after migrations and at the end of the daily maintenance tick.
func (s *Store) Optimize(ctx context.Context) error {
	if _, err := s.writer.ExecContext(ctx, `PRAGMA analysis_limit=400`); err != nil {
		return fmt.Errorf("setting analysis_limit: %w", err)
	}
	if _, err := s.writer.ExecContext(ctx, `PRAGMA optimize`); err != nil {
		return fmt.Errorf("optimize: %w", err)
	}
	return nil
}

// CheckpointWAL flushes the write-ahead log into the main DB file and truncates
// the -wal file. The ~5k-row ingestion and scoring batches grow the WAL between
// SQLite's own PASSIVE auto-checkpoints, which can never shrink it; a TRUNCATE
// checkpoint at the end of maintenance keeps the on-disk -wal bounded.
func (s *Store) CheckpointWAL(ctx context.Context) error {
	if _, err := s.writer.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	return nil
}
