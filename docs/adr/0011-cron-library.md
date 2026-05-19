# ADR-0011: Use robfig/cron/v3 for the downsampler schedule

## Status

Accepted — 2026-05-19

## Context

M2 introduces the daily snapshot downsampler (`docs/ROADMAP.md` § M2 storage maintenance). The job needs to run at a user-configurable time of day — default `03:00 UTC` — to amortise its work outside the homelab's usual evening Plex peak.

The existing poller machinery (`internal/pollers`) is interval-based (`time.Ticker` wrapped in `tickLoop`). It cannot express "fire at 03:00 every day"; the closest approximation, `Ticker{24h}` started at boot, drifts every restart and fires at whatever wall-clock minute the daemon happened to start.

Three options:

1. **`time.Ticker` with a 24 h period.** Loses time-of-day predictability; defeats the purpose of choosing a quiet hour.
2. **Hand-rolled 5-field cron parser in stdlib.** ~150-300 LOC + tests for a problem already solved. The cron syntax has enough corners (step values `*/5`, ranges `1-5`, day-of-week vs day-of-month conflict semantics) that doing it ourselves is real work to no benefit. Conflicts with the spirit of ADR-0001 (stdlib first) by introducing fragile, low-leverage code we own.
3. **External cron library.** `github.com/robfig/cron/v3` is the dominant Go cron implementation (used by Caddy, k6, Loki, OpenFaaS). v3 is stable since 2019; the API is small (`cron.New()`, `c.AddFunc(spec, fn)`, `c.Start()`, `c.Stop()`).

## Decision

Adopt `github.com/robfig/cron/v3` (v3.0.x) as a direct dependency.

Scope of use is intentionally narrow:

- `internal/pollers/downsampler.go` schedules the downsampler/retention/VACUUM job at `polling.downsample_cron`.
- The poller manager remains unchanged; the cron scheduler is started alongside the interval pollers and stopped together.
- No other code path uses cron syntax. If a second cron-driven job appears (e.g., M4 disk-pressure scan), it joins the same scheduler.

The default schedule is `"0 3 * * *"` (03:00 UTC daily).

## Consequences

**Easier:**
- Correct cron parsing (ranges, steps, weekday) for free.
- One-line schedule changes from config — the user writes the cron expression they already know from Linux cron.
- Test surface is small: we test our handler, not the scheduling.

**Harder:**
- One more direct dep (`go.mod` count moves toward the 30-dep budget in `docs/STACK.md`).
- robfig/cron/v3 is in maintenance mode (no new features since 2022, last patch 2024). For our use — parse a 5-field expression and call a func — feature stagnation is fine. If the project goes truly unmaintained, replacement is mechanical (the API is trivial).

**Traded away:**
- Pure stdlib purity per ADR-0001. The tradeoff is documented in [ADR-0001](0001-pure-go-stdlib-first.md) itself: external deps are acceptable when they replace meaningfully fragile in-house code.

## Re-evaluation triggers

- Project formally archived on GitHub
- Security advisory unresolved for >90 days
- A stdlib cron primitive lands (no Go proposal currently in flight)

## References

- Library: https://github.com/robfig/cron
- Pin: v3.0.x (latest v3.0.1 as of 2026-05)
