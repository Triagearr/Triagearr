# ADR-0023: The TRaSH-guides shared-mount convention is a hard requirement

## Status

Accepted — 2026-05-22

## Context

The Decider attributes a torrent to a volume by prefix-matching qBit's
`save_path` against `config.volumes[].path` (ADR-0014). This only works when
the path strings line up across systems — and in Docker they often don't.
Each container has its own mount namespace, so one inode can surface as
`/data/torrents/Foo.mkv` in qBit, `/tv/Foo.mkv` in Sonarr, and
`/share/files/torrents/Foo.mkv` in Triagearr.

We have already paid for this once. ADR-0010 tried to bridge the namespaces
with auto-inferred path-remap rules; it shipped in M2 and was retired within
days (ADR-0012) when the QNAP deployment hit UID/GID + ACL friction on a
bind-mounted media root. ADR-0012 reacted by going fully API-only, which in
turn leaves the M5 Actor's per-file `nlink` safety check
(`docs/HARDLINK_TOPOLOGY.md`, step T3.5) unimplementable — it is deferred to
M8 with the note "requires FS access; out of scope".

The root cause was never the namespaces themselves. It was that the M2
Triagearr container was mounted *inconsistently* with the rest of the stack:
qBit and *arr saw the shared storage at `/files/`, Triagearr saw it at
`/share/files/`. That is precisely the layout the TRaSH-guides — the de facto
reference for Plex/*arr/qBittorrent homelabs, and the topology Triagearr
already assumes (`README.md` § "Who it's for", `docs/HARDLINK_TOPOLOGY.md`) —
call out as wrong. From their Docker setup guide
(<https://trash-guides.info/File-and-Folder-Structure/How-to-set-up/Docker/>):

> "The default path setup … that encourages people to use mounts like
> `/movies`, `/tv`, `/books` or `/downloads` is very suboptimal and it makes
> them look like two or three file systems, even if they aren't … losing the
> ability to hardlink or instant move."

> "all your applications should have a consistent view of where your files
> and folders are."

ADR-0010 was, in effect, ~200 lines of inference engine plus filesystem
syscalls written to paper over a misconfigured container — to avoid asking
the operator to copy-paste one volume line they had already written for qBit,
Sonarr, Radarr and Plex.

A second, quieter symptom of the same confusion: `VolumeConfig.Path` is used
in two namespaces at once. `statfs(Path)` (the disk poller) needs Triagearr's
view; `strings.HasPrefix(save_path, Path)` (the Decider) needs qBit's view.
Both are correct only when the two views coincide — i.e. only under the TRaSH
convention.

## Decision

**Triagearr requires the TRaSH-guides shared-mount convention, extended to its
own container. No path-translation layer exists.**

1. **Single shared data root, one filesystem.** Each watched volume is one
   filesystem mount, holding both the download tree and the imported library
   (the TRaSH `data/torrents` + `data/media` layout). Per TRaSH, *"all your
   media files and folders [must] be in the same file system"*.

2. **Identical container path in every container — including Triagearr.** The
   shared root is mounted into the Triagearr container at the *same path* qBit
   and *arr use for it. The path itself is free — TRaSH: *"The `data` folder
   can be placed wherever you like"* — only the *consistency* is mandatory.

3. **`config.volumes[].path` is that container path.** It is valid
   simultaneously for `statfs()` and for `save_path` prefix matching, because
   under the convention there is only one namespace. The two-namespace
   overload of `Path` ceases to be a latent bug.

4. **Triagearr runs as the stack's shared PUID/PGID.** The distroless image
   ships `USER nonroot` (UID 65532); deployments override it with the compose
   `user:` directive to match the UID that owns the media — itself a TRaSH
   rule (*"recursively chown user and group"*, consistent `PUID`/`PGID`
   across services). This is what makes `stat()` on the shared mount succeed.

5. **No translation code.** Path-remap, inference (ADR-0010), Docker-socket
   introspection, and import-history "Rosetta Stone" reconstruction are all
   rejected and none ship. The Decider's prefix match (ADR-0014) is correct
   **by contract**, not by coincidence.

6. **Boot-time validation, fail loud.** At startup Triagearr samples a handful
   of qBit `save_path` values and *arr import paths and `stat()`s them in its
   own namespace. If they resolve, the convention holds and the daemon
   proceeds. If they do not, the deployment violates the convention: Triagearr
   logs a diagnostic naming the unresolved path and the expected mount, and
   **refuses to start**. There is no silent auto-correction — the failure is
   the operator's to fix, exactly as a misconfigured mount should be.

## Consequences

### Positive

- Zero path-translation code, zero new dependencies, zero new syscalls beyond
  the bounded boot-validation `stat()` sample.
- ADR-0014's prefix match is correct by contract; the `VolumeConfig.Path`
  two-namespace overload is resolved (one namespace by construction).
- The "save-path-on-a-different-volume" edge case ADR-0014 acknowledged is
  closed: the single-filesystem-per-volume rule means a torrent's downloads
  and its imported library are on the same volume.
- Triagearr becomes a well-behaved member of an existing TRaSH stack. It is
  the *last* container added; the ask is one volume line the operator already
  wrote four times.
- With the UID aligned, `stat()` on the shared mount is safe again. This
  unblocks the M5 Actor's per-file `nlink` cross-seed check
  (`HARDLINK_TOPOLOGY.md` T3.5), currently deferred to M8 as "requires FS
  access" — see "Revisit when".

### Negative / acknowledged

- Drops the aspiration of "works on any layout". But ADR-0010 already proved
  that aspiration is undeliverable without filesystem access, and filesystem
  access on a *non-conforming* layout is exactly what broke. We are not losing
  a capability we had.
- A non-conforming layout now causes a refuse-to-start instead of a silent
  best-effort. This is deliberate: a wrong mount means hardlinks and atomic
  moves are *already* broken for the operator's whole stack — failing loud
  pushes them to the layout where Triagearr (and the rest of *arr) works at
  all.
- A remote qBit with no shared filesystem (a seedbox) is unsupported. It
  always was: the hardlink topology Triagearr is built on requires a shared
  filesystem (`docs/HARDLINK_TOPOLOGY.md`). No regression.
- The image's default `sqlite_path` (`/data/triagearr.db`) collides with the
  TRaSH-canonical media root `/data`. Deployments whose shared mount is `/data`
  must set `sqlite_path` onto Triagearr's private volume (e.g.
  `/config/triagearr.db`). Documented in `docs/DEPLOYMENT.md`.

## Relationship to prior ADRs

- **ADR-0010** (path-remap inference) — already superseded by ADR-0012. This
  ADR closes the path-translation direction for good: the problem is removed
  by convention, not solved by code.
- **ADR-0012** (API-only mapping) — its motivation was the FS-access friction
  on a *misconfigured* mount. Under this convention `stat()` is safe; the
  "no FS access ever" stance is an over-correction. ADR-0012 is not reverted
  here, but the constraint it imposed on the M5 Actor is explicitly reopened
  (see "Revisit when").
- **ADR-0014** (Decider volume targeting) — not superseded. Reframed: the
  prefix match is now correct by contract. Annotated in place.

## Revisit when

- The boot-validation sample proves too strict in practice (false refusals on
  legitimate layouts) — relax the sample-resolution threshold rather than
  reintroduce translation.
- Multi-volume hardlink topologies become common — the convention already
  scales (N volumes = N consistent shared mounts), but `st_dev`-based
  attribution may be worth adopting over string-prefix for robustness.
- The M5/M8 `nlink` cross-seed check is picked up — this ADR removes the FS
  access blocker; ADR-0012's API-only constraint on the Actor should be
  formally revisited at that point.
