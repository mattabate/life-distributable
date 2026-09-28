#!/usr/bin/env bash
# Over-the-air app update (paid tier, since 2026-08-22). Archives + exports an
# AD-HOC ("release-testing") signed .ipa WITHOUT the phone attached — the
# ad-hoc profile carries the iPhone's UDID and lasts 1 year — and publishes
# it for install from Safari on the phone, anywhere on the tailnet, via
# itms-services:// served by the hub over the Tailscale HTTPS cert.
# `make ship` = this script. LAN fallback: ops/install-phone.sh.
#
# Ad-hoc builds carry aps-environment=production: the hub must run with
# apns_production=true (ops/hub.json) while an OTA build is on the phone.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd .. && pwd)
source ./app-identity.sh
: "${LIFE_TEAM_ID:?set DEVELOPMENT_TEAM in app/local.xcconfig}"
OTA="$ROOT/app/build/ota"
PUB="$ROOT/data/ota"            # served by the hub at /ota/<token>/…
LOG="$ROOT/ops/logs/ota.log"
mkdir -p logs "$OTA" "$PUB"

cd "$ROOT/app"
xcodegen generate -q
cat > "$OTA/export.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>method</key><string>release-testing</string>
<key>teamID</key><string>$LIFE_TEAM_ID</string>
<key>signingStyle</key><string>automatic</string>
<key>thinning</key><string>&lt;none&gt;</string>
</dict></plist>
EOF
N=$(git -C "$ROOT" rev-list --count HEAD)

# The build number is the repo's commit count, so a docs-only commit raises it
# without changing a line of Swift — and shipping that mints a new build, a new
# install card and two minutes of the owner's attention for an app byte-identical to
# the one already on their phone. If app/ has not moved since the published build, say so and
# stop. FORCE=1 overrides; a profile inside 30 days of expiry rebuilds anyway.
if [ -z "${FORCE:-}" ] && [ -f "$PUB/Life.ipa" ] && [ -f "$PUB/current.json" ]; then
  PUB_COMMIT=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("commit",""))' "$PUB/current.json")
  PUB_BUILD=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("build",""))' "$PUB/current.json")
  PUB_EXP=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("profile_expires",""))' "$PUB/current.json")
  DAYS_LEFT=$(python3 - "$PUB_EXP" <<'PY'
import datetime, sys
try:
    print((datetime.date.fromisoformat(sys.argv[1]) - datetime.date.today()).days)
except Exception:
    print(0)
PY
)
  if [ -n "$PUB_COMMIT" ] && [ "$DAYS_LEFT" -gt 30 ] \
     && git -C "$ROOT" cat-file -e "$PUB_COMMIT^{commit}" 2>/dev/null \
     && [ -z "$(git -C "$ROOT" diff --name-only "$PUB_COMMIT"..HEAD -- app)" ] \
     && [ -z "$(git -C "$ROOT" status --porcelain -- app)" ]; then
    echo "✔ app/ unchanged since $PUB_COMMIT — build $PUB_BUILD is already the newest app there is (FORCE=1 to rebuild anyway)"
    echo "  open on the phone: $(cat "$PUB/url")"
    exit 0
  fi
fi

echo "▶ archiving 1.0.$N (team $LIFE_TEAM_ID)…"
rm -rf "$OTA/Life.xcarchive"
xcodebuild -project Life.xcodeproj -scheme Life -configuration Debug \
  -destination 'generic/platform=iOS' -archivePath "$OTA/Life.xcarchive" \
  -allowProvisioningUpdates -allowProvisioningDeviceRegistration \
  DEVELOPMENT_TEAM="$LIFE_TEAM_ID" \
  CURRENT_PROJECT_VERSION="$N" MARKETING_VERSION="1.0.$N" archive \
  > "$LOG" 2>&1 \
  || { grep -E "error:" "$LOG" | head -5; echo "archive failed; see ops/logs/ota.log"; exit 1; }
# Every NS…UsageDescription in the source Info.plist must survive into the
# built app. iOS does not warn about a missing one — the feature just fails
# (or the app is killed) the first time it asks for the permission, on the
# phone, after an install. Build 663 shipped without NSFaceIDUsageDescription
# because the source edit was reverted between the commit and the archive,
# and nothing noticed until the plist was read by hand (2026-08-30).
BUILT_PLIST="$OTA/Life.xcarchive/Products/Applications/Life.app/Info.plist"
MISSING=""
while read -r k; do
  /usr/libexec/PlistBuddy -c "Print :$k" "$BUILT_PLIST" >/dev/null 2>&1 || MISSING="$MISSING $k"
done < <(grep -o 'NS[A-Za-z]*UsageDescription' Life/Info.plist | sort -u)
if [ -n "$MISSING" ]; then
  echo "✖ the built app is missing usage descriptions:$MISSING"
  echo "  they are in app/Life/Info.plist but not in the archive — nothing published."
  exit 4
fi

echo "▶ exporting ad-hoc .ipa…"
rm -rf "$OTA/export"
LANE="ad-hoc"
if ! xcodebuild -exportArchive -archivePath "$OTA/Life.xcarchive" -exportPath "$OTA/export" \
  -exportOptionsPlist "$OTA/export.plist" -allowProvisioningUpdates -allowProvisioningDeviceRegistration \
  >> "$LOG" 2>&1; then
  # Ad-hoc needs an "Apple Distribution" identity and a signed-in Apple ID in
  # Xcode; both went missing on 2026-08-23 mid-afternoon and shipping stopped
  # dead. The development lane needs neither — the Xcode-managed dev profile
  # carries the same iPhone UDID, lasts a year, and installs over the air just
  # as well — so fall back to it instead of leaving the owner on an old build.
  grep -E "error:" "$LOG" | tail -3
  echo "▶ ad-hoc export failed; falling back to a development-signed build…"
  cat > "$OTA/export-dev.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>method</key><string>debugging</string>
<key>teamID</key><string>$LIFE_TEAM_ID</string>
<key>signingStyle</key><string>automatic</string>
<key>thinning</key><string>&lt;none&gt;</string>
</dict></plist>
EOF
  rm -rf "$OTA/export"
  xcodebuild -exportArchive -archivePath "$OTA/Life.xcarchive" -exportPath "$OTA/export" \
    -exportOptionsPlist "$OTA/export-dev.plist" >> "$LOG" 2>&1 \
    || { grep -E "error:" "$LOG" | head -5; echo "both export lanes failed; see ops/logs/ota.log"; exit 3; }
  LANE="development"
fi
IPA=$(ls "$OTA"/export/*.ipa | head -1)
PROFILE_EXP=$(unzip -p "$IPA" 'Payload/Life.app/embedded.mobileprovision' | grep -a -A1 ExpirationDate | grep -o "[0-9-]*T" | tr -d T || true)
APS=$(unzip -p "$IPA" 'Payload/Life.app/embedded.mobileprovision' | grep -a -A1 aps-environment | grep -o "<string>[a-z]*" | cut -c9- || true)

# The signing lane decides which APNs environment the app registers with, so
# the hub has to be on the same one or every push silently disappears. Sync it
# here instead of leaving it as a thing to remember.
WANT=false; [ "$APS" = production ] && WANT=true
if python3 - "$WANT" "$ROOT/ops/hub.json" <<'PY'
import json, sys
want, path = sys.argv[1] == "true", sys.argv[2]
cfg = json.load(open(path))
if cfg.get("apns_production") == want:
    sys.exit(1)
cfg["apns_production"] = want
json.dump(cfg, open(path, "w"), indent=2)
open(path, "a").write("\n")
PY
then
  echo "▶ apns_production → $WANT (build is aps=$APS); restarting the hub…"
  "$ROOT/ops/hub.sh" restart >/dev/null
fi

# Unguessable path segment: itms-services fetches with no auth header, so the
# secret lives in the URL (Tailscale-only anyway). Stable per machine.
TOKEN_FILE="$PUB/.token"
[ -f "$TOKEN_FILE" ] || head -c 16 /dev/urandom | xxd -p > "$TOKEN_FILE"
TOKEN=$(cat "$TOKEN_FILE")
HUB=$(plutil -extract public_host raw -o - "$ROOT/ops/hub.json") # host:port, the one place it lives
BASE="https://$HUB/ota/$TOKEN"
VER=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$OTA/Life.xcarchive/Products/Applications/Life.app/Info.plist")
BUILDN=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$OTA/Life.xcarchive/Products/Applications/Life.app/Info.plist")

cp "$IPA" "$PUB/Life.ipa"
cat > "$PUB/manifest.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>items</key><array><dict>
<key>assets</key><array><dict><key>kind</key><string>software-package</string><key>url</key><string>$BASE/Life.ipa</string></dict></array>
<key>metadata</key><dict>
<key>bundle-identifier</key><string>$LIFE_BUNDLE_ID</string>
<key>bundle-version</key><string>$VER</string>
<key>kind</key><string>software</string>
<key>title</key><string>Life</string>
</dict></dict></array></dict></plist>
EOF
cat > "$PUB/install.html" <<EOF
<!doctype html><meta name=viewport content="width=device-width,initial-scale=1">
<title>Install Life</title>
<body style="font:-apple-system-body;font-family:-apple-system;padding:2em;text-align:center">
<h1>Life $VER ($BUILDN)</h1><p>built $(date '+%Y-%m-%d %H:%M')</p>
<p><a style="font-size:1.5em" href="itms-services://?action=download-manifest&url=$BASE/manifest.plist">Install / Update</a></p>
<p style="color:#888">$LANE signed · profile expires $PROFILE_EXP · aps $APS. Tap, accept, then check the Home Screen.</p>
</body>
EOF
echo "$BASE/install.html" > "$PUB/url"
cat > "$PUB/current.json" <<EOF
{"version":"$VER","build":$BUILDN,"method":"$LANE","aps":"$APS","profile_expires":"$PROFILE_EXP","built_at":"$(date -u +%Y-%m-%dT%H:%M:%SZ)","url":"$BASE/install.html","commit":"$(git -C "$ROOT" rev-parse --short HEAD)"}
EOF
echo "✔ published $VER ($BUILDN) $LANE, profile → $PROFILE_EXP, aps=$APS"
echo "  open on the phone: $BASE/install.html"
