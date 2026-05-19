package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ts formats a time.Time for storage. We use ISO 8601 / RFC 3339 nanosecond
// precision in UTC so any sqlite tool (usql, sqlite3 CLI, Datasette, DBeaver)
// can render timestamps directly. modernc.org/sqlite parses this format back
// into time.Time transparently on read.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// -----------------------------------------------------------------------------
// arr_instances
// -----------------------------------------------------------------------------

// ArrInstanceRow is the persisted view of an *arr instance health.
type ArrInstanceRow struct {
	Name            string     `db:"name"`
	Type            string     `db:"type"`
	URL             string     `db:"url"`
	Healthy         bool       `db:"healthy"`
	LastHealthCheck *time.Time `db:"last_health_check"`
	LastError       *string    `db:"last_error"`
}

// UpsertArrInstance records the last-known health for an *arr instance.
func (s *Store) UpsertArrInstance(ctx context.Context, name string, typ triagearr.ArrType, url string, healthy bool, lastErr string) error {
	now := time.Now().UTC()
	var lastErrCol any
	if lastErr != "" {
		lastErrCol = lastErr
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO arr_instances(name, type, url, healthy, last_health_check, last_error)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name, type) DO UPDATE SET
			url=excluded.url,
			healthy=excluded.healthy,
			last_health_check=excluded.last_health_check,
			last_error=excluded.last_error
	`, name, string(typ), url, healthy, ts(now), lastErrCol)
	if err != nil {
		return fmt.Errorf("upserting arr_instance %s/%s: %w", typ, name, err)
	}
	return nil
}

// ListArrInstances returns every recorded *arr instance.
func (s *Store) ListArrInstances(ctx context.Context) ([]ArrInstanceRow, error) {
	var rows []ArrInstanceRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT name, type, url, healthy, last_health_check, last_error FROM arr_instances ORDER BY type, name`,
	); err != nil {
		return nil, fmt.Errorf("listing arr_instances: %w", err)
	}
	return rows, nil
}

// -----------------------------------------------------------------------------
// torrents + snapshots_raw
// -----------------------------------------------------------------------------

// UpsertTorrent records (or refreshes) the current state of a torrent.
func (s *Store) UpsertTorrent(ctx context.Context, t triagearr.Torrent) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO torrents(hash, name, category, save_path, size, added_on, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO UPDATE SET
			name=excluded.name,
			category=excluded.category,
			save_path=excluded.save_path,
			size=excluded.size,
			added_on=excluded.added_on,
			last_seen=excluded.last_seen
	`, string(t.Hash), t.Name, t.Category, t.SavePath, t.Size, ts(t.AddedOn), ts(now))
	if err != nil {
		return fmt.Errorf("upserting torrent %s: %w", t.Hash, err)
	}
	return nil
}

// InsertSnapshot appends a point-in-time observation for a torrent.
func (s *Store) InsertSnapshot(ctx context.Context, snap triagearr.Snapshot) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO snapshots_raw(torrent_hash, ts, ratio, uploaded, seeders, leechers, state, last_activity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, string(snap.Hash), ts(snap.Timestamp), snap.Ratio, snap.Uploaded, snap.Seeders, snap.Leechers, string(snap.State), ts(snap.LastActivity))
	if err != nil {
		return fmt.Errorf("inserting snapshot %s@%s: %w", snap.Hash, snap.Timestamp, err)
	}
	return nil
}

// TorrentRow is a denormalised view used by `inspect torrents`. It joins the
// latest snapshot onto each torrent.
type TorrentRow struct {
	Hash       string     `db:"hash"`
	Name       string     `db:"name"`
	Category   string     `db:"category"`
	Size       int64      `db:"size"`
	AddedOn    time.Time  `db:"added_on"`
	LastSeen   time.Time  `db:"last_seen"`
	Ratio      *float64   `db:"ratio"`
	Seeders    *int       `db:"seeders"`
	Leechers   *int       `db:"leechers"`
	State      *string    `db:"state"`
	SnapshotAt *time.Time `db:"snap_ts"`
}

// ListTorrentsLatest returns torrents with their latest snapshot, sorted+limited.
// sortBy: name|seeders|ratio|size|last_seen. limit: <=0 means no limit.
func (s *Store) ListTorrentsLatest(ctx context.Context, sortBy string, limit int) ([]TorrentRow, error) {
	orderBy, err := torrentOrderBy(sortBy)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT t.hash, t.name, t.category, t.size, t.added_on, t.last_seen,
		       s.ratio AS ratio, s.seeders AS seeders, s.leechers AS leechers,
		       s.state AS state, s.ts AS snap_ts
		FROM torrents t
		LEFT JOIN snapshots_raw s
		  ON s.torrent_hash = t.hash
		 AND s.ts = (SELECT MAX(ts) FROM snapshots_raw WHERE torrent_hash = t.hash)
		ORDER BY ` + orderBy
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	var rows []TorrentRow
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, fmt.Errorf("listing torrents: %w", err)
	}
	return rows, nil
}

func torrentOrderBy(sortBy string) (string, error) {
	switch strings.ToLower(sortBy) {
	case "", "name":
		return "t.name ASC", nil
	case "seeders":
		return "s.seeders DESC NULLS LAST, t.name ASC", nil
	case "ratio":
		return "s.ratio DESC NULLS LAST, t.name ASC", nil
	case "size":
		return "t.size DESC, t.name ASC", nil
	case "last_seen":
		return "t.last_seen DESC, t.name ASC", nil
	default:
		return "", fmt.Errorf("unknown sort key %q (want: name|seeders|ratio|size|last_seen)", sortBy)
	}
}

// -----------------------------------------------------------------------------
// media
// -----------------------------------------------------------------------------

// UpsertMedia records a media item from an *arr.
func (s *Store) UpsertMedia(ctx context.Context, m triagearr.MediaItem) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media(id, arr_name, arr_type, title, path, size, tags, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, arr_name, arr_type) DO UPDATE SET
			title=excluded.title,
			path=excluded.path,
			size=excluded.size,
			tags=excluded.tags,
			last_seen=excluded.last_seen
	`, int64(m.ID), m.ArrName, string(m.ArrType), m.Title, m.Path, m.Size, strings.Join(m.Tags, ","), ts(now))
	if err != nil {
		return fmt.Errorf("upserting media %s/%s/%d: %w", m.ArrType, m.ArrName, m.ID, err)
	}
	return nil
}

// CountMedia returns the number of media rows for the given *arr (for testing/inspect).
func (s *Store) CountMedia(ctx context.Context, arrName string, arrType triagearr.ArrType) (int, error) {
	var n int
	if err := s.db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM media WHERE arr_name = ? AND arr_type = ?`,
		arrName, string(arrType),
	); err != nil {
		return 0, fmt.Errorf("counting media: %w", err)
	}
	return n, nil
}

// -----------------------------------------------------------------------------
// disk_pressure
// -----------------------------------------------------------------------------

// InsertDiskUsage appends a disk-pressure observation.
func (s *Store) InsertDiskUsage(ctx context.Context, d triagearr.DiskUsage) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO disk_pressure(volume_name, ts, path, total_bytes, used_bytes, free_bytes, free_percent)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, d.VolumeName, ts(d.Timestamp), d.Path, d.TotalBytes, d.UsedBytes, d.FreeBytes, d.FreePercent)
	if err != nil {
		return fmt.Errorf("inserting disk_pressure %s@%s: %w", d.VolumeName, d.Timestamp, err)
	}
	return nil
}

// LatestDiskUsage returns the latest reading per volume.
func (s *Store) LatestDiskUsage(ctx context.Context) ([]triagearr.DiskUsage, error) {
	type row struct {
		VolumeName  string    `db:"volume_name"`
		Path        string    `db:"path"`
		Ts          time.Time `db:"ts"`
		TotalBytes  int64     `db:"total_bytes"`
		UsedBytes   int64     `db:"used_bytes"`
		FreeBytes   int64     `db:"free_bytes"`
		FreePercent float64   `db:"free_percent"`
	}
	var rows []row
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT volume_name, path, ts, total_bytes, used_bytes, free_bytes, free_percent
		FROM disk_pressure d
		WHERE ts = (SELECT MAX(ts) FROM disk_pressure WHERE volume_name = d.volume_name)
		ORDER BY volume_name
	`); err != nil {
		return nil, fmt.Errorf("listing latest disk_pressure: %w", err)
	}
	out := make([]triagearr.DiskUsage, len(rows))
	for i, r := range rows {
		out[i] = triagearr.DiskUsage{
			VolumeName: r.VolumeName,
			Path:       r.Path,
			Timestamp:  r.Ts,
			// The bytes columns were written from uint64s (Statfs results) and
			// stored as INTEGER. The int64→uint64 round-trip is safe by construction.
			TotalBytes:  uint64(r.TotalBytes), //nolint:gosec // value originated from uint64
			UsedBytes:   uint64(r.UsedBytes),  //nolint:gosec // value originated from uint64
			FreeBytes:   uint64(r.FreeBytes),  //nolint:gosec // value originated from uint64
			FreePercent: r.FreePercent,
		}
	}
	return out, nil
}
