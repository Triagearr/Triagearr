package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// InsertAction persists a new action row in the `running` state and returns
// its assigned id. started_at is taken from the input.
func (s *Store) InsertAction(ctx context.Context, a triagearr.Action) (int64, error) {
	res, err := s.writer.ExecContext(ctx, `
		INSERT INTO actions(run_id, rank, torrent_hash, started_at, status, freed_bytes)
		VALUES (?, ?, ?, ?, ?, ?)
	`, a.RunID, a.Rank, string(a.TorrentHash), ts(a.StartedAt), string(a.Status), a.FreedBytes)
	if err != nil {
		return 0, fmt.Errorf("inserting action: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading inserted action id: %w", err)
	}
	return id, nil
}

// FinishAction sets the action's terminal status, finished_at and freed_bytes.
func (s *Store) FinishAction(ctx context.Context, id int64, status triagearr.ActionStatus, finishedAt time.Time, freedBytes int64) error {
	res, err := s.writer.ExecContext(ctx, `
		UPDATE actions
		SET status = ?, finished_at = ?, freed_bytes = ?
		WHERE id = ?
	`, string(status), ts(finishedAt), freedBytes, id)
	if err != nil {
		return fmt.Errorf("updating action %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected on action %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("action %d not found", id)
	}
	return nil
}

// AppendAudit writes one audit row attached to an action.
func (s *Store) AppendAudit(ctx context.Context, e triagearr.AuditEntry) error {
	var arrType sql.NullString
	if e.ArrType != "" {
		arrType = sql.NullString{String: e.ArrType, Valid: true}
	}
	var arrFileID sql.NullInt64
	if e.ArrFileID != 0 {
		arrFileID = sql.NullInt64{Int64: e.ArrFileID, Valid: true}
	}
	var detail sql.NullString
	if e.Detail != "" {
		detail = sql.NullString{String: e.Detail, Valid: true}
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO audit_log(action_id, ts, step, arr_type, arr_file_id, outcome, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, e.ActionID, ts(e.Timestamp), string(e.Step), arrType, arrFileID, string(e.Outcome), detail)
	if err != nil {
		return fmt.Errorf("appending audit row: %w", err)
	}
	return nil
}

type actionRow struct {
	ID          int64        `db:"id"`
	RunID       int64        `db:"run_id"`
	Rank        int          `db:"rank"`
	TorrentHash string       `db:"torrent_hash"`
	StartedAt   time.Time    `db:"started_at"`
	FinishedAt  sql.NullTime `db:"finished_at"`
	Status      string       `db:"status"`
	FreedBytes  int64        `db:"freed_bytes"`
}

func (r actionRow) toAction() triagearr.Action {
	a := triagearr.Action{
		ID:          r.ID,
		RunID:       r.RunID,
		Rank:        r.Rank,
		TorrentHash: triagearr.Hash(r.TorrentHash),
		StartedAt:   r.StartedAt,
		Status:      triagearr.ActionStatus(r.Status),
		FreedBytes:  r.FreedBytes,
	}
	if r.FinishedAt.Valid {
		a.FinishedAt = r.FinishedAt.Time
	}
	return a
}

// GetAction returns one action by id. Returns sql.ErrNoRows when unknown.
func (s *Store) GetAction(ctx context.Context, id int64) (triagearr.Action, error) {
	var row actionRow
	if err := s.reader.GetContext(ctx, &row, `
		SELECT id, run_id, rank, torrent_hash, started_at, finished_at, status, freed_bytes
		FROM actions WHERE id = ?
	`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return triagearr.Action{}, err
		}
		return triagearr.Action{}, fmt.Errorf("loading action %d: %w", id, err)
	}
	return row.toAction(), nil
}

// ListActionsByRun returns every action attached to a run, ordered by rank.
func (s *Store) ListActionsByRun(ctx context.Context, runID int64) ([]triagearr.Action, error) {
	var rows []actionRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT id, run_id, rank, torrent_hash, started_at, finished_at, status, freed_bytes
		FROM actions WHERE run_id = ? ORDER BY rank ASC
	`, runID); err != nil {
		return nil, fmt.Errorf("listing actions for run %d: %w", runID, err)
	}
	out := make([]triagearr.Action, len(rows))
	for i, r := range rows {
		out[i] = r.toAction()
	}
	return out, nil
}

type auditRow struct {
	ID        int64          `db:"id"`
	ActionID  int64          `db:"action_id"`
	Timestamp time.Time      `db:"ts"`
	Step      string         `db:"step"`
	ArrType   sql.NullString `db:"arr_type"`
	ArrFileID sql.NullInt64  `db:"arr_file_id"`
	Outcome   string         `db:"outcome"`
	Detail    sql.NullString `db:"detail"`
}

func (r auditRow) toEntry() triagearr.AuditEntry {
	e := triagearr.AuditEntry{
		ID:        r.ID,
		ActionID:  r.ActionID,
		Timestamp: r.Timestamp,
		Step:      triagearr.AuditStep(r.Step),
		Outcome:   triagearr.AuditOutcome(r.Outcome),
	}
	if r.ArrType.Valid {
		e.ArrType = r.ArrType.String
	}
	if r.ArrFileID.Valid {
		e.ArrFileID = r.ArrFileID.Int64
	}
	if r.Detail.Valid {
		e.Detail = r.Detail.String
	}
	return e
}

// ListAuditByAction returns every audit row for an action, in insertion order.
func (s *Store) ListAuditByAction(ctx context.Context, actionID int64) ([]triagearr.AuditEntry, error) {
	var rows []auditRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT id, action_id, ts, step, arr_type, arr_file_id, outcome, detail
		FROM audit_log WHERE action_id = ? ORDER BY id ASC
	`, actionID); err != nil {
		return nil, fmt.Errorf("listing audit for action %d: %w", actionID, err)
	}
	out := make([]triagearr.AuditEntry, len(rows))
	for i, r := range rows {
		out[i] = r.toEntry()
	}
	return out, nil
}
