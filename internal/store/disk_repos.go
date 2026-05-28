package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// InsertDiskUsage appends a disk-pressure observation.
func (s *Store) InsertDiskUsage(ctx context.Context, d triagearr.DiskUsage) error {
	_, err := s.writer.ExecContext(ctx, `
		INSERT OR REPLACE INTO disk_pressure(ts, path, total_bytes, used_bytes, free_bytes, free_percent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, ts(d.Timestamp), d.Path, d.TotalBytes, d.UsedBytes, d.FreeBytes, d.FreePercent)
	if err != nil {
		return fmt.Errorf("inserting disk_pressure @%s: %w", d.Timestamp, err)
	}
	return nil
}

// LatestDiskUsage returns the most recent disk-pressure reading, or nil when
// no snapshot has been recorded yet.
func (s *Store) LatestDiskUsage(ctx context.Context) (*triagearr.DiskUsage, error) {
	type row struct {
		Path        string    `db:"path"`
		Ts          time.Time `db:"ts"`
		TotalBytes  int64     `db:"total_bytes"`
		UsedBytes   int64     `db:"used_bytes"`
		FreeBytes   int64     `db:"free_bytes"`
		FreePercent float64   `db:"free_percent"`
	}
	var rows []row
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT path, ts, total_bytes, used_bytes, free_bytes, free_percent
		FROM disk_pressure
		ORDER BY ts DESC
		LIMIT 1
	`); err != nil {
		return nil, fmt.Errorf("reading latest disk_pressure: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &triagearr.DiskUsage{
		Path:      r.Path,
		Timestamp: r.Ts,
		// The bytes columns were written from uint64s (Statfs results) and
		// stored as INTEGER. The int64→uint64 round-trip is safe by construction.
		TotalBytes:  uint64(r.TotalBytes), //nolint:gosec // value originated from uint64
		UsedBytes:   uint64(r.UsedBytes),  //nolint:gosec // value originated from uint64
		FreeBytes:   uint64(r.FreeBytes),  //nolint:gosec // value originated from uint64
		FreePercent: r.FreePercent,
	}, nil
}
