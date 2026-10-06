#!/usr/bin/env bash
# The desktop app's Install button (hub: POST /api/v1/app/install lane=mac,
# from the "Install desktop build N" card on any surface). The owner's click,
# not a session's, puts the build in.
#   ops/mac-apply.sh <build>
# A staged build (ops/install-mac.sh with the app open →
# ~/Library/Application Support/life-mac/next) at or past <build> is used as
# it is; otherwise this builds one first (install-mac.sh: staged while the
# app is open, installed and opened when it is not). Then the open app is
# asked to quit — the `apply` flag, which MacUpdate.swift polls; an app that
# does not quit is told to after 15 s — and ops/mac-swap.sh (already holding
# a copy beside the old app) swaps and reopens it within seconds. `relaunch`
# is that reopen request.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd .. && pwd)
# shellcheck disable=SC1091
source ./app-identity.sh
N=${1:?build number}
UPD="$HOME/Library/Application Support/life-mac"
NEXT="$UPD/next/life.app"
staged() { defaults read "$NEXT/Contents/Info.plist" CFBundleVersion 2>/dev/null || echo 0; }
running() { pgrep -f "^/Applications/life.app/Contents/MacOS/life" >/dev/null; }
swap_alive() { [ -f "$UPD/swap.pid" ] && kill -0 "$(cat "$UPD/swap.pid")" 2>/dev/null; }

if [ "$(staged)" -lt "$N" ]; then
  echo "▶ nothing staged at build $N (staged: $(staged)); building…"
  bash "$ROOT/ops/install-mac.sh"
  # Not running → install-mac.sh installed and opened it; the app reports
  # its build to the hub on launch and the card closes there.
  [ -d "$NEXT" ] || { echo "✔ installed and opened build $N"; exit 0; }
fi

if ! running; then
  # Staged but the app is closed: the swap runs now, and reopens it.
  swap_alive || { nohup bash "$ROOT/ops/mac-swap.sh" >/dev/null 2>&1 & disown; }
  touch "$UPD/relaunch"
  echo "✔ build $(staged) staged and the app is closed; swapping and opening it"
  exit 0
fi

mkdir -p "$UPD"
swap_alive || { nohup bash "$ROOT/ops/mac-swap.sh" >/dev/null 2>&1 & disown; }
touch "$UPD/relaunch" "$UPD/apply"
echo "▶ build $(staged) staged; asked the open app to quit and come back on it"
for _ in $(seq 1 15); do running || { echo "✔ the app quit; the updater swaps and reopens it"; exit 0; }; sleep 1; done
echo "▶ the app did not quit on its own; quitting it"
osascript -e "tell application id \"$LIFE_BUNDLE_ID.mac\" to quit" >/dev/null 2>&1 || true
for _ in $(seq 1 5); do running || { echo "✔ the app quit; the updater swaps and reopens it"; exit 0; }; sleep 1; done
pkill -TERM -f "^/Applications/life.app/Contents/MacOS/life" || true
echo "✔ the app was stopped; the updater swaps and reopens it"
