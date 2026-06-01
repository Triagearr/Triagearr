#!/usr/bin/env bash
# Record the README demo and bake it to an animated WebP, in one command.
#
# Boots the ARMED demo stack (config.demo.yml + the `demo` scenario) on a fresh
# DB, drives the dashboard through the autonomous-reap story with Playwright
# (grabbing deviceScaleFactor:2 screenshots), assembles them into an animated
# WebP, and tears the stack down. Re-run after a UI or scenario change to
# regenerate docs/assets/demo.webp.
#
# Deps: the dev stack (process-compose/watchexec/bun), Playwright + system
# Chrome (web/ devDep, driven via channel:chrome), and ffmpeg with libwebp.
# (No Playwright video → no bundled-ffmpeg dependency; screenshots are native.)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR="$ROOT/.dev/demo"
OUTPUT="$ROOT/docs/assets/demo.webp"

command -v ffmpeg >/dev/null || { echo "[demo] ffmpeg not found (needed for WebP)" >&2; exit 1; }

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

echo "[demo] recording (Playwright → 2x screenshots)"
# node resolves `playwright` from web/node_modules, so run with web/ as cwd;
# OUT_DIR is absolute so the frames + manifest still land in .dev/demo.
( cd web && OUT_DIR="$OUT_DIR" node demo/record.mjs )

MANIFEST="$OUT_DIR/frames.txt"
[ -f "$MANIFEST" ] || { echo "[demo] no frames produced" >&2; exit 1; }
echo "[demo] captured $(ls "$OUT_DIR"/f_*.png 2>/dev/null | wc -l) frames"

echo "[demo] assembling → $OUTPUT"
# The frames are 2560x1600 (deviceScaleFactor:2) lossless PNGs; the manifest
# carries each one's real on-screen duration. fps=12 resamples that variable
# timeline to a constant 12 fps that plays back at the captured wall-clock speed
# (long holds duplicate frames → the WebP encoder dedupes them, so the file
# stays small). Downscale to 900px with lanczos to match the README's display
# width 1:1 (GitHub's README column caps width near 900): a sharp supersample of
# the dense 2x capture, with no soft browser-side shrink.
# q:v 90: the source frames are pristine, so a high quality keeps text crisp and
# the focus beats' dimmed regions band-free; the deduped holds keep size down.
ffmpeg -y -loglevel error -f concat -safe 0 -i "$MANIFEST" \
  -vf "fps=12,scale=900:-1:flags=lanczos" \
  -loop 0 -compression_level 6 -q:v 90 \
  "$OUTPUT"

SIZE="$(du -h "$OUTPUT" | cut -f1)"
echo "[demo] done: $OUTPUT ($SIZE)"
