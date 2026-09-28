#!/usr/bin/env bash
# Build, sign, and install the Life app on the owner's iPhone.
# Paid team: the profile lasts 1 year (hub status shows the date).
# Needs: DEVELOPMENT_TEAM in app/local.xcconfig and LIFE_DEVICE_ID in
# ops/app.env (both written at setup; see ops/app-identity.sh).
# First install needs the phone on USB-C + unlocked; later runs usually work
# over Wi-Fi (same LAN) because the phone is CoreDevice-paired.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd .. && pwd)
source ./app-identity.sh
: "${LIFE_TEAM_ID:?set DEVELOPMENT_TEAM in app/local.xcconfig}"
: "${LIFE_DEVICE_ID:?set LIFE_DEVICE_ID in ops/app.env}"
DEVICE=$LIFE_DEVICE_ID
BUILD="$ROOT/app/build"
mkdir -p logs

cd "$ROOT/app"
xcodegen generate -q
echo "▶ building + signing (team $LIFE_TEAM_ID)…"
xcodebuild -project Life.xcodeproj -scheme Life -configuration Debug \
  -destination "id=$DEVICE" -derivedDataPath "$BUILD" \
  -allowProvisioningUpdates -allowProvisioningDeviceRegistration \
  DEVELOPMENT_TEAM="$LIFE_TEAM_ID" CURRENT_PROJECT_VERSION="$(git -C "$ROOT" rev-list --count HEAD)" \
  MARKETING_VERSION="1.0.$(git -C "$ROOT" rev-list --count HEAD)" \
  build > "$ROOT/ops/logs/install-phone.log" 2>&1 \
  || {
    if grep -q "Unable to find a destination" "$ROOT/ops/logs/install-phone.log"; then
      # Phone not reachable over the LAN (CoreDevice is Bonjour-only, never
      # crosses Tailscale). OTA (ops/ota.sh) does NOT install on the free
      # tier (2026-08-21, see DESIGN.md) — so this is a hard stop: the
      # install has to wait until the phone is on the Mac's Wi-Fi.
      echo "phone not reachable by Xcode (not on the Mac's LAN). Retry when it is; OTA is not an option on the free tier."
      exit 2
    fi
    grep -E "error:" "$ROOT/ops/logs/install-phone.log" | head -5; echo "build failed; see ops/logs/install-phone.log"; exit 1; }
APP="$BUILD/Build/Products/Debug-iphoneos/Life.app"
echo "▶ installing on iPhone…"
xcrun devicectl device install app --device "$DEVICE" "$APP"
echo "▶ launching…"
xcrun devicectl device process launch --device "$DEVICE" "$LIFE_BUNDLE_ID" >/dev/null 2>&1 || true
date +%F > "$ROOT/ops/logs/install-phone.log.last"
echo "✔ installed $(date +%F) (profile: $(grep -a -A1 ExpirationDate "$APP/embedded.mobileprovision" | grep -o "[0-9-]*T" | tr -d T || true))."
