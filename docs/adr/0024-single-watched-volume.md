# ADR-0024: Triagearr watches exactly one volume

## Status

Accepted — 2026-05-22

## Context

[ADR-0023](0023-trash-shared-mount-convention.md) made the TRaSH-guides
shared-mount layout a hard requirement: one shared data root, **one
filesystem**, mounted at one consistent path across the whole stack.

The config has always carried `volumes []VolumeConfig`, and
[ADR-0014](0014-decider-volume-targeting.md) built the Decider around
attributing each torrent to one of N volumes by `save_path` prefix.

That array models a topology the rest of the design forbids. TRaSH is
single-filesystem *by definition* — *"all your media files and folders [must]
be in the same file system"*. A homelab with several physical disks presents
them to the OS as **one** logical filesystem (mergerfs, unionfs, a ZFS/btrfs
pool); that unified mount *is* the volume. qBittorrent and the *arrs already
see exactly one data root — Triagearr is the last container to join it.

So `volumes[]` is dead surface: extra validation, a find-by-name loop in the
Decider, a `volume_name` column in time-series tables, per-volume API
plumbing, a volume selector in the UI — all to express a cardinality that is
always 1. And ADR-0014's reason to exist — *which* volume is a torrent on —
evaporates: with one volume, every torrent is on it.

## Decision

**Triagearr watches exactly one volume.** Config `volumes []VolumeConfig`
becomes a single `volume VolumeConfig`.

- The volume is the single TRaSH shared data root. Its `path` is `statfs()`-ed
  for disk usage and is the prefix every qBit `save_path` sits under (correct
  by contract — ADR-0023).
- Multi-disk homelabs unify their disks below the OS and point Triagearr at
  the unified mount — exactly as they already do for qBit and the *arrs.
- The plural is removed **everywhere**: config, the Decider, the disk poller,
  the disk-pressure watcher, the HTTP API (`GET /api/v1/volume` — a single
  object), the `disk_pressure` and `runs` DB schema (no `volume_name` column),
  and the dashboard.
- Volume *attribution* is no longer a concept. The Decider's `save_path`
  prefix match (ADR-0014) is kept only as a cheap sanity filter — under
  ADR-0023 every torrent already sits under the volume path.

## Consequences

### Positive

- Removes dead configuration, validation, API and schema surface. The Decider
  loses its find-by-name loop and multi-volume budgeting.
- The config asks one unambiguous question — "where is your media?" — with one
  answer.
- The data model finally matches the topology ADR-0023 already mandates.

### Negative / acknowledged

- A user who wants Triagearr to treat two *separate* filesystems independently
  cannot. But that stack already violates the TRaSH hardlink layout — hardlinks
  do not cross filesystems — so Triagearr could not safely act on it anyway.
  The supported answer is mergerfs/unionfs/pool, the standard homelab pattern.

## Relationship to prior ADRs

**Supersedes [ADR-0014](0014-decider-volume-targeting.md) entirely** — volume
targeting/attribution is no longer a concept. Builds directly on
[ADR-0023](0023-trash-shared-mount-convention.md) §1 (one shared filesystem).

## Revisit when

- A real user presents a multi-filesystem topology that genuinely cannot be
  unified below the OS — at which point the question is whether Triagearr
  should support it at all, given the hardlink constraint.
