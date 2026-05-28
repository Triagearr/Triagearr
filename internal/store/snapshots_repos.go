package store

import (
	"context"
	"fmt"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// InsertSnapshot appends a point-in-time observation for a torrent.
func (s *Store) InsertSnapshot(ctx context.Context, snap triagearr.Snapshot) error {
	_, err := s.writer.ExecContext(ctx, `
		INSERT OR REPLACE INTO snapshots_raw(torrent_hash, ts, ratio, uploaded, seeders, leechers, state, last_activity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, string(snap.Hash), ts(snap.Timestamp), snap.Ratio, snap.Uploaded, snap.Seeders, snap.Leechers, string(snap.State), ts(snap.LastActivity))
	if err != nil {
		return fmt.Errorf("inserting snapshot %s@%s: %w", snap.Hash, snap.Timestamp, err)
	}
	return nil
}

// InsertSnapshots batches InsertSnapshot for the qBit-tick fanout. Same
// trade-off as UpsertTorrents: one tx + one prepared statement per tick.
func (s *Store) InsertSnapshots(ctx context.Context, snaps []triagearr.Snapshot) error {
	if len(snaps) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for snapshots batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT OR REPLACE INTO snapshots_raw(torrent_hash, ts, ratio, uploaded, seeders, leechers, state, last_activity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare snapshots insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, snap := range snaps {
		if _, err := stmt.ExecContext(ctx,
			string(snap.Hash), ts(snap.Timestamp), snap.Ratio, snap.Uploaded,
			snap.Seeders, snap.Leechers, string(snap.State), ts(snap.LastActivity),
		); err != nil {
			return fmt.Errorf("inserting snapshot %s@%s: %w", snap.Hash, snap.Timestamp, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshots batch: %w", err)
	}
	return nil
}
