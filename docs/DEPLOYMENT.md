# Deployment

Triagearr is shipped as a single Docker image: `ghcr.io/triagearr/triagearr`. Releases follow SemVer; `:latest` always points at the last stable release.

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
  -p 9494:9494 \
  -v /opt/triagearr/config:/config \
  -v /opt/triagearr/data:/data \
  -v /share/files:/share/files:ro \
  -e TZ=Europe/Paris \
  -e TRIAGEARR_API_KEY=$(openssl rand -hex 32) \
  -e SONARR_API_KEY=... \
  -e RADARR_API_KEY=... \
  ghcr.io/triagearr/triagearr:latest
```

The container exposes:
- `9494/tcp` — HTTP API + UI
- (V2) `9495/tcp` — Prometheus `/metrics` if `metrics.bind` configured separately

Volumes:
- `/config` — must contain `config.yml`
- `/data` — SQLite DB lives here, must be persistent
- `/share/files` (or whatever you call your media root) — mount **read-only**, used only by the mapper to stat inodes

## Compose example (full *arr stack)

```yaml
# docker-compose.yml
services:
  triagearr:
    image: ghcr.io/triagearr/triagearr:latest
    container_name: triagearr
    restart: unless-stopped
    user: "${PUID}:${PGID}"
    environment:
      TZ: ${TZ}
      TRIAGEARR_API_KEY: ${TRIAGEARR_API_KEY}
      SONARR_API_KEY: ${SONARR_API_KEY}
      RADARR_API_KEY: ${RADARR_API_KEY}
      TELEGRAM_CHAT_ID: ${TELEGRAM_CHAT_ID}
      TELEGRAM_BOT_TOKEN: ${TELEGRAM_BOT_TOKEN}
    volumes:
      - ./config/triagearr.yml:/config/config.yml:ro
      - triagearr-data:/data
      - /share/files:/share/files:ro
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

volumes:
  triagearr-data:

networks:
  arr-net:
    external: true
```

## Integration with the reference homelab (Traefik + tinyauth)

For the QNAP+Traefik+Pocket-ID setup described in the parent project's CLAUDE.md, the snippet is:

```yaml
services:
  triagearr:
    image: ghcr.io/triagearr/triagearr:v0.1.0
    container_name: triagearr
    restart: unless-stopped
    user: "${TRIAGEARR_PUID}:${TRIAGEARR_PGID}"
    environment:
      TZ: Europe/Paris
      TRIAGEARR_API_KEY: ${TRIAGEARR_API_KEY}
      SONARR_API_KEY: ${SONARR_API_KEY}
      RADARR_API_KEY: ${RADARR_API_KEY}
      TELEGRAM_CHAT_ID: ${TELEGRAM_CHAT_ID}
      TELEGRAM_BOT_TOKEN: ${TELEGRAM_BOT_TOKEN}
    volumes:
      - /share/Container/homelab/cleanup/triagearr/config.yml:/config/config.yml:ro
      - /share/docker/triagearr/data:/data
      - /share/files:/share/files:ro
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

The container UID/GID must:
- Have **read** permission on `/share/files`
- Have **read+write** on `/data`
- Have **read** on `/config/config.yml`

On QNAP, the standard fix for ACL inheritance issues applies — `setfacl -bR /share/docker/triagearr/` after first deploy. The parent project's Taskfile does this automatically after rsync.

### SQLite file mode (host-side recommendation)

The daemon does **not** enforce a strict permission mode on `triagearr.db` —
Docker/UID mismatches across NAS setups (PUID, ACLs, share inheritance)
make a hard `chmod 0640` more disruptive than protective. The database is
written through the container UID and inherits the parent directory's
defaults.

If your host policy allows it, recommend:

```bash
chmod 600 /opt/triagearr/data/triagearr.db
chmod 600 /opt/triagearr/data/api_key
chmod 750 /opt/triagearr/data
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

Rollback: stop the container, restore `/data/triagearr.db.backup-<timestamp>` (auto-created before any migration), pull the previous image, start.

## Backups

The only stateful artifact is `/data/triagearr.db`. Standard SQLite backup pattern:

```bash
sqlite3 /data/triagearr.db ".backup /data/backup-$(date +%F).db"
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
docker volume rm <stack>_triagearr-data    # destroys all snapshots & audit history
```

The host-mounted media is **never** modified by uninstalling Triagearr.
