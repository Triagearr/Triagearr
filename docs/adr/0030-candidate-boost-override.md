# ADR-0030: per-torrent candidate boost may override the rare-content guard

## Status

Accepted — 2026-05-31

## Context

Triagearr has a sticky, user-driven **Protect** toggle (`torrents.protected`,
ADR surface in SCORING.md §Factor 8): a per-torrent exclusion veto that the
Decider filters out of the candidate set. There is no symmetric way to push a
*specific* torrent toward deletion. Operators reaping a graveyard library
regularly know "this one, definitely, now" and today have to wait for the
passive factors (age, velocity, dead-tracker bonus) to rank it high enough — or
relax global thresholds, which is a blunt instrument that affects everything.

The request is the mirror image of Protect: a per-torrent **Prioritize
deletion** toggle that adds a large positive contribution to the `DeleteScore`
so the chosen torrent jumps to the top of the reap queue.

The design question is how the boost interacts with Triagearr's near-veto guard
rails. Two of the factors carry extreme weights so that no normal combination
overrides them: the **rare-content guard** (`seeders_low_guard = −1000`, the
swarm still depends on this content) and the **HnR window veto** (`−10000`, a
live hit-and-run obligation). CLAUDE.md lists the HnR window as a hard,
non-configurable veto.

## Decision

**The candidate boost is a fixed `+2000` scoring factor that overrides the
rare-content guard but not the HnR window veto.**

- **Strength `+2000`.** Larger than the rare-content guard (`−1000`), so a
  boosted torrent reaps even when its swarm is thin — this is the operator's
  deliberate "I really mean it" override. Still far smaller than the HnR veto
  (`−10000`): a boosted in-window private torrent nets `−8000` and is never
  deleted. **HnR stays inviolable**, consistent with the non-negotiable safety
  rules.
- **Fixed constant, not a tunable weight.** It lives next to `HnRVetoWeight` as
  a hard-coded `scorer.CandidateBoostWeight`, not in `scoring.weights`. It is a
  per-torrent user action, not a global heuristic, so exposing it in
  Settings → Scoring would be misleading and invite mis-tuning of a safety knob.
- **Mutually exclusive with Protect.** A torrent cannot be both protected and
  boosted; setting either flag clears the other in the same store write, so the
  two opposite intents can never coexist.
- **Sticky and auditable.** Stored in `torrents.candidate_boost` /
  `candidate_boost_at`, excluded from the qBit UPSERT so it survives re-syncs
  (same mechanism as `protected`). It appears as a named factor in the score
  breakdown, so a boosted score is self-explaining.

## Consequences

### Positive

- Operators get a precise, reversible "reap this next" control that complements
  Protect, without relaxing global thresholds.
- The HnR guarantee is untouched — the one veto that protects live obligations
  remains absolute.

### Negative / acknowledged

- **A boost can delete a rare seed the swarm depends on.** This is intentional
  and documented, but it is a real loss of the rare-content safety net for that
  one torrent. The confirmation lives in the deliberate, per-torrent nature of
  the toggle (and the live-mode + per-*arr `act` gates still apply before any
  actual deletion).
- The score breakdown can now show a large positive contribution that dwarfs the
  organic factors; the UI labels it as a user override so it is not mistaken for
  an emergent score.

## Revisit when

- Operators want a tunable boost magnitude, or a boost that also overrides the
  HnR window (it must not, today) — either would be a new decision superseding
  this one.
