#!/usr/bin/env bash
# Smoke-test the laptop console against the running hub, headless.
#
# The console has no build step and no test lane, so the only thing that
# proves app.js still boots is a browser rendering it. Chrome renders the page
# with the hub token, waits for the first poll, and dumps the DOM; if the
# script threw at load, #view is empty and the tabs never light up.
#
# Usage: make web-smoke   (after `make build` + `ops/hub.sh restart`)
set -euo pipefail
cd "$(dirname "$0")/.."
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[ -x "$chrome" ] || { echo "web-smoke: SKIPPED — no Chrome" >&2; exit 0; }

# `web-smoke.sh preview '#/asks'` — screenshot the console against FIXTURES
# (ops/web-preview.html), for states the live hub is not in: a pending
# approval, a goal editor. Real data is never touched and no request leaves
# the page; no hub is needed, so it works before setup too.
if [ "${1:-}" = "preview" ]; then
  mkdir -p ops/logs/web
  out=ops/logs/web/${4:-preview}.png
  # This tree's console and shared/fixtures, not the hub's: the hub serves
  # main until a merge restarts it, so a branch's change was never drawn.
  "$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
    --allow-file-access-from-files \
    --window-size=1500,1100 --virtual-time-budget=6000 \
    --screenshot="$out" \
    "file://$PWD/ops/web-preview.html?web=file://$PWD/hub/internal/server/web&fixtures=file://$PWD/shared/fixtures${3:+&open=1}${2:-#/asks}" >/dev/null 2>&1 || true
  ls -l "$out"
  exit 0
fi

url=$(ops/hub.sh url)

# `web-smoke.sh shot [thread-id] [route]` — screenshots of the LIVE hub
# instead of assertions, so a session can look at what it changed
# (ops/logs/web/*.png). A third argument shoots any other route as view.png.
if [ "${1:-}" = "shot" ]; then
  mkdir -p ops/logs/web
  tid=${2:-$(ops/bin/lifectl threads | sed -n 's/.*"id": "\([^"]*\)".*/\1/p' | head -1)}
  "$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
    --window-size=1500,1000 --virtual-time-budget=9000 \
    --screenshot=ops/logs/web/list.png "$url" >/dev/null 2>&1 || true
  "$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
    --window-size=1500,1000 --virtual-time-budget=9000 \
    --screenshot=ops/logs/web/chat.png "$url#/sessions/$tid" >/dev/null 2>&1 || true
  if [ -n "${3:-}" ]; then
    # SHOT_H: the panes scroll inside the window, so a long page (Calendar) is
    # only visible in one picture when the window itself is tall.
    "$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
      --window-size=1500,"${SHOT_H:-2200}" --virtual-time-budget=9000 \
      --screenshot=ops/logs/web/view.png "$url$3" >/dev/null 2>&1 || true
  fi
  ls -l ops/logs/web/
  exit 0
fi
dom=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
      --virtual-time-budget=8000 --dump-dom "$url" 2>/dev/null || true)

fail() { echo "web-smoke: FAILED — $1"; exit 1; }
[ -n "$dom" ] || fail "no DOM (hub down? token stale?)"
case "$dom" in
  *Unauthorized*) fail "unauthorized — token not accepted" ;;
esac
# app.js writes the view; an empty <main> means it threw before rendering.
case "$dom" in
  *'<main id="view"></main>'*) fail "app.js rendered nothing (JS error at boot)" ;;
esac
# The bare URL lands on Sessions (2026-09-02, when the Your turn page it used
# to land on was deleted), so the root DOM proves boot + landing page. Sessions
# has no <h2> — it is two panes — so the assertion is its list column.
case "$dom" in
  *'id="list"'*) ;;
  *) printf '%s\n' "$dom" | tail -c 1200; fail "landing page is not Sessions (no #list pane)" ;;
esac
# …and the your-turn oval hangs off Sessions now, not off a tab of its own.
case "$dom" in
  *'id="badge-asks"'*) fail "the nav still has a Your turn tab (badge-asks)" ;;
esac
case "$dom" in
  *'id="badge-sessions"'*) ;;
  *) fail "Sessions has no badge element (badge-sessions) — the your-turn count has nowhere to go" ;;
esac
echo "web-smoke: OK (landed on Sessions)"
dom=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
      --virtual-time-budget=8000 --dump-dom "$url#/sessions" 2>/dev/null || true)
for want in 'id="list"' 'New session'; do
  case "$dom" in
    *"$want"*) ;;
    *) printf '%s\n' "$dom" | tail -c 1200; fail "sessions view missing: $want" ;;
  esac
done
echo "web-smoke: OK (sessions list rendered)"

# Phase 2: open a real session — the chat is where the work is, and a thrown
# render leaves the pane empty rather than erroring anywhere visible.
tid=$(ops/bin/lifectl threads | sed -n 's/.*"id": "\([^"]*\)".*/\1/p' | head -1)
[ -n "$tid" ] || { echo "web-smoke: no threads to open"; exit 0; }
chat=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
       --virtual-time-budget=9000 --dump-dom "$url#/sessions/$tid" 2>/dev/null || true)
# ids are the shared composer's, one per key (composer.js `cdom`): the chat's
# box is c-chat, its textarea c-chat-draft — they were plain "composer"/"draft"
# until the box became one component used by chat, calendar steps and recs.
for want in 'id="c-chat"' 'id="c-chat-draft"' 'class="msg'; do
  case "$chat" in
    *"$want"*) ;;
    *) fail "chat view missing: $want (thread $tid)" ;;
  esac
done
case "$chat" in
  *'class="run"'*|*'class="run" '*) echo "web-smoke: OK (chat + run activity rendered, thread $tid)" ;;
  *) echo "web-smoke: OK (chat rendered, thread $tid — no activity block in this thread)" ;;
esac

# Phase 3: the other tabs. Each one renders its own heading, so an empty
# <main> or a missing heading means that route threw — which is otherwise
# invisible, since a JS error just leaves the page blank.
for pair in 'goals:Goals' 'recs:Recommendations' 'spend:Spend' 'sources:Sources'; do
  tab=${pair%%:*}; head=${pair#*:}
  page=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
         --virtual-time-budget=8000 --dump-dom "$url#/$tab" 2>/dev/null || true)
  # A heading may carry a count now ("Recommendations · 4"), so match the h2's
  # opening text rather than the whole element — but keep it anchored to <h2
  # so the nav link of the same name cannot satisfy the assertion.
  case "$page" in
    *"<h2"*">$head</h2>"*|*"<h2"*">$head <"*) ;;
    *) fail "$tab view did not render (no <h2> starting …$head)" ;;
  esac
done
# Calendar is not in that loop: since 08-31 its <h2> is the RANGE ("Aug 30 –
# Sep 5, 2026"), which changes every day, so there is no literal to match. The
# grid itself is the better assertion anyway — the title bar, the scroller the
# hours live in, and the rail that holds the anytime rows.
cal=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
      --virtual-time-budget=9000 --dump-dom "$url#/calendar/week" 2>/dev/null || true)
for want in 'class="cal-title"' 'id="cal-scroll"' 'class="cal-rail"'; do
  case "$cal" in
    *"$want"*) ;;
    *) fail "calendar grid did not render (no $want)" ;;
  esac
done
echo "web-smoke: OK (your turn, goals, calendar grid, recs, spend, sources rendered)"

# Phase 4: one real goal's detail page — the head panel (name, statement, the
# active switch), the digest and the notes are a second route, so the list
# rendering proves nothing about them.
gid=$(ops/bin/lifectl goals | sed -n 's/.*"id": "\([^"]*\)".*/\1/p' | head -1)
if [ -n "$gid" ]; then
  gp=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
       --virtual-time-budget=8000 --dump-dom "$url#/goals/$gid" 2>/dev/null || true)
  for want in 'id="ghead"' 'class="switch"' 'Where this stands' 'Recent notes'; do
    case "$gp" in
      *"$want"*) ;;
      *) fail "goal detail missing: $want (goal $gid)" ;;
    esac
  done
  echo "web-smoke: OK (goal detail rendered, $gid)"
fi

# Phase 4b: controls that must DO something, not merely draw (ops/web-check.html
# — an ask card built from a hostile title, its Reply button clicked). Rendering
# is what every phase above tests; this is the only one that tests behaviour,
# and the Reply button is why it exists.
base=${url%%/\?token=*}
cw=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
     --virtual-time-budget=9000 --dump-dom "file://$PWD/ops/web-check.html?hub=$base" 2>/dev/null || true)
# The result block is multi-line, so it comes out as a range, tags stripped.
res=$(printf '%s\n' "$cw" | sed -n '/<pre id="result">/,/<\/pre>/p' | sed 's/<[^>]*>//g')
[ -n "$res" ] || fail "web-check produced no result (app.js failed to load?)"
case "$res" in
  *FAIL*) printf '%s\n' "$res"; fail "an ask-card control is wired wrong" ;;
esac
echo "web-smoke: OK (ask card controls behave — $(printf '%s\n' "$res" | grep -c PASS) checks)"

# Phase 5: one real recommendation's page. The list draws cards from a
# template; the detail route renders `because`/`expect`, the decision and the
# outcome, and it is the one place scoring can be done by hand.
rid=$(ops/bin/lifectl recs all | sed -n 's/.*"id": "\(rec-[^"]*\)".*/\1/p' | head -1)
if [ -n "$rid" ]; then
  rp=$("$chrome" --headless --disable-gpu --no-first-run --ignore-certificate-errors \
       --virtual-time-budget=8000 --dump-dom "$url#/recs/$rid" 2>/dev/null || true)
  for want in 'Decision' 'Outcome' '‹ Recommendations'; do
    case "$rp" in
      *"$want"*) ;;
      *) fail "rec detail missing: $want (rec $rid)" ;;
    esac
  done
  # A rec must never PUSH. The console nav carries a count oval, which
  # is a count of what is sitting on the page — so what this guards now is the
  # push channel itself: recs stay out of document.title (pull, not push;
  # docs/design/recommendations.md).
  case "$rp" in
    *'<title>'*'life'*) ;;
    *) fail "console title missing" ;;
  esac
  case "$rp" in
    *'rec'*'</title>'*) fail "recs reached document.title — recs must never notify" ;;
  esac
  case "$rp" in
    *'data-tab="recs"'*) ;;
    *) fail "recs nav link missing" ;;
  esac
  echo "web-smoke: OK (rec detail rendered, $rid)"
fi
