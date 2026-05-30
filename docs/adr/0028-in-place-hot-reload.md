# ADR-0028: in-place hot reload — the HTTP server survives config changes

## Status

Accepted — 2026-05-30

## Context

Every config mutation that lands in the database — a `settings_overrides` write
(ADR-0020), an *arr connection edit (ADR-0022), a torrent-client connection edit
(ADR-0025) — must take effect without a manual restart. Until now the daemon
achieved this with a **self-SIGHUP restart**: the HTTP handler persisted the
change, called a `selfReload()` that sent `SIGHUP` to its own PID, and returned
`202 Accepted` immediately. The `serveAction` boot loop caught the signal,
cancelled the run context, and **tore down and rebuilt the entire daemon — the
`http.Server` included** — from the fresh config.

This had two costs:

- **The listener bounced on every save.** The in-flight `PUT` survived (it ran on
  the old server about to be replaced), but the rebuild raced the response: the
  socket closed and reopened underneath the SPA. The window was small but real.
- **The 202 was a lie the frontend had to paper over.** Because the handler
  returned before the reload was live, the React Query layer could not invalidate
  immediately — a refetch would have hit the *old* config. So `web/src/api/hooks.ts`
  carried a `scheduleReloadInvalidate` helper with a **hardcoded 1500 ms delay**
  before invalidating `settings` / `config` / connection queries. The number was
  arbitrary: too short and the UI showed stale values, too long and saves felt
  sluggish. It was a guess at "how long does a daemon rebuild take", baked into
  the client.

The restart was overkill. Of everything the daemon builds, only the
config-derived subsystems actually change when config changes. The listener, the
store, the API key, the rate limiters, the auth state — none of them depend on
the editable config surface (the `settings_overrides` whitelist is `scoring`,
`polling`, `volume.disk_pressure`, `notifications`; connections live in their own
tables). Rebuilding the listener to pick up a scoring-weight change is throwing
away the house to change a lightbulb.

## Decision

**The `http.Server` is long-lived. A config reload rebuilds only the
config-derived subsystems and swaps them in atomically, in place. The listener
never restarts.**

- **`server.Engine`** — a new immutable value bundling everything the HTTP
  handlers read that derives from config: `Config`, `Scorer`, `Decider`, `Actor`,
  `Volume`, `DaemonLive`, `Notifier`. `server.Options` keeps only the long-lived
  dependencies (`Store`, `Linker`, `Bind`, `APIKey`, rate limits, `ConfigPath`,
  build `Version`, and the reload hooks). `server.New(opts, engine)` takes the
  initial engine; the `Server` holds it in an `atomic.Pointer[Engine]`.
- **`Server.SwapEngine(*Engine)`** replaces the pointer. Handlers read the
  current engine via `s.engine()`. A handler that touches several engine fields
  (e.g. `handlePostRun` reading `Volume`, `Decider`, `DaemonLive`, `Actor`)
  snapshots the pointer once at the top so a concurrent swap cannot splice old
  and new values into one response. A detached live run keeps the `Actor` it was
  armed with — the engine is immutable, and the Actor runs on the daemon-lifetime
  `baseCtx`, not the reloadable poller context.
- **`buildEngine(ctx, store, cfg)`** (in `cmd/triagearr`) constructs the engine
  plus its poller set and runs the ADR-0023 preflight. Extracted from the old
  `runDaemon`. Returns an error (including preflight failure) without starting
  anything, so a bad reload is recoverable.
- **Reload controller** — `serveAction` owns the engine/poller lifecycle. Pollers
  run under an `engineCtx` separate from the listener's context. On a reload
  request it: loads the fresh config, calls `buildEngine` (validate + preflight),
  cancels `engineCtx` and **drains the old pollers**, starts the new poller set,
  then `SwapEngine`. Drain-then-start is deliberate: two disk-pressure
  `DiskWatcher`s must never run concurrently, or a live deletion could double-fire
  during the swap.
- **Synchronous reload, honest status.** The HTTP `Reload` hook funnels a request
  to the controller and **blocks until the swap is live**, returning the
  build/validation error. `handlePutSettings` / `handleDeleteSetting` and the
  connection CRUD now return **`200 OK` only after the reload succeeds**, or
  `500` (engine unchanged) if it fails. The `202 Accepted` is gone.
- **Frontend invalidates immediately.** `scheduleReloadInvalidate` and its 1500 ms
  timer are deleted. `useUpdateSettings` and the connection hooks invalidate their
  queries in `onSuccess` — a 200 means the new config is already live.
- **SIGHUP is preserved** for manual YAML edits (`kill -HUP`): it enters the same
  reload path, fire-and-forget. It no longer rebuilds the listener.

## Consequences

### Positive

- The listener never bounces on a save. No transient network error in the SPA.
- The save round-trip carries the real outcome: a 200 means the value is live and
  the UI can refetch at once; a 500 with the error message means it didn't take.
  No magic number, no stale-vs-sluggish tuning.
- A failed reload (invalid config, failed preflight) leaves the daemon on the last
  good engine instead of crashing the rebuild loop.

### Negative / acknowledged

- **Infrastructure knobs are now restart-only.** `http.bind`, `http.rate_limits`,
  and the API key are read once when the listener is built. A YAML edit to those
  plus a SIGHUP no longer rebinds — it needs a real process restart. This is
  acceptable: those keys are YAML-only (not in the editable whitelist),
  git-audited, and rarely changed. `storage.sqlite_path` was already restart-only
  (the store is opened once before the reload loop). Documented here and in
  `ARCHITECTURE.md`.
- **A connection reload runs after persistence.** The CRUD handlers persist the
  row, then reload. If `buildEngine` then fails, the row is already in the table
  but the daemon stays on the old engine and the handler returns 500. The next
  successful reload (or restart) picks it up. The settings endpoints avoid this
  for schema errors by validating via `ReloadValidate` before persisting; the
  connection inputs are validated at the handler but a deeper build failure is
  still possible. Rare, and surfaced to the operator as an error rather than a
  false "Saved".

## Revisit when

- Live rebinding of `http.bind` becomes a real need — would require making the
  listener itself reloadable (close + relisten), a larger change deferred here.
- A second concurrent-safety hazard appears in the poller set — the
  drain-then-start ordering is the current guard; a more granular per-poller swap
  could reduce reload latency if drain time ever becomes noticeable.
