package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// -----------------------------------------------------------------------------
// Torrents — filtered list + count for the dashboard's Torrents page.
// -----------------------------------------------------------------------------

// ListTorrentsOpts tunes ListTorrentsFiltered.
type ListTorrentsOpts struct {
	Sort         string // name|seeders|ratio|size|last_seen|score
	Order        string // asc|desc; "" uses the per-column default direction
	Query        string // case-insensitive substring on torrent.name
	Category     string // exact match; "" disables
	PrivateOnly  bool   // when true, only private torrents
	ExcludedOnly bool   // when true, only torrents flagged excluded by the scorer
	Limit        int    // <= 0 falls back to 50
	Offset       int    // >= 0
}

// ListTorrentsFiltered returns torrents with their latest snapshot and persisted
// score, applying the filters from opts. The scores table is always joined so
// the dashboard list can show score and status without a second round-trip.
func (s *Store) ListTorrentsFiltered(ctx context.Context, opts ListTorrentsOpts) ([]TorrentRow, error) {
	args, where, _ := buildTorrentFilter(opts)
	orderBy, err := torrentOrderByExtended(opts.Sort, opts.Order, true)
	if err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	q := `
		SELECT t.hash, t.name, t.category, t.size, t.added_on, t.last_seen,
		       t.private AS private, t.candidate_boost AS candidate_boost,
		       s.ratio AS ratio, s.seeders AS seeders, s.leechers AS leechers,
		       s.state AS state, s.ts AS snap_ts,
		       sc.score AS score, sc.excluded AS excluded,
		       sc.any_tracker_alive AS any_tracker_alive
		FROM torrents t
		LEFT JOIN (
		    SELECT torrent_hash, MAX(ts) AS ts FROM snapshots_raw GROUP BY torrent_hash
		) sm ON sm.torrent_hash = t.hash
		LEFT JOIN snapshots_raw s
		  ON s.torrent_hash = sm.torrent_hash AND s.ts = sm.ts
		LEFT JOIN scores sc ON sc.torrent_hash = t.hash`
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY " + orderBy + " LIMIT ? OFFSET ?"
	args = append(args, limit, opts.Offset)

	var rows []TorrentRow
	if err := s.reader.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("listing filtered torrents: %w", err)
	}
	return rows, nil
}

// CountTorrentsFiltered returns the total count matching opts (ignores limit/offset).
func (s *Store) CountTorrentsFiltered(ctx context.Context, opts ListTorrentsOpts) (int, error) {
	args, where, joinScore := buildTorrentFilter(opts)
	q := "SELECT COUNT(*) FROM torrents t"
	if joinScore {
		q += " LEFT JOIN scores sc ON sc.torrent_hash = t.hash"
	}
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	if err := s.reader.GetContext(ctx, &n, q, args...); err != nil {
		return 0, fmt.Errorf("counting filtered torrents: %w", err)
	}
	return n, nil
}

// buildTorrentFilter assembles the WHERE clause shared by the list and count
// queries. joinScore is true when a clause references the scores table, so the
// count query knows to add the join (the list query always joins scores).
func buildTorrentFilter(opts ListTorrentsOpts) (args []any, where string, joinScore bool) {
	var clauses []string
	if q := strings.TrimSpace(opts.Query); q != "" {
		clauses = append(clauses, "LOWER(t.name) LIKE ?")
		args = append(args, "%"+strings.ToLower(q)+"%")
	}
	if c := strings.TrimSpace(opts.Category); c != "" {
		clauses = append(clauses, "t.category = ?")
		args = append(args, c)
	}
	if opts.PrivateOnly {
		clauses = append(clauses, "t.private = 1")
	}
	if opts.ExcludedOnly {
		clauses = append(clauses, "sc.excluded = 1")
		joinScore = true
	}
	return args, strings.Join(clauses, " AND "), joinScore
}

// orderDir resolves an explicit asc/desc request, falling back to def for an
// empty or unrecognised value.
func orderDir(order, def string) string {
	switch strings.ToLower(order) {
	case "asc":
		return "ASC"
	case "desc":
		return "DESC"
	default:
		return def
	}
}

func torrentOrderByExtended(sortBy, order string, joinedScore bool) (string, error) {
	switch strings.ToLower(sortBy) {
	case "", "name":
		return "t.name " + orderDir(order, "ASC"), nil
	case "category":
		return "t.category " + orderDir(order, "ASC") + ", t.name ASC", nil
	case "seeders":
		return "s.seeders " + orderDir(order, "DESC") + " NULLS LAST, t.name ASC", nil
	case "ratio":
		return "s.ratio " + orderDir(order, "DESC") + " NULLS LAST, t.name ASC", nil
	case "size":
		return "t.size " + orderDir(order, "DESC") + ", t.name ASC", nil
	case "last_seen":
		return "t.last_seen " + orderDir(order, "DESC") + ", t.name ASC", nil
	case "score":
		if !joinedScore {
			return "", errors.New("score sort requires scores join")
		}
		return "sc.score " + orderDir(order, "DESC") + " NULLS LAST, t.name ASC", nil
	default:
		return "", fmt.Errorf("unknown sort key %q (want: name|category|seeders|ratio|size|last_seen|score)", sortBy)
	}
}

// DistinctCategories returns the sorted set of non-empty torrent categories,
// used to populate the dashboard's category filter.
func (s *Store) DistinctCategories(ctx context.Context) ([]string, error) {
	var cats []string
	err := s.reader.SelectContext(ctx, &cats,
		`SELECT DISTINCT category FROM torrents WHERE category != '' ORDER BY category`)
	if err != nil {
		return nil, fmt.Errorf("listing categories: %w", err)
	}
	return cats, nil
}

// GetTorrent returns the persisted torrent row and its latest snapshot.
func (s *Store) GetTorrent(ctx context.Context, hash triagearr.Hash) (TorrentDetailRow, error) {
	var row TorrentDetailRow
	err := s.reader.GetContext(ctx, &row, `
		SELECT t.hash, t.name, t.category, t.save_path, t.size,
		       t.added_on, t.completion_on, t.private, t.tags, t.last_seen,
		       t.protected, t.protected_at, t.candidate_boost, t.candidate_boost_at,
		       s.ratio AS ratio, s.uploaded AS uploaded, s.seeders AS seeders,
		       s.leechers AS leechers, s.state AS state, s.ts AS snap_ts
		FROM torrents t
		LEFT JOIN snapshots_raw s
		  ON s.torrent_hash = t.hash
		 AND s.ts = (SELECT MAX(ts) FROM snapshots_raw WHERE torrent_hash = t.hash)
		WHERE t.hash = ?
	`, string(hash))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TorrentDetailRow{}, err
		}
		return TorrentDetailRow{}, fmt.Errorf("loading torrent %s: %w", hash, err)
	}
	return row, nil
}

// TorrentDetailRow extends TorrentRow with save_path, completion_on, private, tags, uploaded.
type TorrentDetailRow struct {
	Hash         string     `db:"hash"`
	Name         string     `db:"name"`
	Category     string     `db:"category"`
	SavePath     string     `db:"save_path"`
	Size         int64      `db:"size"`
	AddedOn      time.Time  `db:"added_on"`
	CompletionOn *time.Time `db:"completion_on"`
	Private      bool       `db:"private"`
	Tags         string     `db:"tags"`
	LastSeen     time.Time  `db:"last_seen"`
	Protected        bool       `db:"protected"`
	ProtectedAt      *time.Time `db:"protected_at"`
	CandidateBoost   bool       `db:"candidate_boost"`
	CandidateBoostAt *time.Time `db:"candidate_boost_at"`
	Ratio            *float64   `db:"ratio"`
	Uploaded     *int64     `db:"uploaded"`
	Seeders      *int       `db:"seeders"`
	Leechers     *int       `db:"leechers"`
	State        *string    `db:"state"`
	SnapshotAt   *time.Time `db:"snap_ts"`
}

// -----------------------------------------------------------------------------
// Snapshots — time series for the torrent detail drawer.
// -----------------------------------------------------------------------------

// SnapshotPoint is one row of a torrent's history.
type SnapshotPoint struct {
	Timestamp time.Time `db:"ts"`
	Ratio     float64   `db:"ratio"`
	Uploaded  int64     `db:"uploaded"`
	Seeders   int       `db:"seeders"`
	Leechers  int       `db:"leechers"`
	State     string    `db:"state"`
}

// ListSnapshotsRaw returns the raw snapshot points for a torrent since `since`,
// ordered by timestamp ascending. Limit caps the row count (<= 0 means 2000).
func (s *Store) ListSnapshotsRaw(ctx context.Context, hash triagearr.Hash, since time.Time, limit int) ([]SnapshotPoint, error) {
	if limit <= 0 {
		limit = 2000
	}
	var rows []SnapshotPoint
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT ts, ratio, uploaded, seeders, leechers, state
		FROM snapshots_raw
		WHERE torrent_hash = ? AND ts >= ?
		ORDER BY ts ASC
		LIMIT ?
	`, string(hash), ts(since.UTC()), limit); err != nil {
		return nil, fmt.Errorf("listing snapshots for %s: %w", hash, err)
	}
	return rows, nil
}

// -----------------------------------------------------------------------------
// Disk pressure history — time series for volume gauges.
// -----------------------------------------------------------------------------

// DiskUsagePoint is one row of the volume's pressure history.
type DiskUsagePoint struct {
	Timestamp   time.Time `db:"ts"`
	TotalBytes  int64     `db:"total_bytes"`
	UsedBytes   int64     `db:"used_bytes"`
	FreeBytes   int64     `db:"free_bytes"`
	FreePercent float64   `db:"free_percent"`
}

// ListDiskUsageHistory returns disk_pressure points since `since`.
func (s *Store) ListDiskUsageHistory(ctx context.Context, since time.Time, limit int) ([]DiskUsagePoint, error) {
	if limit <= 0 {
		limit = 2000
	}
	var rows []DiskUsagePoint
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT ts, total_bytes, used_bytes, free_bytes, free_percent
		FROM disk_pressure
		WHERE ts >= ?
		ORDER BY ts ASC
		LIMIT ?
	`, ts(since.UTC()), limit); err != nil {
		return nil, fmt.Errorf("listing disk_pressure history: %w", err)
	}
	return rows, nil
}

// -----------------------------------------------------------------------------
// Actions — global timeline across runs.
// -----------------------------------------------------------------------------

// ListActionsRecent returns actions ordered by started_at descending (newest first).
func (s *Store) ListActionsRecent(ctx context.Context, limit, offset int) ([]triagearr.Action, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var rows []actionRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT id, run_id, rank, torrent_hash, started_at, finished_at, status, freed_bytes
		FROM actions
		ORDER BY started_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, limit, offset); err != nil {
		return nil, fmt.Errorf("listing recent actions: %w", err)
	}
	out := make([]triagearr.Action, len(rows))
	for i, r := range rows {
		out[i] = r.toAction()
	}
	return out, nil
}

// CountActions returns the total number of action rows. Used for paging.
func (s *Store) CountActions(ctx context.Context) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n, `SELECT COUNT(*) FROM actions`); err != nil {
		return 0, fmt.Errorf("counting actions: %w", err)
	}
	return n, nil
}

// CountTorrents returns the total torrents row count.
func (s *Store) CountTorrents(ctx context.Context) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n, `SELECT COUNT(*) FROM torrents`); err != nil {
		return 0, fmt.Errorf("counting torrents: %w", err)
	}
	return n, nil
}

// CountScored returns the number of scored torrents (excluded=0).
func (s *Store) CountScored(ctx context.Context) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n, `SELECT COUNT(*) FROM scores WHERE excluded = 0`); err != nil {
		return 0, fmt.Errorf("counting scored torrents: %w", err)
	}
	return n, nil
}
