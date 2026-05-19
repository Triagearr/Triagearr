# Changelog

All notable changes to Triagearr will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
