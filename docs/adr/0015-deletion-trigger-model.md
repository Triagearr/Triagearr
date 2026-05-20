# ADR-0015: Deletion is gated on disk pressure or explicit human trigger — never on a schedule

## Status

Accepted — 2026-05-20

## Context

M4 introduced the Decider and three ways to fire a run: the disk-pressure watcher, `POST /api/v1/runs`, and `triagearr run --now`. All three produce a dry-run plan persisted to the `runs` table. M5 will turn plans into actual deletions through the Actor.

Before wiring the Actor, the question is: **which triggers are allowed to lead to a real deletion?** The roadmap left this ambiguous. Two models were on the table:

- **Model A — Disk pressure only**: only the watcher can cause an automatic deletion. Manual CLI/API triggers may also delete, but only when the human explicitly opts in. A scheduled (cron) trigger never deletes, even in `mode: live`.
- **Model B — Any run can delete in live mode**: a `mode: live` daemon deletes whenever a run fires, regardless of trigger. Cron becomes a nightly housekeeping cadence.

Triagearr's pitch (`README.md`) is a "disk-pressure-aware media reaper" — pressure is the central signal, not a clock. Model B would normalize a "delete because it's 3am" behaviour that is closer to what Maintainerr or Janitorr already do, and that the project explicitly stays away from (`CLAUDE.md` § Out of scope).

## Decision

**Automated deletion is gated on disk pressure. Human triggers may delete, but only with an explicit opt-in. Scheduled (cron) triggers never delete.**

Concretely, M5's Actor is allowed to execute a run when:

| Trigger | Auto-execute in `mode: live`? | Requires explicit opt-in? |
|---|---|---|
| `triagearr.RunTriggerDiskPressure` (watcher) | **Yes** | No — `mode: live` + per-instance `act: true` already gate this |
| `triagearr.RunTriggerHTTP` (`POST /api/v1/runs`) | No | Yes — request body must carry `"mode":"live"` (or equivalent flag); rejected otherwise |
| `triagearr.RunTriggerCLI` (`triagearr run`) | No | Yes — `--live` flag required; the flag is rejected today and unlocked in M5 |
| Any future scheduled trigger (cron) | **No, ever** | n/a — cron runs stay dry-run even in `mode: live` |

A run row carries `triggered_by` since M4. The Actor consults this column before acting.

## Consequences

### Positive

- The product identity stays sharp: Triagearr deletes when the disk is full, not when the clock ticks. Aligns with the pitch and with the M3 safety contract (HnR window, `act: false` defaults).
- M5 design is simplified: a single check (`triggered_by != "cron"` ∧ explicit-live-or-pressure) gates the Actor.
- A future cron (M6+ for dashboard data) can be added without re-opening this debate. Its rows are observational.
- The disk-pressure watcher remains the only "set-and-forget" automation, which is also the only one with a clear stopping criterion (`target_free_percent`) — so it cannot spiral into runaway deletion.

### Negative (acknowledged)

- Operators wanting a scheduled cleanup ("delete every Sunday at 3am") will not get it from Triagearr. They keep their existing tools (Maintainerr for watch-history-driven cleanup, Cleanuparr for malware) — which is the documented scope boundary anyway.
- The Actor's allow-list is slightly more code than "if mode==live, execute" — but it is a thin guard, not a real complexity cost.

## Implementation notes for M5

- `Actor.Execute(run)` must reject runs where `triggered_by == "cron"` (or any future scheduled value) regardless of mode.
- `POST /api/v1/runs` accepts a `mode` field in the request body. Without it, the run is forced to dry-run even if the daemon is `mode: live`.
- `triagearr run --live` is currently rejected by `runAction` — M5 unlocks it but keeps the explicit flag mandatory.
- The `mode` column already exists in `runs` (M4). M5 writes `live` only when the trigger + flags allow it.

## Revisit when

- A concrete user request emerges for scheduled-but-bounded automatic deletion (e.g. "delete dead-tracker private torrents on a schedule") that disk pressure cannot express. The right answer then is probably a new factor or a new bounded trigger, not flipping cron's gate.
