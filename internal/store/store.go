// Package store wraps the SQLite database used by Triagearr.
package store

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// Store is the persistence layer. It owns a single sqlx.DB and exposes
// typed repository methods. It is safe for concurrent use by multiple
// goroutines (sqlite WAL allows concurrent readers + a single writer).
type Store struct {
	db *sqlx.DB
}

// Open opens (or creates) the SQLite database at path and configures it for
// Triagearr's access patterns (WAL, foreign keys, sane busy timeout).
// Callers must invoke (*Store).Close.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db %q: %w", path, err)
	}
	// sqlite is happiest with a small pool — WAL allows readers but only one writer.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging sqlite db: %w", err)
	}
	return &Store{db: db}, nil
}

// DB exposes the underlying sqlx handle for callers that need raw access
// (tests, migrate runner). Production code should prefer the repo methods.
func (s *Store) DB() *sqlx.DB { return s.db }

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
