#!/usr/bin/env bash
# One-command hub lifecycle.
#   hub.sh install   build + (re)load launchd agents (hub, keepawake, renewcert)
#   hub.sh install-backup  (re)load the nightly backup agent (after backup.sh init)
#   hub.sh restart   rebuild + restart the hub only
#   hub.sh stop      unload the hub agent
#   hub.sh status    launchd state + healthz
#   hub.sh logs      tail the hub log
#   hub.sh url       print the console URL with the token baked in (first visit)
#   hub.sh web       open the console in the browser (sets the cookie for a year)
set -euo pipefail
cd "$(dirname "$0")"
SELF="$PWD/hub.sh"
ROOT=$(cd .. && pwd)
UID_=$(id -u)
AGENTS=~/Library/LaunchAgents
HOST=$(plutil -extract public_host raw -o - hub.json) # the one place the address lives

build() { make -s -C "$ROOT" build; } # signs the hub when TEAM_ID is set (Makefile: sign)
# rotate: on install/restart, a hub.log over 20 MB becomes hub.log.1 (the one
# kept copy, replacing the last) and the new hub starts a fresh file — it had
# grown to 138 MB (2026-09-14 cleanup). launchd reopens the path on start.
rotate() {
  local f=logs/hub.log
  [[ -f $f ]] || return 0
  if (( $(stat -f%z "$f") > 20*1024*1024 )); then mv -f "$f" "$f.1"; fi
}
# The com.life.*.plist files are templates: __HOME__ becomes $HOME and
# __CLAUDE_BIN_DIR__ the directory of hub.json claude_bin (launchd has no nvm
# PATH, and an npm-installed claude needs its node next to it).
render() { # $1 = label
  local bindir
  bindir=$(dirname "$(plutil -extract claude_bin raw -o - hub.json)")
  bindir=${bindir/#\~/$HOME}
  mkdir -p "$AGENTS"
  sed -e "s#__HOME__#$HOME#g" -e "s#__CLAUDE_BIN_DIR__#$bindir#g" "$1.plist" > "$AGENTS/$1.plist"
}
load() { # $1 = label
  render "$1"
  launchctl bootout "gui/$UID_/$1" 2>/dev/null || true
  launchctl bootstrap "gui/$UID_" "$AGENTS/$1.plist"
}

case "${1:-}" in
  install) build; mkdir -p logs; rotate; for a in com.life.hub com.life.keepawake com.life.renewcert; do load $a; done; sleep 1; "$SELF" status ;;
  install-backup) mkdir -p logs; load com.life.backup ;;
  restart) build; rotate; launchctl kickstart -k "gui/$UID_/com.life.hub"; sleep 1; "$SELF" status ;;
  stop)    launchctl bootout "gui/$UID_/com.life.hub" ;;
  status)  launchctl print "gui/$UID_/com.life.hub" | grep -E "state|pid" | head -3; curl -fsS --max-time 3 "https://$HOST/healthz" && echo ;;
  logs)    tail -f logs/hub.log ;;
  url)     echo "https://$HOST/?token=$(cat secrets/hub.token)" ;;
  web)     open "https://$HOST/?token=$(cat secrets/hub.token)" ;;
  *) sed -n '2,10p' "$0"; exit 1 ;;
esac
