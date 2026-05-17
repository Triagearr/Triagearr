# ADR-0004: Multi-factor weighted DeleteScore over hardcoded rules

## Status

Accepted — 2026-05-17

## Context

Two patterns dominate existing media-cleanup tools:

1. **Hardcoded rules**: "if ratio > 1 and seed_time > X, delete." Easy to implement, easy to reason about. But brittle — every new requirement adds an `if` branch, edge cases proliferate, and tuning is binary (rule fires or not).
2. **Rule engines**: user writes Boolean expressions over fields. Powerful but complex; Maintainerr does this. Users need to understand the rule language, the field schema, and the operator semantics.

Triagearr's requirements:
- Multi-criteria: ratio AND velocity AND seeders AND age, with tradeoffs between them
- Tunable without code changes
- Explainable (the user must understand why a torrent was deleted)
- Safe by default (some conditions are near-vetoes, not "yes/no" inputs)
- No requirement for a query language (single maintainer, no UX budget for a DSL)

## Decision

Adopt a **multi-factor weighted score**:

```
DeleteScore = Σ (weight_i × value_i)
```

Where:
- Each factor returns a normalized value (usually [0..1] or [0..N])
- Each factor's weight lives in YAML config
- Some weights are extreme (-1000, -10000) to act as veto factors
- The final score is comparative, not absolute (top-K are selected)

This is essentially a tiny domain-specific linear model. It's not machine learning — the weights are user-set, not learned.

The score is computed per-torrent, persisted to the `scores` table, and exposed via `/api/v1/scores/{hash}/explain` with per-factor breakdown.

## Consequences

**Easier:**
- Adding a new factor = one new function + one new weight in config. No rule-engine complexity.
- Tuning = edit YAML, restart (or SIGHUP). No code change.
- Explainability = serialize the per-factor breakdown. UI renders bars per factor.
- Veto behavior = use extreme negative weight. Composes with everything else cleanly.
- Comparative selection means scores naturally adapt to library shape (no need to recalibrate for users with different libraries).

**Harder:**
- "Why was X deleted but Y kept?" requires showing two breakdowns side-by-side. UI must support this.
- A user wanting strict "delete only if ratio > 1.5" must use a near-veto weight to emulate the rule. Not as clean as a literal rule.
- Multi-factor interactions can surprise users (e.g., "I expected veto to win but it didn't" — happens when factor outputs are not [0..1] normalized).

**Traded away:**
- Rule-language familiarity. Users coming from Maintainerr will need to map their rule mental model to weights.

## Mitigations

- Ship conservative defaults in V1 (lots of negative weight on seeders_low, generous HnR window, no exotic factors)
- Provide `triagearr score --explain <hash>` CLI to inspect before going live
- Document a recipes file (`docs/SCORING_RECIPES.md` — future) with "to emulate Maintainerr rule X, set weights to Y"

## Re-evaluation triggers

- If users repeatedly request literal rules ("just give me an `if ratio > X` toggle"), reconsider adding a thin rule-passthrough on top of the score
- If a future ML angle becomes interesting (learned weights from user feedback), the foundation is already a linear model — easy to swap in learned coefficients

## References

- [docs/SCORING.md](../SCORING.md) — the algorithm spelled out
- Maintainerr rules engine for contrast: https://docs.maintainerr.info/4.0.0/rules
