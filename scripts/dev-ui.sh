#!/usr/bin/env bash
# Boot the full local dev stack:
#   1. fake Sonarr/Radarr/qBit (cmd/devfixtures)
#   2. triagearr daemon with config.dev.yml (dry-run, points at the fakes)
#   3. Vite dev server (React UI)
#
# Ctrl-C tears down all three. No process leaks if any of them dies early —
# the SIGTERM propagates to siblings via the EXIT trap.
#
# Usage:
#   ./scripts/dev-ui.sh [SCENARIO]
# where SCENARIO defaults to "default" and resolves to
#   fixtures/scenarios/$SCENARIO.yaml

set -euo pipefail

SCENARIO="${1:-default}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SCENARIO_PATH="fixtures/scenarios/${SCENARIO}.yaml"
if [ ! -f "$SCENARIO_PATH" ]; then
  echo "scenario not found: $SCENARIO_PATH" >&2
  echo "available scenarios:" >&2
  ls fixtures/scenarios/*.yaml 2>/dev/null | sed 's|.*/||;s|\.yaml$||' >&2
  exit 1
fi

mkdir -p .dev
PIDS=()

cleanup() {
  echo
  echo "[dev-ui] shutting down ($(date +%T))"
  for pid in "${PIDS[@]:-}"; do
    if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  # Give them a moment, then SIGKILL stragglers.
  sleep 1
  for pid in "${PIDS[@]:-}"; do
    if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
}
trap cleanup EXIT INT TERM

echo "[dev-ui] scenario: $SCENARIO_PATH"
echo "[dev-ui] starting fakes (sonarr:18989 radarr:17878 qbit:18090)"
go run ./cmd/devfixtures --scenario "$SCENARIO_PATH" &
PIDS+=($!)

# Wait until the three fakes accept connections before booting triagearr,
# so the first poll succeeds instead of logging connection-refused errors.
for port in 18989 17878 18090; do
  for _ in $(seq 1 50); do
    if (echo > /dev/tcp/127.0.0.1/$port) 2>/dev/null; then break; fi
    sleep 0.1
  done
done

echo "[dev-ui] starting triagearr daemon (config.dev.yml)"
go run ./cmd/triagearr serve --config config.dev.yml &
PIDS+=($!)

# Triagearr needs a moment to migrate the SQLite schema before vite hits its API.
sleep 1

echo "[dev-ui] starting vite (http://localhost:5173)"
(cd web && bun run dev) &
PIDS+=($!)

echo "[dev-ui] all up. Ctrl-C to stop."
wait
