# Hardlink Topology

Why Triagearr deletes via the *arr API first, and qBittorrent second.

## Background: the standard hardlink layout

In the TRaSH-guides reference setup (and most Plex/Jellyfin homelabs), the filesystem looks like this:

```
/data/torrents/<category>/Foo.mkv      ← qBit downloaded it here
/data/media/Foo (2024)/Foo.mkv         ← *arr imported it here via hardlink
```

Two paths, **one inode**. The kernel keeps a reference count (`nlink`). The disk block is freed only when `nlink == 0`.

| State | `/torrents/Foo.mkv` | `/media/.../Foo.mkv` | `nlink` | Disk usage |
|---|---|---|---|---|
| Initial download (no import yet) | exists | absent | 1 | counted once |
| After *arr import | exists | exists | 2 | counted once (still one block) |
| qBit deletes its copy only | absent | exists | 1 | counted once (no change) |
| *arr deletes its copy only | exists | absent | 1 | counted once (no change) |
| Both copies deleted | absent | absent | 0 | **block freed** |

This is what makes hardlinks magical: zero space overhead for two coexisting references, atomic.

## The naive (wrong) order

Imagine Triagearr deletes via the qBit API first:

```
1. POST qbit /api/v2/torrents/delete?hashes=X&deleteFiles=true
   → /data/torrents/Foo.mkv unlinked
   → nlink drops 2 → 1
   → ❌ no disk space freed (media copy still references the block)
2. POST sonarr /api/v3/episode/123?deleteFiles=true
   → /data/media/.../Foo.mkv unlinked
   → nlink drops 1 → 0
   → ✅ disk space freed
```

This works but has problems:
- Between step 1 and step 2, qBit thinks the torrent is gone and stops seeding. **If the *arr API call fails (network, auth, 500), we've stopped seeding for nothing.**
- We can't retry the deletion cleanly because the torrent is already gone from qBit.
- Audit trail is split awkwardly across two systems.

## The correct order (what Triagearr does)

```
1. POST sonarr /api/v3/episode/123?deleteFiles=true
   → /data/media/.../Foo.mkv unlinked
   → nlink drops 2 → 1
   → ❌ no disk space freed yet
   → ✅ but the torrent is still seeding, ratio still climbs

2. Triagearr stats /data/torrents/Foo.mkv
   → nlink == 1 → only the torrent reference remains
   → conclusion: this file is now "ours alone"

3. POST qbit /api/v2/torrents/delete?hashes=X&deleteFiles=true
   → /data/torrents/Foo.mkv unlinked
   → nlink drops 1 → 0
   → ✅ disk space freed
```

Why this order is better:
- **If step 1 fails**, we abort. The torrent kept seeding, no data lost, no disk impact, full rollback for free.
- **Step 2's stat call** is the safety net: it confirms our model of the world matches reality before we destroy the last reference.
- The order maps onto how *arr-driven workflows already work: *arr is the system of record for "should this media exist?"; qBit is downstream.

## Edge cases

### Cross-seeding

Cross-seeding means the same file is downloaded once but seeded on multiple trackers via multiple qBit torrents pointing to the same inode (via hardlinks managed by [cross-seed](https://www.cross-seed.org/)).

```
/data/torrents/main/Foo.mkv         ← torrent A (tracker 1)
/data/torrents/cross/Foo.mkv        ← torrent B (tracker 2, same inode as A)
/data/media/Foo (2024)/Foo.mkv      ← *arr import (same inode again)
nlink = 3
```

If Triagearr decides to delete the media:

```
1. *arr deletes its copy → nlink 3 → 2
2. Triagearr stats /torrents/main/Foo.mkv → nlink == 2 (NOT 1)
   → conclusion: another torrent shares this file
3. Decision per config `action.cross_seed.on_conflict`:
   - skip:        log, leave both torrents alive, abort qbit step
                  → media is gone, both torrents will eventually fail
                  → least surprise but leaves orphans
   - warn_only:   delete torrent A metadata (qbit) but NOT files (deleteFiles=false)
                  → torrent A stops seeding, torrent B keeps seeding
                  → cleanest unless you really wanted both trackers
   - force_delete: delete torrent A with files → torrent B errors (file missing)
                   → not recommended, included for completeness
```

V1 default: `skip` (safest). The dashboard surfaces cross-seed conflicts so the user can decide.

### Stale linking

A media file moved by *arr (e.g. rename after metadata refresh) cannot rely on cached paths. Under ADR-0012 the linker is API-only: each *arr poll refreshes `arr_imports` from history, and consumers always read the current row. The `nlink` check is taken at action time, never cached — see T3.5 below.

### Multiple files per torrent (TV season packs, music albums, packs)

This is the **norm**, not the edge case: season packs, multi-CD albums, anthology packs all produce one qBit torrent containing N files, each hardlinked individually into the *arr-managed library.

#### Granularity by layer

| Layer | Unit |
|---|---|
| Scoring / decision | 1 **torrent** (qBit `hash`) |
| Linker | 1 torrent → **0..N** `arr_file_id` (via *arr history) |
| Actor — *arr step (T3) | 1 *arr **file** at a time (`episodeFile.id` / `movieFile.id`) |
| Actor — qBit step (T4) | 1 **torrent** as a whole (`deleteFiles=true`) |

The score is taken at the torrent level — qBit does not let you seed half a torrent. The linker and Actor must handle the N→1 fan-in/fan-out without dropping safety.

#### Concrete loop

```
candidate = torrent H
qbit_files     = qbit.Files(H)               # [f1, f2, ..., fN]
arr_targets    = linker.ArrImports(H)        # [{instance, file_id, imported_path} ...]
                                              # — often < N (some files may NOT be *arr-imported)

# T3 — fan out *arr deletes
for (instance, file_id) in arr_targets:
  POST {instance} DELETE /api/v3/{episodefile|moviefile}/{file_id}
  → on failure: retry, then ABORT the whole candidate
     (already-deleted *arr files are NOT rolled back — but nlink stays ≥1
      thanks to the surviving torrent, so disk is untouched. *arr will just
      re-monitor and re-grab those episodes.)

# T3.5 — per-file nlink check (not a single torrent-level stat)
# Paths coincide between containers per ADR-0023, no translation needed.
for f in qbit_files:
  nlink = stat(f.path).nlink
  if nlink > 1:
    apply on_conflict   # cross-seed, upgrade, or external hardlink we don't own
                        # skip → ABORT the qbit-delete, keep the torrent alive

# T4 — qBit delete is whole-torrent
POST qbit /api/v2/torrents/delete?hashes=H&deleteFiles=true
```

The per-file stat is the safety net. A torrent-level "is nlink right?" question doesn't exist — you have to check each file.

#### Real-world cases

1. **Partial import.** 10-episode pack, *arr imported 8 (2 skipped for quality). `arr_targets` has 8 entries; the other 2 files were already `nlink=1`. T3 deletes 8 *arr-side → all 10 reach `nlink=1`. T4 frees everything. ✅
2. **Upgrade.** Sonarr replaced S01E03 with a higher-quality grab from a different release. The original file in the pack is no longer linked to anything in `/media/`. `arr_targets` simply has no entry for that file (the linker finds no `arr_imports` row). Its nlink was already 1. Same path as case 1. ✅
3. **Cross-seed.** Pack S01 exists on tracker A (torrent H1) AND tracker B (torrent H2), same inodes. Files start at `nlink=3` (qbit×2 + arr×1). T3 *arr deletes → `nlink=2`. The T3.5 re-stat sees `nlink=2`, `on_conflict: skip` (default) → ABORT, H1 stays alive. Marked `skipped_cross_seed` in `audit_log`. H2 will be scored independently on a later run. ✅
4. **Partial *arr failure.** 8/10 deletes OK, the 9th returns 500. Retry ×3, hard fail. ABORT. Resulting state: 8 *arr files removed (`nlink=1`), 2 still linked (`nlink=2`), qBit torrent intact and seeding. Disk: **zero bytes freed** (every nlink ≥ 1). `audit_log` notes `partial_arr_delete, aborted`. *arr re-monitors the 8 missing episodes and re-grabs them — not pretty, but no data loss and no broken seed obligation.
5. **Multi-instance.** Same file imported by Sonarr AND a second "Sonarr 4K" instance. `arr_targets` has 2 entries pointing at one qbit file. T3 deletes both. If only one succeeds, you fall through to case 4.
6. **Music album / multi-CD.** Identical mechanics to season packs, just 12 tracks instead of 10 episodes. No special-casing.

#### Conflict policy is per-file, not per-torrent

If 9/10 files have `nlink=1` and a single file has `nlink=2` (cross-seed on just one track), `on_conflict: skip` aborts the **whole torrent**. There is no half-delete primitive at the qBit layer — `deleteFiles=true` is all-or-nothing. The conservative semantic is "if any file would harm someone else, leave the torrent alone."

#### Audit log granularity

The audit log records **per-file** outcomes (8 OK + 1 failed + 1 not-attempted), not just per-torrent ("aborted"). Case 4 is unreadable in post-mortem without that granularity — the schema must reflect this when M5 lands.

### "Episodes only" deletions

Triagearr V1 deletes **entire torrents** as atomic units, not individual episodes. This is a deliberate constraint — partial deletion within a torrent leads to a broken seed (files missing) which trackers may penalize.

If you have a season pack and want to delete a single watched episode, Triagearr is not your tool — Maintainerr at the *arr layer is. Triagearr's "delete a torrent" decision is *all or nothing*.

## Why not just `find / -links 1 -delete`?

The user has `qbit_manage` in their stack which does exactly this (orphan cleanup based on `nlink==1`). The difference:

- `qbit_manage` is **reactive**: it cleans up after *something* (the user, *arr, Maintainerr) has already broken the hardlink.
- Triagearr is **proactive**: it *initiates* the break, deliberately, on the right media at the right time.

They are complementary:
- Triagearr drives the *arr→qbit chain for its own decisions
- `qbit_manage` continues to handle orphans created by everything else (manual *arr deletions, Maintainerr, user pruning)

Triagearr does not depend on `qbit_manage`. If the user removes it, Triagearr keeps working (it self-contains the qbit cleanup step). If both run, they don't fight each other — Triagearr's actions complete before `qbit_manage`'s next tick.

## Implementation notes

The `linker` and `actor` packages encode this logic. Key implementation choices:

- **Linker is API-only** (ADR-0012): torrent↔arr-file is resolved via the *arr `history` endpoint, not filesystem inode comparison. The historical inode-mapper design (ADR-0010) is superseded.
- **Atomic nlink check**: at action time (T3.5), between *arr delete and qbit delete, we `stat(path).Sys().(*syscall.Stat_t).Nlink`. Paths coincide across containers per ADR-0023, so no translation is needed. If the inode disappears mid-process, we treat that as "fine, someone else cleaned up."
- **Read-only mount of `/data`**: the container mounts media as read-only (`:ro`) — all destructive ops go through APIs, never raw filesystem writes. Safety belt against bugs.

This logic is documented in [ADR-0003](adr/0003-arr-side-deletion.md) and [ADR-0005](adr/0005-self-contained-pipeline.md).
