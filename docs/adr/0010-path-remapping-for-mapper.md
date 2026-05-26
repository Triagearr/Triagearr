# ADR-0010: Auto-infer the path remap between source-system paths and Triagearr's view; manual override only as escape hatch

## Status

Superseded by [ADR-0012](0012-api-only-mapping.md) — 2026-05-19.

The path-remap inference described below was implemented and shipped in v0.3.0 (M2),
then immediately retired when the M2 deployment surfaced operational friction with
filesystem access on QNAP (UID/GID + ACL mismatches on the bind-mounted media root).
ADR-0012 replaces filesystem `stat()` with *arr import-history lookup; the mapper
no longer needs to translate paths because it no longer reads files. This ADR is
kept for historical context only.

---

(Original status: Accepted — 2026-05-19.)

## Context

The M2 mapper must resolve **the same on-disk inode** from two different starting points:

- `torrents.save_path + file.name` — as reported by qBit
- `media.path` — as reported by *arr

Both are then `stat()`ed by Triagearr to confirm `st_ino` matches and `nlink ≥ 2`. This is the foundation of safe deletion (see `docs/HARDLINK_TOPOLOGY.md`).

The M1 deployment (2026-05-19, QNAP homelab) surfaced the problem: qBit and *arr share gluetun's network namespace and mount storage at `/files/`, while Triagearr runs in plain Docker and mounts the same storage at `/share/files/`. The strings persisted in the DB (`/files/media/...`, `/files/torrents/...`) are **not** paths Triagearr can dereference. Hardcoded translation breaks every deployment that uses a different layout.

A first iteration of this ADR proposed a fully-manual `volumes[*].path_remap` config block. That works but pushes onto the user a translation table they often don't fully understand (Docker bind semantics, multi-stack mounts). For an "I just want it to work" daemon, manual config here is a foot-gun by default.

We have enough signal at startup to **infer** the remap with high confidence, and to refuse-to-start when we cannot.

## Decision

**Inference first, manual override as escape hatch.** Procedure at boot, in order:

1. **Index the local volume.** Walk each `volumes[].path` once, building an in-memory index keyed by `(basename, size)` → list of absolute local paths. Bounded; we cap depth and total entries (configurable, default 200k entries; this is generous for any real Plex library).
2. **Sample source paths.** From the most recent qBit poll and *arr polls, collect 20 file paths per source (or all if fewer). qBit samples come from `torrents.save_path + file.name` (`/api/v2/torrents/files`); *arr samples come from `media_files.path` (Sonarr `/api/v3/episodefile`, Radarr `/api/v3/moviefile`) — file paths, not the series/movie root, so the trailing-suffix match has more depth to lock onto. For each sample, look up `(basename, size)` in the index. A sample "matches" if exactly one local path has the same trailing path components as the source path's trailing components (longest common suffix).
3. **Derive prefix substitutions.** For each matched sample, compute the prefix difference: e.g., source `/files/torrents/tv/Foo.mkv` and local `/share/files/torrents/tv/Foo.mkv` → rule `{from: /files/, to: /share/files/}`. Group by `(from, to)` and count.
4. **Acceptance threshold.** If a single rule explains **≥80 %** of matched samples *and* at least 5 samples matched, the rule is published as the inferred remap. The daemon logs `path_remap_inferred` at INFO with the rule, sample count, and confidence.
5. **Failure modes.**
   - **No samples matched** (likely empty index or wildly different layout): refuse to start, log `path_remap_inference_failed` ERROR, point the user at the manual override.
   - **Multiple competing rules** (none reach 80 %): refuse to start, log the candidate distribution to help the user diagnose.
   - **Identity match** (source paths stat as-is): publish a no-op remap, log it explicitly so the user sees that *no remap was needed*.
6. **Manual override always wins.** If `volumes[*].path_remap` is set in config, inference is skipped for that volume and the manual rules are used verbatim. Each manual `to:` is still stat-ed; missing dir = refuse to start (same hard validation as before).
7. **Re-inference on config reload.** A SIGHUP that adds a new volume re-runs inference for it. A SIGHUP that adds a manual `path_remap` immediately replaces the inferred one. Inferred rules are never persisted to disk — they are derived state, not config.

### Bounds and safety

- The index walk is single-pass and read-only. It runs after the first successful qBit + *arr poll (so we have something to sample against). The mapper waits on that gate; until inference settles, no mapping queries are answered.
- The matching strategy uses `(basename, size)` to disambiguate same-named files. Two files with the same name AND same size in the same trailing-path layout would still be ambiguous; we treat them as no-match rather than guess.
- We never write inferred rules to the config file. The user owns `config.yml`; we don't mutate it.

### Where this lives in code

- `internal/mapper.Resolver` exposes `Translate(sourcePath string) (localPath string, ok bool)`.
- A new `internal/mapper/inference.go` runs the boot procedure and feeds the resolver.
- `triagearr inspect mapping <hash>` prints the source path, the resolved local path, the inode, AND the remap rule that produced the resolution (inferred or manual). Misconfiguration is one command away from being obvious.
- `triagearr inspect remap` is a new CLI that prints the active remap rules per volume and their origin (`inferred (12/14 samples)` vs `config`).

### Worked examples

**Homelab QNAP (M1 deployment).** qBit/*arr see `/files/...`, Triagearr sees `/share/files/...`. Sampling matches every file under both prefixes. Inferred rule: `{from: /files/, to: /share/files/}`. No user config needed.

**TRaSH-guides single-namespace.** Everything mounts at `/data`. Sampled paths stat as-is. Inferred rule: identity. Logged once, no further noise.

**Per-category split.** qBit reports `/tv/Foo.mkv`, Triagearr indexes `/mnt/tv/Foo.mkv` and `/mnt/movies/...`. Inference produces `{from: /tv/, to: /mnt/tv/}` (and a second rule for movies after sampling Radarr). If the two rules conflict on a path, the longest matching `from:` wins.

**Failed inference.** A user mounts the wrong directory in Triagearr (`/share/files/torrents` instead of `/share/files`). Sampled qBit paths match (`torrents/...` is under the indexed root), but Radarr media paths don't → no single rule explains ≥80 % → refuse to start with the candidate distribution in the log. User adds a manual `path_remap` or fixes the bind.

## Consequences

**Easier:**
- Zero config for the common cases (single-namespace AND the bind-mismatch we hit on the homelab). The daemon "just works".
- The hard case (truly weird layouts) is loud, not silent — inference refuses to start with a diagnostic, instead of producing wrong mappings.
- Debugging is `triagearr inspect remap` away. The origin (inferred N/M vs config) is always visible.

**Harder:**
- Mapper must wait on the first qBit + *arr poll before answering queries. That window is bounded by `min(torrent_client_interval, arr_interval)` at worst (~30 min default), or by the explicit `--initial-sync` triggered at startup (the M1 daemon already does an immediate first poll on each poller, so practically the wait is ≤ a few seconds).
- Index walk cost. On a 200k-file library it's ~1-2 s of `os.ReadDir` on a warm cache. Acceptable for a once-per-boot operation; we don't re-walk on every poll.
- One more "are we ready" gate to handle in tests.

**Risk: index drift.**
A file moved between inference and the first scoring run could escape the index. Mitigation: we re-walk lazily on `Translate` cache misses (single dir, not the whole tree) before declaring a path unresolvable.

**Risk: ambiguity in tightly-deduplicated libraries.**
If the user has many `(basename, size)` collisions (cross-seed across categories with identical files), some samples won't match. As long as 5+ samples DO match and one rule dominates ≥80 %, inference still succeeds. Empirical confidence on the homelab library (262 torrents, no synthetic deduplication): every sample matched uniquely.

**Traded away:**
- Predictability of "what config did I write?" — the answer is now "look at the inspect CLI". Mitigated by logging the inferred rule loudly at boot.

## Alternatives considered

1. **Manual `path_remap` only (the first version of this ADR).** Rejected: pushes Docker bind semantics onto the user as a precondition for the daemon to work. Wrong default for a "homelab quality of life" tool.
2. **Parse `/proc/self/mountinfo` to detect Triagearr's bind layout.** Useful but insufficient: it tells us what Triagearr sees, not what qBit/*arr see. Inference via sampling source paths against the local index sidesteps this entirely.
3. **Stat-by-content walk per resolution (find the inode by full-tree walk every time).** Rejected: O(library) per call. The inference approach is a one-time O(library) cost paying for O(1) lookups thereafter.
4. **Run Triagearr in the same netns/mount-ns as qBit/*arr.** Rejected: forces a deployment topology onto users (and on the homelab, gluetun's netns specifically). Breaks single-binary use outside Docker.
