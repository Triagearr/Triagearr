# ADR-0020: Scoring is event-driven — re-scored on poller signals, no fixed interval

## Status

Accepted — 2026-05-22

## Context

The scorer (`internal/scorer/`) computes a `DeleteScore` per torrent by reading
data the pollers persist: qbit (`torrents` + `snapshots_raw`), tracker
(`torrent_trackers`), and *arr (`media` / `media_files` / `arr_imports`). It is
therefore a strict *consumer* of poller output.

Until now the scorer ran on its own fixed cadence (`scoring.interval`, default
1h) as just another entry in the poller `Manager`. All pollers — scorer
included — start concurrently and `pollers.TickLoop` fires an immediate tick at
t=0. On a **fresh database** this races: the scorer's first `ScoreAll` runs
before qbit/tracker/*arr have persisted anything, so it scores zero torrents.
The next pass is a full interval later (1h by default). A run does not help —
`decider.Plan` only *reads* the persisted `scores` table, it never scores.

The user-visible symptom: on a first launch, the dashboard's "Top candidates"
stays empty for up to an hour. A daemon restart "fixes" it only because the
SQLite file now already holds torrents from the previous session.

The interval was always a poor fit: scoring has nothing to do on a clock, it
has something to do *when its input data changes*. The clock was a stand-in for
"data probably changed by now".

## Decision

**Scoring is event-driven. There is no fixed scoring interval.**

- `pollers.TickLoop` takes an optional `notify chan<- struct{}`. After every
  successful tick it does a non-blocking send on that channel.
- The three feeding pollers (qbit, tracker, *arr) are wired with a shared
  signal channel (`chan struct{}`, buffered at 1). Disk/maintenance/triggers
  pass `nil` — they do not feed scoring.
- `scorer.Loop` no longer uses `TickLoop`. It blocks on the signal channel;
  on a signal it waits out a short debounce window (default 5s), drains
  coalesced signals, then runs one `ScoreAll` pass. No immediate pass at t=0.
- `scoring.interval` (config) and `scorer.Loop.Interval` are **removed**.

There is **no fallback heartbeat timer**. Justification: the pollers run in an
infinite `TickLoop` until daemon shutdown, so signals keep arriving as long as
the daemon lives. The only "no signal ever" case is every qbit/tracker/*arr
poll failing permanently — in which case no input data is changing and a
re-score would only re-persist identical numbers. Event-driven alone is
sufficient.

## Consequences

### Positive

- The cold-start race is gone: scoring runs as soon as the first feeding poll
  lands (~debounce + poll duration), not up to an hour later. Fixes dev and
  prod identically; no dependence on a hand-tuned interval.
- Scores are always fresh — every poll cycle drives a re-score, so the dashboard
  and the Decider see current data instead of up-to-1h-stale data.
- One fewer config knob; the scorer's cadence is derived, not configured.

### Negative / acknowledged

- A `ScoreAll` runs once per (debounced) poll cycle instead of once per hour.
  In prod the most frequent feeding poller is qbit (default 30m), so `ScoreAll`
  runs ≈every 30m — acceptable for homelab library sizes. The 5s debounce
  collapses the start-up burst (three pollers ticking near-simultaneously) into
  a single pass.
- A pass may run on partially-loaded data at startup (qbit done, *arr still
  fanning out per-file calls). This self-corrects: the *arr poller's completion
  signals another pass.
- If `ScoreAll` cost ever becomes a concern for very large libraries, a
  minimum-interval floor between passes is the natural knob to add. Not
  implemented now (YAGNI).

## Revisit when

- `ScoreAll` duration becomes significant relative to the poll cadence — then
  add a min-interval floor or make scoring incremental (score only changed
  hashes).
