#!/usr/bin/env bash
# Screenshot the desktop app, page by page, against the hub: each page is one
# launch of the fresh build with LIFE_TAB=<page>, and the app draws its own
# window into ops/logs/screens/mac/<page>.png and quits (MacShot.swift).
# Git-ignored: they show the owner's data. Usage: ops/mac-screens.sh [page ...]
#   pages: sessions recs calendar config settings (goals and spend are pages
#          behind Configuration and take a LIFE_TAB of their own),
#          thread:<id> (Sessions with that chat open), goal:<id> (that goal's
#          page), open:<id> (rec-…, cal-…, ask-…: its tab with that thing
#          open, as after a tap on it in a card).
# LIFE_CAL_DAY=YYYY-MM-DD puts the Calendar on that day's week.
# Needs a desktop build first (`make mac`, or `make mac-build` unsigned).
# WAIT=<seconds> for a slow page (default 6); W=<points> for a narrower
# window than 1280 (the minimum is 1100); H=<points> for a taller one than 860.
# STEPS=1 opens every run block in a thread shot and each of its rows.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/ops/logs/screens/mac"; mkdir -p "$OUT"
# The build just made, run as its own process beside any open copy: never
# /Applications, and never a quit of the installed app.
BIN="$ROOT/app/build-mac/Build/Products/Debug-maccatalyst/life.app/Contents/MacOS/life"
[ -x "$BIN" ] || { echo "no desktop build; run: make mac (or make mac-build)" >&2; exit 1; }
PAGES=("$@"); [ ${#PAGES[@]} -gt 0 ] || PAGES=(sessions recs calendar config settings)
for p in "${PAGES[@]}"; do
  tab="$p"; name="$p"; thread=""; goal=""; open=""
  case "$p" in
    thread:*) thread="${p#thread:}"; tab=sessions; name="thread-$thread";;
    goal:*) goal="${p#goal:}"; tab=goals; name="goal-$goal";;
    open:*) open="${p#open:}"; tab=sessions; name="open-$open";;
  esac
  rm -f "$OUT/$name.png"
  # The app quits itself after the shot; a watchdog kills a hung launch.
  # ApplePersistenceIgnoreState: a shot copy neither reads nor writes the
  # window state it shares (same bundle id) with the installed app, so a
  # killed one never leaves a "quit unexpectedly while reopening windows"
  # dialog behind. The copy also hides itself from the Dock.
  ApplePersistenceIgnoreState=YES \
  LIFE_TAB="$tab" LIFE_THREAD="$thread" LIFE_GOAL="$goal" LIFE_OPEN="$open" LIFE_SHOT="$OUT/$name.png" LIFE_SHOT_WAIT="${WAIT:-6}" LIFE_SHOT_W="${W:-}" LIFE_SHOT_H="${H:-}" LIFE_SHOT_STEPS="${STEPS:-}" \
    "$BIN" >>"$ROOT/ops/logs/mac-screens.log" 2>&1 &
  pid=$!
  ( sleep 60; kill "$pid" 2>/dev/null ) & dog=$!; disown "$dog"
  wait "$pid" 2>/dev/null || true
  kill "$dog" 2>/dev/null || true
  if [ -s "$OUT/$name.png" ]; then echo "✔ $OUT/$name.png"; else echo "✘ $name: no shot"; fi
done
