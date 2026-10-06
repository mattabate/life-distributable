#!/usr/bin/env bash
# Screenshot every tab of the app in the iOS Simulator against the live hub,
# so an agent can *look* at the UI it is improving. Output: ops/logs/screens/<tab>.png
# (git-ignored; they show your data). Usage: ops/screens.sh [tab ...]
# Needs: a simulator build (make check / app-build-sim) and ops/secrets/hub.token.
#
# APPEARANCE=dark ops/screens.sh …  writes dark-<tab>.png instead. The
# appearance used to be pinned to light with no way to change it, so no
# screenshot anyone ever took showed the app the way iOS draws it after sunset
# — a whole half of the UI that no pass could look at.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/app"
OUT="$ROOT/ops/logs/screens"; mkdir -p "$OUT"
APPEARANCE="${APPEARANCE:-light}"
PREFIX=""; [ "$APPEARANCE" = light ] || PREFIX="$APPEARANCE-"
TOKEN="$(tr -d '[:space:]' < "$ROOT/ops/secrets/hub.token")"
source "$ROOT/ops/app-identity.sh"
BUNDLE=$LIFE_BUNDLE_ID
TABS=("$@"); [ ${#TABS[@]} -gt 0 ] || TABS=(sessions recs calendar money more)

APP="$(ls -d build/Build/Products/Debug-iphonesimulator/Life.app 2>/dev/null || true)"
if [ -z "$APP" ]; then echo "no simulator build; run: make app-build-sim" >&2; exit 1; fi

# SIM='iPhone 17 Pro Max' ops/screens.sh …  — a bigger or smaller screen. The
# device used to be whatever the name-matching heuristic here landed on, so
# "how does this look on a small phone" was a question no pass could answer.
# ops/app-ui.sh picks its device the same way.
SIM="${SIM:-iPhone 17}"
UDID="$(xcrun simctl list devices available -j | "$ROOT/ops/py.sh" sim-pick.py "$SIM")"
[ -n "$UDID" ] || { echo "no simulator named '$SIM' (xcrun simctl list devices available)" >&2; exit 1; }

xcrun simctl boot "$UDID" 2>/dev/null || true
xcrun simctl bootstatus "$UDID" -b >/dev/null
xcrun simctl ui "$UDID" appearance "$APPEARANCE" >/dev/null 2>&1 || true
# If every shot shows "life Would Like to Send You Notifications": a launch of
# this bundle without LIFE_TAB (a `make app-test` run before 2026-08-26, a hand
# launch) asked, nobody answered, and SpringBoard keeps that alert item alive
# for the bundle across launches and even a reinstall. Only answering it or
# `xcrun simctl shutdown <udid>` (the next run boots again) clears it.
xcrun simctl install "$UDID" "$APP"

for tab in "${TABS[@]}"; do
  # "thread:<id>" opens Sessions and pushes that conversation, so the chat
  # bubbles themselves can be looked at, not only the board.
  thread=""; name="$tab"; compose=""
  case "$tab" in thread:*) thread="${tab#thread:}"; tab=sessions; name="thread-$thread";; esac
  # "compose" opens the new-session sheet with the draft box focused — the
  # composer's typing state, which no tab screenshot ever shows.
  case "$tab" in compose) tab=sessions; compose=1;; esac
  # "share" is the same sheet as it opens after Share → Life on a PDF in
  # another app (a stand-in file, no upload).
  case "$tab" in share) tab=sessions; compose=share;; esac
  # "sent:<id>" opens that same sheet as it is AFTER Send — turned into the
  # session's chat. A home screen in the picture means the sheet crashed.
  case "$tab" in sent:*) compose="${tab#sent:}"; tab=sessions; name="sent-$compose";; esac
  # "more:<dest>" (money|spend|goals|engagement|sources|settings) opens the More tab
  # already pushed into that screen — they are two taps deep, so a plain tab
  # screenshot can never show one.
  more=""
  case "$tab" in more:*) more="${tab#more:}"; tab=more; name="more-$more";; esac
  # "money:equity" opens Money already pushed into the equity page;
  # "money:contracts" the same page scrolled to the foot of its contract list.
  money=""
  case "$tab" in money:*) money="${tab#money:}"; tab=money; name="money-$money";; esac
  # "open:<id>" (rec-…, ask-…, cal-…, a proposal id) is the app after that id
  # was tapped in a card's text: its tab, with the thing open (Refs.swift).
  open=""
  case "$tab" in open:*) open="${tab#open:}"; tab=sessions; name="open-$open";; esac
  xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1 || true
  # "calendar:day" / "calendar:schedule" opens the tab in that mode.
  calmode=""
  case "$tab" in calendar:*) calmode="${tab#calendar:}"; tab=calendar; name="calendar-$calmode";; esac
  # SEND_ERROR=decider ops/screens.sh thread:<id> — the chat opens with the
  # refused-send banner (the decider-code case) staged above the composer.
  SIMCTL_CHILD_LIFE_HUB_TOKEN="$TOKEN" SIMCTL_CHILD_LIFE_TAB="$tab" SIMCTL_CHILD_LIFE_THREAD="$thread" \
    SIMCTL_CHILD_LIFE_COMPOSE="$compose" SIMCTL_CHILD_LIFE_MORE="$more" SIMCTL_CHILD_LIFE_MONEY="$money" SIMCTL_CHILD_LIFE_SEND_ERROR="${SEND_ERROR:-}" SIMCTL_CHILD_LIFE_OPEN="$open" SIMCTL_CHILD_LIFE_CAL_MODE="$calmode" SIMCTL_CHILD_LIFE_CAL_DAY="${LIFE_CAL_DAY:-}" \
    xcrun simctl launch "$UDID" "$BUNDLE" >/dev/null
  sleep 5   # first hub round-trips + list render (+ the push into a thread)
  xcrun simctl io "$UDID" screenshot --type=png "$OUT/$PREFIX$name.png" >/dev/null
  echo "$OUT/$PREFIX$name.png"
done
xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1 || true
