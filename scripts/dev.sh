#!/usr/bin/env bash
# Drive the local dev stack (fakes + daemon + vite) via process-compose.
#
# Non-blocking and command-driven: `up` runs detached, and every other verb
# pokes the running instance over a pinned unix socket — so an agent (or you)
# can start it, query it, restart a single service, tail one log, and tear it
# down without holding a terminal. Stack topology lives in process-compose.yaml.
#
# Usage:
#   scripts/dev.sh up [SCENARIO]     boot the stack detached (SCENARIO=default)
#   scripts/dev.sh down              stop everything
#   scripts/dev.sh status            per-process state table
#   scripts/dev.sh logs <svc> [-f]   tail a service's logs (fakes|daemon|vite|watch|build)
#   scripts/dev.sh restart <svc>     restart one service
#   scripts/dev.sh ready [timeout]   block until daemon + vite answer (default 90s)
#   scripts/dev.sh attach            open the process-compose TUI (for humans)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mkdir -p .dev

export PC_SOCKET_PATH="$ROOT/.dev/pc.sock"
PC=(process-compose -U -u "$PC_SOCKET_PATH")
COMPOSE=("${PC[@]}" -f "$ROOT/process-compose.yaml")

running() { "${PC[@]}" project state >/dev/null 2>&1; }

cmd="${1:-}"
[ $# -gt 0 ] && shift || true

case "$cmd" in
  up)
    if running; then
      echo "[dev] stack already up — scripts/dev.sh status" >&2
      exit 0
    fi
    rm -f "$PC_SOCKET_PATH"
    # Stub dirs the fake qBit save_paths reference, so the daemon's ADR-0023
    # preflight mount check passes without Docker.
    mkdir -p /tmp/triagearr-dev/torrents/tv \
             /tmp/triagearr-dev/torrents/movies \
             /tmp/triagearr-dev/torrents/orphans
    export SCENARIO="${1:-${SCENARIO:-default}}"
    echo "[dev] starting stack (scenario=$SCENARIO) detached"
    "${COMPOSE[@]}" up --detached --tui=false --log-file "$ROOT/.dev/process-compose.log"
    echo "[dev] up. next: scripts/dev.sh ready   (UI: http://localhost:5173)"
    ;;
  down)
    running || { echo "[dev] not running"; exit 0; }
    "${PC[@]}" down "$@"
    rm -f "$PC_SOCKET_PATH"
    ;;
  status|ps)
    "${PC[@]}" process list "$@"
    ;;
  logs)
    [ $# -ge 1 ] || { echo "usage: dev.sh logs <svc> [-f]" >&2; exit 2; }
    "${PC[@]}" process logs "$@"
    ;;
  restart)
    [ $# -ge 1 ] || { echo "usage: dev.sh restart <svc>" >&2; exit 2; }
    "${PC[@]}" process restart "$@"
    ;;
  ready)
    timeout="${1:-90}"
    deadline=$(( $(date +%s) + timeout ))
    for url in "http://127.0.0.1:9494/healthz" "http://127.0.0.1:5173/"; do
      until curl -fsS -o /dev/null --max-time 2 "$url"; do
        [ "$(date +%s)" -lt "$deadline" ] || { echo "[dev] timeout waiting for $url" >&2; exit 1; }
        sleep 1
      done
      echo "[dev] ready: $url"
    done
    ;;
  attach)
    "${PC[@]}" attach
    ;;
  *)
    sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
