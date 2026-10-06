#!/usr/bin/env bash
# Walk a flow in the phone app and screenshot every step.
# `ops/screens.sh` opens a screen and photographs it; this one TAPS.
#
#   make app-ui STEPS='tab more; tap Money; shot money; tap Checking; shot account'
#   make app-ui FLOW=ops/flows/money-phone.txt SIM='iPhone 17 Pro' APPEARANCE=dark
#   make app-ui OPEN=cal-0001 STEPS='tap rec-0001; shot rec'   (start behind an id)
#
# Output: ops/logs/screens/ui/NN-<name>.png (+ .txt for `tree`), git-ignored —
# they show your data. The step grammar lives in app/LifeUITests/FlowTests.swift.
#
# Steps reach the test runner as TEST_RUNNER_* variables (xcodebuild strips the
# prefix), and the test passes the hub token on to the app itself, the same way
# ops/screens.sh does — so the flow runs against the live hub, not a mock.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

STEPS="${STEPS:-}"
[ -n "${FLOW:-}" ] && STEPS="$(grep -v '^[[:space:]]*#' "$FLOW" | tr '\n' ';')"
[ -n "$STEPS" ] || { echo "app-ui: no steps. STEPS='tab more; tap Money; shot money'" >&2; exit 2; }

SIM="${SIM:-iPhone 17}"
APPEARANCE="${APPEARANCE:-light}"
OUT="$ROOT/ops/logs/screens/ui"
TOKEN="$(tr -d '[:space:]' < "$ROOT/ops/secrets/hub.token")"
RESULT="$ROOT/app/build/ui.xcresult"

# One walk at a time. Two sessions share this tree, and a walk owns the
# Simulator, `app/build/ui.xcresult`, `ops/logs/app-ui.log` and $OUT — which the
# next line WIPES. Two overlapping runs therefore delete each other's pictures
# and trap the test (08-28: a chat flow and a rate flow both came back with a
# screen recording and no screenshots). Queue instead of racing.
LOCK="$ROOT/app/build/.ui-lock"
mkdir -p "$ROOT/app/build"
for i in $(seq 1 240); do
  mkdir "$LOCK" 2>/dev/null && break
  [ "$i" = 1 ] && echo "app-ui: another walk holds the Simulator — waiting"
  sleep 5
done
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT

rm -rf "$OUT" "$RESULT"; mkdir -p "$OUT"

# Appearance is a property of the DEVICE, not the run, so it is set before the
# test boots the app (dark mode is half the UI and no pass ever saw it until
# ops/screens.sh grew this switch).
UDID="$(xcrun simctl list devices available -j | "$ROOT/ops/py.sh" sim-pick.py "$SIM")"
[ -n "$UDID" ] || { echo "app-ui: no simulator named '$SIM'" >&2; exit 1; }
xcrun simctl boot "$UDID" 2>/dev/null || true
xcrun simctl bootstatus "$UDID" -b >/dev/null
xcrun simctl ui "$UDID" appearance "$APPEARANCE" >/dev/null 2>&1 || true

# NOBUILD=1 walks the LAST GOOD build in app/build instead of compiling the
# tree first: with several sessions in one tree, somebody's half-made change
# often stops it compiling, and "what does the app that is on his phone do
# when I tap this" should not wait on them (10-05: a decider-row repro was
# blocked by another session's pending delete).
ACTION=test; [ -z "${NOBUILD:-}" ] || ACTION=test-without-building

echo "app-ui: $SIM ($APPEARANCE) — $(printf '%s' "$STEPS" | tr ';' '\n' | grep -c .) steps"
set +e
TEST_RUNNER_LIFE_UI_STEPS="$STEPS" \
TEST_RUNNER_LIFE_UI_OUT="$OUT" \
TEST_RUNNER_LIFE_HUB_TOKEN="$TOKEN" \
TEST_RUNNER_LIFE_TAB="${TAB:-}" \
TEST_RUNNER_LIFE_THREAD="${THREAD:-}" \
TEST_RUNNER_LIFE_MORE="${MORE:-}" \
TEST_RUNNER_LIFE_COMPOSE="${COMPOSE:-}" \
TEST_RUNNER_LIFE_OPEN="${OPEN:-}" \
xcodebuild -project app/Life.xcodeproj -scheme Life \
  -destination "platform=iOS Simulator,id=$UDID" \
  -derivedDataPath app/build -resultBundlePath "$RESULT" \
  -only-testing:LifeUITests CODE_SIGNING_ALLOWED=NO "$ACTION" \
  > "$ROOT/ops/logs/app-ui.log" 2>&1
rc=$?
set -e

# The test prints one UISTEP line per step; that log is the readable account of
# what the run did, so it goes to the terminal and the rest stays in the file.
grep -E '^UISTEP|Test Case .*(failed|passed)|error:' "$ROOT/ops/logs/app-ui.log" | sed 's/^ *//' || true

# Simulator sandboxing can refuse the host path; the pictures are in the result
# bundle either way, so fall back to exporting them rather than losing the run.
if [ -z "$(ls -A "$OUT" 2>/dev/null)" ] && [ -d "$RESULT" ]; then
  echo "app-ui: no files written directly — exporting attachments from the result bundle"
  xcrun xcresulttool export attachments --path "$RESULT" --output-path "$OUT" >/dev/null 2>&1 || true
  # The export names files by test + index; the manifest maps them back.
  "$ROOT/ops/py.sh" xcresult-rename.py "$OUT" || true
fi

# KEEP=dir copies the pictures somewhere the NEXT walk's wipe cannot reach —
# with several sessions queued on the lock, $OUT can be gone before anyone
# has looked (09-09: two walks in a row lost their shots to a third).
if [ -n "${KEEP:-}" ]; then
  mkdir -p "$KEEP" && cp "$OUT"/* "$KEEP"/ 2>/dev/null || true
  echo "app-ui: kept a copy in $KEEP"
fi

ls -1 "$OUT" 2>/dev/null | sed "s|^|$OUT/|"
[ $rc -eq 0 ] || echo "app-ui: xcodebuild exited $rc — see ops/logs/app-ui.log (steps above are still valid)"
exit 0
