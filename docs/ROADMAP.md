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

## M2 — Mapping & storage maintenance

**Estimated**: 8-10 h · **Tag**: `v0.3.0`

### Mapping (hardlink-aware)

- [ ] `internal/mapper` package
- [ ] Inode resolution via `syscall.Stat_t` (Linux)
- [ ] Cache of `torrent_hash ↔ inode ↔ arr_media_id`, invalidated on *arr poll diffs
- [ ] CLI: `triagearr inspect mapping <hash>` shows all paths linked to a torrent
- [ ] Detect cross-seed conflicts in mapping (multiple torrents → same inode)
- [ ] Tests on real filesystem with `os.TempDir` + hardlinks

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

- [ ] `internal/actor` package
- [ ] `mode: live` config requirement
- [ ] Per-*arr-instance `act: true` requirement (defense in depth)
- [ ] `arr-then-qbit` deletion pipeline per `docs/HARDLINK_TOPOLOGY.md`
- [ ] Cross-seed conflict handling (skip / warn_only / force_delete)
- [ ] `actions` + `audit_log` tables populated atomically
- [ ] Rate limiting (`max_deletions_per_run`, `inter_action_delay`)
- [ ] Retry with backoff on transient *arr/qbit failures
- [ ] Smoke test against a throwaway Sonarr+qBit pair in CI (testcontainers)
- [ ] Log redaction: never write `api_key`, `password`, or auth-bearing query strings to slog output
- [ ] New SQLite file created with `0640` mode (umask-respected via `os.OpenFile`); existing-file perms left alone — Docker UID/GID mismatches across homelab NAS setups are real and a strict enforcement would break more deployments than it would harden. Document the host-side recommendation (`chmod 600`, restrict the parent directory) in `DEPLOYMENT.md`.

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
