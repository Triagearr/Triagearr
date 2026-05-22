# ADR-0022: *arr connections owned by the database, YAML as seed

## Status

Accepted — 2026-05-22

## Context

Until now every *arr instance lived in the `arrs:` block of `config.yml`:
URL, API key, and the `enabled`/`poll`/`act` flags. Adding, editing, or
removing an instance meant hand-editing YAML and restarting (or SIGHUP-ing)
the daemon.

ADR-0019 (opt-in auth) and ADR-0021 (UI-managed notification credentials)
already moved the project toward operator-facing configuration: the dashboard
can enable auth and edit the Telegram bot token. The remaining friction is the
*arr connections — the one piece of setup a homelab operator most often
revisits (a Sonarr moved hosts, an API key rotated, a new Radarr instance).

The runtime-override mechanism (ADR-0020 / `settings_overrides`) is keyed by
dotted koanf paths and works well for scalars. It is a poor fit for *arr
instances: they are *arrays* of structs, so a per-field override would need
synthetic indexed keys (`arrs.sonarr.0.url`) that break the moment an instance
is inserted or removed. Overrides cannot express "add" or "delete" at all.

## Decision

**The `arr_connections` SQLite table is the source of truth for *arr
instances.** The YAML `arrs:` block becomes a one-time seed.

- **Schema**: migration `0012_arr_connections.sql` — one row per instance,
  `UNIQUE(kind, name)`. `act` defaults to `0` (the non-negotiable safety rule:
  acting requires an explicit per-instance opt-in).
- **Seeding**: on boot, if `arr_connections` is empty, the instances from the
  YAML `arrs:` block are inserted once. This makes the migration transparent
  for existing operators — their YAML config keeps working with no edit.
- **Resolution**: after seeding, `cfg.Arrs` is rebuilt from the table before
  the client registry is constructed. From that point the YAML `arrs:` block is
  ignored; the table wins. `config.Validate` re-runs on the resolved set.
- **CRUD over HTTP**: `GET/POST /api/v1/arr-connections`,
  `PUT/DELETE /api/v1/arr-connections/{id}`. Each mutation triggers the same
  self-SIGHUP used by the Settings endpoints (ADR-0020) — the daemon tears down
  and rebuilds with a fresh registry, so a connection edit takes effect without
  a manual restart.
- **Test action**: `POST /api/v1/arr-connections/test` builds an ephemeral
  client from the posted `kind`/`url`/`api_key` and runs `HealthCheck`,
  surfacing the failure to the operator — the same pattern as the notification
  test (ADR-0021).
- **Credentials**: the API key is stored in SQLite and returned verbatim by the
  GET endpoint (behind auth, rendered as a password field client-side). This
  follows the ADR-0021 precedent — the operator explicitly asked for UI-managed
  connection management, and editing a key requires reading it back.

## Consequences

### Positive

- Full add/edit/delete of *arr instances from the dashboard; no YAML edit, no
  manual restart.
- The registry-rebuild path is unchanged — `registry.BuildFromConfig` still
  reads `cfg.Arrs`; only the *origin* of `cfg.Arrs` changed. Pollers, the
  actor, and `arrURLMap` are untouched.
- Existing YAML configs migrate silently via the empty-table seed.

### Negative / acknowledged

- *arr API keys now live in SQLite, outside the git-audited YAML — same
  trade-off ADR-0021 accepted for the Telegram token. `DEPLOYMENT.md`'s
  `chmod 600` recommendation on the DB file covers this.
- The YAML `arrs:` block becomes inert after first boot. Editing it has no
  effect; this is documented in `config.example.yml`. An operator who wants to
  return to YAML-driven config must clear the table.
- Two source-of-truth surfaces for the same data on first boot only (YAML seeds
  the table, then YAML is dead). Acceptable — the seed runs once and is logged.

## Revisit when

- An operator needs to re-seed from an edited YAML — consider a
  `triagearr arrs reseed` CLI command rather than reintroducing YAML
  precedence.
- qBit connection management is wanted in the UI — it would follow this same
  pattern (single row, not an array, so a dedicated table is less obviously
  warranted).
