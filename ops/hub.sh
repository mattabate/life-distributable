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
# The four launchd agents, written here into ~/Library/LaunchAgents (never
# load one by hand). hub: the server, kept alive, with the directory of
# hub.json claude_bin on its PATH (launchd has no nvm PATH, and an
# npm-installed claude needs its node next to it). keepawake: caffeinate -s,
# no idle sleep while on AC power, so the hub and its sessions stay reachable
# (on battery the Mac sleeps normally; no sudo, unlike pmset). renewcert:
# Tailscale certs last ~90 days, so renew-cert.sh runs on the 1st at 04:00.
# backup: backup.sh nightly at 03:30, loaded separately once restic.env exists.
agent_body() { # $1 = label; the <dict> entries after Label
  case "$1" in
    com.life.hub) cat <<EOF
  <key>ProgramArguments</key>
  <array><string>$ROOT/ops/bin/hub</string><string>-config</string><string>$ROOT/ops/hub.json</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$BINDIR:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
    <key>HOME</key><string>$HOME</string>
    <key>LANG</key><string>en_US.UTF-8</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>$ROOT/ops/logs/hub.log</string>
  <key>StandardErrorPath</key><string>$ROOT/ops/logs/hub.log</string>
EOF
    ;;
    com.life.keepawake) cat <<EOF
  <key>ProgramArguments</key>
  <array><string>/usr/bin/caffeinate</string><string>-s</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
EOF
    ;;
    com.life.renewcert) cat <<EOF
  <key>ProgramArguments</key>
  <array><string>$ROOT/ops/renew-cert.sh</string></array>
  <key>StartCalendarInterval</key>
  <dict><key>Day</key><integer>1</integer><key>Hour</key><integer>4</integer><key>Minute</key><integer>0</integer></dict>
  <key>StandardOutPath</key><string>$ROOT/ops/logs/renew-cert.log</string>
  <key>StandardErrorPath</key><string>$ROOT/ops/logs/renew-cert.log</string>
EOF
    ;;
    com.life.backup) cat <<EOF
  <key>ProgramArguments</key>
  <array><string>$ROOT/ops/backup.sh</string></array>
  <key>StartCalendarInterval</key>
  <dict><key>Hour</key><integer>3</integer><key>Minute</key><integer>30</integer></dict>
  <key>StandardOutPath</key><string>$ROOT/ops/logs/backup.log</string>
  <key>StandardErrorPath</key><string>$ROOT/ops/logs/backup.log</string>
EOF
    ;;
    *) echo "hub.sh: no agent named $1" >&2; return 1 ;;
  esac
}
render() { # $1 = label
  BINDIR=$(dirname "$(plutil -extract claude_bin raw -o - hub.json)")
  BINDIR=${BINDIR/#\~/$HOME}
  mkdir -p "$AGENTS"
  {
    echo '<?xml version="1.0" encoding="UTF-8"?>'
    echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">'
    echo '<plist version="1.0">'
    echo '<dict>'
    echo "  <key>Label</key><string>$1</string>"
    agent_body "$1"
    echo '</dict>'
    echo '</plist>'
  } > "$AGENTS/$1.plist"
  plutil -lint -s "$AGENTS/$1.plist"
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
