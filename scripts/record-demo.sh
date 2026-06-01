#!/usr/bin/env bash
# Record the README demo and bake it to an animated WebP, in one command.
#
# Boots the ARMED demo stack (config.demo.yml + the `demo` scenario) on a fresh
# DB, drives the dashboard through the autonomous-reap story with Playwright,
# converts the captured video, and tears the stack down. Re-run after a UI or
# scenario change to regenerate docs/assets/demo.webp.
#
# Deps: the dev stack (process-compose/watchexec/bun), Playwright + system
# Chrome (web/ devDep, driven via channel:chrome), and ffmpeg with libwebp.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR="$ROOT/.dev/demo"
OUTPUT="$ROOT/docs/assets/demo.webp"

command -v ffmpeg >/dev/null || { echo "[demo] ffmpeg not found (needed for WebP)" >&2; exit 1; }

# Playwright records video through its own bundled ffmpeg, which it refuses to
# install on unsupported distros (e.g. WSL's ubuntu26.04). The encode args are
# stock, so point its expected path at the system ffmpeg when it's missing.
PW_FFMPEG="$HOME/.cache/ms-playwright/ffmpeg-1011/ffmpeg-linux"
if [ ! -e "$PW_FFMPEG" ]; then
  echo "[demo] linking system ffmpeg for Playwright video capture"
  mkdir -p "$(dirname "$PW_FFMPEG")"
  ln -sf "$(command -v ffmpeg)" "$PW_FFMPEG"
fi

cleanup() { scripts/dev.sh down >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "[demo] resetting state + booting armed demo stack"
scripts/dev.sh down >/dev/null 2>&1 || true
rm -f .dev/triagearr-demo.db .dev/triagearr-demo.db-* 2>/dev/null || true
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR" "$(dirname "$OUTPUT")"

# The daemon config + scenario the recording expects. dev.sh exports SCENARIO;
# CONFIG is read by process-compose's daemon command (${CONFIG:-config.dev.yml}).
export SCENARIO=demo CONFIG=config.demo.yml
scripts/dev.sh up demo
scripts/dev.sh ready 120

echo "[demo] recording (Playwright → webm)"
# node resolves `playwright` from web/node_modules, so run with web/ as cwd;
# OUT_DIR is absolute so the video still lands in .dev/demo.
( cd web && OUT_DIR="$OUT_DIR" node demo/record.mjs )

WEBM="$(ls -t "$OUT_DIR"/*.webm 2>/dev/null | head -1 || true)"
[ -n "$WEBM" ] || { echo "[demo] no video produced" >&2; exit 1; }
echo "[demo] captured $(basename "$WEBM")"

echo "[demo] converting → $OUTPUT"
# 12 fps is plenty for a UI walkthrough and keeps the file small; lanczos
# downscale stays crisp on the gauge/score text. -loop 0 = loop forever.
ffmpeg -y -loglevel error -i "$WEBM" \
  -vf "fps=12,scale=1280:-1:flags=lanczos" \
  -loop 0 -compression_level 6 -q:v 62 \
  "$OUTPUT"

SIZE="$(du -h "$OUTPUT" | cut -f1)"
echo "[demo] done: $OUTPUT ($SIZE)"
