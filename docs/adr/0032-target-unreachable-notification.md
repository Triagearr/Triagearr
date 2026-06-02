# ADR-0032: Target-unreachable notification + notification event generalisation

## Status

Proposed — 2026-06-02

## Context

A disk-pressure run can complete `no_more_candidates` (see
`internal/decider/decider.go`): the Decider walked every eligible candidate and
still could not close the gap to `target_free_percent`. Today this surfaces only
on the dashboard gauge (`comp_gauge_shortfall`). For an unattended daemon that
is the one condition the operator most needs pushed to them — "I am under
pressure and even deleting everything I'm allowed to delete will not reach
target." It is silent precisely when it matters.

ADR-0021 deliberately scoped notifications to a single event ("a disk-pressure
run reached the Actor and executed ≥1 candidate") and recorded a "revisit when"
trigger: *a second event type is genuinely needed (e.g. `health_degraded`) —
then generalise `Report` into an event union*. This is that moment.

Two design pressures:

1. **De-duplication.** `DiskWatcher.fire()` runs whenever free% is below
   threshold, and re-fires every `ReFireGrace` (default 1h). A naive alert would
   fire hourly. The operator asked for one reminder per day, configurable.
2. **Reload-survival.** The daemon rebuilds the watcher on every config reload
   (SIGHUP → `buildEngine`). Any in-memory "last alerted at" resets on every
   Settings save and re-spams. Throttle state must be persisted.

## Decision

**Add a `target_unreachable` notification, emitted from `DiskWatcher.fire()`
when `plan.StopReason == StopNoMoreCandidates`, throttled to one message per
configurable interval, with throttle state persisted in SQLite.**

### Event scope

- **Always active, not independently toggleable.** The alert is intrinsic to
  notifications: it rides on whatever providers are configured. If ≥1 provider
  is set up it is sent; if none are, nothing is (same gate as run reports —
  `Dispatcher.Empty()`). There is no per-alert `enabled` flag — only its
  reminder cadence is configurable.
- Fires in **both dry-run and live** modes. The condition is real regardless of
  whether the daemon acts; in dry-run it is *more* important (nothing is being
  freed and target is unreachable even if live were enabled). This is a
  conscious extension of ADR-0021's "live-only" rule: that rule governs the
  *deletion* event (don't announce what the operator triggered); a structural
  shortfall is a *condition*, not an action.
- Only fires while under the **pressure threshold** (the watcher's existing
  gate). No alerts when comfortably above threshold but below target — that band
  is not pressure.
- **No "all-clear" message.** When the condition clears (`target_reached`, or
  free% climbs back above threshold) the throttle row is reset so the *next*
  episode alerts immediately rather than being muted by the daily window. A
  resolution message is deferred (revisit if oscillation proves confusing).

### Notification shape — provider becomes a text sink (Option C)

The existing `Notifier` provider (Telegram) never reads `Report`'s structure: it
calls `notify.FormatText(report)` and POSTs the resulting plain text. So the
provider boundary is generalised to carry **preformatted text**, and all
formatting moves into the `notify` package:

- `Notifier.Send(ctx, notify.Message)` where `Message{Kind, Text}` — providers
  are dumb text sinks. Adding event #3 never touches a provider again.
- `notify.FormatRunReport(Report) Message` and `notify.FormatAlert(Alert)
  Message` own formatting per event. `Report` keeps its current shape (the
  "executed run" payload); `Alert` is the new advisory payload
  (volume, free%/target, need/reclaimable bytes, candidate count, mode).
- `Dispatcher` gains `DispatchAlert(ctx, Alert)` alongside `Dispatch` (run
  report); both format → `Message` → fan out best-effort. `SendTest` formats a
  test `Message`.

Rejected alternatives:
- **D — `Report.Kind` + extra fields, switch in `FormatText`.** Least code, but
  overloads the "a run executed" payload with a non-deletion advisory event —
  the special-case-on-shared-infra smell.
- **B — fully structured per-event templates with rich payloads at the provider
  boundary.** Closest to ADR-0021's wording, but providers only ever emit plain
  text today, so structured payloads at the boundary benefit nobody. Over-built;
  revisit when a provider needs rich output (Discord embeds).

### Throttle persistence

New table `notification_state(event_key TEXT PRIMARY KEY, last_sent_at
TIMESTAMP NOT NULL)`, added **directly to `0001_init.sql`** — the project is
pre-1.0 alpha with a disposable DB (no back-compat, schema changes wipe), so a
separate versioned migration is unwarranted. `event_key` is
`target_unreachable:<volume_name>` (single volume today per ADR-0024, keyed for
future). Store methods: `GetNotificationState`, `MarkNotificationSent`,
`ClearNotificationState`. The watcher consults `last_sent_at` before emitting
and writes it after a successful dispatch; clears it when the condition resolves.

### Config

```yaml
notifications:
  telegram: { ... }            # unchanged
  target_unreachable:
    reminder_interval: 24h     # 0 = alert once, no reminders; clamped ≥ 1h
```

No `enabled` flag — the alert is always active when a provider is configured
(see Event scope). Only `reminder_interval` is tunable. `notifications.*` is
already in `config.editablePrefixes`, so the field is UI-editable (Settings →
Notifications) with no whitelist change.

## Consequences

### Positive

- The one condition an unattended daemon can't self-heal now reaches the
  operator, throttled to one message/day by default.
- The provider/`notify` seam is generalised once; future events (`health_degraded`,
  webhook payloads) are self-contained additions.
- Throttle state survives config reloads and restarts.
- The Actor is untouched — no integration-test obligation (CLAUDE.md).

### Negative / acknowledged

- The `Notifier` interface changes signature (`Send(Report)` →
  `Send(Message)`). Painless under the project's alpha/no-back-compat rule;
  touches only the Telegram client + its tests + the Dispatcher.
- One extra SQLite read per pressure fire (throttle lookup) and one write when an
  alert is emitted. Pressure fires are infrequent; negligible.
- A dry-run daemon now emits notifications, departing from ADR-0021's live-only
  framing. Justified above; the throttle bounds the noise.

## Revisit when

- A resolution/all-clear message is wanted (oscillation around threshold proves
  the silent-reset confusing).
- A provider needs structured (non-text) output — then reconsider Option B for
  that provider behind the same `Message` seam.
