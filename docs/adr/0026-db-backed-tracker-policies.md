# ADR-0026: DB-backed tracker policies with conservative defaults

**Status**: Accepted (2026-05-26)

**Supersedes** the YAML-only `scoring.per_tracker` and `scoring.rare_content_threshold` of ADR-0004.

## Context

Factor 1 (`ratio_obligation_met`, +50) and Factor 4 (`seeders_low_guard`, −1000) both look up per-tracker policy via `scoring.per_tracker[host]` in the YAML config. The lookup falls back to the Go zero-value `TrackerPolicy{MinRatio: 0, MinSeedDays: 0, RareThreshold: nil}` when the tracker host is not declared — at which point:

- Factor 1 fires on *any* private torrent (ratio ≥ 0 and seed_days ≥ 0 are trivially true), crediting the +50 ratio-obligation bonus without the user having configured anything.
- Factor 4's `rare_threshold` falls back to the global `rare_content_threshold` (default 3) which is sane but still hides the silent fallback.

In practice, on a real library where the user has not enumerated every tracker host in YAML, **every private torrent with `ratio < 1` scores +50 instead of 0**. The bug is not in the factor math; it is in the config defaults.

Beyond the math, the project memory ([[project_ui_managed_config]], ADR-0022, ADR-0025) commits to moving config off YAML and into the database with the UI as the source of truth. Per-tracker policies are squarely UI-managed territory: the user discovers a tracker by observing its torrents, and wants to assign a policy from that same UI rather than editing YAML and SIGHUPing the daemon.

## Decision

1. **Move per-tracker policies from YAML to the database.** A new `tracker_policies` table holds one row per `tracker_host` with `min_ratio`, `min_seed_days`, `rare_threshold`, and an `enabled` flag. The YAML `scoring.per_tracker` block is removed (Triagearr is alpha; no migration shim — [[feedback_alpha_no_backcompat]]).

2. **Introduce a `scoring_defaults` singleton.** One row, primary key forced to `1`, holding `min_ratio`, `min_seed_days`, `rare_threshold`. Seeded on first boot with conservative defaults:
   - `min_ratio = 1.0`
   - `min_seed_days = 30`
   - `rare_threshold = 3`
   
   The YAML `scoring.rare_content_threshold` is removed; its value moves to the DB defaults.

3. **`trackerPolicyFor` falls back to defaults**, not to Go zero-values. When a torrent's tracker has no row in `tracker_policies` (or has `enabled = 0`), the effective policy comes from `scoring_defaults`. When the torrent has multiple trackers, the strictest-wins rule still applies (max ratio, max seed_days, min rare_threshold), evaluated row by row, with each missing row resolved through the defaults.

4. **Factor 1 (`ratio_obligation_met`) grows a `tracker_dead` gate.** When `!anyTrackerAlive(trackers)` (every tracker reports `status=4` not_working), Factor 1's contribution is forced to 0 with `Gate = GateAllDead`. Symmetric with Factor 6 (HnR) which already degrades on dead trackers: when there is no counterparty, there is no obligation to enforce — neither reward nor penalty applies. Factor 7's `tracker_dead_bonus` continues to surface graveyard candidates.

5. **The `enabled` toggle on `tracker_policies` is a soft delete.** Setting `enabled = 0` makes `trackerPolicyFor` ignore the row and fall through to defaults. The UI uses this to "temporarily disable" a policy without losing the configured values (handy when a tracker is dead and the user wants the defaults to win without retyping the policy later).

6. **Tracker discovery is driven by `torrent_trackers`.** The UI lists distinct `tracker_host` values observed in `torrent_trackers`, augmented with the policy row when one exists, plus a torrent count and the last observed status (alive/dead). Free-text entry is *not* offered — the only trackers worth configuring are the ones already attached to torrents the user owns.

## Schema

Both tables are added to the squashed init migration (Triagearr is alpha; the schema baseline is mutable until v1.0 per `0001_init.sql`).

```sql
-- Global defaults for tracker policy lookups. Single row, PK forced to 1.
CREATE TABLE scoring_defaults (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    min_ratio     REAL    NOT NULL DEFAULT 1.0,
    min_seed_days INTEGER NOT NULL DEFAULT 30,
    rare_threshold INTEGER NOT NULL DEFAULT 3,
    updated_at    TIMESTAMP NOT NULL
);
INSERT INTO scoring_defaults(id, min_ratio, min_seed_days, rare_threshold, updated_at)
VALUES (1, 1.0, 30, 3, CURRENT_TIMESTAMP);

-- Per-tracker overrides. tracker_host is the natural key (matches
-- torrent_trackers.tracker_host). Disabled rows are ignored at lookup time
-- and the defaults apply instead.
CREATE TABLE tracker_policies (
    tracker_host   TEXT      PRIMARY KEY,
    min_ratio      REAL      NOT NULL,
    min_seed_days  INTEGER   NOT NULL,
    rare_threshold INTEGER,   -- NULL = inherit default
    enabled        INTEGER   NOT NULL DEFAULT 1,
    updated_at     TIMESTAMP NOT NULL
) WITHOUT ROWID;
```

## API

```
GET    /api/v1/scoring/defaults
PUT    /api/v1/scoring/defaults
GET    /api/v1/scoring/tracker-policies         -- list, joined with torrent_trackers discovery
PUT    /api/v1/scoring/tracker-policies/{host}
DELETE /api/v1/scoring/tracker-policies/{host}  -- reset to defaults
```

The list endpoint returns one entry per host observed in `torrent_trackers`, with the effective policy (override if any, else default) and operational metadata (`torrent_count`, `any_alive`, `policy_source: "override" | "default"`).

## Consequences

- **Breaking config change**: `scoring.per_tracker` and `scoring.rare_content_threshold` removed from YAML. Existing deployments lose their YAML overrides on next boot — alpha, no shim.
- **Safer defaults**: an unconfigured private tracker now requires ratio ≥ 1.0 and 30 days of seeding before Factor 1 credits the +50, instead of crediting it unconditionally.
- **One source of truth**: per-tracker policy lives in the DB; the UI edits it directly. No YAML/DB drift.
- **Dead-tracker symmetry**: Factor 1 joins Factor 4 and Factor 6 in degrading on `!anyTrackerAlive`. The graveyard worked example (SCORING.md §Example D) gets simpler — no more "ratio_obligation contributes 0 because tracker_policy says so", just "contributes 0 because the obligation is moot".
