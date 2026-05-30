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

// lastActivityArg renders Torrent.LastActivity for the upsert, mapping the zero
// time to SQL NULL so the first_seen_dead proxy falls through to completion_on.
func lastActivityArg(t triagearr.Torrent) any {
	if t.LastActivity.IsZero() {
		return nil
	}
	return ts(t.LastActivity)
}

// UpsertTorrent records (or refreshes) the current state of a torrent.
func (s *Store) UpsertTorrent(ctx context.Context, t triagearr.Torrent) error {
	now := time.Now().UTC()
	var completion any
	if !t.CompletionOn.IsZero() {
		completion = ts(t.CompletionOn)
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO torrents(hash, name, category, save_path, size, added_on, completion_on, last_activity, private, tags, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO UPDATE SET
			name=excluded.name,
			category=excluded.category,
			save_path=excluded.save_path,
			size=excluded.size,
			added_on=excluded.added_on,
			completion_on=excluded.completion_on,
			last_activity=excluded.last_activity,
			private=excluded.private,
			tags=excluded.tags,
			last_seen=excluded.last_seen
	`, string(t.Hash), t.Name, t.Category, t.SavePath, t.Size, ts(t.AddedOn), completion, lastActivityArg(t), t.Private, t.Tags, ts(now))
	if err != nil {
		return fmt.Errorf("upserting torrent %s: %w", t.Hash, err)
	}
	return nil
}

// UpsertTorrents batches UpsertTorrent for a whole qBit tick in a single
// transaction with one prepared statement, removing per-row round-trips on the
// hot polling path (~5k torrents per tick).
func (s *Store) UpsertTorrents(ctx context.Context, torrents []triagearr.Torrent) error {
	if len(torrents) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for torrents batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO torrents(hash, name, category, save_path, size, added_on, completion_on, last_activity, private, tags, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO UPDATE SET
			name=excluded.name,
			category=excluded.category,
			save_path=excluded.save_path,
			size=excluded.size,
			added_on=excluded.added_on,
			completion_on=excluded.completion_on,
			last_activity=excluded.last_activity,
			private=excluded.private,
			tags=excluded.tags,
			last_seen=excluded.last_seen
	`)
	if err != nil {
		return fmt.Errorf("prepare torrents upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	now := ts(time.Now().UTC())
	for _, t := range torrents {
		var completion any
		if !t.CompletionOn.IsZero() {
			completion = ts(t.CompletionOn)
		}
		if _, err := stmt.ExecContext(ctx,
			string(t.Hash), t.Name, t.Category, t.SavePath, t.Size,
			ts(t.AddedOn), completion, lastActivityArg(t), t.Private, t.Tags, now,
		); err != nil {
			return fmt.Errorf("upserting torrent %s: %w", t.Hash, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit torrents batch: %w", err)
	}
	return nil
}

// SetTorrentProtected toggles the user-driven protection flag for one torrent.
// Protecting stamps protected_at = now; unprotecting clears it. Returns
// sql.ErrNoRows when the hash is unknown so callers can map to 404.
func (s *Store) SetTorrentProtected(ctx context.Context, hash triagearr.Hash, protected bool) error {
	var stampedAt any
	if protected {
		stampedAt = ts(time.Now().UTC())
	}
	res, err := s.writer.ExecContext(ctx, `
		UPDATE torrents SET protected = ?, protected_at = ? WHERE hash = ?
	`, boolToInt(protected), stampedAt, string(hash))
	if err != nil {
		return fmt.Errorf("updating protected for %s: %w", hash, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for %s: %w", hash, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetTorrentProtected reads the protection flag and timestamp. Returns
// sql.ErrNoRows when the hash is unknown.
func (s *Store) GetTorrentProtected(ctx context.Context, hash triagearr.Hash) (bool, *time.Time, error) {
	var protected int
	var at *time.Time
	err := s.reader.QueryRowContext(ctx, `
		SELECT protected, protected_at FROM torrents WHERE hash = ?
	`, string(hash)).Scan(&protected, &at)
	if err != nil {
		return false, nil, err
	}
	return protected != 0, at, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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

// HashesWithoutTrackers returns torrent hashes that have no row in
// torrent_trackers yet. Drives the tracker poller's event-driven catchup mode:
// after qBit ingestion, any freshly-seen hash is fetched immediately instead
// of waiting for the next 6h periodic sweep.
func (s *Store) HashesWithoutTrackers(ctx context.Context) ([]triagearr.Hash, error) {
	var raw []string
	if err := s.reader.SelectContext(ctx, &raw, `
		SELECT t.hash
		FROM torrents t
		LEFT JOIN torrent_trackers tt ON tt.torrent_hash = t.hash
		WHERE tt.torrent_hash IS NULL
		ORDER BY t.hash`); err != nil {
		return nil, fmt.Errorf("listing hashes without trackers: %w", err)
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
	placeholders, args := hashPlaceholders(hashes)
	var rows []struct {
		Hash string `db:"hash"`
		Name string `db:"name"`
	}
	q := "SELECT hash, name FROM torrents WHERE hash IN (" + placeholders + ")"
	if err := s.reader.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("resolving torrent names: %w", err)
	}
	for _, r := range rows {
		out[triagearr.Hash(r.Hash)] = r.Name
	}
	return out, nil
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
