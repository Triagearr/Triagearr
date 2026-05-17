# ADR-0006: Each *arr type is configured as a list of instances, all opt-in

## Status

Accepted — 2026-05-17

## Context

Real-world *arr stacks rarely have just "one Sonarr." Common patterns:
- Main Sonarr + 4K Sonarr (different quality tiers)
- Main Sonarr + Anime Sonarr (different metadata source)
- Per-user Radarrs in shared homelabs
- Triagearr should support all the major *arrs: Sonarr, Radarr, Lidarr, Readarr, Whisparr (v2 and v3)

Additionally, the user explicitly asked for two independent dimensions per instance:
- **Poll**: should Triagearr collect data from this *arr?
- **Act**: should Triagearr be allowed to delete via this *arr?

The combination supports nuanced setups: e.g., observe a 4K Sonarr to understand the library but never delete from it; act on the main one only.

The reference precedent is Maintainerr, whose `RadarrSettings` and `SonarrSettings` entities allow multiple instances by storing them as relational rows keyed by `serverName`. Each instance has its own `url`, `apiKey`.

## Decision

Configuration represents each *arr type as a **list** in YAML:

```yaml
arrs:
  sonarr:
    - name: main
      enabled: true
      url: http://sonarr:8989
      api_key: ${SONARR_API_KEY}
      poll: true
      act: true
    - name: 4k
      enabled: false
      url: http://sonarr-4k:8989
      api_key: ${SONARR_4K_API_KEY}
  radarr:
    - name: main
      enabled: true
      url: http://radarr:7878
      api_key: ${RADARR_API_KEY}
      poll: true
      act: false       # read-only
  lidarr: []           # empty list = disabled
  readarr: []
  whisparr_v2: []
  whisparr_v3: []
```

Defaults:
- `enabled: false` — every instance must be explicitly enabled
- `act: false` — every instance must be explicitly authorized to act
- An empty list (or missing key) is valid and means "no instances of this type"

Internally, every *arr client implements the same `ArrInstance` interface (see `docs/ARCHITECTURE.md` § Key interfaces). A central registry exposes `registry.All()`, `registry.AllPolling()`, `registry.AllActing()`. Pollers and actor iterate these collections.

Whisparr v2 and v3 are listed separately because their APIs are not compatible (v3 is a near-rewrite).

## Consequences

**Easier:**
- Adding a new *arr (e.g., supporting a hypothetical "Mylar" comic *arr in V2) = one new client implementation, no schema change
- Per-instance opt-in matches the principle of least privilege
- "Read-only on this instance, full access on this other" is expressible without extra concepts
- Audit log can attribute every action to a specific named instance

**Harder:**
- Config schema is more complex than "one URL per *arr type"
- Users with single-instance setups have to write a list with one element (minor cognitive cost)

**Traded away:**
- The simpler "one URL per type" config model used by qbit_manage. Acceptable cost given the flexibility gain.

## Implementation notes

- Each client lives in its own package: `internal/clients/{sonarr,radarr,lidarr,readarr,whisparr_v2,whisparr_v3}`
- A factory in `internal/clients/registry/registry.go` builds the live set from config at startup
- Health checks run at startup and on a long interval; an unhealthy instance is logged and marked degraded but doesn't stop Triagearr from working with other instances
- The mapper resolves "this media belongs to which instance" by querying every polling instance and matching by path/title (when paths overlap across instances, the first match wins, with a warning)

## Re-evaluation triggers

- If users routinely run >10 instances and the config becomes unwieldy, consider a templating layer (`anchors` in YAML is already half the answer).
- If we ever support a *arr-like that doesn't fit the `ArrInstance` interface (e.g., something fundamentally non-media), introduce a separate interface — don't bend `ArrInstance`.

## References

- Maintainerr's settings entity: `apps/server/src/modules/settings/entities/radarr_settings.entities.ts`
- *arr API reference: https://servarr.com/api-docs
