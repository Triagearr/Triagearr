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

### Mapping (hardlink-aware)

- [ ] `internal/mapper` package
- [ ] Inode resolution via `syscall.Stat_t` (Linux)
- [ ] Path remap auto-inference at boot per ADR-0010 (sample qBit + *arr paths against local volume index, derive prefix substitution, ≥80 % confidence on ≥5 samples to accept)
- [ ] Mapper gate: hold queries until first qBit + *arr poll completes and inference settles
- [ ] Manual override via `volumes[*].path_remap` skips inference; each `to:` stat-ed at startup, missing dir = refuse to start
- [ ] Refuse-to-start with candidate distribution logged when inference produces no dominant rule
- [ ] Cache of `torrent_hash ↔ inode ↔ arr_file_id`, invalidated on *arr poll diffs
- [ ] Mapper exposes per-torrent resolution: `QbitFiles(hash) → []FileRef` AND `ArrTargets(hash) → [{instance, file_id, inode}]` (multi-file torrents per `docs/HARDLINK_TOPOLOGY.md`)
- [ ] CLI: `triagearr inspect mapping <hash>` shows EVERY file (qbit path, translated local path, inode, nlink, matched arr_file_id or "orphan") AND remap-rule origin
- [ ] CLI: `triagearr inspect remap` prints active rules per volume + their origin (inferred N/M vs config)
- [ ] Detect cross-seed conflicts in mapping (multiple torrents → same inode)
- [ ] Tests on real filesystem with `os.TempDir` + hardlinks
- [ ] Tests on inference: identity, simple prefix mismatch, ambiguous (refuse), per-category split

### Tracker capture (ADR-0009)

- [ ] Schema migration `0002`: add `torrent_trackers` table, add `media_files` table, add `completion_on` column to `torrents`
- [ ] qBit client: `ListTrackers(ctx, hash)` against `/api/v2/torrents/trackers`
- [ ] qBit client: capture `completion_on` in `ListTorrents` (one-field extension)
- [ ] New `tracker` poller (default `tracker_interval: 6h`); fan-out one call per torrent
- [ ] Persist parsed `tracker_host` alongside raw `tracker_url`
- [ ] CLI: `triagearr inspect trackers <hash>` prints current tracker statuses

### *arr per-file capture (prerequisite for mapper + M5 Actor)

- [ ] Sonarr client: `ListEpisodeFiles(ctx, seriesID)` against `/api/v3/episodefile?seriesId={id}`
- [ ] Radarr client: `ListMovieFiles(ctx, movieID)` against `/api/v3/moviefile?movieId={id}`
- [ ] Extend `arr` poller to fan out file calls per media item (rate-limited; default 5 req/s burst)
- [ ] Persist `{file_id, path, size}` per file into `media_files` — `file_id` is reused by M5 Actor for granular `DELETE`
- [ ] CLI: `triagearr inspect media <id>` includes the file list with sizes and ages
- [ ] Inference (ADR-0010) prefers `media_files.path` over `media.path` for sampling — deeper suffixes, sharper matches

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
- [ ] ~~Cross-seed conflict handling (T3.5 nlink stat + skip/warn_only/force_delete)~~ — **deferred to M8** (requires FS access; out of scope for full-API per ADR-0012)
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

- [ ] React 19 + Vite scaffold under `web/`
- [ ] shadcn/ui installed, Tailwind v4 wired
- [ ] TanStack Query for `/api/v1` consumption
- [ ] Pages:
  - [ ] Dashboard (volumes + pressure gauges + recent actions)
  - [ ] Torrents (sortable table, filter, score breakdown drawer)
  - [ ] Actions (timeline)
  - [ ] Settings (read-only view of effective config, redacted secrets)
- [ ] Static bundle embedded via `embed.FS`
- [ ] Auth via API key header (delegable to upstream reverse proxy)
- [ ] `X-API-Key` validated with `crypto/subtle.ConstantTimeCompare` (timing-attack resistance)
- [ ] Rate limit `POST /api/v1/runs` (destructive trigger; e.g. 1/min/IP via `golang.org/x/time/rate`)
- [ ] Security headers on UI responses: `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy: ()`
- [ ] Default bind `127.0.0.1:9494`; refuse to start with non-loopback bind unless `api_key` is set (already enforced in config validation — re-verify under tests)
- [ ] `/api/v1/config` redaction audit: every secret-bearing field (api_keys, qbit password, telegram bot_token) returns `"***"`; integration test asserts no leak

## M7 — Notifications

**Estimated**: 1 evening · **Tag**: `v0.8.0`

- [ ] Notifier interface + Telegram adapter
- [ ] Webhook adapter (generic POST JSON)
- [ ] Templates per event type (configurable)
- [ ] Event types: `action_executed`, `action_failed`, `pressure_triggered`, `health_degraded`

## M8 — Polish & v1.0

**Estimated**: 1 weekend · **Tag**: `v1.0.0`

- [ ] Test coverage ≥70% on `scorer`, `mapper`, `decider`, `actor`
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
3. **Multi-qBit support** — for users with multiple download client instances.
4. **Transmission/Deluge clients** — if any user actually asks. qBit is the dominant client in 2026.
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
