# ADR-0016: Actor — arr-first, qBit-second, audit per-file, no FS

## Status

Accepted — 2026-05-21

## Context

M5's Actor turns a Decider plan (a run resolved to `mode: live` by
ADR-0015's gate) into actual deletions. The big shape was settled by
prior ADRs:

- **ADR-0003** fixes the order: *arr first (per-file `episodefile/{id}` or
  `moviefile/{id}` DELETE), then qBit (whole-torrent `POST
  /api/v2/torrents/delete?deleteFiles=true`). A *arr failure aborts cleanly
  before qBit is touched; an already-deleted *arr file is not rolled back
  because `nlink` stays ≥ 1 on the surviving torrent — disk impact is zero.
- **ADR-0012** moved the linker to full-API, so the Actor has no filesystem
  access available.
- **ADR-0014** kept volume attribution coarse at the Decider — per-file
  accounting was reserved for the Actor.
- **ADR-0015** gives the Actor a hard allow-list: pressure-driven runs go
  live automatically; HTTP/CLI need an explicit opt-in; cron stays
  dry-run forever.

The remaining open questions for M5: cross-seed safety, freed-bytes
accounting, retry strategy, audit granularity.

## Decision

**Pipeline.** Per candidate, in order:

1. `InsertAction(run_id, rank, hash, status=running)`
2. `LinksByHash(hash)` → 0..N `(arr_name, file_id)` pairs
3. For each link: `DeleteMediaFile(file_id, deleteFiles=true,
   addImportExclusion=cfg)` with retry on `triagearr.ErrTransient`. One
   `audit_log` row per call (`outcome ∈ {ok, failed, skipped,
   not_attempted}`). On any hard failure the remaining links become
   `not_attempted` and the action ends `aborted_arr_fail`. No rollback.
4. `qbit.Delete(hash, deleteFiles=true)` with the same retry policy. One
   `audit_log` row. On failure the action ends `failed_qbit`.
5. `FinishAction(succeeded, freed_bytes=size_bytes)`.

**Cross-seed safety net is out of scope for M5.** The HARDLINK_TOPOLOGY.md
T3.5 step (per-file `nlink` re-stat between *arr DELETE and qBit DELETE)
requires filesystem access, which ADR-0012 removed. Re-adding it
responsibly needs path translation, refuse-to-start checks when the share
isn't readable, and a config surface — too much for this milestone.
**Deferred to M8** along with its own ADR. The target homelab topology
does not practise cross-seeding, so the gap is acceptable in the interim.

**freed_bytes accounting.** Recorded as `item.SizeBytes` — what would be
freed in the no-cross-seed case. This is optimistic in the presence of
unobservable hardlink siblings, but the daemon has no API-only way to do
better. The audit_log narrates each step so a delta can be reconstructed
manually if needed.

**Retry.** Pure stdlib: 3 attempts, 500ms → 2s → 4s exponential, with
`crypto/rand` jitter, total budget under ~10s. Only `errors.Is(err,
triagearr.ErrTransient)` retries — 4xx (404, 401) is a hard fail. ADR-0001
(stdlib-first) is honoured; no new dep was needed.

**Audit granularity.** Per *arr file_id for `arr_delete`, one row for
`qbit_delete`. Sufficient to reconstruct "8 OK + 1 failed + 1
not-attempted" from `SELECT … WHERE action_id = ?` alone.

**Gates.** Three must align before any DELETE is issued:

1. `cfg.mode == live`
2. The *arr instance opted in (`act: true`) — surfaced via
   `registry.Deleter(name) (FileDeleter, bool)`; stub *arr types
   deliberately do not implement `FileDeleter`.
3. The run's `mode` resolved to `live` via `triagearr.ResolveRunMode`
   (ADR-0015).

The Actor checks (1) by reading `runs.mode`, (2) on each link, and (3) on
the `runs.triggered_by` allow-list. Defense in depth — any single
misconfiguration blocks the destructive call.

## Consequences

### Positive

- Pure-API path keeps the M3 → M4 → M5 chain consistent with ADR-0012;
  no FS coupling re-introduced.
- Audit per-file makes failure modes legible from the DB. The dashboard
  (M6) can render them without adding new state.
- Tests use in-process httptest fakes; the actor state machine is fully
  covered without container infra (testcontainers reclassed to M8).

### Negative (acknowledged)

- Cross-seed-aware users get no protection in M5. They must either avoid
  cross-seeding or wait for M8. CHANGELOG flags this explicitly.
- `freed_bytes` is reported even when the value is fictitious (cross-seed
  case). Operators reading it need to know the contract.

### Alternatives considered

- **DB-only cross-seed detection** (count distinct `download_id` per
  `file_id` in `arr_imports`) was sketched and dropped: it only catches
  cross-seed configurations where the second torrent was also imported
  through *arr, which is exactly the rare case. Real cross-seed setups
  use the cross-seed tool that bypasses *arr for the sibling torrent —
  so the DB has only one `arr_import` row and the detection misses.
- **Re-sampling disk_usage** for actual freed bytes was dropped after
  ADR-0015 settled: with cron observational and pressure auto, the
  re-sample is too noisy (concurrent writes during the sleep) and adds
  one HTTP-blocked sleep per candidate. Optimistic `size_bytes`
  accounting is honest enough and free.

## Revisit when

- The cross-seed M8 milestone lands — the safety net's contract may
  generalise to a per-action precondition that the Actor can call into.
- A user reports a `freed_bytes` discrepancy that materially misleads
  the dashboard or notifications.

## References

- `docs/HARDLINK_TOPOLOGY.md` — the pipeline narrative and the
  topological reason for arr-first/qBit-second.
- ADR-0003, ADR-0012, ADR-0014, ADR-0015 — the precursor decisions.
- `internal/actor/actor.go` — the implementation.
