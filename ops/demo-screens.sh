#!/usr/bin/env bash
# Photograph the real iPhone app (Simulator) showing the README's demo world,
# not your hub: ops/demo-hub.js serves ops/demo-fixtures.js on a local port,
# the simulator build is pointed at it (hub.baseURL in its UserDefaults, a
# throwaway token in the environment) and each screen is shot the way
# ops/screens.sh shoots the live one. Output: ops/logs/demo/<name>.png.
# Usage: ops/demo-screens.sh [tab ...]   tabs as ops/screens.sh takes them
#        (sessions, thread:<id>, more:config, goal:<id>, recs, calendar).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/app"
OUT="$ROOT/ops/logs/demo"; mkdir -p "$OUT"
PORT="${PORT:-8787}"
source "$ROOT/ops/app-identity.sh"
BUNDLE=$LIFE_BUNDLE_ID
TABS=("$@"); [ ${#TABS[@]} -gt 0 ] || TABS=(sessions "thread:weekly-investing-a4d1" calendar)

APP="$(ls -d build/Build/Products/Debug-iphonesimulator/Life.app 2>/dev/null || true)"
if [ -z "$APP" ]; then echo "no simulator build; run: make app-build-sim" >&2; exit 1; fi

SIM="${SIM:-iPhone 17}"
UDID="$(xcrun simctl list devices available -j | "$ROOT/ops/py.sh" sim-pick.py "$SIM")"
[ -n "$UDID" ] || { echo "no simulator named '$SIM'" >&2; exit 1; }

node "$ROOT/ops/demo-hub.js" "$PORT" & HUB=$!
trap 'kill $HUB 2>/dev/null || true; xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1 || true' EXIT
sleep 1

xcrun simctl boot "$UDID" 2>/dev/null || true
xcrun simctl bootstatus "$UDID" -b >/dev/null
xcrun simctl ui "$UDID" appearance "${APPEARANCE:-light}" >/dev/null 2>&1 || true
xcrun simctl install "$UDID" "$APP"
# The app reads its hub address from UserDefaults once, at launch.
xcrun simctl spawn "$UDID" defaults write "$BUNDLE" hub.baseURL "http://127.0.0.1:$PORT" >/dev/null
# The Calendar tab opens on the Day grid, the view an owner lives in (it
# remembers the last mode; CAL_MODE=schedule shoots the list instead).
xcrun simctl spawn "$UDID" defaults write "$BUNDLE" cal.mode "${CAL_MODE:-day}" >/dev/null

for tab in "${TABS[@]}"; do
  thread=""; more=""; goal=""; name="$tab"
  case "$tab" in thread:*) thread="${tab#thread:}"; tab=sessions; name="thread-$thread";; esac
  case "$tab" in more:*) more="${tab#more:}"; tab=more; name="more-$more";; esac
  case "$tab" in goal:*) goal="${tab#goal:}"; tab=more; more=config; name="goal-$goal";; esac
  xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1 || true
  SIMCTL_CHILD_LIFE_HUB_TOKEN="demo" SIMCTL_CHILD_LIFE_TAB="$tab" SIMCTL_CHILD_LIFE_THREAD="$thread" \
    SIMCTL_CHILD_LIFE_MORE="$more" SIMCTL_CHILD_LIFE_GOAL="$goal" SIMCTL_CHILD_LIFE_SCROLL="${LIFE_SCROLL:-}" \
    xcrun simctl launch "$UDID" "$BUNDLE" >/dev/null
  sleep "${WAIT:-6}"
  xcrun simctl io "$UDID" screenshot --type=png "$OUT/$name.png" >/dev/null
  echo "$OUT/$name.png"
done
