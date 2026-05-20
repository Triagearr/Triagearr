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
