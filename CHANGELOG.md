# Changelog

All notable changes to Triagearr will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- *arr connection management from the dashboard (ADR-0022). Sonarr/Radarr/etc.
  instances now live in the new `arr_connections` SQLite table (migration
  `0012`), the source of truth for the client registry. Settings → *arr
  connections supports full add/edit/delete plus a per-connection "Test
  connection" button (`registry.TestConnection`). HTTP surface:
  `GET/POST /api/v1/arr-connections`, `PUT/DELETE /api/v1/arr-connections/{id}`,
  `POST /api/v1/arr-connections/test`. A mutation triggers the daemon
  self-SIGHUP so the registry rebuilds without a manual restart.
- The YAML `arrs:` block is now a one-time seed: on first boot, when
  `arr_connections` is empty, its instances are imported. Existing configs
  migrate transparently.

### Fixed
- `internal/notify`: gosec G115 lint failure on the disk-free-bytes
  `uint64 → int64` conversion in the notification message formatter.

## [0.6.1] - 2026-05-21

Patch release. Same-day fix for SQLite contention observed under prod load.

### Fixed
- `internal/store`: structural rewrite to the two-pool pattern (writer with `MaxOpenConns(1)`, reader with `MaxOpenConns(8)`, both backed by the same SQLite file via WAL). Eliminates the ~12 % failure rate observed on the tracker poller's `ReplaceTrackers` after v0.6.0 deploy:
  - `SQLITE_BUSY (5)` becomes structurally impossible (no second writer to compete for the lock).
  - `SQLITE_BUSY_SNAPSHOT (517)` becomes structurally impossible (no second writer to invalidate the snapshot between read and write).
  - HTTP API reads no longer wait behind poller writes — WAL pays for itself.
- `busy_timeout` bumped to 10 s (covers external `sqlite3` CLI hold cases only; not in the inner loop).
- `temp_store=MEMORY` added (opportunistic CPU win on the daily downsampler's GROUP BY).

### Added
- ADR-0017 — the two-pool decision and how it relates to ADR-0002.
- Stress tests `TestConcurrentWritesNoBusy` + `TestReadsConcurrentWithWrites` lock the new invariant in.

## [0.6.0] - 2026-05-21

M5 — Actor. The release where Triagearr actually deletes things. A run resolved
to `mode: live` now fans out per-file *arr DELETEs and then issues the
whole-torrent qBit DELETE, persisting a per-file audit trail. The daemon stays
`mode: dry-run` by default and the Actor refuses to act unless three gates
align (`mode: live`, per-*arr `act: true`, ADR-0015 trigger × opt-in).

### Added
- `internal/actor`: arr-first → qBit-second pipeline (ADR-0003, narrated in ADR-0016). Per-candidate state machine driven by `runs.mode` + `runs.triggered_by`; per-file fan-out audit so cases like "8 OK + 1 failed + 1 not-attempted" on a season pack are reconstructible from `audit_log` alone (HARDLINK_TOPOLOGY.md case 4).
- `internal/logging.RedactHandler`: slog middleware that scrubs `api_key`/`apikey`/`password`/`bot_token` attributes and known secret query parameters in URL-shaped string values. Installed by default via `logging.Setup`.
- `internal/store/migrations/0009_actions.sql`: `actions` (one row per executed candidate) + `audit_log` (one row per API call). FK cascades on `runs` deletion.
- `triagearr.ResolveRunMode(daemonLive, trigger, requestedLive)`: pure function applying ADR-0015. Wired into `POST /api/v1/runs` (accepts `"mode":"live"` in body), `triagearr run --live`, and the disk-pressure watcher.
- `triagearr.FileDeleter` optional interface implemented by Sonarr (`/api/v3/episodefile/{id}`) and Radarr (`/api/v3/moviefile/{id}`). Stub *arr types deliberately do not implement it — the actor's deleter resolver gates on that.
- `qbit.Delete(hash, deleteFiles=true)` → `POST /api/v2/torrents/delete`. 5xx and transport failures are wrapped with the new `triagearr.ErrTransient` sentinel so the actor's retry loop can distinguish them from hard 404/401.
- Config `action.{max_deletions_per_run, inter_action_delay, add_import_exclusion}` (defaults: 10, 2s, false).
- `store.MarkRunStatus(id, status)` so the actor can transition runs through running/completed.

### Changed
- `triagearr run`: `--live` now resolves through the gate. `--live` against a dry-run daemon fails fast with a clear error. `--dry-run` continues to be required for non-live invocations.
- `disk_watcher` carries `DaemonLive` + an optional `Actor`; pressure runs auto-execute when the daemon is live, otherwise they fall back to the M4 dry-run plan.
- ROADMAP: cross-seed safety net (T3.5 nlink stat + skip/warn_only/force_delete) **deferred to M8** — requires FS access, out of scope for a full-API release (ADR-0012). Smoke testcontainers also reclassed to M8; M5 ships in-process httptest fakes covering the same state machine.

### Removed
- `ArrInstance.DeleteMedia(MediaID)` and its stubs — the interface required a series/movie-level delete that Triagearr never wired and that has the wrong granularity for per-torrent decisions against per-file *arr DELETE.

## [0.5.0] - 2026-05-20

M4 — Triggers. The Decider turns scores into ordered run plans; disk pressure, HTTP, and CLI all fire dry-run runs that are persisted for audit. Still no destructive action — the Actor lands in M5.

### Added
- `internal/decider`: `Plan(ctx, volume)` selects candidates score-DESC, capped by `target_free_percent` (need_bytes from latest `disk_usage`) and `max_run_size_gb`. Volume attribution is a `save_path` prefix match (ADR-0014), refined per-file in M5.
- `internal/triggers/disk_watcher`: poller that fires on `threshold_free_percent` cross with a 1h re-fire grace, absorbing free% oscillation around the seuil.
- `internal/server`: stdlib `net/http` (1.22 patterns) with `POST/GET /api/v1/runs`, `GET /api/v1/runs/{id}`, `/healthz`. `X-API-Key` constant-time auth; config refuses non-loopback bind without `api_key`.
- `cmd/triagearr/run`: `triagearr run --now --dry-run [--volume X] [--json]`; `--live` rejected with "arrives in M5".
- `internal/store/migrations/0008_runs.sql`: `runs` (header) + `run_items` (per-rank candidate set). Reused by M5 Actor without schema change.
- `serveAction` learns SIGHUP: cancels Manager + HTTP, reloads config, respawns the daemon goroutine tree; failed reload keeps the running config (logged, daemon stays up).
- ADR-0014: Decider attributes torrents to volumes by `save_path` prefix — coarse for M4, refined in M5.
- ADR-0015: deletion gating contract — pressure auto, humans explicit, cron never. Constrains M5 Actor's allow-list.

### Changed
- `http.bind` default `127.0.0.1:9494` (was `:9494`). `disk_pressure.target_free_percent` must be strictly greater than `threshold_free_percent`.
- **HTTP API authentication is now always-on**, Sonarr/Radarr-style: the api_key is no longer a config field, it lives in `${data_dir}/api_key` (next to the SQLite DB), auto-generated on first boot with `0600` perms. Operators read the file once after first start. Removes the previous "open API on loopback" bypass and the bind↔api_key cross-validation.

### Migration
- Deployments that injected `api_key: "${TRIAGEARR_API_KEY}"` in config: drop the line, restart, read the new key from `${data_dir}/api_key`. The `TRIAGEARR_API_KEY` env var becomes orphan.

## [0.4.0] - 2026-05-20

M3 — Scoring engine. The daemon now computes a `DeleteScore` per torrent from passively-collected snapshots/trackers/imports, persists the per-factor breakdown, and exposes it via CLI. Still observation-only — no destructive actions.

### Added
- `internal/scorer`: 7-factor scorer implementing `docs/SCORING.md` (ratio obligation, upload velocity inverse, age, seeders-low guard, swarm health, HnR veto, tracker-dead bonus). Gates `private` and `any_tracker_alive` are recorded explicitly on every factor so the explain output names the reason rather than emitting silent zeros. HnR veto weight is hard-coded `-10000` (non-configurable per the safety contract).
- `internal/store/migrations/0005_scores.sql`: `scores(hash PK, score, private, any_tracker_alive, excluded, exclusion_reasons, factors_json, computed_at)` with a partial index on eligible rows for the upcoming M4 Decider.
- `internal/store/migrations/0006_snapshots_daily_uploaded.sql`: `snapshots_daily.uploaded_max` so Factor 2 honours the 30-day SCORING.md window by blending raw + daily across the downsample boundary.
- Scoring loop: periodic recompute (default `scoring.interval: 1h`) wired next to the existing pollers; two-pass design caches per-torrent snapshot stats to build the global velocity normaliser without a dedicated SQL aggregate.
- Exclusions evaluated up front (`qbit.{category,tags}_exclude`, `arrs.<type>.<name>.tags_exclude`) but torrents are scored anyway and flagged `excluded=true` with reasons — UI visibility now, Decider filters at M4.
- CLI: `triagearr score explain <hash> [--json|--recompute]`, `score recompute <hash>`, `score top [--limit N] [--include-excluded]`.
- `PruneStaleTorrents` extended to cascade `scores` alongside the existing dependents.

### Changed
- `internal/store/repos.go` `DownsampleRange` aggregates `MAX(uploaded)` into the new `uploaded_max` column.

## [0.2.0] - 2026-05-19

M1 — Observation only. The daemon now polls qBittorrent, *arr instances, and watched disks, persisting everything to SQLite. No destructive operations are possible at this stage.

### Added
- `internal/store`: SQLite (modernc, WAL) with embedded `0001_init.sql` migration and an idempotent runner; tables `arr_instances`, `torrents`, `snapshots_raw`, `media`, `disk_pressure`. Timestamps stored as RFC3339Nano so any sqlite tool can read them.
- `internal/clients/qbit`: qBittorrent WebUI v2 client (login + cookie session, `ListTorrents`, `TorrentFiles`).
- `internal/clients/sonarr`, `internal/clients/radarr`: minimal v3 clients with `HealthCheck` and `ListMedia` (tags resolved to labels).
- `internal/clients/{lidarr,readarr,whisparr_v2,whisparr_v3}`: stubs satisfying `ArrInstance`; `DeleteMedia` returns "not implemented in M1".
- `internal/clients/registry`: builds the live client set from config, exposes `All()` and `AllPolling()`.
- `internal/pollers`: manager + `QbitPoller`, `ArrPoller`, `DiskPoller` (Linux `unix.Statfs` with a non-Linux build-tagged stub).
- `internal/config`: koanf v2-based loader with `${VAR}` / `${VAR:-default}` expansion that respects YAML comments; typed structs for the M1 surface.
- `internal/logging`: slog setup, JSON when stderr is not a TTY, text otherwise; level via `TRIAGEARR_LOG_LEVEL`.
- CLI commands: `serve`, `migrate`, `inspect torrents`, `inspect arrs`.

### Initial scaffolding (M0)
- Repository structure, build pipeline, release tooling, docs and ADRs (released as `v0.1.0-rc.0`).
