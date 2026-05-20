# Changelog

All notable changes to Triagearr will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- `http.bind` default `127.0.0.1:9494` (was `:9494`). `config.Validate` refuses non-loopback bind without `api_key`. `disk_pressure.target_free_percent` must be strictly greater than `threshold_free_percent`.

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
