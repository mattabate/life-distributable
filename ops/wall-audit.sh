#!/usr/bin/env bash
# The session user's wall, checked every night from the owner's side as that
# user (#163): session-user-audit.py --wall, the path rows only. A FAIL reaches
# the owner as an ask on the hub thread, the way a failed backup does, never
# a line in logs/ nobody reads; the same title replaces last night's card
# rather than stacking. Run by backup.sh near its top, so a paused or failed
# backup still audits. Needs session_user in ops/hub.json and the sudoers line
# from SETUP.md; with session_user off it says so and exits 0.
#   bash ops/wall-audit.sh
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
LIFECTL=$ROOT/ops/bin/lifectl
U=$(plutil -extract session_user raw -o - "$ROOT/ops/hub.json" 2>/dev/null)
if [[ -z "$U" ]]; then
  echo "wall-audit: session_user is off, no wall to audit"
  exit 0
fi
out=$(sudo -n -H -u "$U" /bin/bash -c "cd '$ROOT' && ops/py.sh session-user-audit.py --wall" 2>&1)
rc=$?
echo "$out" | tail -1
[[ $rc -eq 0 ]] && exit 0
rows=$(printf '%s\n' "$out" | grep -E '^FAIL' | head -10)
[[ -n "$rows" ]] || rows=$(printf '%s\n' "$out" | tail -3)
"$LIFECTL" ask add "Session wall audit failed last night" --thread hub --kind other \
  --detail "ops/wall-audit.sh ran session-user-audit.py --wall as $U at $(date '+%F %T') and it exited $rc. A FAIL on a shut or no-write row is a path the sessions can reach:

$rows" 2>/dev/null || echo "wall-audit: could not raise the ask (hub down?)" >&2
exit 1
