# ADR-0025: torrent client connections owned by the database, YAML as seed

## Status

Accepted — 2026-05-25

## Context

Until now the qBittorrent client lived in the flat `qbit:` block of
`config.yml` — a single, unnamed instance. Editing the URL, the username, the
password, or the exclude lists meant hand-editing YAML and SIGHUP-ing the
daemon. ADR-0022 already moved the *arr instances into a DB-owned table with
YAML-as-seed semantics; the qBit block was the last credential surface still
yaml-only.

The roadmap (`docs/ROADMAP.md` item #3) anticipated this restructure as soon
as multi-client support became plausible — Transmission/Deluge/rTorrent are
plausible future backends, and item #4 lists them. Doing the schema work now
keeps the daemon ready for those backends without committing to building them.

The override mechanism (ADR-0020 `settings_overrides`) is the wrong tool for
credentials: it is keyed by dotted koanf paths and works well for scalars, but
exposing a password through a "set this override" endpoint pollutes the
overrides surface with secrets and offers no obvious place for "test
connection". The *arr connections precedent (ADR-0022) is the right shape to
mirror.

The operator wants one connection per kind. Multiple qBit instances of the
same kind ("main" + "backup") are explicitly out of scope — qBit deployments
are typically single-host in homelab setups, and the UNIQUE(kind) constraint
keeps the schema and the UI simple. If the need arises, the table can be
migrated to UNIQUE(name) later.

## Decision

**The `torrent_client_connections` SQLite table is the source of truth for
torrent clients.** The YAML `torrent_clients:` block becomes a one-time seed.

- **Schema** — migration `0006_torrent_client_connections.sql`:
  ```sql
  CREATE TABLE torrent_client_connections (
      id                INTEGER PRIMARY KEY AUTOINCREMENT,
      kind              TEXT      NOT NULL UNIQUE,   -- qbittorrent | transmission | deluge | rtorrent
      url               TEXT      NOT NULL,
      username          TEXT      NOT NULL DEFAULT '',
      password          TEXT      NOT NULL DEFAULT '',
      enabled           INTEGER   NOT NULL DEFAULT 1,
      category_exclude  TEXT      NOT NULL DEFAULT '[]',
      tags_exclude      TEXT      NOT NULL DEFAULT '[]',
      delete_with_files INTEGER   NOT NULL DEFAULT 1,
      timeout_ms        INTEGER   NOT NULL DEFAULT 30000,
      created_at        TIMESTAMP NOT NULL,
      updated_at        TIMESTAMP NOT NULL
  );
  ```
  Kind is the sole identity. There is no `poll` / `act` split as on *arr:
  acting on a torrent client is implied by the daemon's global `mode`
  (dry-run / live) and the disk-pressure trigger model (ADR-0015) — gating
  qBit deletes behind a per-instance `act` flag would be redundant.
- **Seeding** — on boot, if `torrent_client_connections` is empty, the
  qbittorrent block from the YAML `torrent_clients:` section is inserted
  once. Mirrors the ADR-0022 seed path; same `Count → Seed → List → re-Validate`
  sequence.
- **Resolution** — after seeding, `cfg.TorrentClients` is rebuilt from the
  table before `torrentregistry.BuildFromConfig` runs. The YAML block is
  ignored thereafter.
- **CRUD over HTTP** — `GET /api/v1/torrent-client-connections`,
  `PUT /api/v1/torrent-client-connections/{kind}`,
  `DELETE /api/v1/torrent-client-connections/{kind}`. Each mutation triggers
  the same self-SIGHUP used by the settings and *arr endpoints — the
  daemon tears down and rebuilds with a fresh torrent registry, so a
  connection edit takes effect without a manual restart.
- **Test action** — `POST /api/v1/torrent-client-connections/test` builds an
  ephemeral client and runs `ListTorrents`, surfacing the failure to the
  operator. Mirrors the ADR-0022 `HealthCheck` test.
- **Credentials** — the password is stored in SQLite and returned verbatim by
  the GET endpoint (behind auth, rendered as a password field client-side).
  Same trade-off as ADR-0021 and ADR-0022.
- **Scaffolded kinds** — transmission, deluge, and rtorrent are recognised
  by the registry (`KnownKind`) but rejected by `ImplementedKind`. The HTTP
  layer answers 400 on PUT for unimplemented kinds. The UI shows greyed-out
  "coming soon" tiles. The daemon refuses to start if the operator enables
  one of these in YAML before its backend lands.

## Consequences

### Positive

- Full add/edit/delete of the qBittorrent connection from the dashboard; no
  YAML edit, no manual restart.
- The schema, the HTTP API, and the UI are ready for Transmission / Deluge /
  rTorrent backends — adding one means writing the client and flipping
  `ImplementedKind`, not changing the data plane.
- `torrentregistry.BuildFromConfig` is the new chokepoint — pollers, actor,
  preflight, and the score CLI all reach the qBit client through it. Future
  backends slot in behind `triagearr.TorrentClient` without rewiring callers.

### Negative / acknowledged

- The qBit password now lives in SQLite, outside the git-audited YAML — same
  trade-off ADR-0021 / ADR-0022 already accepted. `DEPLOYMENT.md`'s
  `chmod 600` on the DB file covers it.
- The YAML `torrent_clients:` block becomes inert after first boot. Editing
  it has no effect.
- The Triagearr alpha had a single `qbit:` YAML key; this ADR renames it to
  `torrent_clients.qbittorrent:`. Pre-alpha operators must update their
  YAML — no compatibility shim is provided (the project is still pre-v1.0).
- One source-of-truth surface flips for one boot only (YAML seeds the table,
  then YAML is dead). Logged at seed time.

## Revisit when

- A Transmission / Deluge / rTorrent backend lands — the registry switch
  (`ImplementedKind`) is the only place to change.
- Multi-qBit-instance support becomes desirable — migrate UNIQUE(kind) to
  UNIQUE(name) + a `kind` column, similar to the multi-arr discussion that
  preceded ADR-0022.
