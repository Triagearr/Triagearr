# ADR-0014: Decider attributes torrents to volumes by `save_path` prefix

## Status

Superseded by [ADR-0024](0024-single-watched-volume.md) — 2026-05-22.

Originally Accepted — 2026-05-20; amended by
[ADR-0023](0023-trash-shared-mount-convention.md). ADR-0024 removes the
multi-volume model entirely — Triagearr watches exactly one volume, so the
per-torrent volume *attribution* that is this ADR's whole subject no longer
exists. The `save_path` prefix match described below survives only as a cheap
sanity filter (correct by contract under ADR-0023). Kept for historical
context.

## Context

M4's Decider needs to answer: "which scored torrents live on the volume that just crossed `threshold_free_percent`?" The original M2 roadmap planned a `internal/mapper` package that resolved torrents to volumes via inode + per-file hardlink topology, with autoinferred `path_remap` rules (ADR-0010).

In M2.1 we superseded the hardlink-aware mapper with `internal/linker` (ADR-0012): an API-only resolution that joins qBit hashes to `*arr` import history. The hardlink mapper was never built. So when M4 arrived, the volume-attribution primitive the roadmap assumed (`mapper.ArrTargets(hash)` returning per-file inode/volume) did not exist.

Building it just for M4's dry-run targeting is overkill:

- It would re-introduce filesystem syscalls and the path-remap inference engine ADR-0012 explicitly removed for portability on the QNAP setup.
- The Decider only needs **coarse** attribution — "is this torrent on the pressured volume?" — not per-file accounting. The per-file accounting matters at deletion time (the Actor in M5), to honour the nlink ≥ 1 invariant per `docs/HARDLINK_TOPOLOGY.md`, but not for choosing candidates.
- qBit already reports each torrent's `save_path`, captured in the `torrents` table since M1. A volume is configured with a `path` in `config.volumes[]`. Prefix-matching is sufficient for the realistic homelab topology where each volume is one filesystem mount and torrents live in subdirectories of `volume.path`.

## Decision

**The Decider attributes a torrent to volume `V` when `torrent.save_path == V.path` or `torrent.save_path` starts with `V.path + "/"`.**

No filesystem access, no inode resolution, no path-remap inference. The match is a pure string operation on data already present in SQLite.

The match logic lives in `internal/decider/decider.go::Plan`:

```go
volumePath := strings.TrimRight(v.Path, "/")
prefix := volumePath + "/"
// ...
sp := strings.TrimRight(t.SavePath, "/")
if sp != volumePath && !strings.HasPrefix(t.SavePath, prefix) {
    continue
}
```

`would_free_bytes` is set to `size_bytes` in M4 — i.e. we assume the torrent's full size is reclaimable. M5 will refine this per-file via nlink re-stat (a hardlinked file in two torrents only frees space when the *last* torrent goes away).

## Consequences

### Positive

- Zero new dependencies, zero new syscalls. Pure stdlib `strings`.
- Tests are trivial (no temp filesystem fixtures).
- M5 can override `would_free_bytes` without changing the Decider's contract — the field already exists in `run_items`.

### Negative (acknowledged)

- **Save-path-on-different-volume edge case**: if a torrent's `save_path` is on volume A but its hardlinks (after `*arr` import) live on volume B, the Decider attributes it to A. Deleting it via the Actor in M5 would free space on B (where the imported library lives), not A. The disk-pressure trigger could fire a run that, after Actor execution, doesn't actually relieve the pressured volume.

  This is acceptable in M4 because:
  1. The homelab setups Triagearr targets keep download and library on the same volume (it's literally the point of the hardlink topology — see `docs/HARDLINK_TOPOLOGY.md`).
  2. M4 is dry-run only. The verdict is observable in the `runs` table before any action.
  3. M5 will refine `would_free_bytes` per-file and re-evaluate.

- **No per-category split**: if a volume is shared by qBit categories with different save-path roots, we still match by prefix on the volume root. This is the desired behaviour.

### Alternatives considered

- **Build the hardlink mapper now**: rejected — too much scope for a dry-run feature, re-introduces the portability friction ADR-0012 removed.
- **Use `linker.Links()` to attribute via library path**: would require the library to live on a different volume than `save_path` to differ from the prefix match — the rare edge case above. The complexity isn't warranted yet; M5 will handle the proper per-file accounting at the moment that matters (actual deletion).

## Revisit when

- Multi-volume hardlink topologies become common in the user base.
- The M5 Actor's per-file `would_free_bytes` reveals systematic over-estimation that surprises operators.
