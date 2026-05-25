package store

import (
	"context"
	"errors"
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
	Kind            string     `db:"kind"`
	URL             string     `db:"url"`
	Healthy         bool       `db:"healthy"`
	LastHealthCheck *time.Time `db:"last_health_check"`
	LastError       *string    `db:"last_error"`
}

// UpsertArrInstance records the last-known health for an *arr instance.
func (s *Store) UpsertArrInstance(ctx context.Context, typ triagearr.ArrType, url string, healthy bool, lastErr string) error {
	now := time.Now().UTC()
	var lastErrCol any
	if lastErr != "" {
		lastErrCol = lastErr
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO arr_instances(kind, url, healthy, last_health_check, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET
			url=excluded.url,
			healthy=excluded.healthy,
			last_health_check=excluded.last_health_check,
			last_error=excluded.last_error
	`, string(typ), url, healthy, ts(now), lastErrCol)
	if err != nil {
		return fmt.Errorf("upserting arr_instance %s: %w", typ, err)
	}
	return nil
}

// ListArrInstances returns every recorded *arr instance.
func (s *Store) ListArrInstances(ctx context.Context) ([]ArrInstanceRow, error) {
	var rows []ArrInstanceRow
	if err := s.reader.SelectContext(ctx, &rows,
		`SELECT kind, url, healthy, last_health_check, last_error FROM arr_instances ORDER BY kind`,
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
	var completion any
	if !t.CompletionOn.IsZero() {
		completion = ts(t.CompletionOn)
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO torrents(hash, name, category, save_path, size, added_on, completion_on, private, tags, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO UPDATE SET
			name=excluded.name,
			category=excluded.category,
			save_path=excluded.save_path,
			size=excluded.size,
			added_on=excluded.added_on,
			completion_on=excluded.completion_on,
			private=excluded.private,
			tags=excluded.tags,
			last_seen=excluded.last_seen
	`, string(t.Hash), t.Name, t.Category, t.SavePath, t.Size, ts(t.AddedOn), completion, t.Private, t.Tags, ts(now))
	if err != nil {
		return fmt.Errorf("upserting torrent %s: %w", t.Hash, err)
	}
	return nil
}

// ErrHashNotFound is returned by ResolveTorrentHash when no torrent matches
// the given prefix.
var ErrHashNotFound = errors.New("torrent hash not found")

// ErrHashAmbiguous is returned by ResolveTorrentHash when several torrents
// share the given prefix. The message lists up to a handful of candidates so
// the CLI can echo it as-is.
type ErrHashAmbiguous struct {
	Prefix     string
	Candidates []triagearr.Hash
}

func (e *ErrHashAmbiguous) Error() string {
	preview := make([]string, 0, len(e.Candidates))
	for _, h := range e.Candidates {
		preview = append(preview, string(h))
	}
	return fmt.Sprintf("ambiguous hash prefix %q matches %d torrents: %s",
		e.Prefix, len(e.Candidates), strings.Join(preview, ", "))
}

// ResolveTorrentHash returns the unique full hash matching the given prefix.
// Lowercased before matching since qBit stores lowercase. Full 40-char hashes
// pass through after a single existence check.
func (s *Store) ResolveTorrentHash(ctx context.Context, prefix string) (triagearr.Hash, error) {
	p := strings.ToLower(strings.TrimSpace(prefix))
	if p == "" {
		return "", ErrHashNotFound
	}
	var matches []string
	if err := s.reader.SelectContext(ctx, &matches,
		`SELECT hash FROM torrents WHERE hash LIKE ? || '%' ORDER BY hash LIMIT 8`,
		p,
	); err != nil {
		return "", fmt.Errorf("resolving hash prefix %q: %w", prefix, err)
	}
	switch len(matches) {
	case 0:
		return "", ErrHashNotFound
	case 1:
		return triagearr.Hash(matches[0]), nil
	default:
		cands := make([]triagearr.Hash, len(matches))
		for i, h := range matches {
			cands[i] = triagearr.Hash(h)
		}
		return "", &ErrHashAmbiguous{Prefix: prefix, Candidates: cands}
	}
}

// ListTorrentHashes returns every torrent hash currently tracked. Used by the
// tracker poller to enumerate known torrents without loading the full row
// payload.
func (s *Store) ListTorrentHashes(ctx context.Context) ([]triagearr.Hash, error) {
	var raw []string
	if err := s.reader.SelectContext(ctx, &raw, `SELECT hash FROM torrents ORDER BY hash`); err != nil {
		return nil, fmt.Errorf("listing torrent hashes: %w", err)
	}
	out := make([]triagearr.Hash, len(raw))
	for i, h := range raw {
		out[i] = triagearr.Hash(h)
	}
	return out, nil
}

// TorrentSavePath returns the persisted save_path for a single torrent hash.
// Used by the Actor's T3.5 step to build absolute paths from the per-file
// qBit names. Returns the sql.ErrNoRows wrapped error when unknown.
func (s *Store) TorrentSavePath(ctx context.Context, hash triagearr.Hash) (string, error) {
	var sp string
	if err := s.reader.GetContext(ctx, &sp,
		`SELECT save_path FROM torrents WHERE hash = ?`, string(hash)); err != nil {
		return "", fmt.Errorf("loading save_path for %s: %w", hash, err)
	}
	return sp, nil
}

// TorrentNamesByHashes resolves display names for a set of torrent hashes.
// Hashes absent from the torrents table are omitted from the result — callers
// fall back to the raw hash for display.
func (s *Store) TorrentNamesByHashes(ctx context.Context, hashes []triagearr.Hash) (map[triagearr.Hash]string, error) {
	out := make(map[triagearr.Hash]string, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(hashes))
	args := make([]any, len(hashes))
	for i, h := range hashes {
		placeholders[i] = "?"
		args[i] = string(h)
	}
	var rows []struct {
		Hash string `db:"hash"`
		Name string `db:"name"`
	}
	q := "SELECT hash, name FROM torrents WHERE hash IN (" + strings.Join(placeholders, ",") + ")"
	if err := s.reader.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("resolving torrent names: %w", err)
	}
	for _, r := range rows {
		out[triagearr.Hash(r.Hash)] = r.Name
	}
	return out, nil
}

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

// TorrentRow is a denormalised view used by `inspect torrents` and the
// dashboard's torrent list. It joins the latest snapshot onto each torrent.
// The score-related fields are only populated by ListTorrentsFiltered, which
// joins the scores table; other callers leave them at their zero value.
type TorrentRow struct {
	Hash       string     `db:"hash"`
	Name       string     `db:"name"`
	Category   string     `db:"category"`
	Size       int64      `db:"size"`
	AddedOn    time.Time  `db:"added_on"`
	LastSeen   time.Time  `db:"last_seen"`
	Private    bool       `db:"private"`
	Ratio      *float64   `db:"ratio"`
	Seeders    *int       `db:"seeders"`
	Leechers   *int       `db:"leechers"`
	State      *string    `db:"state"`
	SnapshotAt *time.Time `db:"snap_ts"`

	Score           *float64 `db:"score"`
	Excluded        *bool    `db:"excluded"`
	AnyTrackerAlive *bool    `db:"any_tracker_alive"`
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
		LEFT JOIN (
		    SELECT torrent_hash, MAX(ts) AS ts FROM snapshots_raw GROUP BY torrent_hash
		) sm ON sm.torrent_hash = t.hash
		LEFT JOIN snapshots_raw s
		  ON s.torrent_hash = sm.torrent_hash AND s.ts = sm.ts
		ORDER BY ` + orderBy
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	var rows []TorrentRow
	if err := s.reader.SelectContext(ctx, &rows, q); err != nil {
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
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO media(id, arr_type, title, title_slug, path, size, tags, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, arr_type) DO UPDATE SET
			title=excluded.title,
			title_slug=excluded.title_slug,
			path=excluded.path,
			size=excluded.size,
			tags=excluded.tags,
			last_seen=excluded.last_seen
	`, int64(m.ID), string(m.ArrType), m.Title, m.TitleSlug, m.Path, m.Size, strings.Join(m.Tags, ","), ts(now))
	if err != nil {
		return fmt.Errorf("upserting media %s/%d: %w", m.ArrType, m.ID, err)
	}
	return nil
}

// CountMedia returns the number of media rows for the given *arr (for testing/inspect).
func (s *Store) CountMedia(ctx context.Context, arrType triagearr.ArrType) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM media WHERE arr_type = ?`,
		string(arrType),
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

// -----------------------------------------------------------------------------
// torrent_trackers (ADR-0009)
// -----------------------------------------------------------------------------

// ReplaceTrackers atomically replaces the set of trackers for one torrent.
// Trackers disappear from qBit (user removed one) — without this, stale rows
// would survive forever.
//
// first_seen_dead is preserved across the rewrite when a tracker stays in
// not_working, set to now on the alive→dead transition (or first observation
// of a dead tracker), and cleared back to NULL when the tracker recovers.
// qBit does not expose a "status changed at" field, so the transition must
// be observed here.
func (s *Store) ReplaceTrackers(ctx context.Context, hash triagearr.Hash, infos []triagearr.TrackerInfo) error {
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for trackers %s: %w", hash, err)
	}
	defer func() { _ = tx.Rollback() }()

	type prev struct {
		status        int
		firstSeenDead *time.Time
	}
	prevByURL := map[string]prev{}
	rows, err := tx.QueryxContext(ctx,
		`SELECT tracker_url, status, first_seen_dead FROM torrent_trackers WHERE torrent_hash = ?`,
		string(hash),
	)
	if err != nil {
		return fmt.Errorf("loading prior trackers %s: %w", hash, err)
	}
	for rows.Next() {
		var u string
		var st int
		var fsd *time.Time
		if err := rows.Scan(&u, &st, &fsd); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scanning prior tracker %s: %w", hash, err)
		}
		prevByURL[u] = prev{status: st, firstSeenDead: fsd}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterating prior trackers %s: %w", hash, err)
	}
	_ = rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_trackers WHERE torrent_hash = ?`, string(hash)); err != nil {
		return fmt.Errorf("clearing trackers %s: %w", hash, err)
	}
	now := time.Now().UTC()
	deadStatus := int(triagearr.TrackerNotWorking)
	for _, info := range infos {
		var firstSeenDead any
		if int(info.Status) == deadStatus {
			if p, ok := prevByURL[info.URL]; ok && p.status == deadStatus && p.firstSeenDead != nil {
				firstSeenDead = ts(*p.firstSeenDead)
			} else {
				firstSeenDead = ts(now)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO torrent_trackers(torrent_hash, tracker_url, tracker_host, status, last_msg, last_checked, first_seen_dead)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, string(hash), info.URL, info.Host, int(info.Status), info.Msg, ts(now), firstSeenDead); err != nil {
			return fmt.Errorf("inserting tracker %s/%s: %w", hash, info.URL, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit trackers %s: %w", hash, err)
	}
	return nil
}

// TrackerRow is the persisted view used by `inspect trackers`.
type TrackerRow struct {
	TorrentHash   string                  `db:"torrent_hash"`
	URL           string                  `db:"tracker_url"`
	Host          string                  `db:"tracker_host"`
	Status        triagearr.TrackerStatus `db:"status"`
	Msg           string                  `db:"last_msg"`
	LastChecked   time.Time               `db:"last_checked"`
	FirstSeenDead *time.Time              `db:"first_seen_dead"`
}

// ListTrackers returns all trackers attached to a torrent.
func (s *Store) ListTrackers(ctx context.Context, hash triagearr.Hash) ([]TrackerRow, error) {
	var rows []TrackerRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT torrent_hash, tracker_url, tracker_host, status, last_msg, last_checked, first_seen_dead
		FROM torrent_trackers
		WHERE torrent_hash = ?
		ORDER BY tracker_host, tracker_url
	`, string(hash)); err != nil {
		return nil, fmt.Errorf("listing trackers for %s: %w", hash, err)
	}
	return rows, nil
}

// -----------------------------------------------------------------------------
// media_files
// -----------------------------------------------------------------------------

// UpsertMediaFile records (or refreshes) one *arr-owned file. The file_id is
// the *arr-side primary key (episodeFile.id / movieFile.id), reused by M5
// Actor for granular DELETEs.
func (s *Store) UpsertMediaFile(ctx context.Context, f triagearr.MediaFile) error {
	now := time.Now().UTC()
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO media_files(arr_type, file_id, media_id, path, size, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(arr_type, file_id) DO UPDATE SET
			media_id=excluded.media_id,
			path=excluded.path,
			size=excluded.size,
			last_seen=excluded.last_seen
	`, string(f.ArrType), f.FileID, int64(f.MediaID), f.Path, f.Size, ts(now))
	if err != nil {
		return fmt.Errorf("upserting media_file %s/%d: %w", f.ArrType, f.FileID, err)
	}
	return nil
}

// MediaFileRow is the persisted view used by `inspect media` and the linker.
type MediaFileRow struct {
	ArrType  string    `db:"arr_type"`
	FileID   int64     `db:"file_id"`
	MediaID  int64     `db:"media_id"`
	Path     string    `db:"path"`
	Size     int64     `db:"size"`
	LastSeen time.Time `db:"last_seen"`
}

// ListMediaFilesByMedia returns the files attached to one media item.
func (s *Store) ListMediaFilesByMedia(ctx context.Context, arrType triagearr.ArrType, mediaID triagearr.MediaID) ([]MediaFileRow, error) {
	var rows []MediaFileRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT arr_type, file_id, media_id, path, size, last_seen
		FROM media_files
		WHERE arr_type = ? AND media_id = ?
		ORDER BY path
	`, string(arrType), int64(mediaID)); err != nil {
		return nil, fmt.Errorf("listing media_files: %w", err)
	}
	return rows, nil
}

// -----------------------------------------------------------------------------
// torrent_files (per-file nlink capture for cross-seed pre-filter and T3.5)
// -----------------------------------------------------------------------------

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
	placeholders := make([]string, len(hashes))
	args := make([]any, len(hashes))
	for i, h := range hashes {
		placeholders[i] = "?"
		args[i] = string(h)
	}
	q := `SELECT torrent_hash, MAX(nlink) AS m
	      FROM torrent_files
	      WHERE nlink IS NOT NULL AND torrent_hash IN (` + strings.Join(placeholders, ",") + `)
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

// -----------------------------------------------------------------------------
// snapshots_daily
// -----------------------------------------------------------------------------

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

// -----------------------------------------------------------------------------
// arr_imports (ADR-0012)
// -----------------------------------------------------------------------------

// UpsertArrImport records one *arr-side import (downloadFolderImported event).
// PK is (arr_type, file_id) — re-imports under the same fileId update the row;
// *arr's behaviour is to allocate a fresh fileId on every import, so collisions
// in practice are rare.
func (s *Store) UpsertArrImport(ctx context.Context, arrType triagearr.ArrType, rec triagearr.ImportRecord) error {
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO arr_imports(arr_type, file_id, download_id, dropped_path, imported_path, size, history_id, imported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(arr_type, file_id) DO UPDATE SET
			download_id=excluded.download_id,
			dropped_path=excluded.dropped_path,
			imported_path=excluded.imported_path,
			size=excluded.size,
			history_id=excluded.history_id,
			imported_at=excluded.imported_at
	`, string(arrType), rec.FileID, string(rec.DownloadID),
		rec.DroppedPath, rec.ImportedPath, rec.Size, rec.HistoryID, ts(rec.ImportedAt))
	if err != nil {
		return fmt.Errorf("upserting arr_import %s/%d: %w", arrType, rec.FileID, err)
	}
	return nil
}

// MaxHistoryID returns the highest history.id we've ingested for one *arr
// instance, so the next poll can fetch only the delta.
func (s *Store) MaxHistoryID(ctx context.Context, arrType triagearr.ArrType) (int64, error) {
	var v *int64
	if err := s.reader.GetContext(ctx, &v, `
		SELECT MAX(history_id) FROM arr_imports WHERE arr_type = ?
	`, string(arrType)); err != nil {
		return 0, fmt.Errorf("max history_id for %s: %w", arrType, err)
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

// LinksByHash returns the per-file links for a torrent: every *arr file that
// was imported from this download_id AND still exists in media_files. The
// JOIN drops imports whose fileId no longer matches a current media_files
// entry (post-upgrade, manual delete) — keeping the linker output aligned
// with what M5 actor can actually act on.
func (s *Store) LinksByHash(ctx context.Context, hash triagearr.Hash) ([]triagearr.Link, error) {
	type row struct {
		ArrType      string `db:"arr_type"`
		FileID       int64  `db:"file_id"`
		DownloadID   string `db:"download_id"`
		DroppedPath  string `db:"dropped_path"`
		ImportedPath string `db:"imported_path"`
		Size         int64  `db:"size"`
		LivePath     string `db:"live_path"`
		TitleSlug    string `db:"title_slug"`
	}
	var rows []row
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT ai.arr_type, ai.file_id, ai.download_id,
		       ai.dropped_path, ai.imported_path, ai.size, mf.path AS live_path,
		       COALESCE(m.title_slug, '') AS title_slug
		FROM arr_imports ai
		JOIN media_files mf
		  ON mf.arr_type = ai.arr_type
		 AND mf.file_id  = ai.file_id
		LEFT JOIN media m
		  ON m.arr_type = mf.arr_type
		 AND m.id       = mf.media_id
		WHERE ai.download_id = ?
		ORDER BY ai.arr_type, ai.file_id
	`, strings.ToLower(string(hash))); err != nil {
		return nil, fmt.Errorf("listing links for %s: %w", hash, err)
	}
	out := make([]triagearr.Link, len(rows))
	for i, r := range rows {
		out[i] = triagearr.Link{
			ArrType:      triagearr.ArrType(r.ArrType),
			FileID:       r.FileID,
			DownloadID:   triagearr.Hash(r.DownloadID),
			TitleSlug:    r.TitleSlug,
			DroppedPath:  r.DroppedPath,
			ImportedPath: r.ImportedPath,
			LivePath:     r.LivePath,
			Size:         r.Size,
		}
	}
	return out, nil
}

// CountArrImports returns the number of imports stored for one *arr instance,
// surfaced by `inspect imports`.
func (s *Store) CountArrImports(ctx context.Context, arrType triagearr.ArrType) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM arr_imports WHERE arr_type = ?`,
		string(arrType),
	); err != nil {
		return 0, fmt.Errorf("counting arr_imports: %w", err)
	}
	return n, nil
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
