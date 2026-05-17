# ADR-0002: Use SQLite (modernc) for all persistent state

## Status

Accepted — 2026-05-17

## Context

Triagearr persists three kinds of data:

1. **Time-series snapshots** of every active torrent (ratio, seeders, uploaded bytes, last activity) every ~30 minutes
2. **Relational entities** (torrents, media items, *arr instances, actions, audit log) with joins between them
3. **Configuration mirror** (effective config snapshot for the UI to show)

At our scale (a 500-torrent homelab, 1 year of history), the worst-case raw volume is ~700 MB, which falls to ~50 MB after daily downsampling.

Candidate stores evaluated:

| Store | Time-series fit | Relational fit | Pure Go | Cross-compile QNAP | Tooling |
|---|---|---|---|---|---|
| `modernc.org/sqlite` | OK with indexes | ✅ excellent | ✅ | ✅ trivial | universal (`sqlite3`, Datasette, DBeaver) |
| `ncruces/go-sqlite3` (WASM) | OK with indexes | ✅ excellent | ✅ (WASM runtime) | ✅ | same as modernc |
| `mattn/go-sqlite3` (CGO) | OK with indexes | ✅ excellent | ❌ CGO | requires cross-toolchain | same as modernc |
| DuckDB via `go-duckdb` (CGO) | ✅ columnar excellent | ✅ | ❌ CGO | requires cross-toolchain | DuckDB CLI less universal |
| BadgerDB | ❌ KV only | ❌ KV only | ✅ | ✅ | requires our own viewer |
| bbolt | ❌ KV only | ❌ KV only | ✅ | ✅ | requires our own viewer |
| InfluxDB / VictoriaMetrics | ✅ excellent | ❌ no joins | partial | n/a (separate service) | excellent for TS, none for relational |

Two of the candidates (`mattn/go-sqlite3` and `go-duckdb`) require CGO. CGO is solvable in CI (qemu + goreleaser), but it breaks `go install` for end users and complicates local development.

`mattn/go-sqlite3` is additionally **dormant**: last release 2022-10. Not acceptable for a project that needs current SQLite features.

KV stores (Badger, bbolt) cannot perform the joins we need (torrent ↔ media ↔ action). Building a relational layer on top is effectively reimplementing SQLite.

Dedicated TSDBs (InfluxDB, VictoriaMetrics) excel at time-series but have no story for relational data. Pairing them with SQLite means two stores with manual coordination and no cross-store transactions.

## Decision

Use `modernc.org/sqlite` v1.50.x as the only persistent store.

Concretely:
- Schema mixes time-series (`snapshots_raw`, `snapshots_daily`) and relational (`torrents`, `media`, `actions`, etc.) tables in the same database file
- Time-series tables use `WITHOUT ROWID` and composite primary keys `(torrent_hash, ts DESC)` for efficient range queries
- A daily downsampling job aggregates `snapshots_raw` (>2d old) into `snapshots_daily`, then deletes raw rows older than retention
- All inter-table operations occur in transactions for atomicity
- WAL mode (`PRAGMA journal_mode=WAL`) for concurrent reads during writes

`ncruces/go-sqlite3` is the fallback if a blocker is found in modernc — it is API-compatible enough that swapping is a config change.

## Consequences

**Easier:**
- Single store, single file, single backup target (`/data/triagearr.db`)
- ACID across all data (relational + time-series) without coordination
- `sqlite3 /data/triagearr.db "SELECT …"` for debugging on QNAP
- `go install` works for end users (no CGO)
- Multi-arch cross-compile is just `GOOS=… GOARCH=… go build`

**Harder:**
- Analytical queries over millions of TS rows are slower than columnar (DuckDB). At our scale, irrelevant.
- Single writer (WAL mode allows concurrent readers, one writer). At our scale, irrelevant — write rate is ~1 op/sec peak.
- Downsampling is our responsibility, not a feature of the engine. Acceptable cost for the simplicity gain.

**Traded away:**
- Pure-TSDB conveniences: out-of-the-box retention policies, continuous queries, automatic downsampling
- DuckDB's superior compression and analytical query performance

## Re-evaluation triggers

We should revisit this decision if:
- DB size exceeds ~5 GB despite downsampling (would indicate scale we didn't plan for)
- A query that runs >5 seconds in production becomes essential (could indicate columnar would help)
- A future feature requires multi-writer concurrency (would force a server-based DB)

## References

- modernc.org/sqlite: https://gitlab.com/cznic/sqlite (v1.50.1, May 2026, SQLite 3.53.1)
- SQLite limits: https://www.sqlite.org/limits.html
