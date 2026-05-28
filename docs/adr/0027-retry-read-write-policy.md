# ADR-0027: Retry is a shared primitive; reads retry in the client, writes in the Actor

## Status

Accepted — 2026-05-29

## Context

Retry started life as `withRetry`, an unexported helper inside `internal/actor`.
It gated on `triagearr.ErrTransient` (3 attempts, 500ms base, exponential to a
4s cap, jitter) and wrapped the Actor's destructive calls — the per-file *arr
`DeleteFile` fan-out and the whole-torrent qBit `Delete`. Two problems surfaced:

1. **Reads had no retry at all.** A Sonarr or qBit instance answering `503`
   during a poll tick sank the entire tick. Pollers have no retry layer of their
   own, so a one-second blip on an upstream looked like a hard failure.

2. **The Actor's read retry was a silent no-op.** `actor.checkNlink` wrapped the
   qBit `TorrentFiles` read in `withRetry`, but `qbit.getJSON` never wrapped its
   5xx responses in `ErrTransient` — so the gate never matched and nothing was
   ever retried. The wrapper gave a false sense of resilience.

Separately, the HTTP timeout was a single `http.Client{Timeout: 30s}` covering
header **and** body. A *arr streaming a large `/series` or `/history` payload
slowly could expire mid-`json.Decode`, turning a slow-but-healthy upstream into
a failure.

## Decision

**Retry is a shared primitive.** `withRetry` moves to `internal/x/retry` with an
options API — `Do(ctx, op, opts...)` plus `WithMaxAttempts/WithBaseDelay/`
`WithMaxDelay/WithSleep`. Defaults are unchanged (3 / 500ms / 4s), so the Actor's
behaviour is preserved bit-for-bit. The gate stays `errors.Is(err, ErrTransient)`
and the `sleep` hook stays injectable for tests.

**Clients retry their own reads; the Actor retries its own writes.**

- Reads (`arrclient.Get`, `qbit.getJSON`) self-retry inside the client. They
  wrap the retryable conditions in `ErrTransient` themselves.
- Writes (`arrclient.DeleteFile`, `qbit.Delete`) do **not** self-retry — the
  Actor owns write retries via `retry.Do`, because only the Actor knows the
  surrounding deletion order (ADR-0003) and audit semantics. They keep wrapping
  their failures in `ErrTransient` so the Actor's gate matches.
- Consequence: the no-op `withRetry` around `TorrentFiles` is removed. The Actor
  now calls `TorrentFiles` exactly once and trusts the qBit client to have
  already absorbed transient blips. The `TorrentClient` interface documents this
  contract for any future client (Deluge, etc.).

**Reads and writes use different retryable-status sets — deliberately.**

- Reads retry `429, 502, 503, 504` + transport errors. They do **not** retry
  `500/501`: a malformed request or a server-side bug on a GET won't fix itself,
  and re-hammering it just delays surfacing the real error.
- Writes retry **all** 5xx. A `DELETE` is idempotent from Triagearr's side
  (deleting an already-deleted file is harmless), so absorbing any 5xx is the
  safer bet for the destructive path.

**Per-request timeouts replace the end-to-end one.** Each attempt derives a
fresh `context.WithTimeout(ctx, 25s)` from the caller's `ctx`; `http.Client`
keeps a 60s end-to-end backstop. The deadline lives inside the retried closure,
so every attempt gets its own budget, and the timeout is per-request rather than
spanning a slow body read.

## Consequences

- A transient upstream hiccup no longer sinks a poll tick or a deletion step.
- `internal/x/retry` is reusable by any future caller; no new external dependency
  (ADR-0001 honoured — no STACK.md entry).
- qBit's `403` (session expired) is intentionally **not** retried in-loop: a
  same-loop retry would re-fail identically since `ensureLogin` already ran for
  that call. It resets the session flag so the next call re-authenticates.
- A slow streaming decode is bounded at 25s per attempt instead of a single 30s
  for the whole transaction.

## Follow-ups

- `docs/CONFIGURATION.md` documents `action.retry: {max_attempts, backoff}`, but
  it is not yet wired. `retry.Do`'s options now make this a small change (pass
  `WithMaxAttempts`/`WithBaseDelay` from config at the Actor's call sites). The
  documented `backoff: 30s` would need reconciling with the current 500ms base
  when that wiring lands. The budget stays hardcoded for now — no config surface
  is added in this ADR.
