#!/usr/bin/env bash
# One-command hub lifecycle.
#   hub.sh install   build + (re)load launchd agents (hub, keepawake, renewcert)
#   hub.sh install-backup  (re)load the nightly backup agent (after backup.sh init)
#   hub.sh restart   rebuild + restart the hub only (after running actions; --now: not)
#   hub.sh stop      unload the hub agent
#   hub.sh status    launchd state + healthz
#   hub.sh logs      tail the hub log
#   hub.sh url       print the console URL with a one-minute login code (first visit; a Terminal only)
#   hub.sh web       open the console in the browser (sets the cookie for a year)
#   hub.sh token     print the owner's hub token, for Apple Passwords (a Terminal only)
# On Linux the same commands drive systemd user units in ~/.config/systemd/user
# (life-hub.service, life-renewcert.timer, life-backup.timer) instead.
set -euo pipefail
cd "$(dirname "$0")"
SELF="$PWD/hub.sh"
ROOT=$(cd .. && pwd)
UID_=$(id -u)
OS=$(uname -s)
AGENTS=~/Library/LaunchAgents
# cfg <key>: one hub.json value (plutil on a Mac, jq on Linux); fails when unset.
cfg() {
  if [[ $OS == Darwin ]]; then plutil -extract "$1" raw -o - hub.json
  else jq -er --arg k "$1" '.[$k] // empty' hub.json; fi
}
HOST=$(cfg public_host) # the one place the address lives

build() { make -s -C "$ROOT" build; } # signs the hub when TEAM_ID is set (Makefile: sign)
# rotate: on install/restart, a hub.log over 20 MB becomes hub.log.1 (the one
# kept copy, replacing the last) and the new hub starts a fresh file — it had
# grown to 138 MB (2026-09-14 cleanup). launchd reopens the path on start.
rotate() {
  local f=logs/hub.log
  [[ -f $f ]] || return 0
  if (( $(wc -c < "$f") > 20*1024*1024 )); then mv -f "$f" "$f.1"; fi
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
  BINDIR=$(dirname "$(cfg claude_bin)")
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

# Linux: the same jobs as systemd user units. life-hub.service is the hub
# (restarted on crash; KillMode=process so a restart leaves the sessions'
# tmux server and running actions alone, as launchd does); the timers run
# renew-cert.sh on the 1st at 04:00 and backup.sh nightly at 03:30. No
# keep-awake unit: SETUP.md turns off suspend instead. The units start at
# boot and outlive a logout only with `loginctl enable-linger`.
UNITS=~/.config/systemd/user
[[ $OS == Darwin ]] || export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$UID_}
sd() { systemctl --user "$@"; }
unit_body() { # $1 = unit file name
  case "$1" in
    life-hub.service) cat <<EOF
[Unit]
Description=life hub
After=network-online.target

[Service]
ExecStart=$ROOT/ops/bin/hub -config $ROOT/ops/hub.json
Environment=PATH=$BINDIR:/usr/local/bin:/usr/bin:/bin
Environment=HOME=$HOME
Environment=LANG=C.UTF-8
Restart=always
RestartSec=5
KillMode=process
StandardOutput=append:$ROOT/ops/logs/hub.log
StandardError=append:$ROOT/ops/logs/hub.log

[Install]
WantedBy=default.target
EOF
    ;;
    life-renewcert.service|life-backup.service) local job=${1#life-}; job=${job%.service}
      [[ $job == renewcert ]] && job=renew-cert
      cat <<EOF
[Unit]
Description=life $job

[Service]
Type=oneshot
ExecStart=$ROOT/ops/$job.sh
StandardOutput=append:$ROOT/ops/logs/$job.log
StandardError=append:$ROOT/ops/logs/$job.log
EOF
    ;;
    life-renewcert.timer|life-backup.timer)
      local when='*-*-01 04:00:00'; [[ $1 == life-backup.timer ]] && when='*-*-* 03:30:00'
      cat <<EOF
[Unit]
Description=life ${1%.timer} schedule

[Timer]
OnCalendar=$when
Persistent=true

[Install]
WantedBy=timers.target
EOF
    ;;
    *) echo "hub.sh: no unit named $1" >&2; return 1 ;;
  esac
}
unit() { # $1 = unit file name: write it
  BINDIR=$(dirname "$(cfg claude_bin)")
  BINDIR=${BINDIR/#\~/$HOME}
  mkdir -p "$UNITS"
  unit_body "$1" > "$UNITS/$1"
}
linger_note() {
  [[ $(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null) == yes ]] ||
    echo "note: run \`sudo loginctl enable-linger $(id -un)\` once, so the hub starts at boot and keeps running after you log out"
}

case "${1:-}" in
  install) build; mkdir -p logs; rotate
           if [[ $OS == Darwin ]]; then for a in com.life.hub com.life.keepawake com.life.renewcert; do load $a; done
           else for u in life-hub.service life-renewcert.service life-renewcert.timer; do unit $u; done
                sd daemon-reload; sd enable life-hub.service life-renewcert.timer
                sd restart life-hub.service; sd start life-renewcert.timer; linger_note; fi
           sleep 1; "$SELF" status ;;
  install-backup) mkdir -p logs
           if [[ $OS == Darwin ]]; then load com.life.backup
           else unit life-backup.service; unit life-backup.timer; sd daemon-reload; sd enable --now life-backup.timer; fi ;;
  # An approved action still running is stopped by the next hub (its group
  # outlives this one; closeOrphans kills it) and comes back "uncertain". Wait for those first, up to 31 min (the action timeout is
  # 30), never on the action this restart runs inside (LIFE_ACTION: a land's
  # own `make build && nohup … restart` inherits it). --now skips the wait.
  restart) build; rotate
           if [[ "${2:-}" != "--now" ]]; then
             q="select count(*) from items where src='action' and state='running' and id<>'${LIFE_ACTION:-}'"
             bad=0
             for i in $(seq 1 1860); do
               # A read that fails is not zero: retry it, and after 10 in a
               # row refuse rather than kill an action we could not see.
               n=$("$ROOT/ops/db.sh" "$q" 2>/dev/null) || n=""
               if ! [[ "$n" =~ ^[0-9]+$ ]]; then
                 (( ++bad < 10 )) || { echo "✘ cannot read running actions; not restarting (--now forces it)" >&2; exit 1; }
                 sleep 1; continue
               fi
               bad=0
               (( n > 0 )) || break
               (( i % 60 == 1 )) && echo "▶ waiting on $n running action(s) before the restart"
               sleep 1
             done
           fi
           if [[ $OS == Darwin ]]; then launchctl kickstart -k "gui/$UID_/com.life.hub"
           else sd restart life-hub.service; fi
           sleep 1; "$SELF" status ;;
  stop)    if [[ $OS == Darwin ]]; then launchctl bootout "gui/$UID_/com.life.hub"
           else sd stop life-hub.service; fi ;;
  status)  if [[ $OS == Darwin ]]; then launchctl print "gui/$UID_/com.life.hub" | grep -E "state|pid" | head -3
           else echo "state = $(sd show -p SubState --value life-hub.service)"
                echo "pid = $(sd show -p MainPID --value life-hub.service)"; fi
           su_=$(cfg session_user 2>/dev/null || true)
           if [[ -n $su_ ]]; then echo "sessions run as: $su_"; else echo "sessions run as: $(id -un) (the owner: the gate is a rule, not a wall)"; fi
           curl -fsS --max-time 3 "https://$HOST/healthz" && echo ;;
  logs)    tail -f logs/hub.log ;;
  # A session user has only its own token (LIFE_SESSION_TOKEN): the console
  # opens as that session, so browse.js and web-smoke.sh work from its clone.
  # The owner's token only to a person at a Terminal (stdin a tty, as
  # decider-set.sh): run from a session it put the token in the session's
  # transcript (#129). web-smoke.sh builds its own URL.
  # The URL carries a one-minute single-use login code, never the token: a
  # ?token= link stayed in the browser's history (#161). The token goes to
  # curl on stdin, so no process listing shows it either.
  url)     if [[ -n "${LIFE_SESSION_TOKEN:-}" ]]; then tok=$LIFE_SESSION_TOKEN
           elif [[ -t 0 ]]; then tok=$(cat secrets/hub.token)
           else echo "https://$HOST/"; echo "hub.sh url: the owner's code is minted only in a Terminal" >&2; exit 2; fi
           code=$(printf 'Authorization: Bearer %s\n' "$tok" | curl -fsS --max-time 5 -X POST -H @- "https://$HOST/api/v1/login-codes" | sed -n 's/.*"code": *"\([0-9a-f]*\)".*/\1/p') || code=
           [[ -n "$code" ]] || { echo "hub.sh url: the hub gave no login code (is it up?)" >&2; exit 1; }
           echo "https://$HOST/?code=$code" ;;
  web)     if [[ $OS == Darwin ]]; then open "$("$SELF" url)"; else xdg-open "$("$SELF" url)"; fi ;;
  # The owner's token, for the Passwords entry `life hub` that the desktop app
  # on another Mac and the phone's Settings paste from (SETUP step 5.7). Only
  # to a person at a Terminal, as url: never into a session's transcript.
  token)   [[ -t 0 && -t 1 ]] || { echo "hub.sh token: printed only in a Terminal window" >&2; exit 2; }
           cat secrets/hub.token; echo ;;
  *) sed -n '2,13p' "$0"; exit 1 ;;
esac
