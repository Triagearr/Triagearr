package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// TorrentFileRow is the persisted view used by the Decider's cross-seed
// pre-filter (max-nlink lookup) and the Actor's T3.5 stat re-check.
type TorrentFileRow struct {
	TorrentHash string     `db:"torrent_hash"`
	RelPath     string     `db:"rel_path"`
	SizeBytes   int64      `db:"size_bytes"`
	Nlink       *int64     `db:"nlink"`
	SampledAt   *time.Time `db:"sampled_at"`
}

// UpsertTorrentFile records one (hash, rel_path) row, refreshing nlink and
// sampled_at when the files-poller revisits. nlink is nullable: a fresh row
// from a qBit-only snapshot (no stat yet) carries NULL until the next poll.
func (s *Store) UpsertTorrentFile(ctx context.Context, hash triagearr.Hash, relPath string, size int64, nlink *int64, sampledAt time.Time) error {
	var sampledArg any
	if sampledAt.IsZero() {
		sampledArg = nil
	} else {
		sampledArg = ts(sampledAt)
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO torrent_files(torrent_hash, rel_path, size_bytes, nlink, sampled_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(torrent_hash, rel_path) DO UPDATE SET
			size_bytes=excluded.size_bytes,
			nlink=excluded.nlink,
			sampled_at=excluded.sampled_at
	`, string(hash), relPath, size, nlink, sampledArg)
	if err != nil {
		return fmt.Errorf("upserting torrent_file %s/%s: %w", hash, relPath, err)
	}
	return nil
}

// UpsertTorrentFiles batches UpsertTorrentFile for one files-poller pass in a
// single transaction with one prepared statement. The poller stats every file
// of every torrent (~50k rows on a large library); writing them one Exec at a
// time meant 50k separate round-trips on the writer connection. Each row's
// SampledAt maps a nil pointer to SQL NULL, same as the single-row variant.
func (s *Store) UpsertTorrentFiles(ctx context.Context, rows []TorrentFileRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for torrent_files batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO torrent_files(torrent_hash, rel_path, size_bytes, nlink, sampled_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(torrent_hash, rel_path) DO UPDATE SET
			size_bytes=excluded.size_bytes,
			nlink=excluded.nlink,
			sampled_at=excluded.sampled_at
	`)
	if err != nil {
		return fmt.Errorf("prepare torrent_files upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, r := range rows {
		var sampledArg any
		if r.SampledAt != nil && !r.SampledAt.IsZero() {
			sampledArg = ts(*r.SampledAt)
		}
		if _, err := stmt.ExecContext(ctx, r.TorrentHash, r.RelPath, r.SizeBytes, r.Nlink, sampledArg); err != nil {
			return fmt.Errorf("upserting torrent_file %s/%s: %w", r.TorrentHash, r.RelPath, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit torrent_files batch: %w", err)
	}
	return nil
}

// TorrentFilesByHash returns all files tracked for the given torrent.
func (s *Store) TorrentFilesByHash(ctx context.Context, hash triagearr.Hash) ([]TorrentFileRow, error) {
	var rows []TorrentFileRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT torrent_hash, rel_path, size_bytes, nlink, sampled_at
		FROM torrent_files
		WHERE torrent_hash = ?
		ORDER BY rel_path
	`, string(hash)); err != nil {
		return nil, fmt.Errorf("listing torrent_files for %s: %w", hash, err)
	}
	return rows, nil
}

// MaxNlinkByHashes returns the max(nlink) per torrent hash, scoped to the
// passed hash set. Hashes with no sampled file (all NULL) are absent from the
// result — callers treat absence as "unsampled, keep eligible" (the Decider
// pre-filter), letting T3.5 catch the conflict atomically at action time.
func (s *Store) MaxNlinkByHashes(ctx context.Context, hashes []triagearr.Hash) (map[triagearr.Hash]int64, error) {
	out := map[triagearr.Hash]int64{}
	if len(hashes) == 0 {
		return out, nil
	}
	placeholders, args := hashPlaceholders(hashes)
	q := `SELECT torrent_hash, MAX(nlink) AS m
	      FROM torrent_files
	      WHERE nlink IS NOT NULL AND torrent_hash IN (` + placeholders + `)
	      GROUP BY torrent_hash`
	rows, err := s.reader.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("max nlink: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h string
		var m int64
		if err := rows.Scan(&h, &m); err != nil {
			return nil, fmt.Errorf("scanning max nlink: %w", err)
		}
		out[triagearr.Hash(h)] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating max nlink: %w", err)
	}
	return out, nil
}
