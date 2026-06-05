# ADR-0031: one run-lock guards live runs across every trigger

## Status

Accepted — 2026-06-02

## Context

A destructive "run" — the Decider plan that `Actor.Execute` deletes from *arr
then qBit (ADR-0003) — can be started from three places, governed by the trigger
model in ADR-0015:

- **HTTP** `POST /api/v1/runs`,
- the **disk-pressure watcher** (`internal/triggers`), firing automatically in
  `mode: live`,
- the **CLI** `triagearr run --live`, a separate OS process.

The HTTP handler already serialized *itself* with a capacity-1 channel
(`Server.liveRun`): a second concurrent `POST` got `409 Conflict` instead of
racing the pipeline. But the guard stopped there. The disk-pressure watcher and
the HTTP server share the **same `*actor.Actor` instance** (wired in
`cmd/triagearr/daemon.go`), and the watcher called `Actor.Execute` directly
without acquiring anything. So a pressure-triggered reap and an HTTP-triggered
reap could run the destructive pipeline **at the same time** — double-deleting,
racing the T3.5 nlink re-check, and writing competing `actions` rows. The CLI,
being a different process, could collide with the daemon regardless of any
in-memory guard.

Two runs deleting concurrently is exactly the failure the `liveRun` semaphore was
meant to prevent; it just wasn't shared widely enough.

## Decision

One **single-run lock** admits at most one live run at a time across every
trigger. It is created once in the long-lived `serveAction` scope and injected
into both the HTTP server and the (reload-rebuilt) disk-pressure watcher; the CLI
opens its own handle to the same backing file. It lives in a new leaf package
`internal/runlock` (stdlib only — no new dependency, so no `docs/STACK.md`
entry).

The lock layers two mechanisms behind one non-blocking `TryAcquire`/`Release`:

- **A capacity-1 channel** serializes the two *in-process* triggers (HTTP handler
  goroutine vs. disk-pressure watcher goroutine), which share one `Lock`
  instance.
- **A `flock(2)` advisory lock** on `${data_dir}/run.lock` serializes the daemon
  against the separate `triagearr run --live` process. The kernel drops a
  `flock` when the holding process dies, so a crashed run never leaves a stale
  lock — the property a DB-row lock would have to reimplement with heartbeats and
  staleness windows. `flock` is taken only for the duration of a run, so daemon
  and CLI are mutually exclusive only while actually reaping, not for the
  daemon's whole lifetime.

Acquisition is **fail-fast everywhere**, never queued:

- HTTP → `409 Conflict` (unchanged behaviour),
- disk pressure → log `skipping pressure run, a live run is already in progress`
  and skip; the watcher does **not** advance `lastFire`, so the next tick retries
  promptly once the slot frees rather than waiting out the re-fire grace,
- CLI → exit with `a live run is already in progress (daemon or another CLI)`.

Each trigger claims the lock **before** planning/persisting a live run, so a
contended trigger writes no orphan run record that would never execute.

`internal/actor` is unchanged: the guard sits at the trigger/orchestration layer,
where "which entrypoints may start a run" is decided, not inside the destructive
executor.

## Consequences

- Live runs can no longer overlap: HTTP-vs-pressure within the daemon, and
  daemon-vs-CLI across processes, are both excluded.
- The pre-existing `Server.liveRun` channel is replaced by the shared
  `runlock.Lock`; `server.New` falls back to a private memory-only lock when none
  is injected, so tests and standalone use are unaffected.
- Cross-process exclusion is **Linux-only**, matching the release targets
  (`.goreleaser.yaml` ships `linux/{amd64,arm64}`). A `//go:build !unix` no-op
  keeps cross-compilation and `go vet` green; on a hypothetical non-unix build the
  in-process channel still guards the daemon, only the CLI fence is absent.
- The lock is intentionally non-reentrant and non-blocking. Queued/blocking
  acquisition is out of scope — a contender fails fast and retries (pressure
  watcher) or is told to try later (HTTP/CLI).
