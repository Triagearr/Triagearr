# ADR-0033: Multi-channel notifications — Apprise fan-out, native webhook, typed events, severity routing

## Status

Accepted — 2026-06-02

## Context

ADR-0021 shipped notifications as a single event (a disk-pressure run reached
the Actor), best-effort fan-out, plain text, Telegram-only via a hand-written
~100-line `net/http` adapter. ADR-0032 added a second event
(`target_unreachable`) and generalised the provider boundary to a text sink
(`Message{Kind, Text}`), explicitly deferring rich output and extra providers
"until a second provider or a non-pressure event actually needs them."

That moment arrived on three fronts at once:

1. **More events.** Operators want to know about *failed/partial* runs and
   *connection-health* transitions (a configured *arr/qBit becoming unreachable),
   not just "a run executed." These are different severities that should route
   differently.
2. **More channels.** Telegram alone is limiting for a homelab audience —
   Discord, ntfy, email and Slack are all common. Hand-writing a `net/http`
   adapter per channel (the ADR-0021 bar) does not scale to N channels.
3. **Rich vs text.** ADR-0032's deferred "Option B" (structured payloads) only
   pays off for a machine consumer (automation/integration); for human channels
   the ceiling is title + body + severity.

ADR-0001 is stdlib-first: any new direct dependency needs an ADR + STACK.md
entry. So the dependency question had to be settled deliberately, not by reflex.

## Decision

**Adopt `github.com/unraid/apprise-go` for the human text channels, keep one
hand-written native webhook for structured JSON, model events as a typed union
with a fixed per-kind severity, and route per provider by a severity floor plus
a mute list.**

### Dependency: Apprise over shoutrrr over hand-writing each adapter

- **Hand-write every channel (status quo, ADR-0021 bar).** Rejected: ~100 lines
  × Telegram/Discord/ntfy/SMTP/Slack, each with its own auth quirks and URL
  formats, is unbounded maintenance for a non-core feature.
- **`containrrr/shoutrrr`.** The obvious Go pick, but its last release is
  **v0.8.0 (Aug 2023)** — ~2 years stale — and its config seam is opaque service
  URLs with no semantic notification type.
- **`unraid/apprise-go` (chosen).** A maintained (v0.2.x, 2026) 1:1 Go port of
  the de-facto-standard Apprise URL schema, **+1 direct dependency** (its tree is
  `gomarkdown/markdown` + `golang.org/x/crypto` + `golang.org/x/net`, the latter
  two already in our graph), no heavy SDKs. It exposes a semantic
  `WithNotifyType(info|success|warning|failure)` that maps directly onto our
  severity, so each service renders it natively (ntfy priority, Discord embed
  colour) without us hand-crafting per-service payloads. Maintained by Unraid —
  the same homelab audience as Triagearr. This overrides ADR-0001 for a scoped,
  high-leverage gain; STACK.md records the justification.

### Native webhook stays hand-written

shoutrrr/Apprise are *text* sinks: they cannot emit arbitrary structured JSON.
The one genuinely "rich" capability worth owning is a machine-readable payload
for automation (n8n, Home Assistant, scripts). That is ~60 lines of `net/http`
serialising the typed event, with optional HMAC-SHA256 body signing — the only
provider that reads the event's typed fields rather than its text. So the
deferred "Option B" is realised exactly where it pays off, and nowhere else.

### Typed event seam

The text-only `Message{Kind, Text}` (ADR-0032) grows into a typed `Event`:

```go
type Event struct {
    Kind     EventKind     // dotted: run.executed/partial/failed,
    Severity Severity      //         disk.target_unreachable,
    Title    string        //         health.degraded/recovered, test
    Text     string        // universal plain-text fallback (text sinks)
    Run    *Report         // exactly one payload pointer set, matching Kind
    Alert  *Alert
    Health *HealthEvent
}
```

`Notifier.Send(ctx, Event)` — Apprise providers read `Text` + `Severity`; the
webhook reads the typed payload. Severity is **intrinsic to the kind** (a fixed
table, not operator-tunable): `run.executed`=info, `run.partial`=warning,
`run.failed`=error, `disk.target_unreachable`=warning, `health.degraded`=error,
`health.recovered`=info, `test`=info.

### Severity-threshold routing (not an N×M matrix)

Each provider carries `Routing{MinSeverity, Mute[]EventKind}`: it receives every
event at or above its floor, minus muted kinds. Default floor is info (everything
on), default-on/opt-out. The `Dispatcher` applies `Routing.Allows(ev)` before
`Send`. Rejected the full per-event × per-provider boolean matrix: N×M config and
a heavy UI grid for control nobody asked for.

### Run outcome

`RunOutcome(Report)` classifies on succeeded-vs-hard-failed: 0 items → emit
nothing; 0 succeeded → `run.failed`; some hard-failed → `run.partial`; else
`run.executed`. `ActionSkippedCrossSeed` is benign (the torrent was intentionally
left because siblings still hardlink it) and never demotes the outcome.

### Health events

A new `HealthWatcher` poller (`internal/triggers/health_watcher.go`) reads the
health the arr/torrent pollers already persist (`arr_instances`,
`torrent_client_instances`) and emits `health.degraded`/`health.recovered` on
transitions. It reuses `notification_state` (ADR-0032) keyed
`health:<kind>:<component>` for both transition-edge detection and reminder
de-dup — degraded marks the row, recovered clears it. A single decoupled
emission point with no change to the existing pollers; a healthy-from-start
component never fires.

### Recent deliveries: in-memory ring, not a DB log

The dashboard "recent deliveries" panel is backed by a fixed-size (100) in-memory
ring on the `Dispatcher`, exposed at `GET /api/v1/notifications/deliveries`. It
does not survive a restart. Notifications are advisory (ADR-0021); a durable
delivery log is a table + migration + retention concern for marginal pre-1.0
value. Deferred (revisit-when below).

### Config

```yaml
notifications:
  telegram: { enabled, bot_token, chat_id, min_severity, mute: [] }
  discord:  { enabled, webhook_url, min_severity, mute: [] }
  ntfy:     { enabled, server, topic, username, password, min_severity, mute: [] }
  email:    { enabled, host, port, username, password, from, to: [], use_starttls, min_severity, mute: [] }
  slack:    { enabled, webhook_url, min_severity, mute: [] }
  webhook:  { enabled, url, secret, min_severity, mute: [] }       # native JSON
  target_unreachable: { reminder_interval: 24h }                   # ADR-0032, unchanged
```

`min_severity`/`mute` embed via koanf `,squash` into each provider. The Apprise
service URL is built server-side at wiring time (`internal/notify/apprisex`) from
these structured fields and **never** logged or returned to the UI — secrets are
URL-escaped via `net/url`. `notifications.*` is already in `editablePrefixes`, so
everything is UI-editable with no whitelist change.

## Consequences

### Positive

- Five human channels + a structured webhook, with one maintained dependency
  instead of six hand-written adapters.
- Severity flows natively into each channel's rendering via Apprise's NotifyType.
- New events (`run.failed`, `health.*`) are self-contained; adding another never
  touches a provider.
- Routing is one floor + a mute list per provider — small config, legible UI.
- The Actor is untouched (CLAUDE.md): no integration-test obligation.

### Negative / acknowledged

- **+1 direct dependency** overriding ADR-0001. Direct count 13 → 14, far under
  the <30 goal; transitive tree is small (`gomarkdown/markdown` + `x/crypto` +
  `x/net`). apprise-go is young (v0.2.x) — but the Apprise URL schema is the
  industry standard and stable, and the project is alpha (pin + bump).
- The `Notifier` interface changes again (`Send(Message)` → `Send(Event)`, plus
  `Routing()`). Painless under alpha/no-back-compat; the native Telegram adapter
  (`internal/notify/telegram`) is deleted — Telegram now rides Apprise.
- Secrets now flow into URLs internally; escaping is a correctness/security
  requirement, covered by `apprisex` URL-builder tests that round-trip through
  Apprise's own parser.
- Deliveries are not persisted across restarts.

## Revisit when

- Operators need delivery **history** (promote the ring buffer to a
  `notification_deliveries` table with retention).
- A provider needs **interactive** controls (Stop/Ack from the notification) —
  out of scope here; that needs an inbound callback receiver + auth, and Apprise
  would no longer suffice.
- A channel Apprise does not support is required (write a native adapter behind
  the same `Notifier` seam, as the webhook already is).
