# Deployment

Triagearr is shipped as a single Docker image: `ghcr.io/triagearr/triagearr`. Releases follow SemVer; `:latest` always points at the last stable release.

## Mount & UID contract — read this first

Triagearr **requires** the [TRaSH-guides shared-mount layout](https://trash-guides.info/File-and-Folder-Structure/How-to-set-up/Docker/) — the same layout that makes hardlinks and atomic moves work for the rest of your *arr stack. This is a hard requirement, formalised in [ADR-0023](adr/0023-trash-shared-mount-convention.md). A non-conforming deployment is detected at boot and **refuses to start** with a diagnostic.

Two rules:

1. **Same mount, same path.** The shared data root — your `data/torrents` + `data/media` tree, on a single filesystem — must be mounted into the Triagearr container at the **identical container path** qBittorrent and your *arrs use for it. The path itself is free (`/data` is the TRaSH canonical) but it must match. If qBit reports `save_path: /data/torrents/…`, Triagearr must see that exact path. Per-app mounts like `/tv`, `/movies`, `/downloads` are **not** supported — they break the layout for your whole stack, not just Triagearr.

2. **Same UID.** The image runs as `nonroot` (UID 65532). Override it with the compose `user:` directive to the **same PUID/PGID that owns your media** — the one Sonarr, Radarr and qBittorrent already run as. That is what lets Triagearr `stat()` the shared mount. The distroless image has no shell, so LinuxServer-style `PUID`/`PGID` env vars do **not** apply — only `user:` works.

Why: Triagearr matches qBittorrent's `save_path` against your configured `volume.path` to decide which torrents sit on the pressured disk, and `statfs()`-es the same path for disk usage. With an inconsistent mount those strings never line up. The convention removes the problem instead of translating around it.

> **Naming clash:** the default `sqlite_path` is `/data/triagearr.db`. If your stack's shared mount is the TRaSH-canonical `/data`, point `sqlite_path` at Triagearr's own private volume instead (e.g. `/config/triagearr.db`) so the database and the media root do not collide. The examples below do this.

## Image variants

| Tag | Contents |
|---|---|
| `:vX.Y.Z` | Pinned release (recommended for production) |
| `:vX.Y` | Latest patch of a minor (auto-updates on patch releases) |
| `:vX` | Latest minor of a major |
| `:latest` | Most recent stable |
| `:edge` | Built from `main` (preview, expect breakage) |

Architectures: `linux/amd64`, `linux/arm64`. ARM v7 is **not** supported.

## Minimal `docker run`

```bash
docker run -d \
  --name triagearr \
  --restart unless-stopped \
  --user 1000:1000 \
  -p 9494:9494 \
  -v /opt/triagearr/config:/config \
  -v /mnt/user/data:/data:ro \
  -e TZ=Europe/Paris \
  -e TRIAGEARR_API_KEY=$(openssl rand -hex 32) \
  -e SONARR_API_KEY=... \
  -e RADARR_API_KEY=... \
  ghcr.io/triagearr/triagearr:latest
```

`--user 1000:1000` and the `/data` container path must match the rest of your
*arr stack — see the [Mount & UID contract](#mount--uid-contract--read-this-first)
above. Set `sqlite_path: /config/triagearr.db` in `config.yml` so the database
stays on the private `/config` volume.

The container exposes:
- `9494/tcp` — HTTP API + UI
- (V2) `9495/tcp` — Prometheus `/metrics` if `metrics.bind` configured separately

Volumes:
- `/config` — Triagearr's private state: `config.yml` plus the SQLite DB (when `sqlite_path` points here). Must be persistent.
- the shared data root — mounted **read-only** at the **same container path your *arr stack uses** (`/data` in the example). Triagearr reads it only to `stat()` paths for volume attribution and the boot-time layout check; every destructive op goes through *arr/qBit APIs.

## Compose example (full *arr stack)

```yaml
# docker-compose.yml
services:
  triagearr:
    image: ghcr.io/triagearr/triagearr:latest
    container_name: triagearr
    restart: unless-stopped
    # Same PUID:PGID that owns your media — the one Sonarr/Radarr/qBit run as.
    # Overrides the image's `nonroot` (65532) user; required to stat the mount.
    user: "${PUID}:${PGID}"
    environment:
      TZ: ${TZ}
      TRIAGEARR_API_KEY: ${TRIAGEARR_API_KEY}
      SONARR_API_KEY: ${SONARR_API_KEY}
      RADARR_API_KEY: ${RADARR_API_KEY}
      TELEGRAM_CHAT_ID: ${TELEGRAM_CHAT_ID}
      TELEGRAM_BOT_TOKEN: ${TELEGRAM_BOT_TOKEN}
    volumes:
      - ./config:/config                  # config.yml + SQLite DB (sqlite_path: /config/triagearr.db)
      - /mnt/user/data:/data:ro           # shared root — SAME container path as qBit/*arr
    ports:
      - "9494:9494"
    networks:
      - arr-net
    depends_on:
      - sonarr
      - radarr
      - qbittorrent
    healthcheck:
      test: ["CMD", "/triagearr", "health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 30s

networks:
  arr-net:
    external: true
```

The `/data` container path and `${PUID}:${PGID}` here are **not arbitrary** —
they must be identical to what `qbittorrent`, `sonarr` and `radarr` use in the
same compose file. That is the whole point of the [mount & UID
contract](#mount--uid-contract--read-this-first): one shared mount, one path,
one UID across the stack.

## Integration with the reference homelab (Traefik + tinyauth)

For the QNAP+Traefik+Pocket-ID setup described in the parent project's CLAUDE.md, the snippet is below. **Note the mount:** on this host qBittorrent and the *arrs see the shared storage at the container path `/files`, so Triagearr mounts host `/share/files` at `/files` too — *not* at `/share/files`. The original M2 deployment mounted it at `/share/files`, an inconsistent path; that is exactly the misconfiguration [ADR-0023](adr/0023-trash-shared-mount-convention.md) now rejects at boot.

```yaml
services:
  triagearr:
    image: ghcr.io/triagearr/triagearr:v0.1.0
    container_name: triagearr
    restart: unless-stopped
    # Shared stack PUID/PGID — identical to qBit/*arr on this host.
    user: "${PUID}:${PGID}"
    environment:
      TZ: Europe/Paris
      TRIAGEARR_API_KEY: ${TRIAGEARR_API_KEY}
      SONARR_API_KEY: ${SONARR_API_KEY}
      RADARR_API_KEY: ${RADARR_API_KEY}
      TELEGRAM_CHAT_ID: ${TELEGRAM_CHAT_ID}
      TELEGRAM_BOT_TOKEN: ${TELEGRAM_BOT_TOKEN}
    volumes:
      - /share/docker/triagearr/config:/config   # config.yml + SQLite DB (sqlite_path: /config/triagearr.db)
      - /share/files:/files:ro                   # shared root — container path /files matches qBit/*arr
    networks:
      - frontend
    labels:
      traefik.enable: "true"
      traefik.http.routers.triagearr.rule: "Host(`triagearr.${DOMAIN}`)"
      traefik.http.routers.triagearr.entrypoints: "websecure"
      traefik.http.routers.triagearr.middlewares: "tinyauth@docker"
      traefik.http.services.triagearr.loadbalancer.server.port: "9494"

networks:
  frontend:
    external: true
```

Tinyauth bypass for `/api/*` (so the React UI can fetch without auth bounce):

```env
# in stacks/infra/.env (sops-encrypted)
TINYAUTH_APPS_TRIAGEARR_PATH_ALLOW=^/api/.*
```

## Filesystem permissions

Run the container as the PUID/PGID that owns your media — the `user:` directive of the [Mount & UID contract](#mount--uid-contract--read-this-first). With the UID aligned, permissions follow for free:

- **read + traverse** on the shared data root — granted, because the same UID created that tree through *arr and qBittorrent;
- **read + write** on `/config` — Triagearr's own private volume.

The shared root is mounted `:ro`, so Triagearr cannot write to it even by accident; every destructive op goes through *arr/qBit APIs.

If you genuinely cannot run as the media-owning UID (heterogeneous ownership across the library), add the container to a shared media group with `group_add:` rather than loosening file modes on the library.

On QNAP, the ACL-inheritance fix still applies to Triagearr's **own** `/config` volume — `setfacl -bR /share/docker/triagearr/` after first deploy. The parent project's Taskfile does this automatically after rsync.

### SQLite file mode (host-side recommendation)

The daemon does **not** enforce a strict permission mode on `triagearr.db` —
Docker/UID mismatches across NAS setups (PUID, ACLs, share inheritance)
make a hard `chmod 0640` more disruptive than protective. The database is
written through the container UID and inherits the parent directory's
defaults.

If your host policy allows it, recommend:

```bash
chmod 600 /opt/triagearr/config/triagearr.db
chmod 600 /opt/triagearr/config/api_key
chmod 750 /opt/triagearr/config
```

after the first boot. `api_key` is auto-generated with `0600` already; the
SQLite database picks up the directory's umask.

## Secrets handling

Triagearr **never** reads secrets from the YAML file directly. Use environment variable substitution (`${VAR}` syntax in the YAML). Secrets come from:

- Docker Compose `environment:` block + a `.env` file (encrypted via sops in the reference homelab)
- Docker secrets (if you swarm)
- Kubernetes secrets (if you K8s)
- A vault-style sidecar (out of scope for V1)

The config validator refuses to start if a referenced `${VAR}` is unset and has no default.

## First run

1. Write the config file (`config.yml`), starting with `mode: dry-run` and at most a few *arr instances with `act: false`.
2. Run the container. Watch logs: `docker logs -f triagearr`.
3. Hit `http://localhost:9494/api/v1/health` to confirm liveness.
4. Open the UI: `http://localhost:9494/`. The first page is a setup wizard if no usable config is detected.
5. Let it observe for at least 24-48 hours. The scoring table won't be useful until `snapshots_raw` has enough data.
6. Inspect the UI's "Would-have-deleted" list. Are the candidates sensible? Adjust scoring weights if not.
7. When confident: flip one *arr instance to `act: true` and set `mode: live`. Watch the next run.

## Updating

```bash
docker pull ghcr.io/triagearr/triagearr:latest
docker compose up -d triagearr
```

Triagearr runs SQLite migrations automatically on startup. The DB is forward-compatible across minor versions; **major version bumps may include breaking schema changes** — always read the CHANGELOG before a major upgrade.

Rollback: stop the container, restore `triagearr.db.backup-<timestamp>` from the `/config` volume (auto-created before any migration), pull the previous image, start.

## Backups

The only stateful artifact is the SQLite DB on the `/config` volume. Standard SQLite backup pattern:

```bash
sqlite3 /config/triagearr.db ".backup /config/backup-$(date +%F).db"
```

Or use Litestream for continuous replication if you're paranoid.

## Resource budget

Triagearr is light:
- **Memory**: ~30-50 MB resident, even with thousands of torrents
- **CPU**: idle 99% of the time; ~10% of one core during a scoring run
- **Disk I/O**: a few KB/s during polls, brief bursts of MB/s during downsampling
- **Network**: a few API calls per poll cycle, dominated by *arr `?includeAll=true` responses

On a Raspberry Pi 4 or a QNAP TS-453D, you won't notice it running.

## Uninstalling

```bash
docker compose rm -sf triagearr
rm -rf ./config    # destroys config + DB: all snapshots & audit history
```

The host-mounted media is **never** modified by uninstalling Triagearr.
