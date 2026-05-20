# Scoring

The heart of Triagearr is the `DeleteScore`: a number computed per torrent that expresses "how safe is it to delete this from disk." Higher score = more safely deletable.

## Goals of the scoring system

1. **Auditable**: every score must be explainable. Each contributing factor is persisted alongside the final value.
2. **Tunable**: weights live in YAML, not in code. A user can shift the tradeoff between "be aggressive" and "be conservative" without redeploying.
3. **Multi-criteria**: the decision is not based on a single signal. Ratio alone is insufficient (you'd prematurely delete useful seeds). Upload velocity alone is insufficient (you'd punish freshly added torrents). The combination is what makes Triagearr defensible.
4. **Guard rails first**: certain conditions are near-vetoes. Rare-content protection and HnR-window protection use extreme weights (`-1000`) such that no realistic combination of bonuses can override them.

## Inputs

For every torrent, the scorer reads:

| Source | Field | Used for |
|---|---|---|
| `torrents` (current) | category, tags, state, added_on, completion_on, private | exclusion filters, age, HnR, private gating |
| `torrent_trackers` (current, ADR-0009) | tracker_host, status, last_checked | per-tracker policy, dead-tracker bonus, HnR degradation |
| `snapshots_raw` (last 30d) | ratio, uploaded, seeders, leechers | velocity, swarm health |
| `snapshots_daily` (last 365d) | aggregates | long-term trends |
| `media` (joined via `arr_imports` per ADR-0012) | tags, *arr type, monitor status | exclusion filters |
| `arr_instances` config | per-instance tags_exclude | exclusion filters |
| `scoring.per_tracker` config | min_seed_days, min_ratio, rare_threshold | tracker-specific rules (keyed on tracker_host) |
| filesystem | torrent age (added_on / completion_on), last_activity_on | core math |

## The formula

```
DeleteScore = Σ factors

where each factor = weight × value, computed as follows:
```

### Factor 1 — Ratio obligation met

```
if NOT torrent.private:
    value = 0            (public trackers have no enforceable ratio obligation)
else:
    value  = 1.0 if (ratio ≥ min_ratio_for_tracker) AND (seed_days ≥ min_seed_days_for_tracker)
           = 0.0 otherwise
weight = scoring.weights.ratio_obligation_met   (default +50)
```

Boolean, **gated on `private:true`**. Public trackers don't enforce ratio (no account, no penalty), so satisfying or not satisfying a ratio on a public torrent has no economic meaning — this factor contributes 0. For private torrents, either we've met the tracker's stated requirements or we haven't. When unmet, this factor contributes 0 (not a penalty — penalty comes from other factors).

### Factor 2 — Upload velocity (inverse)

```
velocity_30d   = (uploaded[t] - uploaded[t - 30d]) / 30d   (bytes/day)
velocity_norm  = velocity_30d / global_avg_velocity_30d    (1.0 if average)
value          = max(0, 1.0 - velocity_norm)              (clamped to [0, 1])
weight         = scoring.weights.upload_velocity_inv       (default +30)
```

A torrent that uploads nothing recently scores high (delete-safe). A torrent that actively serves the swarm scores low. The normalization makes the scale relative to the user's library (a quiet library has different "active" thresholds than a busy one).

### Factor 3 — Age

```
value  = (now - added_on).days
weight = scoring.weights.age_days                (default +0.1 per day)
```

Old torrents are slightly preferred for deletion, ceteris paribus. The weight is intentionally small — age alone is never enough; it's a tiebreaker.

### Factor 4 — Rare content guard (the big veto)

```
seeders_avg_7d    = average seeders count over last 7 days
threshold         = tracker-specific override OR scoring.rare_content_threshold (default 3)
any_tracker_alive = any(t.status != 4 for t in trackers)

if any_tracker_alive AND seeders_avg_7d ≤ threshold:
    value  = 1.0
    weight = scoring.weights.seeders_low_guard   (default -1000)
else:
    value  = 0
```

This is the swarm-health guard. When a torrent has few seeders, Triagearr essentially refuses to delete it — `-1000` overwhelms any positive contribution. The guard is configurable per tracker (private trackers may want a more conservative threshold).

**Critical gate: `any_tracker_alive`.** When every tracker attached to the torrent reports `status = 4 (not_working)`, the swarm signal becomes meaningless — `seeders=0` is no longer evidence of rare content, it is evidence of dead infrastructure. The guard would mis-fire and sanctuarize precisely the torrents Triagearr exists to reap (dead-tracker graveyards). The `any_tracker_alive` gate ensures the rare-content protection only applies when there is a living counterparty whose swarm count is observable. This is symmetric with Factor 6 (HnR), which also degrades when all trackers are dead.

### Factor 5 — Swarm health bonus

```
seeders_avg_7d = average seeders count over last 7 days
value          = log10(seeders_avg_7d + 1)
weight         = scoring.weights.swarm_health_bonus   (default +5)
```

If the swarm is healthy (many seeders), Triagearr is more willing to step back. Logarithmic so that the jump from 3 seeders to 30 is meaningful, but 100 vs 1000 is only marginally more "delete-safe".

### Factor 6 — HnR window veto

```
seed_start = completion_on if known, else added_on
any_tracker_alive = any(t.status != 4 for t in trackers)   # qBit status 4 = not_working
                    OR all trackers' last_checked is stale (< grace window of working state)

if NOT torrent.private:
    value = 0            (public trackers have no HnR concept — no account, no penalty)
elif (now - seed_start).days < scoring.hnr_window_days AND any_tracker_alive:
    value  = 1.0
    weight = -10000      (hard veto, not configurable)
elif (now - seed_start).days < scoring.hnr_window_days AND NOT any_tracker_alive:
    value  = 0           (veto degraded — no counterparty to enforce HnR)
```

A torrent within the HnR (hit-and-run) window is **never** deleted while any tracker is alive **and the torrent is private** — getting flagged for HnR has consequences (account warning, ratio penalty, ban). Triagearr's safety contract guarantees this for private torrents. Public torrents do not have HnR semantics: no account is at stake, no penalty can be incurred, the factor is inert (`value=0`).

The veto silently downgrades to `0` only when **every** tracker attached to the torrent reports `status = 4 (not_working)` for sustained periods (see ADR-0009 and Factor 7). In that case there is no counterparty to enforce the seed obligation; the HnR contract has lapsed. This is the only documented exception to "HnR is non-configurable". It is conditional on observable state, not on user config.

The window is measured from `completion_on` when available (qBit reports it), falling back to `added_on` for legacy rows. Measuring from `added_on` over-counts the window on slow downloads.

### Factor 7 — Tracker dead bonus (ADR-0009)

```
all_dead    = every tracker for this hash has status == 4 (not_working)
sustained   = max(t.first_seen_dead) for all trackers with status==4 ≤ now − scoring.tracker_dead_grace
value       = 1.0 if all_dead AND sustained, else 0
weight      = scoring.weights.tracker_dead_bonus       (default +40)
```

A torrent whose every tracker has been unreachable for at least `tracker_dead_grace` (default `7d`) carries no seed obligation. It bubbles up the deletion queue without needing user intervention.

`first_seen_dead` is set by the tracker poller the first tick a tracker reports `status=4`, preserved as long as the tracker stays in `not_working`, and cleared on recovery (ADR-0013). qBit does not expose this timestamp directly; `last_checked` is rewritten every poll (default 6h) and therefore cannot be used to measure "sustained dead".

**Interaction with `seeders_low_guard`.** Factor 4 (`seeders_low_guard`) is gated on `any_tracker_alive`, so when every tracker is dead, the rare-content guard does not fire — `seeders=0` on a dead infrastructure is not evidence of rarity. This means dead-tracker torrents naturally surface as candidates (positive net score from age + velocity + dead_bonus), without needing per-tracker policy overrides. The HnR degradation in Factor 6 covers the "obligation" half (no counterparty to punish HnR); the present factor covers the "preference" half (these torrents bubble up the queue). Dead-tracker reaping is the **primary** Triagearr use case, not an opt-in escape hatch.

### Factor 8 — Exclusion overrides

If the torrent matches any of:
- `qbit.category_exclude` category
- `qbit.tags_exclude` tag
- Linked media has `arrs.*.tags_exclude` *arr tag
- *arr media `monitored: false` AND user enabled "skip unmonitored"

Then the torrent is **filtered out before scoring** — it doesn't appear in candidates at all. No score is computed.

## Worked example

### Example A — public, healthy swarm

Torrent: `Ubuntu.22.04.iso`
- Tracker: `archive.example` (**public**, `min_seed_days: 0`, `min_ratio: 0`, `rare_threshold: 0`)
- `private`: false
- Added: 180 days ago
- Ratio: 12.3
- Uploaded last 30d: ~0 bytes (already saturated, nobody downloads it anymore)
- Seeders avg 7d: 287 (huge public swarm)

Score breakdown with default weights:

| Factor | Value | Weight | Contribution |
|---|---|---|---|
| Ratio obligation met | 0 (public — gate) | +50 | **0** |
| Velocity inverse | ~1.0 (no uploads) | +30 | **+30** |
| Age | 180 | +0.1 | **+18** |
| Rare guard | seeders > threshold → not triggered | n/a | **0** |
| Swarm health bonus | log10(288) ≈ 2.46 | +5 | **+12.3** |
| HnR window veto | 0 (public — gate) | n/a | **0** |
| **Total** | | | **+60.3** |

This torrent ranks high. The library is saturated for this content (~300 seeders), no fresh upload activity, public so no ratio obligation. Safe to delete.

### Example B — public, rare content

`Obscure.Documentary.2019.mkv`
- Same tracker, same age, same ratio
- `private`: false
- But seeders avg 7d: **2** (and tracker is alive — `any_tracker_alive = true`)

| Factor | Value | Weight | Contribution |
|---|---|---|---|
| Ratio obligation met | 0 (public — gate) | +50 | 0 |
| Velocity inverse | ~1.0 | +30 | +30 |
| Age | 180 | +0.1 | +18 |
| **Rare guard** | **1.0** (tracker alive AND seeders ≤ 3) | **-1000** | **−1000** |
| Swarm health bonus | log10(3) ≈ 0.48 | +5 | +2.4 |
| HnR window veto | 0 (public — gate) | n/a | 0 |
| **Total** | | | **−949.6** |

The rare-content guard vetoes deletion. Even though everything else suggests "delete," the swarm health argument wins. This is the design intent.

### Example C — private, ratio met, HnR cleared

`Severance.S02.MULTi.1080p.WEB.H265.mkv` on a private tracker with active swarm.
- Tracker: `private.example` (**private**, `min_seed_days: 7`, `min_ratio: 1.0`, `rare_threshold: 5`)
- `private`: true, `any_tracker_alive`: true
- Added: 120 days ago, completion 119 days ago (HnR window cleared at 14d)
- Ratio: 2.4, seed_days: 119
- Seeders avg 7d: 38

| Factor | Value | Weight | Contribution |
|---|---|---|---|
| Ratio obligation met | 1.0 (private, ratio≥1, seed_days≥7) | +50 | **+50** |
| Velocity inverse | ~0.9 (slowing) | +30 | +27 |
| Age | 120 | +0.1 | +12 |
| Rare guard | seeders > threshold (38 > 5) | n/a | 0 |
| Swarm health bonus | log10(39) ≈ 1.59 | +5 | +7.9 |
| HnR window veto | 120 > 14 → not triggered | n/a | 0 |
| **Total** | | | **+96.9** |

Ratio paid, HnR cleared, swarm healthy. Candidate.

### Example D — private, dead tracker (graveyard)

Same torrent as C, but the private tracker has been offline for 30+ days. `any_tracker_alive = false`.

| Factor | Value | Weight | Contribution |
|---|---|---|---|
| Ratio obligation met | 0 (no live counterparty to honor) | +50 | 0 |
| Velocity inverse | 1.0 (no uploads — dead swarm) | +30 | **+30** |
| Age | 120 | +0.1 | +12 |
| Rare guard | 0 (gate: not any_tracker_alive) | n/a | **0** |
| Swarm health bonus | log10(1) = 0 | +5 | 0 |
| HnR window veto | 0 (gate: not any_tracker_alive) | n/a | 0 |
| Tracker dead bonus | 1.0 (sustained dead) | +40 | **+40** |
| **Total** | | | **+82** |

The graveyard case. No obligation, no swarm to protect, dead_bonus fires. Top-tier candidate — exactly what Triagearr exists to reap.

## Score thresholds

Triagearr doesn't use absolute thresholds (e.g. "delete if score > 50"). Instead, the Decider:

1. Computes scores for all candidates
2. Filters out anything with a *negative* score (veto territory)
3. Sorts the remaining candidates by score descending
4. Selects top-K until either:
   - `target_free_percent` is reached on the pressured volume, OR
   - `max_deletions_per_run` is hit, OR
   - candidates exhausted

So the score is **comparative, not absolute**. Two libraries with very different patterns will naturally calibrate themselves: in a library full of saturated public-tracker stuff, all scores will be high and the cutoff is meaningless; in a library of mostly private-tracker recent grabs, scores will cluster low and the cutoff naturally protects everything.

## Explainability API

```
GET /api/v1/scores/{hash}/explain
```

Returns the full breakdown:

```json
{
  "torrent_hash": "abc123...",
  "score": 60.3,
  "verdict": "candidate",
  "computed_at": "2026-05-17T14:30:00Z",
  "private": false,
  "any_tracker_alive": true,
  "factors": [
    { "name": "ratio_obligation_met", "value": 0, "weight": 50.0, "contribution": 0, "gate": "public — inert" },
    { "name": "upload_velocity_inv", "value": 1.0, "weight": 30.0, "contribution": 30.0 },
    { "name": "age_days", "value": 180, "weight": 0.1, "contribution": 18.0 },
    { "name": "seeders_low_guard", "value": 0, "weight": -1000, "contribution": 0 },
    { "name": "swarm_health_bonus", "value": 2.46, "weight": 5.0, "contribution": 12.3 },
    { "name": "hnr_window_veto", "value": 0, "weight": -10000, "contribution": 0, "gate": "public — inert" },
    { "name": "tracker_dead_bonus", "value": 0, "weight": 40, "contribution": 0 }
  ],
  "exclusions_applied": [],
  "tracker": "archive.example",
  "tracker_policy": { "min_seed_days": 0, "min_ratio": 0.0, "rare_threshold": 0 }
}
```

The `gate` field on a factor indicates that a gating condition (private/public, any_tracker_alive) zeroed out the contribution before the value/weight math. The UI surfaces these gates to make it obvious *why* a factor is silent.

The UI renders this as a horizontal bar chart with positive/negative contributions side by side, making it visually obvious why a torrent is or isn't a candidate.

## Tuning advice

- **Too aggressive (deletes good seeds)?** Increase `rare_content_threshold`, increase `seeders_low_guard` magnitude.
- **Too conservative (never deletes anything)?** Decrease `upload_velocity_inv` weight; check if HnR window is excessively long; check tracker `min_seed_days` config.
- **Want age to matter more?** Bump `age_days` weight to 0.5 or 1.0.
- **Want to favor older content harshly?** Add an exponential age factor in V2 (`age_days_exp` with default 0, optional).

The V1 release ships with conservative defaults. The `triagearr score --explain` CLI command lets you simulate scoring against your real library without making any change.

## What is *not* in scoring (yet)

- **Watch history from Plex/Tautulli/Jellystat**. This is intentionally Maintainerr's job, and we don't want to duplicate it. V2 may add an *optional* "if Maintainerr has marked this for deletion, +X bonus" factor that uses Maintainerr's collections as a soft signal.
- **File size**. Surprisingly, size doesn't appear in V1 scoring — the Decider already accounts for size when computing "how many torrents to delete to reach target free percent". Adding it to per-torrent scoring would double-count.
- **Quality score / custom format priority**. Could be useful ("keep 1080p, drop 480p first"). Deferred to V2 with a `quality_preference` factor.
- **Per-user "favorite" markers**. Could come from Overseerr request data. Deferred.
