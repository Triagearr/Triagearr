// Package store wraps the SQLite database used by Triagearr.
package store

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// Store is the persistence layer. It owns TWO sqlx.DB handles pointing at the
// same SQLite file (WAL allows it):
//
//   - writer: MaxOpenConns(1) — every Exec/BeginTxx goes here, so SQLite never
//     sees more than one writing connection. Eliminates `SQLITE_BUSY (5)` and
//     `SQLITE_BUSY_SNAPSHOT (517)` at the structural level: there is no second
//     writer to race against.
//   - reader: MaxOpenConns(N) — every SELECT/Get goes here. WAL guarantees
//     readers see a consistent snapshot without blocking the writer, so HTTP
//     responses don't stall while pollers persist their ticks.
//
// This is the two-pool pattern documented by Ben Johnson (Litestream), rqlite,
// and most production SQLite-in-Go projects. See docs/adr/0017-….md for the
// decision context.
type Store struct {
	writer *sqlx.DB
	reader *sqlx.DB
}

const (
	// readerMaxOpenConns caps the read pool. WAL allows concurrent readers so
	// this can be generous; 8 matches the previous single-pool limit.
	readerMaxOpenConns = 8
)

// commonPragmas applies to every connection in every pool.
//
//   - journal_mode=WAL — required for the two-pool to make sense (readers
//     don't block the writer).
//   - busy_timeout(10s) — guards against transient lock contention from
//     external tools (e.g. someone running `sqlite3` against the file).
//   - synchronous(NORMAL) — durable enough for WAL, faster than FULL.
//   - foreign_keys(1) — explicit ON DELETE CASCADE on actions/run_items.
//   - temp_store(MEMORY) — sort/group temp tables stay in RAM.
//   - cache_size(-65536) — 64 MiB page cache per connection (negative = KiB).
//     The scoring + observe paths stream the whole torrents+snapshots set; the
//     stock ~2 MiB cache forces page churn under 5k+ torrents.
//   - mmap_size(64 MiB) — memory-mapped reads for the same scans, cuts syscalls.
const commonPragmas = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-65536)&_pragma=mmap_size(67108864)"

// Open creates the writer + reader pools backed by the same SQLite file.
// Callers must invoke (*Store).Close.
func Open(path string) (*Store, error) {
	writer, err := openPool(path, 1)
	if err != nil {
		return nil, fmt.Errorf("opening writer pool: %w", err)
	}
	reader, err := openPool(path, readerMaxOpenConns)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("opening reader pool: %w", err)
	}
	return &Store{writer: writer, reader: reader}, nil
}

func openPool(path string, maxOpen int) (*sqlx.DB, error) {
	dsn := fmt.Sprintf("file:%s?%s", path, commonPragmas)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db %q: %w", path, err)
	}
	db.SetMaxOpenConns(maxOpen)
	// Idle conns track open conns: with maxOpen=1 the writer keeps its single
	// connection warm; the reader pool keeps half its capacity warm.
	idle := maxOpen
	if idle > 4 {
		idle = 4
	}
	db.SetMaxIdleConns(idle)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging sqlite db: %w", err)
	}
	return db, nil
}

// DB returns the writer handle. Kept for tests and a handful of inspection
// callers that need raw access; production code should prefer the typed
// repository methods.
func (s *Store) DB() *sqlx.DB { return s.writer }

// Close releases both pools. Errors from either pool are returned (writer
// first).
func (s *Store) Close() error {
	var firstErr error
	if s.writer != nil {
		if err := s.writer.Close(); err != nil {
			firstErr = err
		}
	}
	if s.reader != nil {
		if err := s.reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
