# ADR-0003: Deletion happens *arr-side first, qBittorrent-side second

## Status

Accepted — 2026-05-17

## Context

When a user runs Sonarr/Radarr/etc with hardlinks between `/torrents/` and `/media/`, deleting a file requires breaking both hardlinks. The order matters for safety and observable behavior.

### Naive order: qbit first, then *arr

```
1. qbit DELETE torrent X with deleteFiles=true
   → /torrents/Foo.mkv unlinked, nlink drops 2 → 1
   → torrent stops seeding immediately
2. sonarr DELETE episode 123 with deleteFiles=true
   → /media/Foo.mkv unlinked, nlink drops 1 → 0
   → disk space freed
```

Problems:
- If step 2 fails (auth, 500, timeout), we've stopped seeding for nothing. No way to roll back step 1 without re-downloading.
- The torrent is gone from qBit before the user-visible action (media removal in *arr) completes. Audit trail is split.
- If multiple users / processes touch this file in parallel, we've shrunk our recovery window.

### Correct order: *arr first, then qbit

```
1. sonarr DELETE episode 123 with deleteFiles=true
   → /media/Foo.mkv unlinked, nlink drops 2 → 1
   → torrent KEEPS seeding (its file is still there)
2. Triagearr stats /torrents/Foo.mkv → nlink == 1
   → confirms only our reference remains
3. qbit DELETE torrent X with deleteFiles=true
   → /torrents/Foo.mkv unlinked, nlink drops 1 → 0
   → disk space freed
```

Properties:
- Step 1 failure → abort cleanly. Torrent kept seeding the whole time. No data lost, no disk impact. Free retry.
- Step 2 is a defensive check between steps 1 and 3. Catches surprises (concurrent processes, cross-seed conflicts).
- The *arr API call is the user's mental model of "delete this media." It's the right place for the user-visible action.

## Decision

Triagearr always deletes *arr-side first, then verifies hardlink state, then deletes qBit-side.

If `nlink > 1` after step 1 (cross-seeding), the action defaults to `skip` for the qBit step — the file remains, and the conflict is logged + surfaced in the UI. Behavior configurable via `action.cross_seed.on_conflict`.

## Consequences

**Easier:**
- Rollback semantics are clean — *arr is the only source of truth for media existence
- Network/transient failures don't leave inconsistent state
- Cross-seed handling is a natural extension of the same check (stat → reason about nlink)
- Audit log narrates a coherent story: "decided X, asked *arr to delete, verified, asked qBit to clean up"

**Harder:**
- Slightly more API calls (one stat between two HTTP calls). Trivial cost.
- Triagearr must know which *arr owns a given media. The mapper handles this — the join `qbit_torrent ↔ inode ↔ arr_media_id` is computed during M2.

**Traded away:**
- The simpler "qbit-only" approach used by Cleanuparr's seeding rules. We pay this cost because Cleanuparr's approach **doesn't free space on hardlink setups** (it removes only the torrent reference, leaving the media reference and thus the file on disk). Our use case (disk pressure relief) demands actually freeing space.

## Re-evaluation triggers

- If a user setup is found that does *not* use hardlinks (e.g., copy/move instead), the order may need to invert. We assume hardlinks because the TRaSH-guides standard layout uses them.
- If *arr APIs become unreliable enough that qbit-first becomes operationally safer, we revisit. Not foreseen.

## References

- [docs/HARDLINK_TOPOLOGY.md](../HARDLINK_TOPOLOGY.md) — full walk-through with diagrams
- TRaSH-guides hardlink setup: https://trash-guides.info/Hardlinks/
- Maintainerr's deletion path (for comparison): uses *arr API only, ignores qBit entirely
- Cleanuparr's deletion path (for comparison): uses download client API only, no *arr awareness
