# Configuration

Triagearr is configured via a single YAML file (default `/config/config.yml`), with optional environment variable overlay for secrets. Hot reload on `SIGHUP`.

## Top-level structure

```yaml
mode: dry-run               # dry-run | live
http: { … }
storage: { … }
arrs:                       # seed-only (ADR-0022); DB is source of truth after first boot
  sonarr: { … }
  radarr: { … }
  lidarr: { … }
  whisparr_v2: { … }
  whisparr_v3: { … }
torrent_clients:            # seed-only (ADR-0025); keyed by client kind
  qbittorrent: { … }
maintainerr: { … }          # optional, V2
volume: { … }
polling: { … }
scoring: { … }              # global weights only; per-tracker policy lives in the DB (ADR-0026)
triggers: { … }
action: { … }
notifications: { … }
```

> **Two layers of config.** The YAML file holds daemon-level settings and *seeds* the connection/policy tables on first boot. After that, **\*arr connections** (`arrs:`), **torrent-client connections** (`torrent_clients:`), **scoring policy** (`scoring.per_tracker` / rare-content default) and **notification credentials** are owned by the database and edited from the dashboard — re-editing the YAML has no effect on existing rows (ADR-0022, ADR-0025, ADR-0026). The sections below note which keys are seed-only.

## `mode`

| Value | Behavior |
|---|---|
| `dry-run` *(default)* | All Actor steps are logged but no API call is made. Audit entries are created with `result: would_have_done`. **Safe.** |
| `live` | Destructive actions are executed. Requires explicit setting. |

Even in `live` mode, each *arr instance must have `act: true` for that *arr to be allowed to delete anything.

## `http`

```yaml
http:
  bind: "127.0.0.1:9494"    # listen address; "" disables the HTTP API entirely
  cors_origins: []          # for dev / standalone UI hosts (default: none)
  rate_limits:
    runs_per_minute: 1      # POST /api/v1/runs, per IP
    auth_per_minute: 5      # auth-mutating endpoints, per IP
```

There is **no `api_key` key here.** The API key is auto-generated (Sonarr-style)
to `${data_dir}/api_key` with `0600` perms on first boot — read it from that file.
Authentication itself is opt-in and managed at runtime (ADR-0019): until a user is
created from Settings → Security the API is open; once enabled, requests need a
session cookie or the `X-API-Key` header. See [docs/ARCHITECTURE.md](ARCHITECTURE.md#http-api).

## `storage`

Durations use Go's `time.Duration` syntax (`h`/`m`/`s`) — there is no `d` unit,
so a day is `24h`, a year `8760h`.

```yaml
storage:
  sqlite_path: /config/triagearr.db
  retention:
    snapshots_raw: 168h     # 7d — high-resolution snapshots
    snapshots_daily: 8760h  # 365d — downsampled daily aggregates
  vacuum:
    enabled: true
    min_reclaim_mb: 50      # only VACUUM if at least this much can be reclaimed
```

## `arrs.*` — one instance per *arr kind

Each *arr kind (`sonarr`, `radarr`, …) has **exactly one** instance — the kind is the identity. A homelab runs one Sonarr, one Radarr, one Lidarr, etc. Multi-instance per kind was dropped before V1 (zero real-world payoff, lots of key-composition complexity).

**Seed-only YAML (ADR-0022).** The block below seeds the `arr_connections` database table on first boot only. After that the table is the source of truth and is managed from the dashboard (Settings → *arr connections); edits to YAML have no effect on existing rows.

```yaml
arrs:
  sonarr:
    enabled: true                    # master switch for this kind
    url: http://sonarr:8989
    api_key: ${SONARR_API_KEY}
    poll: true                       # read state from this instance
    act: false                       # ❗ destructive opt-in, per kind
    tags_exclude: [triagearr-keep]   # *arr tags that protect media
    categories_only: []              # optional category filter

  radarr:
    enabled: true
    url: http://radarr:7878
    api_key: ${RADARR_API_KEY}
    poll: true
    act: false

  lidarr:
    enabled: false
  whisparr_v2:
    enabled: false
  whisparr_v3:
    enabled: false
```

### Common fields

| Field | Type | Default | Notes |
|---|---|---|---|
| `enabled` | bool | `false` | Master kill switch for this kind. |
| `url` | string | required when enabled | Base URL, no trailing slash. |
| `public_url` | string | `""` | Optional external URL; UI deep links (`/series/...`) prefer it over `url`. |
| `api_key` | string | required when enabled | API key from *arr settings. Use `${VAR}` for env interpolation. |
| `poll` | bool | `true` | Read-only access. |
| `act` | bool | `false` | Permission to delete media via this instance. |
| `tags_exclude` | []string | `[]` | Media tagged with any of these is never deleted. |
| `categories_only` | []string | `[]` | If non-empty, only media in these categories are considered. |
| `timeout` | duration | `30s` | HTTP timeout per call. |

## `torrent_clients` — one instance per kind (ADR-0025)

The download client is keyed by kind under `torrent_clients`, mirroring `arrs`.
Only the `qbittorrent` kind has a backend today; `transmission` / `deluge` /
`rtorrent` are UI scaffolds ("coming soon") and are rejected by the daemon if
enabled in YAML. Like `arrs`, this block is **seed-only (ADR-0025)** — it seeds
the `torrent_client_connections` table on first boot, after which the DB is the
source of truth and the connection is edited from Settings → Torrent clients.

```yaml
torrent_clients:
  qbittorrent:
    enabled: true
    url: http://gluetun:8090   # qbit shares gluetun's netns in many setups
    public_url: ""             # optional; UI deep links prefer this when set
    username: ""               # often empty if WebUI auth bypassed by network
    password: ""
    category_exclude: [keep, archive]
    tags_exclude: [forever, triagearr-keep]
    delete_with_files: true    # when nlink==1, delete files via qbit
    timeout: 30s
```

## `maintainerr` *(V2, optional)*

Read-only mirroring of Maintainerr collections, used to (a) cross-check that Triagearr's verdict aligns with Maintainerr's plan, and (b) honor a media item's "scheduled to delete" status in Maintainerr by potentially fast-tracking it.

```yaml
maintainerr:
  enabled: false
  url: http://maintainerr:6246
  api_key: ""                # optional; on internal Docker networks not required
```

## `volume`

The single volume Triagearr watches for disk pressure. Under the TRaSH-guides
convention (ADR-0023) there is one shared data root; multi-disk setups present
a unified mount via mergerfs/unionfs (see ADR-0024).

```yaml
volume:
  name: media                       # optional label
  path: /data                        # the TRaSH shared mount, as Triagearr sees it
  disk_pressure:
    enabled: true
    threshold_free_percent: 15      # fire if free < 15%
    target_free_percent: 25         # delete until free >= 25%
```

Per-run volume is capped by `action.max_deletions_per_run` (a count), not by a
byte budget — there is no `max_run_size_gb` key.

Triagearr, qBit and the *arrs all mount the same data root under the same
container path (ADR-0023). No path translation layer exists.

## `polling`

```yaml
polling:
  torrent_client_interval: 30m
  arr_interval: 1h
  arr_file_min_interval: 200ms   # spacing between per-file *arr calls (~5 req/s)
  disk_interval: 5m
  maintainerr_interval: 1h
  tracker_interval: 6h           # ADR-0009 — refresh per-tracker status from qBit
  downsample_cron: "0 3 * * *"   # daily 3 AM, after most user activity
```

## `scoring`

See [SCORING.md](SCORING.md) for the full algorithm. Snippet:

Only the **global weights**, the HnR window and the dead-tracker grace are YAML.
The **per-tracker policy** (`min_ratio`, `min_seed_days`, `rare_threshold`) and the
**rare-content default** moved into the database (ADR-0026): they are seeded with
conservative values on first boot (`min_ratio = 1.0`, `min_seed_days = 30`,
`rare_threshold = 3`) and edited from Settings → Scoring. There is no
`per_tracker:` / `rare_content_threshold:` YAML block any more.

```yaml
scoring:
  weights:
    ratio_obligation_met:  +50    # bonus for "tracker says we're safe"
    upload_velocity_inv:   +30    # inverse: low velocity → high score
    age_days:              +0.1   # per day of seed age
    seeders_low_guard:     -1000  # near-veto when seeders ≤ rare_threshold
    swarm_health_bonus:    +5     # if many seeders, we matter less
    tracker_dead_bonus:    +40    # ADR-0009 — fires when all trackers are dead for a sustained window

  hnr_window_days: 14             # never delete within this window of completion (or add, if unknown)
  tracker_dead_grace: 168h        # ADR-0009 — all trackers must be status=not_working this long before the bonus fires
```

The HnR-window veto weight itself (`-10000`) is hard-coded and non-configurable
(safety contract) — it is not a weight key.

## `triggers`

```yaml
triggers:
  schedule: "0 4 * * *"      # daily run at 4 AM; can be empty to disable cron
  disk_pressure: true        # also fire when the volume crosses threshold
  manual_api: true           # allow POST /api/v1/runs
```

## `action`

Safety caps on what Actor can do per run.

```yaml
action:
  max_deletions_per_run: 10
  inter_action_delay: 2s     # politeness pause between whole-torrent qBit deletes
  add_import_exclusion: false # when true, *arr adds deleted releases to its import-exclusion list
```

Retry (3 attempts, exponential backoff + jitter, ~10s budget) is built in and not
configurable. Cross-seed safety is also not a config knob: the Decider drops
candidates with `nlink > 2` and the Actor's T3.5 `nlink_check` aborts/skips a
delete if a sibling survives (see [HARDLINK_TOPOLOGY.md](HARDLINK_TOPOLOGY.md) and
[SCORING.md](SCORING.md#cross-seed-pre-filter-nlink--2)).

## `notifications`

A notification is sent **only** when a disk-pressure run reaches the Actor and
executed at least one candidate — manual HTTP/CLI runs stay silent (ADR-0021).
The message details the items deleted, their sizes, the total freed, and the
volume's free space before/after.

```yaml
notifications:
  telegram:
    enabled: false
    bot_token: "${TELEGRAM_BOT_TOKEN}"
    chat_id: "${TELEGRAM_CHAT_ID}"
```

| Key                  | Meaning                                                        |
| -------------------- | -------------------------------------------------------------- |
| `telegram.enabled`   | Master switch for the Telegram provider.                       |
| `telegram.bot_token` | Bot API token from BotFather. Required when enabled.           |
| `telegram.chat_id`   | Target chat/channel id. Required when enabled.                  |

`bot_token` and `chat_id` are also editable at runtime from the dashboard
(Settings → Notifications) — the `notifications` section is on the override
whitelist. The token is redacted from the effective-config view.

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
  bind: "127.0.0.1:9494"

storage:
  sqlite_path: /config/triagearr.db

arrs:
  sonarr:
    enabled: true
    url: http://sonarr:8989
    api_key: ${SONARR_API_KEY}
    poll: true
    act: true
  radarr:
    enabled: true
    url: http://radarr:7878
    api_key: ${RADARR_API_KEY}
    poll: true
    act: true

torrent_clients:
  qbittorrent:
    enabled: true
    url: http://gluetun:8090

volume:
  name: media
  path: /data
  disk_pressure:
    enabled: true
    threshold_free_percent: 15
    target_free_percent: 25

scoring:
  hnr_window_days: 14
  # rare-content threshold + per-tracker policy are DB-owned (ADR-0026),
  # edited from Settings → Scoring, not here.

triggers:
  schedule: "0 4 * * *"
  disk_pressure: true

action:
  max_deletions_per_run: 10

notifications:
  telegram:
    enabled: false
    bot_token: ${TELEGRAM_BOT_TOKEN}
    chat_id: ${TELEGRAM_CHAT_ID}
```

For a heavily-commented full reference, see [`config.example.yml`](../config.example.yml) at the repo root.
