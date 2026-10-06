#!/usr/bin/env bash
# The desktop app's updater (started by ops/install-mac.sh or ops/mac-apply.sh,
# never by hand). A build made while the app is open is STAGED, not installed,
# so the app is never pulled out from under the owner. This waits, quietly,
# until that app is no longer running — the Install card's click or the top
# bar's Update (MacUpdate.swift), or a plain Cmd-Q — then puts the staged
# build in /Applications. It reopens the app only when a click asked for it
# (the `relaunch` flag); a Cmd-Q stays quit.
#
# The copy happens WHILE the app still runs: the staged build is copied to
# /Applications/.life-next.app ahead of time (again whenever the staged plist
# changes — a re-stage), so the swap itself is two renames a beat after the
# quit, and `running` is re-read right before them.
set -uo pipefail
DIR="$HOME/Library/Application Support/life-mac"
NEXT="$DIR/next/life.app"
PRE=/Applications/.life-next.app
PIDF="$DIR/swap.pid"
mkdir -p "$DIR"
echo $$ > "$PIDF"
trap 'rm -f "$PIDF"' EXIT
log() { echo "$(date '+%F %T') $*" >> "$DIR/swap.log"; }
running() { pgrep -f "^/Applications/life.app/Contents/MacOS/life" >/dev/null; }
stamp() { stat -f %m "$NEXT/Contents/Info.plist" 2>/dev/null || echo 0; }
PREPPED=""
while [ -d "$NEXT" ]; do
  S=$(stamp)
  if [ "$S" != "$PREPPED" ]; then
    # Copy first, swap second: a failed copy leaves the old app where it was.
    rm -rf "$PRE"
    if ditto "$NEXT" "$PRE" && [ "$(stamp)" = "$S" ]; then
      PREPPED=$S
      log "copied build $(defaults read "$PRE/Contents/Info.plist" CFBundleVersion 2>/dev/null) beside the app; waiting for it to quit"
    else
      if [ "$(stamp)" = "$S" ]; then
        log "copy failed; kept the old app"
        rm -rf "$PRE" "$NEXT"
        exit 0
      fi
      continue  # re-staged mid-copy: copy the new one
    fi
  fi
  if ! running; then
    rm -rf /Applications/life.app
    mv "$PRE" /Applications/life.app
    rm -rf "$NEXT" "$DIR/apply"
    if [ -f "$DIR/relaunch" ]; then
      rm -f "$DIR/relaunch"
      # -n: launch THIS bundle. A plain `open` hands the request to any
      # running app with the same bundle id (a screenshot copy in a build
      # folder), which may quit after its shot.
      open -n /Applications/life.app
    fi
    log "swapped in $(defaults read /Applications/life.app/Contents/Info.plist CFBundleVersion 2>/dev/null)"
    exit 0
  fi
  sleep 1
done
rm -rf "$PRE"
