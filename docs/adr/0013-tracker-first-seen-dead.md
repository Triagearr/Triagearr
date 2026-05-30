# ADR-0013: Derive `first_seen_dead` from observed tracker transitions

## Status

Accepted — 2026-05-20

## Context

M3 scoring shipped in v0.4.0. Post-deploy validation on the homelab library (264 torrents, 99% private, 259 trackers reporting `status=4`) revealed that **Factor 7 (`tracker_dead_bonus`, +40) never fires**.

Root cause: `allTrackersDeadSustained` (`internal/scorer/factors.go`) gated the bonus on `last_checked > now − tracker_dead_grace`. `last_checked` is rewritten by `ReplaceTrackers` (`internal/store/repos.go`) on every tick of the `TrackerPoller` (default 6h), so the value never crosses the 7d grace window — the bonus is structurally unreachable.

The semantic the factor wants is "how long has this tracker been continuously not_working?" — a transition timestamp, not a last-polled one.

We verified live against qBit 5.2.0 (`/api/v2/torrents/trackers`) that the API does **not** expose this information. Fields returned per tracker: `url, status, msg, tier, num_seeds, num_leeches, num_peers, num_downloaded, min_announce, next_announce, updating, endpoints[…]`. `min_announce` and `next_announce` track the announce schedule (rewritten on every retry, success or failure), not status transitions. There is no `last_seen_alive` / `status_changed_at` / `first_failure_at`. The signal must be derived in Triagearr.

## Decision

**Persist a `first_seen_dead TIMESTAMP NULL` column on `torrent_trackers`. `ReplaceTrackers` is the single writer.**

Migration 0007 adds the column and backfills `first_seen_dead = last_checked WHERE status = 4` so existing graveyards mature after one grace window of subsequent polls.

`ReplaceTrackers` reads the prior `(status, first_seen_dead)` for each `(torrent_hash, tracker_url)` pair before the DELETE+INSERT, then for each incoming tracker:

| Prior state | Incoming status | `first_seen_dead` written |
|---|---|---|
| any | `≠ not_working` | NULL |
| absent or `≠ not_working` | `not_working` | now |
| `not_working` (with timestamp) | `not_working` | preserved |
| `not_working` (NULL) | `not_working` | now |

Factor 7 reads `first_seen_dead` instead of `last_checked`. Sustained = `first_seen_dead ≤ now − grace`.

## Alternatives considered

1. **Dedicated transition history table.** Audit-friendly but overkill for a single binary signal; more writes per poll, more code, no payoff for the current scoring needs.
2. **Counter `consecutive_dead_ticks`.** Couples `tracker_dead_grace` semantics to `tracker_interval` — changing one silently recalibrates the other. Surprising for the user.
3. **Redefine the factor to not require a timestamp** (e.g. trigger whenever every tracker is currently `not_working`). Loses the "sustained" property — a single bad poll trips the bonus. Too noisy.
4. **Probe the tracker ourselves to learn its uptime.** Already rejected in ADR-0009; reaffirmed here.

## Consequences

**Easier:**
- Factor 7 finally fires under realistic conditions.
- Restart-safe: state lives in SQL, not in poller memory.
- Decoupled from `tracker_interval` — changing the poll cadence does not recalibrate the grace.
- The same column is reusable by future factors that care about dead-duration (e.g. weighting by how long it's been dead).

**Harder:**
- `ReplaceTrackers` is no longer a pure DELETE+INSERT — it must read prior state in the same tx. Still atomic.
- A user who manually edits `torrent_trackers` (rare) needs to know about the new column.

**Risk: backfill conservatism.**
After migration, historical dead trackers carry `first_seen_dead = last_checked` (≈ migration time). They mature only after a full grace window of subsequent polls (default 7d). Acceptable because the Decider (M4) is not yet live; by the time it ships, the backfilled population will have matured.

**Risk: tracker URL change preserves nothing.**
If a tracker rewrites its announce URL while staying in `status=4`, the old row is deleted and the new row starts a fresh `first_seen_dead`. This is a rare edge case; the cost is one extra grace window of patience.

## Amendment — 2026-05-30: seed the transition from an activity proxy

The decision table above stamped `first_seen_dead = now` on the alive→dead transition. Field experience exposed the flaw: `now` measures *Triagearr's* observation history, not the tracker's real death. The two diverge whenever Triagearr first sees an already-dead tracker — a torrent re-added or cross-seeded from long-dead content is made to serve a full grace window it factually outlived. A wiped DB is the degenerate case (the whole library re-observed at once); it is routine only during alpha, where the schema is reset rather than migrated, so it is an amplifier of the bug, not its root. Re-confirmed live on qBit 5.2.0: still no `status_changed_at` / `last_seen_alive`; the only per-tracker timestamps are the *future* `min_announce`/`next_announce` schedules.

Scope note: the seed fires on the *transition*, so existing rows already preserved as dead keep their old `now`-stamped value until a recovery→dead cycle. Under the alpha wipe-on-deploy flow this self-corrects on the next deploy. When the project moves to migrate-without-wipe, a one-time backfill (re-seed currently-dead `first_seen_dead` from the same proxy) is the clean place to retire the stale values — deferred until that cut.

**Refinement:** on the transition, seed `first_seen_dead = min(now, activity_proxy)` where `activity_proxy = COALESCE(torrents.last_activity, torrents.completion_on, torrents.added_on)`. `torrents.last_activity` is a new column mirroring qBit's per-torrent last-data-moved time, written by the torrent poller — so the proxy is a single race-free read of a row the tracker poller already guarantees exists (it enumerates hashes from `torrents`), with no dependency on a snapshot having landed first. `ReplaceTrackers` computes it in the same tx (one lazy read, reused across all of a hash's trackers). The two "→ now" cells in the decision table become "→ min(now, activity_proxy)"; preservation and dead→alive→dead reset are unchanged.

Rationale: a swarm's `last_activity` is the closest observable signal to "graveyard since" — it does not advance on failed reannounce attempts, so it stays old for genuinely dead torrents (qualify immediately, wipe-proof) yet stays recent for a live swarm whose tracker merely blips (grace still protects it). The `completion_on`/`added_on` fallbacks cover the narrow window after a wipe before the first snapshot lands. When no proxy exists at all (brand-new torrent, no rows) the seed degrades to `now` — the original behaviour. The migration-0007 backfill note above is now moot: long-dead torrents mature on their first post-amendment poll instead of waiting a grace window.

**Cross-client survey (why the proxy, not a client field).** Confirmed across the four mainstream clients that none exposes a *first-failure* timestamp:

| Client | Backend | Per-tracker signal | "Last seen alive" timestamp |
|---|---|---|---|
| qBittorrent | libtorrent | `status`, `fails`, future `min/next_announce` | none |
| Deluge | libtorrent | same `announce_entry`/`announce_endpoint` | none |
| Transmission | own | `last_announce_time`, `last_announce_succeeded`, `announce_state` | partial (last attempt + success flag) |
| rTorrent | own | `t.success_time_last`, `t.failed_time_last`, `t.activity_time_last` | yes (`success_time_last`, epoch) |

qBit and Deluge share libtorrent's blindness, so the activity proxy is the correct cross-client common denominator. Transmission and rTorrent *do* surface a last-alive timestamp (rTorrent's `success_time_last` is the best of any client). Forward hook: when those providers are implemented under `internal/clients/torrent/`, `triagearr.TrackerInfo` can carry an optional `LastAliveAt` that those clients populate and `ReplaceTrackers` prepends to the COALESCE chain; qBit/Deluge leave it nil and fall back to `last_activity` as now.
