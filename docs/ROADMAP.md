# Roadmap

Milestones from project zero to v1.0. Each milestone is a tagged release with verifiable acceptance criteria. Estimates are wall-clock hours assuming part-time evening/weekend work.

## M0 — Bootstrap

**Estimated**: 2–3 hours · **Tag**: `v0.1.0-rc.0`

### Goals

A repo skeleton that builds, tests, releases. No app logic yet.

### Acceptance criteria

- [ ] `go.mod` initialized with Go 1.26 toolchain
- [ ] Directory structure matches `docs/ARCHITECTURE.md` § Repo structure
- [ ] `cmd/triagearr/main.go` prints `triagearr vX.Y.Z` and exits
- [ ] `Dockerfile` builds a < 30 MB multi-stage image
- [ ] `goreleaser` config produces multi-arch binaries (amd64, arm64) and pushes to GHCR
- [ ] GitHub Actions workflows: `test`, `lint`, `release` (tag-triggered)
- [ ] `golangci-lint` configured with strict ruleset
- [ ] `LICENSE`, `README`, `CONTRIBUTING`, `CHANGELOG` in place
- [ ] All docs in `docs/` written (you are reading the result of this)

## M1 — Observation only

**Estimated**: 1 weekend (12-16 h) · **Tag**: `v0.2.0`

### Goals

The app polls everything, persists snapshots, and exposes a basic CLI to inspect. **No destructive actions possible** at this stage.

### Acceptance criteria

- [ ] `internal/store` with SQLite migrations runner, embedded SQL files
- [ ] `internal/clients/qbit` client implementing `QbitClient` interface
- [ ] `internal/clients/sonarr` client implementing `ArrInstance`
- [ ] `internal/clients/radarr` client implementing `ArrInstance`
- [ ] `internal/clients/lidarr` stub (interface satisfied, marked TODO)
- [ ] `internal/clients/readarr` stub
- [ ] `internal/clients/whisparr` stubs (v2 + v3)
- [ ] `internal/pollers` orchestrating the configured pollers on intervals
- [ ] Disk poller using `golang.org/x/sys/unix.Statfs_t`
- [ ] CLI: `triagearr serve` runs the daemon; `triagearr inspect torrents` lists current snapshots
- [ ] CLI: `triagearr inspect arrs` shows configured instances + health
- [ ] Structured `slog` logs in JSON when stdout is not a TTY, text otherwise
- [ ] Tests: ≥60% coverage on `store` and per-client packages

### Deliverable

Deploy on real homelab. Let it run for a week. Inspect the SQLite DB manually. Validate that data shape supports the planned scoring math.

## M2 — Mapping, tracker capture & storage maintenance

**Estimated**: 10-12 h · **Tag**: `v0.3.0`

### Linking (API-only — ADR-0012, supersedes ADR-0010)

- [x] `internal/linker` package
- [x] `arr_imports` table: `(arr_name, arr_type, torrent_hash, file_id, imported_path, dropped_path)` per *arr `history` event
- [x] Sonarr/Radarr clients implement `History(ctx, downloadId)` against `/api/v3/history?eventType=downloadFolderImported`
- [x] Each *arr poll refreshes `arr_imports`; cascade-delete on torrent prune
- [x] Linker exposes `ArrImports(hash) → [{instance, type, file_id, imported_path}]` for the Actor
- [x] CLI: `triagearr inspect mapping <hash>` shows EVERY file (qbit path, *arr import path, matched arr_file_id or "orphan")
- [x] Tests with httptest fakes (no filesystem dependency)

### Tracker capture (ADR-0009)

- [ ] Schema migration `0002`: add `torrent_trackers` table, add `media_files` table, add `completion_on` column to `torrents`
- [ ] qBit client: `ListTrackers(ctx, hash)` against `/api/v2/torrents/trackers`
- [ ] qBit client: capture `completion_on` in `ListTorrents` (one-field extension)
- [ ] New `tracker` poller (default `tracker_interval: 6h`); fan-out one call per torrent
- [ ] Persist parsed `tracker_host` alongside raw `tracker_url`
- [ ] CLI: `triagearr inspect trackers <hash>` prints current tracker statuses

### *arr per-file capture (prerequisite for linker + M5 Actor)

- [ ] Sonarr client: `ListEpisodeFiles(ctx, seriesID)` against `/api/v3/episodefile?seriesId={id}`
- [ ] Radarr client: `ListMovieFiles(ctx, movieID)` against `/api/v3/moviefile?movieId={id}`
- [ ] Extend `arr` poller to fan out file calls per media item (rate-limited; default 5 req/s burst)
- [ ] Persist `{file_id, path, size}` per file into `media_files` — `file_id` is reused by M5 Actor for granular `DELETE`
- [ ] CLI: `triagearr inspect media <id>` includes the file list with sizes and ages

### Storage maintenance (prerequisite for M3 scorer)

- [ ] `snapshots_daily` table (downsampled aggregates: avg/min/max per torrent per day)
- [ ] Daily downsampler job: `snapshots_raw` (>D-2) → `snapshots_daily`
- [ ] Retention enforcement: drop `snapshots_raw` past `retention.snapshots_raw`, `snapshots_daily` past `retention.snapshots_daily`
- [ ] Periodic `VACUUM` gated by `vacuum.enabled` + `vacuum.min_reclaim_mb`
- [ ] Cron-driven via `polling.downsample_cron`, executed by the existing pollers manager
- [ ] Tests with synthetic time-series spanning the retention window

## M3 — Scoring engine

**Estimated**: 1 weekend · **Tag**: `v0.4.0`

- [ ] `internal/scorer` implementing `Scorer` interface
- [ ] All factors from `docs/SCORING.md` implemented
- [ ] Score persistence in `scores` table with per-factor breakdown
- [ ] CLI: `triagearr score --explain <hash>` outputs the breakdown
- [ ] Exclusion logic (categories, tags, monitored status)
- [ ] Tests with synthetic data covering each factor + edge cases

## M4 — Triggers

**Estimated**: 4-6 h · **Tag**: `v0.5.0`

- [ ] Internal cron scheduler
- [ ] Disk-pressure watcher fires on threshold cross
- [ ] `POST /api/v1/runs` (dry-run only at this stage) wired up
- [ ] Decider selects candidates based on scores + target free percent
- [ ] CLI: `triagearr run --now --dry-run` triggers a one-shot decision
- [ ] `SIGHUP` hot-reloads the config without restarting the daemon (re-validates, rebuilds the registry, re-arms intervals)

## M5 — Actor (destructive)

**Estimated**: 1 weekend · **Tag**: `v0.6.0`

⚠️ **The release where Triagearr actually deletes things.**

- [x] `internal/actor` package
- [x] `mode: live` config requirement
- [x] Per-*arr-instance `act: true` requirement (defense in depth)
- [x] Trigger gating per ADR-0015: pressure auto, HTTP/CLI explicit opt-in, cron forever dry-run
- [x] `POST /api/v1/runs` accepts a `mode` field; without it the run stays dry-run even on a live daemon
- [x] `triagearr run --live` unlocked (errors out clearly when daemon mode is dry-run)
- [x] `arr-then-qbit` deletion pipeline per `docs/HARDLINK_TOPOLOGY.md` (T3 per `arr_file_id`, T4 whole-torrent)
- [ ] ~~Cross-seed conflict handling (T3.5 nlink stat + skip/warn_only/force_delete)~~ — **deferred to M8**; ADR-0023's TRaSH mount + UID contract removes the FS-access blocker ADR-0012 cited
- [x] Partial *arr failure handling: hard fail aborts candidate, no rollback, audit_log narrates
- [x] `actions` + `audit_log` tables — per-file granularity (case "8 OK + 1 failed + 1 not-attempted" reconstructible from `SELECT … WHERE action_id = ?`)
- [x] Rate limiting (`max_deletions_per_run`, `inter_action_delay`)
- [x] Retry with backoff on transient failures (stdlib exponential + `crypto/rand` jitter, 3 attempts, ~10s budget)
- [ ] ~~Smoke test against a throwaway Sonarr+qBit pair in CI (testcontainers)~~ — **reclassed to M8**; M5 covers the state machine with in-process httptest fakes
- [x] Log redaction: slog handler scrubs api_key / password / bot_token attrs and secret query params
- [x] `DEPLOYMENT.md` `chmod 600` recommendation; strict SQLite file mode enforcement skipped (Docker/NAS UID friction)
- [x] ADR-0016 — Actor pipeline narrative

### Personal acceptance

Run for 2 weeks in `live` mode on my own homelab. Zero accidental deletions. Audit log makes every decision explainable.

## M6 — Dashboard

**Estimated**: 2-3 evenings · **Tag**: `v0.7.0`

- [x] React 19 + Vite scaffold under `web/`
- [x] shadcn/ui primitives, Tailwind v4 wired (Vite-native plugin)
- [x] TanStack Query + TanStack Router for `/api/v1` consumption
- [x] Pages:
  - [x] Dashboard (disk gauge + recent runs + top candidates)
  - [x] Torrents (sortable table, filter, detail page with score breakdown + history charts)
  - [x] Actions (timeline + per-action audit drawer)
  - [x] Settings (effective config redacted + manual run trigger + version)
- [x] Static bundle embedded via `embed.FS` (`web/web.go`)
- [x] `X-API-Key` validated with `crypto/subtle.ConstantTimeCompare`
- [x] Rate limit `POST /api/v1/runs` at 1/min/IP via `golang.org/x/time/rate`
- [x] Security headers on every response: `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy: ()`
- [x] Default bind `127.0.0.1:9494`
- [x] `/api/v1/config` redaction audit (`Config.Redacted()` + integration test)
- [x] ADR-0018 — frontend stack pins

## M6.1 — Auth opt-in + responsive UI

**Estimated**: 1 evening · **Tag**: `v0.7.1`

- [x] Opt-in built-in authentication (ADR-0019): `auth_users` / `auth_sessions` tables, bcrypt cost 10, opaque session cookie (HttpOnly, SameSite=Lax, Secure when HTTPS), 7-day sliding TTL with periodic sweep
- [x] HTTP surface: `GET/POST/DELETE /api/v1/session`, `POST /api/v1/auth/enable`, `POST /api/v1/auth/disable`, `POST /api/v1/auth/password`
- [x] Auth middleware accepts cookie OR `X-API-Key` in parallel (programmatic clients keep working)
- [x] Auth-mutating endpoints rate-limited 5/min/IP
- [x] `LoginGate` (replaces `ApiKeyGate`); Settings → Security card (enable / change password / disable, password auto-generation with one-time copy)
- [x] SPA drops `localStorage.apiKey`, switches to `credentials: 'include'`
- [x] Removed obsolete `http.auth` / `isLoopbackBind` / `/api/v1/auth-mode` (warning emitted for stale configs)
- [x] Responsive overhaul: mobile drawer + hamburger top bar, content centered at `max-w-screen-2xl`, table-to-card-stack fallback below `md`, touch-friendly nav

## M7 — Notifications

**Estimated**: 1 evening · **Tag**: `v0.8.0`

Scope narrowed by ADR-0021: notify only on disk-pressure runs that actually
executed — manual HTTP/CLI runs stay silent. One event, not four.

- [x] Notifier interface + Dispatcher (best-effort fan-out)
- [x] Telegram adapter (`net/http`, no SDK)
- [x] `notifications` config section + dashboard settings page (UI-editable credentials)
- [x] Post-action report: items deleted + sizes, total freed, disk free before/after
- [x] "Send test notification" endpoint + dashboard button
- [ ] Webhook adapter (generic POST JSON) — deferred until a second provider needs it
- [ ] Additional event types (`health_degraded`, …) — deferred (ADR-0021 "Revisit when")

## M8 — Polish & v1.0

**Estimated**: 1 weekend · **Tag**: `v1.0.0`

- [ ] Mount-convention boot validation (ADR-0023): sample qBit `save_path` + *arr import paths, `stat()` them in Triagearr's namespace, refuse to start with a diagnostic when the layout violates the TRaSH single-shared-mount convention
- [ ] Test coverage ≥70% on `scorer`, `linker`, `decider`, `actor`
- [ ] `govulncheck` clean (and added to CI `test` workflow, not just release)
- [ ] Goreleaser produces signed artifacts (cosign)
- [ ] SBOM generation via `syft` in the goreleaser pipeline (CycloneDX + SPDX)
- [ ] SLSA build provenance attestation via `actions/attest-build-provenance`
- [ ] Container image: distroless or `scratch` base, runs as non-root (`USER 65532:65532`), read-only root filesystem
- [ ] `SECURITY.md` at repo root: supported versions, disclosure policy, contact
- [ ] Renovate (or Dependabot) configured for weekly dep bumps, grouped by ecosystem
- [ ] Documentation reviewed for accuracy against final code
- [ ] Demo GIF in README
- [ ] Announcement post drafted (r/sonarr, r/Plex, Discord communities)

## Post-1.0 backlog (V2 candidates)

Ordered by likelihood, not commitment:

1. **Maintainerr integration** — read-only mirroring of Maintainerr collections; optional scoring factor that boosts items already marked for deletion in Maintainerr.
2. **Prometheus metrics** — `/metrics` endpoint with action counters, score distribution histograms, poll durations.
3. **Multi-qBit support** — for users with multiple download client instances. Triggers a config restructure: collapse the flat `qbit:` key into `torrent_clients:` keyed by instance name, mirroring `arrs:` (ADR-0022 seed-only YAML + DB source of truth). Requires a `TorrentClient` interface and a clients registry parallel to `internal/clients/registry`.
4. **Transmission/Deluge clients** — if any user actually asks. qBit is the dominant client in 2026. Lands on top of the `torrent_clients:` structure from #3; the interface designed there should be the abstraction boundary.
5. **Quality preference factor** — bias toward deleting lower-quality copies when duplicates exist.
6. **"Leaving soon" Plex collection** — surface scheduled deletions in Plex itself (à la Janitorr).
7. **Notification adapters** — Discord, Pushover, ntfy.sh.
8. **Web UI: history replay** — visual time-series of any torrent's ratio/seeders.
9. **Manual override workflow** — UI button to "keep this torrent" creates an exclusion entry with expiry.
10. **Multi-volume rebalancing** — when one volume is full but another has space, suggest moves before deletions (probably out of scope, requires *arr root-folder dance).

## What will NOT be done

To keep the project focused:

- ❌ Native Plex/Jellyfin/Emby integration beyond what *arr already provides
- ❌ Download client management (categories, tags, scheduling) — that's qbit_manage
- ❌ Malware detection — that's Cleanuparr
- ❌ Watch-history-driven library cleanup — that's Maintainerr
- ❌ Distributed/HA deployment
- ❌ Mobile app
- ❌ Per-user policies (Triagearr is single-tenant per instance)
