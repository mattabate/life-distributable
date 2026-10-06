#!/usr/bin/env bash
# Can the desktop build keep the decider code? The build `make mac` just made
# saves a throwaway Keychain item the way Settings saves the code, finds it,
# deletes it and exits before any window (DeciderKeychain.probeIfAsked). The
# owner's own code is another item and is never touched; nothing asks for
# Touch ID.
#   make mac-keychain     → "add=0 find=-25308 delete=0" and exit 0
# add=-34018 = the build is signed without keychain-access-groups / a
# provisioning profile (Settings then says "could not save the code to the
# Keychain (-34018)"); the signature's entitlements are printed either way.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
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
