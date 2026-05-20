package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// InsertRun persists a Run header and returns its assigned ID.
func (s *Store) InsertRun(ctx context.Context, r triagearr.Run) (int64, error) {
	var volume sql.NullString
	if r.VolumeName != "" {
		volume = sql.NullString{String: r.VolumeName, Valid: true}
	}
	var freePct, targetPct sql.NullFloat64
	if r.VolumeName != "" {
		freePct = sql.NullFloat64{Float64: r.FreePctAtFire, Valid: true}
		targetPct = sql.NullFloat64{Float64: r.TargetFreePct, Valid: true}
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO runs(triggered_by, triggered_at, mode, volume_name,
		                 free_pct_at_fire, target_free_pct,
		                 estimated_freed_bytes, stop_reason, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, string(r.TriggeredBy), ts(r.TriggeredAt), r.Mode, volume,
		freePct, targetPct,
		r.EstimatedFreedBytes, string(r.StopReason), r.Status)
	if err != nil {
		return 0, fmt.Errorf("inserting run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading inserted run id: %w", err)
	}
	return id, nil
}

// InsertRunItems writes the candidate set for a run in a single transaction.
func (s *Store) InsertRunItems(ctx context.Context, runID int64, items []triagearr.RunItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for run_items: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO run_items(run_id, rank, torrent_hash, score, size_bytes, would_free_bytes)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare run_items insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, it := range items {
		if _, err := stmt.ExecContext(ctx, runID, it.Rank, string(it.TorrentHash), it.Score, it.SizeBytes, it.WouldFreeBytes); err != nil {
			return fmt.Errorf("insert run_item rank=%d: %w", it.Rank, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run_items: %w", err)
	}
	return nil
}

// runRow is the on-disk shape of one runs row.
type runRow struct {
	ID                  int64           `db:"id"`
	TriggeredBy         string          `db:"triggered_by"`
	TriggeredAt         time.Time       `db:"triggered_at"`
	Mode                string          `db:"mode"`
	VolumeName          sql.NullString  `db:"volume_name"`
	FreePctAtFire       sql.NullFloat64 `db:"free_pct_at_fire"`
	TargetFreePct       sql.NullFloat64 `db:"target_free_pct"`
	EstimatedFreedBytes int64           `db:"estimated_freed_bytes"`
	StopReason          string          `db:"stop_reason"`
	Status              string          `db:"status"`
}

func (r runRow) toRun() triagearr.Run {
	return triagearr.Run{
		ID:                  r.ID,
		TriggeredBy:         triagearr.RunTrigger(r.TriggeredBy),
		TriggeredAt:         r.TriggeredAt,
		Mode:                r.Mode,
		VolumeName:          r.VolumeName.String,
		FreePctAtFire:       r.FreePctAtFire.Float64,
		TargetFreePct:       r.TargetFreePct.Float64,
		EstimatedFreedBytes: r.EstimatedFreedBytes,
		StopReason:          triagearr.RunStopReason(r.StopReason),
		Status:              r.Status,
	}
}

// GetRun returns a run by id. Returns sql.ErrNoRows when unknown.
func (s *Store) GetRun(ctx context.Context, id int64) (triagearr.Run, []triagearr.RunItem, error) {
	var row runRow
	if err := s.db.GetContext(ctx, &row, `
		SELECT id, triggered_by, triggered_at, mode, volume_name,
		       free_pct_at_fire, target_free_pct,
		       estimated_freed_bytes, stop_reason, status
		FROM runs WHERE id = ?
	`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return triagearr.Run{}, nil, err
		}
		return triagearr.Run{}, nil, fmt.Errorf("loading run %d: %w", id, err)
	}
	type itemRow struct {
		Rank           int     `db:"rank"`
		TorrentHash    string  `db:"torrent_hash"`
		Score          float64 `db:"score"`
		SizeBytes      int64   `db:"size_bytes"`
		WouldFreeBytes int64   `db:"would_free_bytes"`
	}
	var items []itemRow
	if err := s.db.SelectContext(ctx, &items, `
		SELECT rank, torrent_hash, score, size_bytes, would_free_bytes
		FROM run_items WHERE run_id = ? ORDER BY rank ASC
	`, id); err != nil {
		return triagearr.Run{}, nil, fmt.Errorf("loading items for run %d: %w", id, err)
	}
	out := make([]triagearr.RunItem, len(items))
	for i, it := range items {
		out[i] = triagearr.RunItem{
			RunID:          id,
			Rank:           it.Rank,
			TorrentHash:    triagearr.Hash(it.TorrentHash),
			Score:          it.Score,
			SizeBytes:      it.SizeBytes,
			WouldFreeBytes: it.WouldFreeBytes,
		}
	}
	return row.toRun(), out, nil
}

// TorrentBasic is the slim torrent view the Decider needs: hash + save_path
// (for volume attribution) + size (for budget accumulation).
type TorrentBasic struct {
	Hash     string `db:"hash"`
	SavePath string `db:"save_path"`
	Size     int64  `db:"size"`
}

// ListTorrentsBasic returns every torrent in the store, slim columns only.
// The result is unordered; the Decider zips it with ListScores by hash.
func (s *Store) ListTorrentsBasic(ctx context.Context) ([]TorrentBasic, error) {
	var rows []TorrentBasic
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT hash, save_path, size FROM torrents
	`); err != nil {
		return nil, fmt.Errorf("listing torrents basic: %w", err)
	}
	return rows, nil
}

// ListRunsOpts tunes ListRuns.
type ListRunsOpts struct {
	Limit int
}

// ListRuns returns runs ordered by triggered_at descending (most recent first).
func (s *Store) ListRuns(ctx context.Context, opts ListRunsOpts) ([]triagearr.Run, error) {
	q := `
		SELECT id, triggered_by, triggered_at, mode, volume_name,
		       free_pct_at_fire, target_free_pct,
		       estimated_freed_bytes, stop_reason, status
		FROM runs
		ORDER BY triggered_at DESC, id DESC
	`
	if opts.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	var rows []runRow
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	out := make([]triagearr.Run, len(rows))
	for i, r := range rows {
		out[i] = r.toRun()
	}
	return out, nil
}
