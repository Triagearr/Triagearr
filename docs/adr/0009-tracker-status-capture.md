# ADR-0009: Capture tracker status, treat dead-tracker as a first-class scoring signal

## Status

Accepted — 2026-05-19

## Context

M1 ran for ~1 h on the homelab library (262 torrents, 2 *arr instances). Inspection of `snapshots_raw` revealed two facts that the M3 scorer cannot currently model:

1. **No tracker information is stored.** `torrents` and `snapshots_raw` only carry qBit's `/api/v2/torrents/info` fields. `scoring.per_tracker` (ADR-0004) and the HnR window assume a tracker host is known per torrent. It is not.
2. **A non-trivial fraction of the library seeds on dead trackers.** Private-tracker shutdowns happen; old grabs survive in qBit but their tracker is unreachable. Today these torrents look identical to a "low-demand but healthy" torrent in our data (`seeders=0`, `leechers=0`, ratio frozen).

Conflating "live tracker, no current demand" with "dead tracker, nobody can ever ask again" is dangerous:

- The first is `rare content` and **must be protected** by `seeders_low_guard (-1000)`.
- The second carries **no seed obligation**, the HnR window is meaningless (no one to report a hit-and-run), and the torrent should be among the **first to delete**.

The `seeders_low_guard` veto, applied uniformly, will protect the dead-tracker torrents indefinitely — the opposite of what we want.

qBit exposes `/api/v2/torrents/trackers?hash=<H>` returning a list of `{url, status, msg, num_seeds, num_leeches, num_peers, num_downloaded, tier}` per tracker. `status` is `0=disabled, 1=not_contacted, 2=working, 3=updating, 4=not_working`.

## Decision

**Capture tracker state in a dedicated table, polled less often than `snapshots_raw`.**

### Schema (migration 0002)

```sql
CREATE TABLE torrent_trackers (
    torrent_hash   TEXT    NOT NULL,
    tracker_url    TEXT    NOT NULL,
    tracker_host   TEXT    NOT NULL,  -- parsed host, used by scoring.per_tracker
    status         INTEGER NOT NULL,  -- qBit raw: 0..4
    last_msg       TEXT    NOT NULL DEFAULT '',
    last_checked   TIMESTAMP NOT NULL,
    PRIMARY KEY (torrent_hash, tracker_url)
) WITHOUT ROWID;

CREATE INDEX idx_torrent_trackers_host ON torrent_trackers(tracker_host);
```

We **do not** snapshot tracker status over time. The set of trackers attached to a torrent is near-stable, and `status` changes on the order of days, not minutes. Time-series here would 100× the snapshot volume for no payoff.

### Polling

A new `tracker_interval` (default `6h`) is added to `polling`. The tracker poller fans out one call per torrent. With 262 torrents at 6 h, that is ~44 req/h — negligible. Existing `qbit_interval` is unchanged.

### Scoring impact

Two changes land in `docs/SCORING.md` alongside this ADR:

1. **New factor `tracker_dead_bonus`** (default `+40`): fires when **all** trackers attached to a torrent report `status = 4` (not_working) for the last `tracker_dead_grace` window (default `7d`). One transient outage will not flip a torrent into delete-bait.
2. **HnR veto degradation**: the `-10000` HnR hard veto only applies when **at least one tracker is alive**. When every tracker is dead, the veto silently downgrades to `0` — the seed-obligation contract no longer has a counterparty. This is the only documented exception to the "HnR is non-configurable" rule (see CLAUDE.md). It is not user-tunable; it is conditional on observable state.
3. **`scoring.per_tracker` keying** changes from "implicit single tracker" to "match on `tracker_host`; when multiple trackers attached, apply the **strictest** policy among matches" (`max(min_seed_days)`, `max(min_ratio)`, `max(rare_threshold)`).

### qBit `completion_on` capture

While we touch the qBit client, we also start storing `completion_on` on `torrents` (currently only `added_on` is captured). The HnR window measured from `added_on` is wrong on slow downloads; it must be measured from completion. This is a one-line addition, bundled in the same migration to avoid a 0003.

## Consequences

**Easier:**
- `per_tracker` config in YAML becomes usable for real (today it is dead code).
- Dead-tracker torrents naturally bubble to the top of the deletion queue — exactly the safest candidates.
- HnR risk modelling matches reality.

**Harder:**
- Schema v2 ships before M3 scorer instead of with it. We must guarantee migration is idempotent and re-runnable.
- We introduce a fourth poller (`tracker`), so the poller manager test matrix grows.
- "Strictest policy across multiple trackers" is more code than "lookup by single key". Worth it — many torrents have a primary + a backup tracker.

**Risk: false-positive "dead" classification.**
A tracker may flap (`status=4` briefly, then `2`). Mitigation: `tracker_dead_grace` window (default 7 d) requires sustained `status=4` before the bonus fires. Single-tick observations never trip the bonus.

**Risk: `tracker_dead_bonus` interacts with `seeders_low_guard`.**
When trackers are dead, `seeders=0` is inevitable, so `seeders_low_guard (-1000)` fires too. Their sum is `-1000 + 40 = -960`, still vetoed. **This is intentional**: the dead-tracker bonus by itself is not strong enough to override rare-content protection, only the HnR-degradation does that. A user who specifically wants dead-tracker torrents to be deletion candidates must additionally relax `seeders_low_guard` for matched trackers via `per_tracker` policy.

## Alternatives considered

1. **Inline trackers in `snapshots_raw`.** Rejected: 100× snapshot volume for data that changes at day-scale.
2. **One row per `(torrent_hash, ts)` in a `tracker_snapshots` table.** Rejected: same reason as 1; current-state is what scoring reads.
3. **Probe tracker reachability ourselves (DNS, TCP).** Rejected: qBit already does this and exposes the result. Doing it ourselves means re-implementing UDP/HTTP tracker protocol logic and racing qBit's own probes.
4. **Treat dead-tracker as a hard auto-delete trigger (no scoring).** Rejected: violates the multi-factor design (ADR-0004). The user should still be able to keep dead-tracker torrents (e.g., to seed via DHT/PEX, or to preserve rare content that left the tracker but is still wanted locally).
