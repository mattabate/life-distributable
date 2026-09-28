#!/usr/bin/env bash
# Run one of the python tools that live in ops/ — and nothing else.
#
#   ops/py.sh bq.py "select 1"
#   ops/py.sh x-report.py
#
# Why this exists: `python3:*` used to be pre-approved for phone sessions,
# which is the same thing as an unrestricted shell — `python3 -c` can delete
# files, push to GitHub, or POST the owner's balances to any host. But ops/ holds a dozen real tools (bq.py,
# gh-pr.py, paystub-parse.py, x-report.py …) that sessions legitimately run.
#
# So: the interpreter is not a tool, a REVIEWED SCRIPT IN THE REPO is. Writing
# a new one is still possible — and shows up in `git status`, which is the
# point: arbitrary code becomes something you can see in a diff.
set -euo pipefail
cd "$(dirname "$0")"

if [[ $# -eq 0 ]]; then
  echo "usage: ops/py.sh <script.py> [args…]   (scripts in ops/ only)" >&2
  ls *.py 2>/dev/null | sed 's/^/  /' >&2
  exit 2
fi

script="$1"; shift
case "$script" in
  -*)         echo "ops/py.sh: no interpreter flags (-c/-m are the hole this closes)" >&2; exit 2 ;;
  */*|..*)    echo "ops/py.sh: name a script in ops/, not a path" >&2; exit 2 ;;
  *.py)       ;;
  *)          echo "ops/py.sh: only .py files in ops/" >&2; exit 2 ;;
esac
if [[ ! -f "$script" ]]; then
  echo "ops/py.sh: no such script in ops/: $script" >&2
  exit 2
fi

exec python3 "$script" "$@"
