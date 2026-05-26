package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ArrConnection is one persisted *arr instance (ADR-0022). The kind (e.g.
// "sonarr") is the sole identity — at most one row per kind is allowed.
type ArrConnection struct {
	ID             int64
	Kind           string
	URL            string
	PublicURL      string
	APIKey         string
	Enabled        bool
	Poll           bool
	Act            bool
	TagsExclude    []string
	CategoriesOnly []string
	TimeoutMS      int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// arrConnectionRow is the DB-facing view: the two string-list columns are
// stored as JSON text, so they scan into strings and are converted at the
// repo boundary.
type arrConnectionRow struct {
	ID             int64     `db:"id"`
	Kind           string    `db:"kind"`
	URL            string    `db:"url"`
	PublicURL      string    `db:"public_url"`
	APIKey         string    `db:"api_key"`
	Enabled        bool      `db:"enabled"`
	Poll           bool      `db:"poll"`
	Act            bool      `db:"act"`
	TagsExclude    string    `db:"tags_exclude"`
	CategoriesOnly string    `db:"categories_only"`
	TimeoutMS      int64     `db:"timeout_ms"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

func (r arrConnectionRow) toConnection() (ArrConnection, error) {
	tags, err := decodeStringList(r.TagsExclude)
	if err != nil {
		return ArrConnection{}, fmt.Errorf("arr_connection %d: tags_exclude: %w", r.ID, err)
	}
	cats, err := decodeStringList(r.CategoriesOnly)
	if err != nil {
		return ArrConnection{}, fmt.Errorf("arr_connection %d: categories_only: %w", r.ID, err)
	}
	return ArrConnection{
		ID: r.ID, Kind: r.Kind, URL: r.URL, PublicURL: r.PublicURL, APIKey: r.APIKey,
		Enabled: r.Enabled, Poll: r.Poll, Act: r.Act,
		TagsExclude: tags, CategoriesOnly: cats, TimeoutMS: r.TimeoutMS,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

// ListArrConnections returns every connection, ordered by kind for a stable
// UI listing.
func (s *Store) ListArrConnections(ctx context.Context) ([]ArrConnection, error) {
	var rows []arrConnectionRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT id, kind, url, public_url, api_key, enabled, poll, act,
		       tags_exclude, categories_only, timeout_ms, created_at, updated_at
		FROM arr_connections
		ORDER BY kind
	`); err != nil {
		return nil, fmt.Errorf("listing arr_connections: %w", err)
	}
	out := make([]ArrConnection, 0, len(rows))
	for _, r := range rows {
		c, err := r.toConnection()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// GetArrConnectionByKind returns one connection by kind. sql.ErrNoRows when absent.
func (s *Store) GetArrConnectionByKind(ctx context.Context, kind string) (ArrConnection, error) {
	var r arrConnectionRow
	err := s.reader.GetContext(ctx, &r, `
		SELECT id, kind, url, public_url, api_key, enabled, poll, act,
		       tags_exclude, categories_only, timeout_ms, created_at, updated_at
		FROM arr_connections
		WHERE kind = ?
	`, kind)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArrConnection{}, err
		}
		return ArrConnection{}, fmt.Errorf("getting arr_connection %s: %w", kind, err)
	}
	return r.toConnection()
}

// CountArrConnections returns the number of rows — used by the boot seed to
// decide whether the YAML `arrs:` block should be imported.
func (s *Store) CountArrConnections(ctx context.Context) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n, `SELECT COUNT(*) FROM arr_connections`); err != nil {
		return 0, fmt.Errorf("counting arr_connections: %w", err)
	}
	return n, nil
}

// UpsertArrConnection inserts or replaces the connection for c.Kind. The UNIQUE(kind)
// constraint is satisfied by INSERT OR REPLACE; created_at is preserved on update
// via the ON CONFLICT clause.
func (s *Store) UpsertArrConnection(ctx context.Context, c ArrConnection) (ArrConnection, error) {
	tags, err := encodeStringList(c.TagsExclude)
	if err != nil {
		return ArrConnection{}, err
	}
	cats, err := encodeStringList(c.CategoriesOnly)
	if err != nil {
		return ArrConnection{}, err
	}
	now := ts(time.Now().UTC())
	_, err = s.writer.ExecContext(ctx, `
		INSERT INTO arr_connections(kind, url, public_url, api_key, enabled, poll, act,
		                            tags_exclude, categories_only, timeout_ms,
		                            created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET
			url             = excluded.url,
			public_url      = excluded.public_url,
			api_key         = excluded.api_key,
			enabled         = excluded.enabled,
			poll            = excluded.poll,
			act             = excluded.act,
			tags_exclude    = excluded.tags_exclude,
			categories_only = excluded.categories_only,
			timeout_ms      = excluded.timeout_ms,
			updated_at      = excluded.updated_at
	`, c.Kind, c.URL, c.PublicURL, c.APIKey, c.Enabled, c.Poll, c.Act,
		tags, cats, c.TimeoutMS, now, now)
	if err != nil {
		return ArrConnection{}, fmt.Errorf("upserting arr_connection %s: %w", c.Kind, err)
	}
	return s.GetArrConnectionByKind(ctx, c.Kind)
}

// DeleteArrConnectionByKind removes one row. Returns sql.ErrNoRows when kind
// is unknown so the HTTP layer can answer 404.
func (s *Store) DeleteArrConnectionByKind(ctx context.Context, kind string) error {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM arr_connections WHERE kind = ?`, kind)
	if err != nil {
		return fmt.Errorf("deleting arr_connection %s: %w", kind, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading delete result for arr_connection %s: %w", kind, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SeedArrConnections bulk-inserts connections in a single transaction. Used
// once at boot to import the YAML `arrs:` block into an empty table (ADR-0022).
func (s *Store) SeedArrConnections(ctx context.Context, conns []ArrConnection) error {
	if len(conns) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for arr_connections seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO arr_connections(kind, url, public_url, api_key, enabled, poll, act,
		                            tags_exclude, categories_only, timeout_ms,
		                            created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare arr_connections seed insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	now := ts(time.Now().UTC())
	for _, c := range conns {
		tags, err := encodeStringList(c.TagsExclude)
		if err != nil {
			return err
		}
		cats, err := encodeStringList(c.CategoriesOnly)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, c.Kind, c.URL, c.PublicURL, c.APIKey,
			c.Enabled, c.Poll, c.Act, tags, cats, c.TimeoutMS, now, now); err != nil {
			return fmt.Errorf("seeding arr_connection %s: %w", c.Kind, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit arr_connections seed: %w", err)
	}
	return nil
}
