# ADR-0012: API-only mapping; drop filesystem access for hardlink resolution

## Status

Accepted — 2026-05-19

Supersedes [ADR-0010](0010-path-remapping-for-mapper.md) (path-remap inference becomes moot).
Modifies [ADR-0003](0003-arr-side-deletion.md) (deletion order unchanged; nlink re-check removed).

## Context

The M2 deployment on the homelab QNAP (2026-05-19) exposed the operational cost of the filesystem-stat approach baked into the mapper since ADR-0010:

- The `triagearr` container runs as `UID 1000:1000` (the homelab convention), but `/share/files/{torrents,media}` is owned by `UID 1005 (torrent)` with `chmod 770`. `filepath.WalkDir` hits `EACCES` on the very first subdirectory, the walker silently skips, the index ends up with `entries: 0`, and inference fails closed with `samples_total=20 matched=0`.
- The fix space is large but each option carries friction: tweak the container's UID/GID (couples Triagearr's identity to the host), `group_add` (still requires the operator to know which numeric GID owns the share), broaden the share's ACLs (security regression), or document a precondition the project's "homelab quality of life" pitch claims to avoid.
- This is not a one-off bug; it's the contour of an architectural decision. Every deployment that doesn't share UID/GID conventions with the user's *arr stack will hit a variation of the same problem (Synology shares, TrueNAS datasets, Unraid bind paths, Docker Desktop on macOS, …).

While diagnosing, we re-asked the foundational question: **does Triagearr actually need filesystem access?**

The two reasons it currently does:

1. **Hardlink equivalence proof** — given a qBit-reported file and an *arr-reported file, are they the same on-disk inode? Today this is answered with `stat()` + inode comparison via the mapper's auto-inferred path remap.
2. **Pre-delete nlink re-check** — between *arr's API delete (T3) and qBit's `delete --with-files` (T4), `docs/HARDLINK_TOPOLOGY.md` § Multiple files per torrent prescribes a T3.5 step where we re-stat each file to confirm `nlink ≥ 2` survived the *arr delete (i.e., at least one hardlink remains, namely qBit's). This is a belt-and-suspenders safety net.

Neither is exposed directly by qBit or *arr APIs. But probing the live homelab Sonarr (24 series) and Radarr (118 movies) revealed that **the *arr history endpoint already records the hardlink relationship at import time**, with everything we need:

```json
GET /api/v3/history?eventType=3
{
  "records": [{
    "downloadId":  "FAE1049A4ACC082A3D33E76409B140031D8E3D2A",  // qBit hash (uppercased)
    "data": {
      "fileId":          "490",
      "droppedPath":     "/files/torrents/tv/<release>.mkv",
      "importedPath":    "/files/media/tv/<show>/<release>.mkv",
      "downloadClient":  "qBittorrent",
      "size":            "887857384"
    },
    "eventType":   "downloadFolderImported"
  }]
}
```

The `downloadId` is the V1 info-hash of the qBit torrent (uppercased — qBit reports lowercase, easy normalisation). `data.fileId` is the same `episodeFile.id` / `movieFile.id` the M5 actor needs for granular DELETEs. The pair is established by *arr itself at import time, when the hardlink was created.

This makes *arr's history a **stronger** source of truth than `stat()` for our purposes: it records the hardlink intent, not just its current physical state. `stat()` confirms "these two paths share an inode right now"; the import history records "*arr deliberately hardlinked this qBit file into the library." For Triagearr's deletion model, the latter is what we actually want to act on.

## Decision

**Drop the filesystem-stat approach. Use *arr import history as the canonical hardlink map.**

### Data flow

1. New table `arr_imports` keyed by `(arr_type, file_id)`, storing `download_id` (qBit hash), `dropped_path`, `imported_path`, `size`, `imported_at`. Persisted so we survive restarts and don't re-fetch the entire history every boot.
2. The *arr poller fans out a paginated history fetch per instance (`/api/v3/history?eventType=3`), upserts new records into `arr_imports`. Cursored on the `id` column so we only pull deltas after the first sync.
3. A new `internal/linker` package (replaces `internal/mapper`) exposes:
   - `Links(hash triagearr.Hash) → []Link{ArrType, FileID, DroppedPath, ImportedPath, Size}` — the *arr-side targets for a qBit torrent.
   - `ByFileID(arrType, fileID) → Link` — reverse lookup for diagnostics.
4. M5 actor uses `Links(hash)` to enumerate the per-file `(arrType, fileID)` DELETE targets, then proceeds with the *arr-then-qBit order. **No filesystem access. No T3.5 re-stat.**

### Hardlink-equivalence guarantee — what we keep, what we lose

**Keep**: the *arr-managed copy and the qBit-side copy are still hardlinks of the same inode (this is set up at import by *arr itself; nothing else changed). Issuing `*arr DELETE` followed by `qBit DELETE --with-files` removes both hardlinks → disk freed.

**Lose**: detection of an **unknown third hardlink** (Plex collection symlinks, manual backup scripts, cross-seed managed by external tools). With T3.5 removed, if a third hardlink existed, both Triagearr-known hardlinks fall but the third survives → disk space stays occupied for that file.

This is recoverable: the post-action [`freed_space` metric][freed-space] re-samples `disk_pressure` after the action and compares to the expected `Σ size(targets)`. A shortfall surfaces as a notification (M7) and a flag on the action row — the user investigates, no data is lost in the meantime.

[freed-space]: ../adr/0004-scoring-algorithm.md

### Orphans (qBit-only, no *arr import)

Per [[project_m2_mapper_scope]], orphan torrents (category="" or non-*arr-managed downloads) still need to be reapable. With the new model, an orphan has `Links(hash) == []` — no *arr-side targets — and the actor's pipeline becomes "qBit `delete --with-files` only" for that hash. No special-casing in the linker; it's just an empty result.

### Migration of M2 deployment

The M2 v0.3.0 release ships the filesystem mapper. M2.1 (v0.3.1) lands this redesign. The homelab compose drops the `/share/files:ro` bind mount in the same step. The UID issue stops being a Triagearr concern.

### What about cross-seed?

Cross-seed creates additional torrent hashes referencing the same on-disk inode. Each cross-seed torrent has its OWN qBit hash; *arr's history records only the *original* import (the first hash that landed). Our `Links(hash)` returns links for the original; for the cross-seed sibling, `Links(siblingHash) == []` (no import record).

This is fine: the actor only acts on a torrent if it has been scored for deletion. If only one of the cross-seed siblings is scored, deleting it via qBit `delete --with-files` removes that torrent's hardlink view, the other sibling still has its own qBit-managed hardlink to the file, *arr's library hardlink survives. Net effect: cross-seed safe by default. The existing `on_conflict: skip/warn_only/force_delete` config (M5) still applies but is now expressed in terms of "is there another qBit hash with the same `data.size + content match`" — implementable via qBit's own torrent list, no FS needed.

## Consequences

**Easier:**
- Zero filesystem access — Triagearr is a pure-API daemon. No bind mounts, no UID/GID friction, no path_remap, no walker, no Linux-only `syscall.Stat_t`.
- Cross-platform by default — Windows users (`gcr.io/distroless/static-windows-…`-ish dreams aside, but the Go binary runs on Windows natively for development) work out of the box.
- M5 actor design shrinks: T3.5 deleted, the per-file targets come from a typed `Link`, no remap step at delete-time.
- Container can run in any UID the operator wants — no minimum-perm requirement.

**Harder:**
- We're now tied to *arr keeping the history. Sonarr/Radarr history retention is configurable (default forever in v3); if an operator nukes history, we lose the linker entries for past imports. Mitigation: persist `arr_imports` ourselves on first sight — we own the table. Even if *arr discards the source row, our copy survives.
- A user who manually moves/symlinks files outside *arr's awareness gets nothing from the linker. They could use the qBit-only path (delete-with-files) but disk space won't free if external hardlinks exist. This is the same limitation as ADR-0010 had against unknown external hardlinks (T3.5 caught it; now we detect post-hoc via freed_space metric).
- The first-time history fetch is page-by-page over the entire *arr history (Sonarr/Radarr return 250 records/page by default). For an established homelab with 1k+ imports that's a few API calls and ~1 MB of JSON — bounded, acceptable.

**Traded away:**
- The "physical-truth" guarantee of `stat()`. We now trust *arr's bookkeeping. The bookkeeping is reliable in the dominant case (TRaSH-guides setups, the project's primary niche) and degrades gracefully (orphan = qBit-only delete, no surprise).

## Alternatives considered

1. **Keep filesystem access, document the UID/GID precondition.** Rejected: the M2 deployment proved this is foot-gun #1 for any non-trivial NAS layout. Pushing the friction onto the operator is the wrong default for a "homelab quality-of-life" pitch (CLAUDE.md positioning).
2. **`group_add: [<gid-of-shares>]` in the compose.** Works mechanically but requires the operator to chase numeric GIDs across mount points and re-tune on every NAS migration. Still a precondition, just slightly less obvious.
3. **Run Triagearr in the same UID/GID as the *arr containers.** Couples Triagearr's identity to the user's *arr stack convention. Breaks the "single-binary, single-config" promise; impossible to ship a Docker image with a fixed `USER` directive.
4. **Probe the filesystem via Sonarr/Radarr API "file system browse" endpoints.** Both expose `/api/v3/filesystem` (`fileSystem`) which returns directory listings *from the *arr container's POV*. We'd inherit *arr's mount, no UID issue. But: the endpoint doesn't return inode/nlink either, and it forces us to do path-by-path lookups for every torrent — N round-trips per scoring run. Strictly worse than ADR-0010 and doesn't solve the equivalence problem.
5. **`POST /api/v3/manualimport` reverse-lookup.** Returns the candidate import matches *arr would compute. Doesn't tell us about *past* imports. Wrong direction.
6. **Just match by `(basename, size)` between qBit and *arr.** Cheap, no API at all. But basenames diverge after *arr renames; size collisions are real on media libraries (two different 1080p episodes can be the same byte count). Brittle; refused.

## References

- Sonarr v3 history endpoint: https://github.com/Sonarr/Sonarr/wiki/History
- Radarr v3 history endpoint: https://github.com/Radarr/Radarr/wiki/History
- M2 deployment notes (homelab QNAP, 2026-05-19): the UID-perm dead-end is the direct prompt for this ADR.
- Memory: [[project_m2_mapper_scope]], [[project_multi_file_torrent_design]], [[project_freed_space_metric]].
