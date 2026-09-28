#!/usr/bin/env bash
# Syntax-check every file of the laptop console (hub/internal/server/web:
# ui.js, views/*.js, app.js, test/*.js) and run its Node tests (web/test/).
#
# The console is served as plain scripts with no build step, so a typo is not
# caught by `make check` — it blanks the whole page at load time instead. This
# is deliberately NOT part of `make check`: the hub is one static binary with
# no node toolchain and the gate should not grow a JS lane (DESIGN.md). Run it
# by hand after editing web/, before `ops/hub.sh restart`.
set -euo pipefail
cd "$(dirname "$0")/.."
web=hub/internal/server/web
files=$(ls "$web"/*.js "$web"/views/*.js "$web"/test/*.js)
n=$(echo "$files" | wc -l | tr -d ' ')

# node: on PATH, else beside the claude binary ops/hub.json points at — the
# scheduled sessions and launchd have no nvm on their PATH.
node=$(command -v node || true)
if [ -z "$node" ]; then
  bin=$(sed -n 's/.*"claude_bin": *"\([^"]*\)".*/\1/p' ops/hub.json | head -1)
  bin=${bin/#\~/$HOME}
  if [ -n "$bin" ] && [ -x "$(dirname "$bin")/node" ]; then node="$(dirname "$bin")/node"; fi
fi
if [ -n "$node" ]; then
  for f in $files; do "$node" --check "$f"; done
  # ui.js is pure enough to run outside a browser: test/ui.test.js loads it
  # into a vm and checks the markdown, the card shapes and the ask/action cards.
  # dot is quiet on a green run; WEBCHECK_VERBOSE=1 says which assert blew.
  rep=dot; [ -n "${WEBCHECK_VERBOSE:-}" ] && rep=spec
  "$node" --test --test-reporter="$rep" "$web/test/"
  echo "webcheck: OK (node, $n files + tests)"
  exit 0
fi

for rt in deno bun; do
  if command -v "$rt" >/dev/null 2>&1; then
    for f in $files; do
      case "$rt" in
        deno) deno check --no-lock "$f" ;;
        bun)  bun build --no-bundle "$f" >/dev/null ;;
      esac
    done
    echo "webcheck: OK ($rt, $n files; tests need node)"
    exit 0
  fi
done

jsc=/System/Library/Frameworks/JavaScriptCore.framework/Versions/A/Helpers/jsc
if [ -x "$jsc" ]; then
  # jsc parses the file; it stops at the first DOM reference, which is fine —
  # a SyntaxError is reported before anything runs.
  for f in $files; do
    out=$("$jsc" "$f" 2>&1 || true)
    case "$out" in
      *SyntaxError*) echo "webcheck: FAILED ($f)"; echo "$out"; exit 1 ;;
    esac
  done
  echo "webcheck: OK (jsc, parse only, $n files; tests need node)"
  exit 0
fi

echo "webcheck: SKIPPED — no JS runtime (node/deno/bun/jsc) on this machine" >&2
exit 0
