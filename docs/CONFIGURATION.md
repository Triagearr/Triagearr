# Configuration

Triagearr is configured via a single YAML file (default `/config/config.yml`), with optional environment variable overlay for secrets. Hot reload on `SIGHUP`.

## Top-level structure

```yaml
mode: dry-run               # dry-run | live
http: { … }
storage: { … }
arrs:
  sonarr: [ … ]
  radarr: [ … ]
  lidarr: [ … ]
  readarr: [ … ]
  whisparr_v2: [ … ]
  whisparr_v3: [ … ]
qbit: { … }
maintainerr: { … }          # optional, V2
volumes: [ … ]
polling: { … }
scoring: { … }
triggers: { … }
action: { … }
notifications: { … }
```

## `mode`

| Value | Behavior |
|---|---|
| `dry-run` *(default)* | All Actor steps are logged but no API call is made. Audit entries are created with `result: would_have_done`. **Safe.** |
| `live` | Destructive actions are executed. Requires explicit setting. |

Even in `live` mode, each *arr instance must have `act: true` for that *arr to be allowed to delete anything.

## `http`

```yaml
http:
  bind: ":9494"             # listen address
  api_key: "${TRIAGEARR_API_KEY}"   # required when bind is not 127.0.0.1
  cors_origins: []          # for dev / standalone UI hosts (default: none)
```

## `storage`

```yaml
storage:
  sqlite_path: /data/triagearr.db
  retention:
    snapshots_raw: 30d      # high-resolution snapshots
    snapshots_daily: 365d   # downsampled daily aggregates
    audit_log: 365d         # per-decision audit trail
  vacuum:
    enabled: true
    min_reclaim_mb: 100     # only VACUUM if at least this much can be reclaimed
```

## `arrs.*` — per-instance *arr configuration

Each *arr type is a **list of instances**, so you can run multi-Sonarr (e.g. main + 4K), multi-Radarr, etc. **Empty lists are valid** and mean "don't poll this type".

```yaml
arrs:
  sonarr:
    - name: main             # unique within type, free text
      enabled: true          # master switch
      url: http://sonarr:8989
      api_key: ${SONARR_API_KEY}
      poll: true             # if false, instance is registered but never polled
      act: true              # if false, never delete via this instance
      tags_exclude: [triagearr-keep]   # *arr tags that protect from deletion
      categories_only: []    # optional category filter

    - name: anime
      enabled: false         # disabled — kept here for future use
      url: http://sonarr-anime:8989
      api_key: ${SONARR_ANIME_API_KEY}

  radarr:
    - name: main
      enabled: true
      url: http://radarr:7878
      api_key: ${RADARR_API_KEY}
      poll: true
      act: false             # read-only mode: poll it but never delete

  lidarr: []                 # no instances → disabled
  readarr: []
  whisparr_v2: []
  whisparr_v3: []
```

### Common fields

| Field | Type | Default | Notes |
|---|---|---|---|
| `name` | string | required | Unique within type. Used in logs and audit. |
| `enabled` | bool | `false` | Master kill switch. |
| `url` | string | required | Base URL, no trailing slash. |
| `api_key` | string | required | API key from *arr settings. Use `${VAR}` for env interpolation. |
| `poll` | bool | `true` | Read-only access. |
| `act` | bool | `false` | Permission to delete media via this instance. |
| `tags_exclude` | []string | `[]` | Media tagged with any of these is never deleted. |
| `categories_only` | []string | `[]` | If non-empty, only media in these categories are considered. |
| `timeout` | duration | `30s` | HTTP timeout per call. |

## `qbit`

Single qBittorrent instance (multi-qbit is not in V1 scope).

```yaml
qbit:
  enabled: true
  url: http://gluetun:8090   # qbit shares gluetun's netns in many setups
  username: ""               # often empty if WebUI auth bypassed by network
  password: ""
  category_exclude: [keep, archive]
  tags_exclude: [forever, triagearr-keep]
  delete_with_files: true    # when nlink==1, delete files via qbit
```

## `maintainerr` *(V2, optional)*

Read-only mirroring of Maintainerr collections, used to (a) cross-check that Triagearr's verdict aligns with Maintainerr's plan, and (b) honor a media item's "scheduled to delete" status in Maintainerr by potentially fast-tracking it.

```yaml
maintainerr:
  enabled: false
  url: http://maintainerr:6246
  api_key: ""                # optional; on internal Docker networks not required
```

## `volumes`

The volumes Triagearr watches for disk pressure. Each volume can have its own thresholds.

```yaml
volumes:
  - name: media
    path: /share/files               # path as Triagearr sees it (used by disk poller AND
                                     # as the root for path-remap inference)
    disk_pressure:
      enabled: true
      threshold_free_percent: 15    # fire if free < 15%
      target_free_percent: 25        # delete until free >= 25%
      max_run_size_gb: 50            # cap per run, even if target not reached
```

Multiple volumes can be declared (e.g. SSD cache + spinning array).

### Path remap (auto-inferred by default)

Triagearr needs to translate paths reported by qBit/*arr (e.g. `/files/torrents/...` as seen from within their container) into paths it can `stat()` locally (e.g. `/share/files/torrents/...` as bound into the Triagearr container). **This is inferred at startup** by sampling source paths against the local `volumes[].path` index — no config needed for the common cases. See `docs/adr/0010-path-remapping-for-mapper.md`.

If inference fails or is ambiguous (the daemon will refuse to start and log a diagnostic), or you simply want to pin the rules explicitly, add a `path_remap` block:

```yaml
volumes:
  - name: media
    path: /share/files
    path_remap:                      # optional override — skips inference when set
      - from: /files/
        to:   /share/files/
```

Rules are evaluated in order, first match wins. Each `to:` is stat-ed at startup; a missing directory is a hard error. To inspect what's active at runtime: `triagearr inspect remap`.

## `polling`

```yaml
polling:
  qbit_interval: 30m
  arr_interval: 1h
  disk_interval: 5m
  maintainerr_interval: 1h
  tracker_interval: 6h           # ADR-0009 — refresh per-tracker status from qBit
  downsample_cron: "0 3 * * *"   # daily 3 AM, after most user activity
```

## `scoring`

See [SCORING.md](SCORING.md) for the full algorithm. Snippet:

```yaml
scoring:
  weights:
    ratio_obligation_met:  +50    # bonus for "tracker says we're safe"
    upload_velocity_inv:   +30    # inverse: low velocity → high score
    age_days:              +0.1   # per day of seed age
    seeders_low_guard:     -1000  # near-veto when seeders ≤ rare_threshold
    swarm_health_bonus:    +5     # if many seeders, we matter less
    tracker_dead_bonus:    +40    # ADR-0009 — fires when all trackers are dead for a sustained window

  rare_content_threshold: 3       # seeders avg 7d ≤ 3 → protected (override absolute)
  hnr_window_days: 14             # never delete within this window of completion (or add, if unknown)
  tracker_dead_grace: 7d          # ADR-0009 — all trackers must be status=not_working for this long before bonus fires

  per_tracker:
    "tracker-prive.example.org":
      min_seed_days: 30
      min_ratio: 1.0
      rare_threshold: 5           # override the global rare_content_threshold
    "public-tracker.example":
      min_seed_days: 0
      min_ratio: 0.0
      rare_threshold: 0           # public trackers, no guard
```

## `triggers`

```yaml
triggers:
  schedule: "0 4 * * *"      # daily run at 4 AM; can be empty to disable cron
  disk_pressure: true        # also fire when any volume crosses threshold
  manual_api: true           # allow POST /api/v1/runs
```

## `action`

Safety caps on what Actor can do per run.

```yaml
action:
  max_deletions_per_run: 10
  inter_action_delay: 5s     # politeness pause between deletions
  retry:
    max_attempts: 3
    backoff: 30s
  cross_seed:
    on_conflict: skip        # skip | force_delete | warn_only
                             # skip   = leave cross-seeded torrent alive
                             # force  = delete anyway (the other seeder dies too)
                             # warn   = qbit delete metadata only, file kept
```

## `notifications`

```yaml
notifications:
  telegram:
    enabled: true
    chat_id: "${TELEGRAM_CHAT_ID}"
    bot_token: "${TELEGRAM_BOT_TOKEN}"
    on:
      - action_executed
      - action_failed
      - disk_pressure_triggered
      - health_degraded
  webhook:
    enabled: false
    url: ""
    headers: {}
```

## Environment variable substitution

Any value in the YAML can use `${VAR}` (or `${VAR:-default}`) syntax. The lookup is performed at load time and on `SIGHUP` reload. This is how secrets stay out of the config file — they live in the orchestrator's secret store (sops, Docker secrets, env file).

```yaml
api_key: ${SONARR_API_KEY}                    # required, fails if unset
url: ${SONARR_URL:-http://sonarr:8989}        # optional with default
```

## Validation

On startup (and on `SIGHUP`), Triagearr:
1. Parses the YAML, applies env overlay
2. Validates the schema (types, required fields, URL formats, cron syntax)
3. For each `enabled: true` instance, runs a health check (`HEAD /api/v3/health` or equivalent)
4. Logs a structured summary of what was loaded
5. Fails fast on any error — never starts with a partially-valid config

In `live` mode, additional pre-flight checks:
- At least one *arr instance has `act: true`
- The qbit client responds to `/api/v2/app/version`
- All volume paths exist and are readable

## Example: minimal production config

```yaml
mode: live

http:
  bind: ":9494"
  api_key: ${TRIAGEARR_API_KEY}

storage:
  sqlite_path: /data/triagearr.db

arrs:
  sonarr:
    - name: main
      enabled: true
      url: http://sonarr:8989
      api_key: ${SONARR_API_KEY}
      poll: true
      act: true
  radarr:
    - name: main
      enabled: true
      url: http://radarr:7878
      api_key: ${RADARR_API_KEY}
      poll: true
      act: true

qbit:
  enabled: true
  url: http://gluetun:8090

volumes:
  - name: media
    path: /share/files
    disk_pressure:
      enabled: true
      threshold_free_percent: 15
      target_free_percent: 25

scoring:
  rare_content_threshold: 3
  hnr_window_days: 14

triggers:
  schedule: "0 4 * * *"
  disk_pressure: true

action:
  max_deletions_per_run: 10

notifications:
  telegram:
    enabled: true
    chat_id: ${TELEGRAM_CHAT_ID}
    bot_token: ${TELEGRAM_BOT_TOKEN}
```

For a heavily-commented full reference, see [`config.example.yml`](../config.example.yml) at the repo root.
