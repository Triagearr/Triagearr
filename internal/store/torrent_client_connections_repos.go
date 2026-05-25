package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TorrentClientConnection is one persisted torrent client instance (ADR-0025).
// The kind (e.g. "qbittorrent") is the sole identity — at most one row per kind
// is allowed. Only qbittorrent has a backend today; other kinds are rejected by
// the HTTP layer.
type TorrentClientConnection struct {
	ID              int64
	Kind            string
	URL             string
	Username        string
	Password        string
	Enabled         bool
	CategoryExclude []string
	TagsExclude     []string
	DeleteWithFiles bool
	TimeoutMS       int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// torrentClientConnectionRow is the DB-facing view: the two string-list columns
// are stored as JSON text, scanned into strings and converted at the repo
// boundary.
type torrentClientConnectionRow struct {
	ID              int64     `db:"id"`
	Kind            string    `db:"kind"`
	URL             string    `db:"url"`
	Username        string    `db:"username"`
	Password        string    `db:"password"`
	Enabled         bool      `db:"enabled"`
	CategoryExclude string    `db:"category_exclude"`
	TagsExclude     string    `db:"tags_exclude"`
	DeleteWithFiles bool      `db:"delete_with_files"`
	TimeoutMS       int64     `db:"timeout_ms"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

func (r torrentClientConnectionRow) toConnection() (TorrentClientConnection, error) {
	cats, err := decodeStringList(r.CategoryExclude)
	if err != nil {
		return TorrentClientConnection{}, fmt.Errorf("torrent_client_connection %d: category_exclude: %w", r.ID, err)
	}
	tags, err := decodeStringList(r.TagsExclude)
	if err != nil {
		return TorrentClientConnection{}, fmt.Errorf("torrent_client_connection %d: tags_exclude: %w", r.ID, err)
	}
	return TorrentClientConnection{
		ID: r.ID, Kind: r.Kind, URL: r.URL,
		Username: r.Username, Password: r.Password,
		Enabled:         r.Enabled,
		CategoryExclude: cats, TagsExclude: tags,
		DeleteWithFiles: r.DeleteWithFiles,
		TimeoutMS:       r.TimeoutMS,
		CreatedAt:       r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

// ListTorrentClientConnections returns every connection, ordered by kind for a
// stable UI listing.
func (s *Store) ListTorrentClientConnections(ctx context.Context) ([]TorrentClientConnection, error) {
	var rows []torrentClientConnectionRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT id, kind, url, username, password, enabled,
		       category_exclude, tags_exclude, delete_with_files,
		       timeout_ms, created_at, updated_at
		FROM torrent_client_connections
		ORDER BY kind
	`); err != nil {
		return nil, fmt.Errorf("listing torrent_client_connections: %w", err)
	}
	out := make([]TorrentClientConnection, 0, len(rows))
	for _, r := range rows {
		c, err := r.toConnection()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// GetTorrentClientConnectionByKind returns one connection by kind.
// sql.ErrNoRows when absent.
func (s *Store) GetTorrentClientConnectionByKind(ctx context.Context, kind string) (TorrentClientConnection, error) {
	var r torrentClientConnectionRow
	err := s.reader.GetContext(ctx, &r, `
		SELECT id, kind, url, username, password, enabled,
		       category_exclude, tags_exclude, delete_with_files,
		       timeout_ms, created_at, updated_at
		FROM torrent_client_connections
		WHERE kind = ?
	`, kind)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TorrentClientConnection{}, err
		}
		return TorrentClientConnection{}, fmt.Errorf("getting torrent_client_connection %s: %w", kind, err)
	}
	return r.toConnection()
}

// CountTorrentClientConnections returns the number of rows — used by the boot
// seed to decide whether the YAML `torrent_clients:` block should be imported.
func (s *Store) CountTorrentClientConnections(ctx context.Context) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n, `SELECT COUNT(*) FROM torrent_client_connections`); err != nil {
		return 0, fmt.Errorf("counting torrent_client_connections: %w", err)
	}
	return n, nil
}

// UpsertTorrentClientConnection inserts or replaces the connection for c.Kind.
// The UNIQUE(kind) constraint is satisfied by the ON CONFLICT clause;
// created_at is preserved on update.
func (s *Store) UpsertTorrentClientConnection(ctx context.Context, c TorrentClientConnection) (TorrentClientConnection, error) {
	cats, err := encodeStringList(c.CategoryExclude)
	if err != nil {
		return TorrentClientConnection{}, err
	}
	tags, err := encodeStringList(c.TagsExclude)
	if err != nil {
		return TorrentClientConnection{}, err
	}
	now := ts(time.Now().UTC())
	_, err = s.writer.ExecContext(ctx, `
		INSERT INTO torrent_client_connections(kind, url, username, password, enabled,
		                                       category_exclude, tags_exclude,
		                                       delete_with_files, timeout_ms,
		                                       created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET
			url               = excluded.url,
			username          = excluded.username,
			password          = excluded.password,
			enabled           = excluded.enabled,
			category_exclude  = excluded.category_exclude,
			tags_exclude      = excluded.tags_exclude,
			delete_with_files = excluded.delete_with_files,
			timeout_ms        = excluded.timeout_ms,
			updated_at        = excluded.updated_at
	`, c.Kind, c.URL, c.Username, c.Password, c.Enabled,
		cats, tags, c.DeleteWithFiles, c.TimeoutMS, now, now)
	if err != nil {
		return TorrentClientConnection{}, fmt.Errorf("upserting torrent_client_connection %s: %w", c.Kind, err)
	}
	return s.GetTorrentClientConnectionByKind(ctx, c.Kind)
}

// DeleteTorrentClientConnectionByKind removes one row. Returns sql.ErrNoRows
// when kind is unknown so the HTTP layer can answer 404.
func (s *Store) DeleteTorrentClientConnectionByKind(ctx context.Context, kind string) error {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM torrent_client_connections WHERE kind = ?`, kind)
	if err != nil {
		return fmt.Errorf("deleting torrent_client_connection %s: %w", kind, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading delete result for torrent_client_connection %s: %w", kind, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SeedTorrentClientConnections bulk-inserts connections in a single
// transaction. Used once at boot to import the YAML `torrent_clients:` block
// into an empty table (ADR-0025).
func (s *Store) SeedTorrentClientConnections(ctx context.Context, conns []TorrentClientConnection) error {
	if len(conns) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for torrent_client_connections seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO torrent_client_connections(kind, url, username, password, enabled,
		                                       category_exclude, tags_exclude,
		                                       delete_with_files, timeout_ms,
		                                       created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare torrent_client_connections seed insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	now := ts(time.Now().UTC())
	for _, c := range conns {
		cats, err := encodeStringList(c.CategoryExclude)
		if err != nil {
			return err
		}
		tags, err := encodeStringList(c.TagsExclude)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, c.Kind, c.URL, c.Username, c.Password,
			c.Enabled, cats, tags, c.DeleteWithFiles, c.TimeoutMS, now, now); err != nil {
			return fmt.Errorf("seeding torrent_client_connection %s: %w", c.Kind, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit torrent_client_connections seed: %w", err)
	}
	return nil
}
