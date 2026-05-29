package store

import (
	"context"
	"fmt"
	"time"
)

// TorrentClientInstanceRow is the persisted view of a torrent client's health.
// Mirrors ArrInstanceRow: distinct from torrent_client_connections (ADR-0025),
// which owns the config; this table is just the most recent health probe.
type TorrentClientInstanceRow struct {
	Kind            string     `db:"kind"`
	URL             string     `db:"url"`
	Healthy         bool       `db:"healthy"`
	LastHealthCheck *time.Time `db:"last_health_check"`
	LastError       *string    `db:"last_error"`
}

// UpsertTorrentClientInstance records the last-known health for a torrent client.
func (s *Store) UpsertTorrentClientInstance(ctx context.Context, kind, url string, healthy bool, lastErr string) error {
	now := time.Now().UTC()
	var lastErrCol any
	if lastErr != "" {
		lastErrCol = lastErr
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO torrent_client_instances(kind, url, healthy, last_health_check, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET
			url=excluded.url,
			healthy=excluded.healthy,
			last_health_check=excluded.last_health_check,
			last_error=excluded.last_error
	`, kind, url, healthy, ts(now), lastErrCol)
	if err != nil {
		return fmt.Errorf("upserting torrent_client_instance %s: %w", kind, err)
	}
	return nil
}

// ListTorrentClientInstances returns every recorded torrent client instance.
func (s *Store) ListTorrentClientInstances(ctx context.Context) ([]TorrentClientInstanceRow, error) {
	var rows []TorrentClientInstanceRow
	if err := s.reader.SelectContext(ctx, &rows,
		`SELECT kind, url, healthy, last_health_check, last_error FROM torrent_client_instances ORDER BY kind`,
	); err != nil {
		return nil, fmt.Errorf("listing torrent_client_instances: %w", err)
	}
	return rows, nil
}
