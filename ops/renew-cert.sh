#!/usr/bin/env bash
# Re-mint the Tailscale HTTPS cert into ops/secrets and restart the hub.
set -euo pipefail
cd "$(dirname "$0")"
# A failed renewal reaches the owner as an ask on the "hub" thread, the way
# backup.sh's does: it used to leave one line in logs/renew-cert.log and
# nothing else until TLS broke (life-distributable#64). The hub's daily cert
# check (hub/cmd/hub/cert.go) also asks at 21 days left, in case this never
# runs at all. If the hub itself is down the trap only logs.
LIFECTL=./bin/lifectl
on_fail() {
  local rc=$?
  echo "renew-cert FAILED (exit $rc) $(date '+%F %T')" >&2
  "$LIFECTL" ask add "Renew the hub's TLS cert: ops/renew-cert.sh failed (exit $rc)" \
    --thread hub --kind other \
    --detail "ops/renew-cert.sh exited $rc at $(date '+%F %T'). Run it again on the Mac the hub runs on; see ops/logs/renew-cert.log for the error." \
    2>/dev/null || echo "renew-cert: could not raise the ask (hub down?)" >&2
}
trap on_fail ERR
HOST=$(plutil -extract public_host raw -o - hub.json) # host:port — the one place it lives
HOST=${HOST%%:*} # hub.json cert_file/key_file must name secrets/<HOST>.crt|.key
TS=/Applications/Tailscale.app/Contents/MacOS/Tailscale
command -v tailscale >/dev/null && TS=tailscale
echo "$(date -Iseconds) renewing cert for $HOST"
"$TS" cert --cert-file "secrets/$HOST.crt" --key-file "secrets/$HOST.key" "$HOST"
chmod 600 "secrets/$HOST.key"
launchctl kickstart -k "gui/$(id -u)/com.life.hub" || true
