#!/usr/bin/env bash
# Photograph the console at the window widths people actually use — full
# screen, a Magnet half, a Magnet third — so a layout change is LOOKED AT
# narrow before it ships (a composer hint line once stood six lines tall in a
# half-screen window, and the nav's last tabs were off the right edge).
#
# Usage: make web-widths [ROUTES='#/recs #/money']     default: the chat of the newest
#        session, Sessions, Recs, Calendar, Configuration
# Output: ops/logs/web/widths/<width>/NN-<route>.png (git-ignored).
# Each width is one headless Chrome (ops/browse.js --size); pictures are the
# viewport only, so what is off screen stays off screen, as it is for you.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
NODE="$(command -v node || ls "$HOME"/.nvm/versions/node/*/bin/node 2>/dev/null | tail -1)"
OUT="$ROOT/ops/logs/web/widths"
WIDTHS="${WIDTHS:-1500 980 820 720 480}"

routes=("$@")
if [ ${#routes[@]} -eq 0 ]; then
  tid="$(ops/bin/lifectl threads | sed -n 's/.*"id": "\([^"]*\)".*/\1/p' | head -1)"
  routes=("#/sessions/$tid" "#/sessions" "#/recs" "#/calendar" "#/config")
fi

for w in $WIDTHS; do
  h=1000; [ "$w" -lt 900 ] && h=900
  steps=""
  for r in "${routes[@]}"; do
    # `make web-widths ROUTES='recs money'` — a bare `#` would start a shell
    # comment in the recipe, so a route may be given without its `#/`.
    r="#/${r#\#/}"
    name="${r#\#/}"; name="${name//\//-}"
    steps+="goto $r; eval window.__fullHeight=function(){return $h}; wait 2000; shotv $name; "
  done
  rm -rf "$OUT/$w"
  "$NODE" ops/browse.js --size "${w}x${h}" --out "$OUT/$w" "$steps" | tail -1 | sed "s/^/$w: /"
done
find "$OUT" -name '*.png' | sort
