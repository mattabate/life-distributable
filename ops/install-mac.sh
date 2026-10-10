#!/usr/bin/env bash
# Build, sign and install the desktop app (LifeMac, Mac Catalyst) into
# /Applications, then open it. The Mac's ops/install-phone.sh: one command
# after any app/ change (`make mac`). Same sources and same team as the phone
# app; the bundle id is $(LIFE_BUNDLE_ID).mac from app/local.xcconfig.
#   ops/install-mac.sh            build + install + open
#   ops/install-mac.sh --no-open  build + install only (screenshots, CI)
#   ops/install-mac.sh --force    quit the open app and replace it now
#   ops/install-mac.sh probe      can the build just made keep the decider code?
# With the app open (and no --force) the build is staged instead: the app's
# top bar shows Update, and ops/mac-swap.sh installs it once the app quits.
# Needs: DEVELOPMENT_TEAM in app/local.xcconfig (setup writes it).
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd .. && pwd)

# probe: the build `make mac` just made saves a throwaway Keychain item the
# way Settings saves the code, finds it, deletes it and exits before any
# window (DeciderKeychain.probeIfAsked). The owner's own code is another item
# and is never touched; nothing asks for Touch ID. Prints
# "add=0 find=-25308 delete=0" and exits 0 when the build can keep the code;
# add=-34018 = signed without keychain-access-groups / a provisioning profile
# (Settings then says "could not save the code to the Keychain (-34018)").
if [ "${1:-}" = "probe" ]; then
  APP="$ROOT/app/build-mac/Build/Products/Debug-maccatalyst/life.app"
  BIN="$APP/Contents/MacOS/life"
  [ -x "$BIN" ] || { echo "no desktop build; run: make mac" >&2; exit 1; }
  OUT=$(mktemp -t life-keychain-probe)
  trap 'rm -f "$OUT"' EXIT
  ApplePersistenceIgnoreState=YES LIFE_KEYCHAIN_PROBE="$OUT" "$BIN" >/dev/null 2>&1 &
  pid=$!
  ( sleep 20; kill "$pid" 2>/dev/null ) & dog=$!; disown "$dog"
  wait "$pid" 2>/dev/null || true
  kill "$dog" 2>/dev/null || true
  echo "build $(defaults read "$APP/Contents/Info.plist" CFBundleVersion 2>/dev/null)"
  [ -f "$APP/Contents/embedded.provisionprofile" ] && echo "profile: embedded" || echo "profile: NONE"
  codesign -d --entitlements - "$APP" 2>/dev/null | grep -A2 -E "keychain-access-groups|application-identifier" || echo "entitlements: no keychain group"
  R=$(cat "$OUT")
  echo "${R:-the app wrote nothing (a build from before the probe?)}"
  # find: 0, or -25308 "there, but behind Touch ID": both are a saved item.
  case "$R" in "add=0 find=0 delete=0"|"add=0 find=-25308 delete=0") echo "✔ this build can keep the decider code";; *) echo "✘ this build cannot keep the decider code"; exit 1;; esac
  exit 0
fi
# shellcheck disable=SC1091
source ./app-identity.sh
: "${LIFE_TEAM_ID:?set DEVELOPMENT_TEAM in app/local.xcconfig}"
MAC_ID="$LIFE_BUNDLE_ID.mac"
BUILD="$ROOT/app/build-mac"
LOG="$ROOT/ops/logs/install-mac.log"
# The build number is the commit count, and never one this Mac already runs:
# a build with uncommitted app/ edits is numbered as the commit it is about
# to become, and whatever the count says, the number goes past the installed
# build's (the hub closes "Install desktop build N" cards by that number).
N=$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 1)
[ -z "$(git -C "$ROOT" status --porcelain -- app 2>/dev/null)" ] || N=$((N + 1))
HAVE=$(defaults read /Applications/life.app/Contents/Info.plist CFBundleVersion 2>/dev/null || echo 0)
case "$HAVE" in ''|*[!0-9]*) HAVE=0 ;; esac
[ "$N" -gt "$HAVE" ] || N=$((HAVE + 1))
mkdir -p "$ROOT/ops/logs"

cd "$ROOT/app"
xcodegen generate -q
echo "▶ building LifeMac $N (team $LIFE_TEAM_ID)…"
xcodebuild -project Life.xcodeproj -scheme LifeMac -configuration Debug \
  -destination 'platform=macOS,variant=Mac Catalyst' -derivedDataPath "$BUILD" \
  -allowProvisioningUpdates -allowProvisioningDeviceRegistration \
  DEVELOPMENT_TEAM="$LIFE_TEAM_ID" CURRENT_PROJECT_VERSION="$N" MARKETING_VERSION="1.0.$N" \
  build > "$LOG" 2>&1 \
  || { grep -aE "error:" "$LOG" | cut -c1-400 | head -20; echo "build failed; see ops/logs/install-mac.log"; exit 1; }
APP="$BUILD/Build/Products/Debug-maccatalyst/life.app"
codesign --verify --deep --strict "$APP"
# The app is open: never quit it under the owner. Stage the build; --force is
# quit-and-replace, for when the running build itself is broken.
UPD="$HOME/Library/Application Support/life-mac"
if [ "${1:-}" != "--force" ] && pgrep -f "^/Applications/life.app/Contents/MacOS/life" >/dev/null; then
  mkdir -p "$UPD/next"
  rm -rf "$UPD/next/life.app"
  ditto "$APP" "$UPD/next/life.app"
  if ! { [ -f "$UPD/swap.pid" ] && kill -0 "$(cat "$UPD/swap.pid")" 2>/dev/null; }; then
    nohup bash "$ROOT/ops/mac-swap.sh" >/dev/null 2>&1 &
    disown
  fi
  echo "✔ staged build $N $(date +%F) — the open app shows Update; it goes in when the app quits"
  exit 0
fi
echo "▶ installing /Applications/life.app…"
osascript -e "tell application id \"$MAC_ID\" to quit" >/dev/null 2>&1 || true
rm -rf /Applications/life.app
ditto "$APP" /Applications/life.app
# -n: this bundle, not whichever same-id copy (a screenshot run) is up.
[ "${1:-}" = "--no-open" ] || open -n /Applications/life.app
echo "✔ installed build $N $(date +%F)"
