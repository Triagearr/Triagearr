# ADR-0021: Notifications — disk-pressure runs only, best-effort, no SDK

## Status

Accepted — 2026-05-22

## Context

Triagearr deletes media. Once a run completes the operator has no active
signal: the only trace is the dashboard and `audit_log`. For an unattended
daemon whose whole job is destructive, "something just got deleted and here is
what" is the one event worth pushing out.

M7 (ROADMAP) scoped notifications broadly: a Notifier interface, a Telegram
adapter, a generic webhook adapter, per-event templates, and four event types
(`action_executed`, `action_failed`, `pressure_triggered`, `health_degraded`).
That is more surface than the actual need. Notifying on *every* trigger —
including manual HTTP/CLI runs the operator just launched themselves — is
noise. Notifying on "pressure detected" separately from "action executed"
doubles the messages for one logical event.

There are three run triggers (ADR-0015): disk pressure (auto), HTTP, CLI.
Manual triggers are deliberate acts — the operator is already watching. Only an
*autonomous* deletion is genuinely unsolicited information.

## Decision

**A notification is sent only when a disk-pressure run reaches the Actor and
executed at least one candidate.** Everything else stays silent.

- **Hook site**: `triggers.DiskWatcher.fire()`, after `Actor.Execute()`. This
  code path is exclusive to disk pressure, so the "disk-pressure only" scope is
  structural — HTTP/CLI runs never pass through it. The Actor
  (`internal/actor/`) is **not** modified: it stays purely destructive, and the
  notification reads back persisted state (`actions`) as the single source of
  truth.
- **No notification when nothing executed.** An empty plan (pressure detected
  but no eligible candidates) produces no `actions` rows and no message.
- **Partial failures are reported.** A message is sent as long as ≥1 action was
  attempted; successes and failures are both listed in the body. The operator
  needs to see a failed deletion more than a successful one.
- **`notify.Report`** is the provider-agnostic payload: volume, run id, mode,
  free space before/after, target, per-item name/size/status, and the
  "a priori" freed total. Free space before/after comes from real `statfs`
  samples (`pollers.Statfs`, re-sampled post-action) — never inferred from
  media sizes, which would over-count shared hardlinks.
- **`notify.Dispatcher`** fans a Report out to every configured provider
  **best-effort**: a provider failure is logged and swallowed. A broken bot
  token must never abort, delay, or taint a run.
- **Telegram adapter** (`internal/notify/telegram/`) calls the Bot API
  `sendMessage` endpoint with `net/http` only — **no SDK, no new dependency**
  (ADR-0001, stdlib-first). Messages are plain text, so torrent names
  containing Markdown/HTML metacharacters need no escaping.
- **Multi-provider by design, single provider shipped.** The Notifier
  interface and Dispatcher are built for N providers; only Telegram is
  implemented and tested now. Webhook/Discord/etc. are future adapters that
  plug into the same interface.
- **Test action**: `POST /api/v1/notifications/test` delivers a synthetic
  connectivity-check message through the saved provider config and — unlike the
  best-effort run-time dispatch — surfaces provider failures so the operator
  can spot a bad token. The dashboard exposes it as a "Send test notification"
  button, disabled while there are unsaved edits (it tests the loaded config,
  not pending form values).
- **Config & credentials**: a `notifications` section in YAML, also added to
  the runtime-override whitelist (`config.editablePrefixes`) so the Telegram
  bot token and chat id are editable from the dashboard (Settings →
  Notifications). This deliberately departs from the "credentials live in YAML
  only" convention used for *arr/qBit secrets — the operator asked for
  UI-managed notification credentials. `BotToken` is added to
  `config.Redacted()` so it is masked in the effective-config view and logs.

## Consequences

### Positive

- One message per autonomous deletion event, with the detail that matters
  (what was freed, what failed). No noise from manual runs.
- The Actor is untouched — no integration-test obligation triggered
  (CLAUDE.md), and the destructive pipeline keeps a single responsibility.
- No new dependency: the Telegram adapter is ~100 lines of `net/http`.
- The Dispatcher/Notifier seam makes adding a webhook or Discord adapter a
  self-contained change.

### Negative / acknowledged

- The notification path issues two extra reads (`ListActionsByRun`,
  `TorrentNamesByHashes`) and one `statfs` per executed pressure run. Pressure
  runs are infrequent; the cost is negligible.
- The post-action `statfs` re-sample runs synchronously inside the watcher
  tick. It is bounded (one syscall) and pressure runs are rare, so blocking the
  tick briefly is acceptable; not worth a goroutine.
- Storing the bot token in `settings_overrides` (SQLite) puts a credential
  outside the git-audited YAML. Accepted as an explicit operator choice; the
  token is redacted from the effective-config view.
- M7's webhook adapter and per-event templates are **not** delivered here —
  deferred until a second provider or a non-pressure event actually needs them.

## Revisit when

- A second event type is genuinely needed (e.g. `health_degraded`) — then
  generalise `Report` into an event union and reconsider templates.
- A second provider ships — confirm the Notifier interface still fits without a
  per-provider formatting hook.
