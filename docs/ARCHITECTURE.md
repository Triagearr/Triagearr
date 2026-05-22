# Architecture

## Overview

Triagearr is a single-binary Go daemon that orchestrates media deletion across a Plex + *arr + qBittorrent stack. It is built as a collection of decoupled components communicating through Go interfaces, with SQLite as the source of truth for both relational data (torrents ↔ media ↔ actions) and time-series snapshots (ratio, seeders, velocity over time).

## High-level diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                          TRIAGEARR                                │
│                                                                   │
│   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐         │
│   │  Qbit    │  │  *arr    │  │ Maintainerr│ │  Disk    │         │
│   │  Poller  │  │  Pollers │  │  Poller   │ │  Poller  │         │
│   │          │  │ (Sonarr, │  │ (optional,│ │          │         │
│   │          │  │  Radarr, │  │  read-only│ │          │         │
│   │          │  │  Lidarr, │  │  in V2)   │ │          │         │
│   │          │  │  Readarr,│  │           │ │          │         │
│   │          │  │ Whisparr)│  │           │ │          │         │
│   └────┬─────┘  └────┬─────┘  └────┬─────┘ └────┬─────┘         │
│        │             │              │             │              │
│        ▼             ▼              ▼             ▼              │
│   ┌──────────────────────────────────────────────────────────┐  │
│   │              Snapshot Store (SQLite, modernc)            │  │
│   │ snapshots_raw · snapshots_daily · torrents · media       │  │
│   │ actions · audit_log · arr_instances · disk_pressure      │  │
│   └─────────────────────────┬────────────────────────────────┘  │
│                             │                                    │
│                ┌────────────▼─────────────┐                      │
│                │         Mapper           │                      │
│                │   torrent ↔ media        │   inode-based join  │
│                │   (fs.Stat + arr index)  │                      │
│                └────────────┬─────────────┘                      │
│                             │                                    │
│                ┌────────────▼─────────────┐  ┌───────────────┐  │
│                │         Scorer           │◄─┤  Config (YAML)│  │
│                │  DeleteScore = f(...)    │  │  per-tracker  │  │
│                │  + exclusions + guards   │  │  + weights    │  │
│                └────────────┬─────────────┘  └───────────────┘  │
│                             │                                    │
│                ┌────────────▼─────────────┐                      │
│                │        Decider           │                      │
│                │  Triggered by:           │                      │
│                │   - schedule (cron)      │                      │
│                │   - disk pressure        │                      │
│                │   - manual API call      │                      │
│                │  Selects top-K candidates│                      │
│                └────────────┬─────────────┘                      │
│                             │                                    │
│                ┌────────────▼─────────────┐  ┌───────────────┐  │
│                │         Actor            │─►│  *arr API     │  │
│                │  1. delete via *arr      │  │ (Sonarr,      │  │
│                │  2. check nlink on /tor  │  │  Radarr, …)   │  │
│                │  3. delete via qbit      │  └───────────────┘  │
│                │  4. persist audit log    │  ┌───────────────┐  │
│                │                          │─►│  qBittorrent  │  │
│                └────────────┬─────────────┘  │     API       │  │
│                             │                 └───────────────┘  │
│                ┌────────────▼─────────────┐  ┌───────────────┐  │
│                │       Notifier           │─►│  Telegram     │  │
│                │  per-action templates    │  │  webhook…     │  │
│                └──────────────────────────┘  └───────────────┘  │
│                                                                  │
│   ┌──────────────────────────────────────────────────────────┐  │
│   │         HTTP Server (chi) — read-only + control plane    │  │
│   │   /api/v1/...  +  embedded React UI (shadcn/ui)          │  │
│   └──────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

## Data flow

### 1. Observation (always on)

Every poller runs on its own configurable interval. Each tick produces structured records appended to SQLite:

- **qBit poller** → `snapshots_raw` (one row per active torrent: ratio, uploaded, seeders, leechers, state, last_activity)
- **\*arr pollers** → `media` upsert (id, title, file paths, size, tags)
- **Disk poller** → `disk_pressure` (one row per watched volume: total, used, free, percent)
- **Maintainerr poller (V2, optional)** → `maintainerr_collections` snapshot (read-only mirror of what Maintainerr plans to delete)

The pollers never block each other and never trigger actions. They are purely observational.

### 2. Downsampling (daily)

A background job once per day:
- Aggregates `snapshots_raw` from D-2 into `snapshots_daily` (avg/min/max per torrent per day)
- Deletes `snapshots_raw` older than `retention.snapshots_raw` (default 30d)
- Keeps `snapshots_daily` for `retention.snapshots_daily` (default 1y)
- Runs `VACUUM` if space reclaim threshold crossed

This keeps the DB lean indefinitely (~50 MB steady state for a 500-torrent library).

### 3. Mapping (continuous, cached)

The mapper resolves three identifiers for each candidate:

```
qbit_torrent_hash ─┐
                   ├── inode (via os.Stat on a file path)
arr_media_id ──────┘
```

The link is the **inode**. On the standard TRaSH-guides hardlink layout:
- `/share/files/torrents/<category>/Foo.mkv` → inode N
- `/share/files/media/Foo (2024)/Foo.mkv` → inode N (hardlink)

Triagearr stats both paths and verifies `Sys().(*syscall.Stat_t).Ino` matches.

The mapping is refreshed each *arr poll (file paths can move via *arr renames). Stale entries are detected and recomputed.

### 4. Scoring (event-driven, cached)

The scorer reads from `snapshots_raw + snapshots_daily + media + arr_instances` and computes a `DeleteScore` per torrent. See [SCORING.md](SCORING.md) for the full formula. Output is persisted to the `scores` table with the contributing factors (auditability).

Scoring is **event-driven** (ADR-0020): a feeding poller (qbit, tracker, *arr) signals the scorer after every successful tick, and the scorer re-scores the whole library after a short debounce window. There is no fixed scoring interval — the pollers' own cadences pace the scorer, which means a fresh start scores as soon as the first poll lands instead of racing an empty store.

### 5. Decision (triggered)

The decider runs when any trigger fires:

- **Cron**: scheduled run (default daily, e.g. `0 4 * * *`)
- **Disk pressure**: when any monitored volume's `free_percent < threshold`, the decider fires immediately
- **Manual**: `POST /api/v1/runs` from the UI or CLI

The decider:
1. Loads the current scores
2. Excludes torrents in HnR window, in `keep` lists, low-seeder protected, etc.
3. Sorts by score descending
4. Selects top-K until target free space is reached (or `max_deletions_per_run` cap)
5. Hands the candidate list to the Actor

### 6. Action (the only destructive step)

For each candidate, the Actor performs an atomic sequence:

```
a. lookup arr_media_id → call *arr API: DELETE /api/v3/movie/{id}?deleteFiles=true
   (or equivalent for series/episode/album/...)
b. wait for *arr to confirm the file is gone
c. stat /share/files/torrents/<path> → check current nlink
d. if nlink == 1 (only the torrent copy remains) → qbit delete with deleteFiles=true
   if nlink == 0 (file already gone, *arr removed both refs) → qbit delete metadata only
   if nlink > 1 (cross-seed: another torrent shares the inode) → qbit delete without files,
        log a warning; the other torrent keeps seeding
e. persist actions + audit_log atomically in SQLite
f. notify via configured notifiers
```

**Pre-flight safety check** before any of the above:
- Mode is `live` (else dry-run logs and exits)
- Per-instance `act: true` for the relevant *arr
- Rate limit not hit
- Re-validates the torrent state is still consistent with the score (defense against race conditions)

If `mode: dry-run`, steps (a)–(d) are skipped; only (e) records a "would-have-done" entry.

### 7. Audit & observability

Every action emits:
- A `actions` row (structured: who, when, what, why, score breakdown, reversibility info)
- An `audit_log` row (free-form with full context dump)
- A notification (if configured)
- A `triagearr_actions_total{result="...",arr="..."}` Prometheus counter increment (V2)

The dashboard renders these as a timeline.

## Key interfaces

All cross-component coupling goes through Go interfaces defined in `internal/triagearr/types.go`:

```go
// ArrInstance is the contract every *arr client implements.
// Multiple instances of each type can coexist (multi-Sonarr, etc.).
type ArrInstance interface {
    Name() string
    Type() ArrType        // sonarr | radarr | lidarr | readarr | whisparr_v2 | whisparr_v3
    Poll() bool           // is read-allowed
    Act()  bool           // is delete-allowed
    ListMedia(ctx context.Context) ([]MediaItem, error)
    DeleteMedia(ctx context.Context, id MediaID, opts DeleteOpts) error
    HealthCheck(ctx context.Context) error
}

// QbitClient abstracts the download client (V1 = qBittorrent only).
type QbitClient interface {
    ListTorrents(ctx) ([]Torrent, error)
    TorrentFiles(ctx, Hash) ([]TorrentFile, error)
    Delete(ctx, Hash, DeleteOpts) error
}

// Scorer computes the DeleteScore for a torrent.
type Scorer interface {
    Score(t Torrent, m []MediaItem, snaps []Snapshot, cfg ScoringConfig) Score
}

// Notifier dispatches a structured action event.
type Notifier interface {
    Notify(ctx context.Context, event ActionEvent) error
}
```

Concrete implementations live in `internal/arrs/{sonarr,radarr,lidarr,readarr,whisparr}/`, `internal/qbit/`, `internal/scorer/`, `internal/notifier/{telegram,webhook}/`.

## Storage schema

See [`docs/STORAGE.md`](STORAGE.md) for the full schema (to be written in M1). Summary:

| Table | Purpose | Estimated row count (500 torrents, 1y) |
|---|---|---|
| `snapshots_raw` | High-resolution qBit snapshots, 30-day retention | ~720k (after rotation) |
| `snapshots_daily` | Downsampled daily aggregates, 1y retention | ~180k |
| `torrents` | Current state of each qBit torrent (last seen) | ~500 |
| `media` | *arr media items, joined to torrents by inode | ~thousands |
| `arr_instances` | Configured *arr instances, last health check | ~5-20 |
| `disk_pressure` | Disk usage snapshots per volume, 30-day | ~10k |
| `scores` | Latest computed score per torrent, with breakdown | ~500 |
| `actions` | Every action ever taken (or would-have-been) | grows slowly |
| `audit_log` | Free-form context dump per decision | grows slowly |
| `maintainerr_collections` | (V2) read-only mirror of Maintainerr collections | small |

All tables use `WITHOUT ROWID` where appropriate and have composite indexes for time-range queries.

## Process model

Single binary, single process, multiple goroutines:

- 1 goroutine per poller (configurable interval)
- 1 goroutine for the scorer loop (event-driven: re-scores on poller signals, debounced)
- 1 goroutine for the downsampler (daily tick)
- 1 goroutine for the decider (subscribes to triggers via channels)
- 1 goroutine for the actor (consumes decisions via channel, processes serially to respect rate limits)
- N goroutines for the HTTP server (chi handlers)

A central `context.Context` is propagated everywhere; `SIGTERM` triggers graceful shutdown that lets the actor finish its current decision before exiting.

## HTTP API

Served on `127.0.0.1:9494` by default. Authentication is Sonarr-style: loopback binds default to `auth: none` (pair with TinyAuth/Authelia/Caddy for external access); any non-loopback bind forces `auth: apikey` (`X-API-Key`, constant-time-compared). The choice is validated at config load — `auth: none` + non-loopback is rejected.

Endpoint surface as of M6:

```
GET    /healthz                            unauthenticated liveness probe
GET    /api/v1/auth-mode                   unauthenticated; UI uses it to decide whether to prompt for a key

GET    /api/v1/summary                     dashboard aggregate
GET    /api/v1/version                     build metadata
GET    /api/v1/config                      effective config, secrets redacted to "***"

GET    /api/v1/volumes                     configured volumes + latest disk_pressure
GET    /api/v1/volumes/{name}/history      ?since=24h    pressure time series
GET    /api/v1/arrs                        instance health

GET    /api/v1/torrents                    ?sort=&q=&category=&private=&limit=&offset=
GET    /api/v1/torrents/{hash}             detail (trackers, links, score)
GET    /api/v1/torrents/{hash}/snapshots   ?since=720h   ratio/seeders/leechers history
GET    /api/v1/scores                      ?limit=50&include_excluded=false

POST   /api/v1/runs                        body: {volume, mode:"live"|undefined}; 1/min/IP
GET    /api/v1/runs                        ?limit=50
GET    /api/v1/runs/{id}                   one run + items
GET    /api/v1/runs/{id}/actions           actions for one run
GET    /api/v1/actions                     ?limit=&offset=   global timeline
GET    /api/v1/actions/{id}                one action + audit trail
```

Every response carries the M6 security header set (`Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy: ()`).

The React UI is served from the same binary via `embed.FS` (`web/web.go`): asset paths serve directly from `web/dist/`, everything else falls back to `index.html` so the in-memory SPA router keeps working on full-page reloads. The Vite build outputs to `web/dist/`; `make build` runs `bun run build` inside `web/` before invoking `go build`.

## What lives outside the binary

- Configuration file (mounted from disk, hot-reloaded on `SIGHUP`)
- SQLite database file
- Optional Prometheus scrape endpoint (`/metrics`, V2)

That's it. No external state, no message queue, no Redis. Triagearr is intentionally a self-contained workhorse.

## Deployment topology

In the reference homelab (QNAP + Container Station), Triagearr lives in the `cleanup` stack next to `qbit_manage` and (optionally) `maintainerr`. See [DEPLOYMENT.md](DEPLOYMENT.md) for the docker-compose snippet.

## Non-goals

Things Triagearr will not do, on purpose:

- **Download client management** (tags, categories, pausing) — that's `qbit_manage`'s job
- **Malware / stalled / blocked download removal** — that's `Cleanuparr`'s job
- **Rule-based library cleanup driven by watch history** — that's `Maintainerr`'s job
- **Orphan detection unrelated to Triagearr's own actions** — `qbit_manage` handles this
- **Distributed multi-node operation** — single homelab, single instance

By staying focused, Triagearr cohabits cleanly with the rest of the ecosystem instead of competing with it.
