# ADR-0029: dry-run/live mode is UI-managed

## Status

Accepted — 2026-05-31

## Context

The global `mode` (`dry-run` | `live`) is the master switch for destructive
behaviour: in `dry-run` the daemon plans deletions but never executes them; in
`live` an armed run can actually delete. Until now `mode` was deliberately kept
out of the `settings_overrides` editable whitelist (ADR-0020) — the comment in
`internal/config/overrides.go` listed it among the "YAML-only, git-audited"
keys, on the reasoning that flipping the master safety switch should leave a
commit behind.

In practice this is operational friction without much payoff. The common
workflow is "switch to live for a one-off cleanup, then back to dry-run" — and
forcing that through a YAML edit plus a reload (SIGHUP or restart) every time is
clumsy enough that operators are tempted to just leave the daemon in `live`,
which is the opposite of what the safety story wants. Meanwhile every other
operator-facing knob (scoring, polling, disk-pressure thresholds, notification
credentials) is already editable from the dashboard and hot-reloaded in place
(ADR-0028). `mode` is the conspicuous exception, and it's the one people most
want to toggle.

## Decision

**`mode` joins the `settings_overrides` editable whitelist and becomes editable
from the dashboard, like any other UI-managed setting.**

- `"mode"` is added to `editablePrefixes`; `IsEditableKey("mode")` is now true.
  A `mode` override is a top-level scalar JSON string (`"live"` / `"dry-run"`)
  and rides the existing PUT `/api/v1/settings` → `mergeOverrides` →
  `ReloadValidate` → upsert → in-place reload path with no mode-specific server
  code. `config.Validate` already rejects any value other than the two enum
  members, so an invalid override is refused before it is persisted.
- The dashboard exposes it as a dedicated **Settings → Mode** section with a
  toggle. **Switching to `live` requires an explicit confirmation dialog**;
  switching back to `dry-run` is immediate and frictionless. This keeps the
  "arming the dangerous mode is a deliberate act" property that the git commit
  used to provide.

## Consequences

### Positive

- The live↔dry-run round-trip is a two-click dashboard action that hot-reloads
  in place — no file edit, no restart. Operators are less tempted to park the
  daemon in `live`.
- Consistent with the rest of the UI-managed config surface (ADR-0020, ADR-0028).

### Negative / acknowledged

- **The audit trail for `mode` moves from git to the database.** A change is
  recorded in `settings_overrides` (key `mode`, with `updated_at`) instead of a
  YAML commit. This is the same trade already accepted for scoring/polling/
  notifications in ADR-0020; `mode` is simply higher-stakes, which the
  confirmation dialog compensates for.
- This does **not** weaken the deeper safety guarantees, which are unchanged:
  `dry-run` remains the default (ADR-0015), destructive action still requires
  the per-*arr-instance `act` flag (still YAML-only, git-audited), the HnR
  window is still a hard non-configurable veto, and the trigger model
  (ADR-0015) still governs which runs may execute. `mode` is the ceiling, not
  the whole gate.

## Revisit when

- An operator needs a tamper-evident, off-box audit of `mode` flips (e.g. a
  compliance requirement) — that would call for an append-only audit log of
  settings changes rather than the last-write-wins `settings_overrides` row.
