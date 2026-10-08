#!/usr/bin/env bash
# Photograph the real desktop app showing the README's demo world, not your
# hub: ops/demo-hub.js serves ops/demo-fixtures.js on a local port and the
# fresh desktop build is launched the way ops/mac-screens.sh launches it
# (LIFE_SHOT: the app draws its own window and quits), pointed at that port
# with LIFE_HUB_URL + LIFE_HUB_TOKEN (read only on a shot run, never written
# to the installed app's settings). Output: ops/logs/demo/mac-<page>.png.
# Usage: ops/demo-mac-screens.sh [page ...]   pages as ops/mac-screens.sh takes
#        them (sessions recs calendar config, thread:<id>, goal:<id>).
# Needs a desktop build (`make mac-build`). W=, H=, WAIT= as in mac-screens.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/ops/logs/demo"; mkdir -p "$OUT"
PORT="${PORT:-8787}"
BIN="$ROOT/app/build-mac/Build/Products/Debug-maccatalyst/life.app/Contents/MacOS/life"
[ -x "$BIN" ] || { echo "no desktop build; run: make mac-build" >&2; exit 1; }
PAGES=("$@"); [ ${#PAGES[@]} -gt 0 ] || PAGES=(sessions "thread:weekly-investing-a4d1" "thread:honeymoon-destinations-c7a2" recs calendar config)

node "$ROOT/ops/demo-hub.js" "$PORT" & HUB=$!
trap 'kill $HUB 2>/dev/null || true' EXIT
sleep 1

for p in "${PAGES[@]}"; do
  tab="$p"; name="$p"; thread=""; goal=""
  case "$p" in
    thread:*) thread="${p#thread:}"; tab=sessions; name="thread-$thread";;
    goal:*) goal="${p#goal:}"; tab=goals; name="goal-$goal";;
  esac
  rm -f "$OUT/mac-$name.png"
  ApplePersistenceIgnoreState=YES \
  LIFE_HUB_URL="http://127.0.0.1:$PORT" LIFE_HUB_TOKEN="demo" \
  LIFE_TAB="$tab" LIFE_THREAD="$thread" LIFE_GOAL="$goal" LIFE_OPEN="" LIFE_SHOT="$OUT/mac-$name.png" LIFE_SHOT_WAIT="${WAIT:-6}" LIFE_SHOT_W="${W:-}" LIFE_SHOT_H="${H:-}" LIFE_SHOT_STEPS="" \
    "$BIN" >>"$ROOT/ops/logs/mac-screens.log" 2>&1 &
  pid=$!
  ( sleep 60; kill "$pid" 2>/dev/null ) & dog=$!; disown "$dog"
  wait "$pid" 2>/dev/null || true
  kill "$dog" 2>/dev/null || true
  if [ -s "$OUT/mac-$name.png" ]; then echo "✔ $OUT/mac-$name.png"; else echo "✘ $name: no shot"; fi
done
