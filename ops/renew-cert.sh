#!/usr/bin/env bash
# Re-mint the Tailscale HTTPS cert into ops/secrets and restart the hub.
set -euo pipefail
cd "$(dirname "$0")"
HOST=$(plutil -extract public_host raw -o - hub.json) # host:port — the one place it lives
HOST=${HOST%%:*} # hub.json cert_file/key_file must name secrets/<HOST>.crt|.key
TS=/Applications/Tailscale.app/Contents/MacOS/Tailscale
command -v tailscale >/dev/null && TS=tailscale
echo "$(date -Iseconds) renewing cert for $HOST"
"$TS" cert --cert-file "secrets/$HOST.crt" --key-file "secrets/$HOST.key" "$HOST"
chmod 600 "secrets/$HOST.key"
launchctl kickstart -k "gui/$(id -u)/com.life.hub" || true
