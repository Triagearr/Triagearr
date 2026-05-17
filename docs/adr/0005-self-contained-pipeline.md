# ADR-0005: Triagearr handles its own qBit cleanup, no qbit_manage dependency

## Status

Accepted — 2026-05-17 (supersedes early design draft that delegated final step to qbit_manage)

## Context

The early design assumed Triagearr would delete via *arr only, then rely on the user's existing `qbit_manage` cron to clean up the resulting orphan in qBittorrent (via its `nlink==1` detection).

This was reconsidered because:
- `qbit_manage` runs on a cron (typically every 30 minutes). Delays disk space release.
- Adds an external dependency that's not actually required by our use case.
- Splits the audit trail across two tools — Triagearr says "I deleted media X" but the actual torrent removal is reported by qbit_manage on its own schedule, with no link to Triagearr's reasoning.
- Couples Triagearr's success criteria to a tool we don't control.

## Decision

Triagearr **owns the full pipeline**: *arr deletion → hardlink stat → qBit deletion → audit log. All within one atomic action.

`qbit_manage` remains complementary (handles orphans created outside Triagearr — manual deletions, *arr-driven deletions not triggered by Triagearr, user pruning) but is not a Triagearr dependency.

Pipeline (excerpted from ADR-0003):
1. Call *arr API to delete media (`deleteFiles=true`)
2. Stat the corresponding `/torrents/...` path
3. Based on remaining `nlink`:
   - `nlink == 0` → file already gone (*arr deleted both hardlinks somehow), just remove torrent entry from qBit (no files)
   - `nlink == 1` → only our torrent ref remains, qBit delete with `deleteFiles=true`
   - `nlink > 1` → cross-seed conflict, apply `action.cross_seed.on_conflict` policy
4. Persist actions + audit_log atomically

## Consequences

**Easier:**
- Single transaction, single audit entry, single notification per action
- Immediate space release (no waiting for qbit_manage's next tick)
- Triagearr can be installed standalone, no other tool required
- Cross-seed handling is centralized in our actor, not split between us and qbit_manage

**Harder:**
- Triagearr must implement a qBit client (was going to anyway, for polling)
- We own the failure modes of qBit API (was previously qbit_manage's problem)

**Traded away:**
- The "single responsibility" purity of "*arr is for media, qbit_manage is for torrents." In practice, the orchestration must live somewhere, and centralizing it in Triagearr produces a better user experience.

## Implementation notes

- The qBit client (`internal/qbit`) is shared between the poller (M1) and the actor (M5)
- The cross-seed detection happens during the post-arr-delete stat — we inspect all torrents that reference the same inode (qBit exposes file paths per torrent, we join by stat)
- If qBit is unreachable when the actor needs it, the *arr deletion has already happened. We log a warning and leave `qbit_manage` (if installed) or the user to clean up. We do not roll back the *arr deletion.

## Re-evaluation triggers

- If users report frequent qBit API issues that we cannot work around, revisiting "delegate cleanup elsewhere" might be wise.
- If a future deployment scenario lacks qBit entirely (Transmission, Deluge, etc.), the abstraction (already in `QbitClient` interface) accommodates this. The decision then becomes: do we ship multiple download client clients ourselves, or push that to qbit_manage / Cleanuparr? V1 says we ship qBit only.

## References

- ADR-0003 (deletion order)
- [docs/HARDLINK_TOPOLOGY.md](../HARDLINK_TOPOLOGY.md)
- qbit_manage: https://github.com/StuffAnThings/qbit_manage
