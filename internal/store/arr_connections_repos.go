package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ArrConnection is one persisted *arr instance (ADR-0022). It is the source
// of truth for the client registry — the YAML `arrs:` block only seeds this
// table on first boot.
type ArrConnection struct {
	ID             int64
	Kind           string
	Name           string
	URL            string
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
	Name           string    `db:"name"`
	URL            string    `db:"url"`
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
		ID: r.ID, Kind: r.Kind, Name: r.Name, URL: r.URL, APIKey: r.APIKey,
		Enabled: r.Enabled, Poll: r.Poll, Act: r.Act,
		TagsExclude: tags, CategoriesOnly: cats, TimeoutMS: r.TimeoutMS,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

// decodeStringList parses a JSON array column, tolerating empty text as [].
func decodeStringList(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON %q: %w", raw, err)
	}
	return out, nil
}

// encodeStringList renders a string slice as a JSON array column. A nil slice
// becomes "[]" so the NOT NULL column always holds valid JSON.
func encodeStringList(v []string) (string, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encoding string list: %w", err)
	}
	return string(b), nil
}

// ListArrConnections returns every connection, ordered by kind then name for
// a stable UI listing.
func (s *Store) ListArrConnections(ctx context.Context) ([]ArrConnection, error) {
	var rows []arrConnectionRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT id, kind, name, url, api_key, enabled, poll, act,
		       tags_exclude, categories_only, timeout_ms, created_at, updated_at
		FROM arr_connections
		ORDER BY kind, name
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

// GetArrConnection returns one connection by id. sql.ErrNoRows when absent.
func (s *Store) GetArrConnection(ctx context.Context, id int64) (ArrConnection, error) {
	var r arrConnectionRow
	err := s.reader.GetContext(ctx, &r, `
		SELECT id, kind, name, url, api_key, enabled, poll, act,
		       tags_exclude, categories_only, timeout_ms, created_at, updated_at
		FROM arr_connections
		WHERE id = ?
	`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArrConnection{}, err
		}
		return ArrConnection{}, fmt.Errorf("getting arr_connection %d: %w", id, err)
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

// CreateArrConnection inserts a new connection and returns its assigned id.
// The UNIQUE(kind, name) index rejects duplicates.
func (s *Store) CreateArrConnection(ctx context.Context, c ArrConnection) (int64, error) {
	tags, err := encodeStringList(c.TagsExclude)
	if err != nil {
		return 0, err
	}
	cats, err := encodeStringList(c.CategoriesOnly)
	if err != nil {
		return 0, err
	}
	now := ts(time.Now().UTC())
	res, err := s.writer.ExecContext(ctx, `
		INSERT INTO arr_connections(kind, name, url, api_key, enabled, poll, act,
		                            tags_exclude, categories_only, timeout_ms,
		                            created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, c.Kind, c.Name, c.URL, c.APIKey, c.Enabled, c.Poll, c.Act,
		tags, cats, c.TimeoutMS, now, now)
	if err != nil {
		return 0, fmt.Errorf("inserting arr_connection %s/%s: %w", c.Kind, c.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading inserted arr_connection id: %w", err)
	}
	return id, nil
}

// UpdateArrConnection overwrites every mutable column of an existing row.
// created_at is preserved. Returns sql.ErrNoRows when id is unknown.
func (s *Store) UpdateArrConnection(ctx context.Context, c ArrConnection) error {
	tags, err := encodeStringList(c.TagsExclude)
	if err != nil {
		return err
	}
	cats, err := encodeStringList(c.CategoriesOnly)
	if err != nil {
		return err
	}
	res, err := s.writer.ExecContext(ctx, `
		UPDATE arr_connections SET
			kind = ?, name = ?, url = ?, api_key = ?,
			enabled = ?, poll = ?, act = ?,
			tags_exclude = ?, categories_only = ?, timeout_ms = ?,
			updated_at = ?
		WHERE id = ?
	`, c.Kind, c.Name, c.URL, c.APIKey, c.Enabled, c.Poll, c.Act,
		tags, cats, c.TimeoutMS, ts(time.Now().UTC()), c.ID)
	if err != nil {
		return fmt.Errorf("updating arr_connection %d: %w", c.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading update result for arr_connection %d: %w", c.ID, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteArrConnection removes one row. Returns sql.ErrNoRows when id is unknown
// so the HTTP layer can answer 404.
func (s *Store) DeleteArrConnection(ctx context.Context, id int64) error {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM arr_connections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting arr_connection %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading delete result for arr_connection %d: %w", id, err)
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
		INSERT INTO arr_connections(kind, name, url, api_key, enabled, poll, act,
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
		if _, err := stmt.ExecContext(ctx, c.Kind, c.Name, c.URL, c.APIKey,
			c.Enabled, c.Poll, c.Act, tags, cats, c.TimeoutMS, now, now); err != nil {
			return fmt.Errorf("seeding arr_connection %s/%s: %w", c.Kind, c.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit arr_connections seed: %w", err)
	}
	return nil
}
