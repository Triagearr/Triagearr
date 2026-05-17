# Hardlink Topology

Why Triagearr deletes via the *arr API first, and qBittorrent second.

## Background: the standard hardlink layout

In the TRaSH-guides reference setup (and most Plex/Jellyfin homelabs), the filesystem looks like this:

```
/share/files/torrents/<category>/Foo.mkv      ← qBit downloaded it here
/share/files/media/Foo (2024)/Foo.mkv         ← *arr imported it here via hardlink
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
   → /share/files/torrents/Foo.mkv unlinked
   → nlink drops 2 → 1
   → ❌ no disk space freed (media copy still references the block)
2. POST sonarr /api/v3/episode/123?deleteFiles=true
   → /share/files/media/.../Foo.mkv unlinked
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
   → /share/files/media/.../Foo.mkv unlinked
   → nlink drops 2 → 1
   → ❌ no disk space freed yet
   → ✅ but the torrent is still seeding, ratio still climbs

2. Triagearr stats /share/files/torrents/Foo.mkv
   → nlink == 1 → only the torrent reference remains
   → conclusion: this file is now "ours alone"

3. POST qbit /api/v2/torrents/delete?hashes=X&deleteFiles=true
   → /share/files/torrents/Foo.mkv unlinked
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
/share/files/torrents/main/Foo.mkv         ← torrent A (tracker 1)
/share/files/torrents/cross/Foo.mkv        ← torrent B (tracker 2, same inode as A)
/share/files/media/Foo (2024)/Foo.mkv      ← *arr import (same inode again)
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

### Stale mapping

A media file moved by *arr (e.g. rename after metadata refresh) breaks the cached inode mapping. The mapper handles this:

- At each *arr poll, the mapper re-stats files for media items whose `path` field changed since last poll
- If an expected inode is no longer found at the recorded path, the mapper triggers a full re-scan of that *arr's library
- During the re-scan window, that media is excluded from scoring (we don't act on stale data)

### Multiple files per torrent (TV season packs)

Sonarr imports a season pack episode-by-episode. The torrent has 10 .mkv files; each gets hardlinked into `/media/Show/Season X/`. If Triagearr decides to delete the show:

- *arr API call removes all 10 hardlinks one by one
- Triagearr then verifies that for *each* torrent file in `/torrents/`, `nlink == 1`
- Only then does the qBit delete proceed

If even one file still has `nlink > 1` (cross-seeded), the cross-seed conflict logic kicks in for the whole torrent.

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

The `mapper` and `actor` packages encode this logic. Key implementation choices:

- **Inode read**: `os.Stat(path).Sys().(*syscall.Stat_t).Ino`. Linux-only; not portable to Windows, but Triagearr targets Linux containers.
- **Atomic nlink check**: between *arr delete and qbit delete, we re-stat to handle TOCTOU race where another process modified the file. If the inode disappears mid-process, we treat that as "fine, someone else cleaned up."
- **Read-only mount of `/share/files`**: the container mounts media as read-only (`:ro`) — all destructive ops go through APIs, never raw filesystem writes. Safety belt against bugs.

This logic is documented in [ADR-0003](adr/0003-arr-side-deletion.md) and [ADR-0005](adr/0005-self-contained-pipeline.md).
