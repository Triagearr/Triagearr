package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migration is a single SQL file applied atomically and recorded in schema_migrations.
type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies any pending embedded migrations in version order.
// Each migration runs inside a transaction; partial failures roll back.
// Safe to call repeatedly — already-applied versions are skipped.
func (s *Store) Migrate() error {
	ctx := context.Background()
	if _, err := s.writer.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied, err := s.appliedVersions()
	if err != nil {
		return err
	}

	migs, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		if err := s.applyMigration(m); err != nil {
			return fmt.Errorf("applying migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func (s *Store) appliedVersions() (map[int]bool, error) {
	rows, err := s.writer.QueryContext(context.Background(), `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning schema_migrations row: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating schema_migrations: %w", err)
	}
	return out, nil
}

func (s *Store) applyMigration(m migration) error {
	ctx := context.Background()
	tx, err := s.writer.Beginx()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("recording version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("listing embedded migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m, err := parseMigration(e.Name())
		if err != nil {
			return nil, err
		}
		b, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", e.Name(), err)
		}
		m.sql = string(b)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func parseMigration(filename string) (migration, error) {
	base := strings.TrimSuffix(filename, ".sql")
	idx := strings.Index(base, "_")
	if idx <= 0 {
		return migration{}, fmt.Errorf("migration filename %q must be NNNN_name.sql", filename)
	}
	v, err := strconv.Atoi(base[:idx])
	if err != nil {
		return migration{}, fmt.Errorf("migration filename %q: parsing version: %w", filename, err)
	}
	return migration{version: v, name: base[idx+1:]}, nil
}
