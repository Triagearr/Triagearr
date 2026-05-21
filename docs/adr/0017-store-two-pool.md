# ADR-0017: Two-pool SQLite (writer + reader) to eliminate SQLITE_BUSY

## Status

Accepted — 2026-05-21

## Context

After v0.6.0 landed, the M2 tracker poller routinely produced ~12 % failed
writes per tick on the prod NAS, all surfaced as `SQLITE_BUSY (5)` or
`SQLITE_BUSY_SNAPSHOT (517)` from `ReplaceTrackers`. The pattern was
reproducible at boot, when every poller fires its first tick in parallel
(disk, qbit, tracker, arr × N, scorer).

The store was a single `*sqlx.DB` with `SetMaxOpenConns(8)`. WAL was
enabled, `busy_timeout=5000` was set per the DSN, and `BeginTxx(ctx,
nil)` opened transactions with SQLite's default isolation (`BEGIN
DEFERRED`).

The 517 errors are the diagnostic clue: `SQLITE_BUSY_SNAPSHOT` is **not**
a wait-for-lock condition. It signals that the transaction's read
snapshot was invalidated by another writer between the first read and
the first write. `busy_timeout` doesn't apply — there is nothing to wait
for; the only remedy is to retry the whole transaction. `ReplaceTrackers`
is the canonical read-then-write pattern (select prior trackers, delete
all, reinsert), so it was the obvious victim.

Three options were on the table:

1. **`BEGIN IMMEDIATE` per call site**. Acquire the writer lock at
   transaction start, eliminating the deferred-promotion race. Standard
   SQLite advice for read-then-write transactions.
2. **`SetMaxOpenConns(1)` on the single pool**. Serialise everything,
   eliminate concurrency at the source.
3. **Two-pool pattern**: a writer pool capped at 1 connection, a reader
   pool with N connections, both backed by the same SQLite file.

## Decision

**Adopt the two-pool pattern (option 3).**

`internal/store.Store` now owns two `*sqlx.DB` handles:

- `writer` with `SetMaxOpenConns(1)`. Every `ExecContext`, `BeginTxx`,
  and write-path `QueryContext` routes here.
- `reader` with `SetMaxOpenConns(8)`. Every `GetContext`,
  `SelectContext`, and read-path `QueryContext` routes here.

SQLite WAL is the load-bearing premise: it lets readers see a consistent
snapshot without blocking the single writer, so the HTTP API doesn't
stall behind poller ticks.

## Why not the other options

### Per-call-site `BEGIN IMMEDIATE`

Correctness-equivalent for the cases it covers, but it imposes a
discipline that the type system cannot enforce. The next contributor who
adds a read-then-write transaction has no compile-time signal that they
need to think about isolation. The bug returns silently. We have already
been bitten by exactly this pattern once; the fix should be structural,
not procedural.

It also leaves the `SQLITE_BUSY (5)` lock contention untouched. The
poller fan-out at boot keeps producing them.

### `SetMaxOpenConns(1)` on the single pool

Eliminates both `SQLITE_BUSY` variants — but also serialises every
read against every write. The HTTP API would block during the tracker
tick (which can take a few seconds against 264 torrents). WAL becomes
nearly pointless: its only remaining contribution is durable journaling.

The two-pool pattern keeps WAL doing what it was designed for.

### Common best-practice alignment

The two-pool pattern is the documented approach in:

- Ben Johnson's "How To Use SQLite In Go" articles (Litestream author).
- The rqlite source (`store/store.go` opens a `db` and a `dbR`).
- The pocketbase codebase.
- Most production-grade SQLite-in-Go libraries.

There is no novelty here; we're aligning with what mature projects do.

## Consequences

### Positive

- `SQLITE_BUSY (5)`: structurally impossible. There is one writer
  connection; nothing competes for the lock.
- `SQLITE_BUSY_SNAPSHOT (517)`: structurally impossible. There is no
  second writer to invalidate the snapshot.
- HTTP API reads and poller writes run truly concurrently. WAL pays for
  itself.
- `busy_timeout=10000` (bumped from 5s) only matters if an external tool
  (`sqlite3` CLI on the NAS host) holds the lock. It's no longer in the
  inner loop.
- `temp_store=MEMORY` added opportunistically — sort/group temporary
  tables stay in RAM (cheap CPU win on the daily downsampler).

### Negative (acknowledged)

- Two `*sql.DB` handles to plumb. The repository methods route on
  `s.writer` / `s.reader` explicitly. Forgetting to use the writer for
  a write *fails fast*: the read pool's connection would still serialise
  via SQLite's lock mechanics, and any write through it would compete
  with the writer pool — so the test suite would surface it as an
  immediate `BUSY` on the read connection.
- One subtle case in `DownsampleRange`: it reads the aggregate on the
  reader pool, then writes inside a writer transaction. Rows inserted
  between the read and the write that land below the cutoff are deleted
  but missed from the aggregate. This was already true before the
  refactor (the read was outside any transaction); the two-pool change
  doesn't make it worse. Documented for future cleanup.

### Migration

Pure code change, no schema change. Drop-in replacement on the existing
`/data/triagearr.db` file. WAL was already enabled by M1.

## References

- `internal/store/store.go` — the two-pool wiring.
- Ben Johnson, *Why you should use SQLite in your next Rails project*
  (also applies to Go) — https://litestream.io/ docs.
- `rqlite/store/store.go` — multi-pool precedent.
- ADR-0001 (stdlib-first), ADR-0002 (SQLite for storage).

## Revisit when

- The reader pool starts being a bottleneck (e.g., M6 dashboard fans
  out many concurrent reads). At that point, bump `readerMaxOpenConns`
  or consider connection-per-request semantics.
- An operational need emerges for multi-process access — at which point
  we'd need to revisit the "single Go writer" assumption.
