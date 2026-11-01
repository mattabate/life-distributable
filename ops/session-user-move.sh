#!/usr/bin/env bash
# The switch: sessions start running as the session user (SETUP.md "Run
# sessions as a separate user"). Run ops/session-user-prep.sh first.
#   bash ops/session-user-move.sh [user]          dry run: what would move, what stops it
#   bash ops/session-user-move.sh --go [user]     moves; refuses while a session is working
#   bash ops/session-user-move.sh --undo          puts the transcripts and the flag back
# Each open session that has a conversation gets that user's clone of this
# checkout on branch th-<id>, and its transcript moves under the clone's folder
# (a copy left behind would count twice on the spend board). Then session_user
# goes on in ops/hub.json and the hub restarts. Nothing is deleted: the old
# transcripts wait in ~/.claude/projects-moved with a list --undo reads.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
mode=dry
case "${1:-}" in --go|--undo) mode=$1; shift ;; esac
cfg() { plutil -extract "$1" raw -o - "$ROOT/ops/hub.json" 2>/dev/null; }
U=${1:-$(cfg session_user)}
U=${U:-life-agent}
H=$(dscl . -read "/Users/$U" NFSHomeDirectory 2>/dev/null | awk '{print $2}')
OLD=$(cfg claude_projects_dir)
OLD=${OLD:-~/.claude/projects}
case "$OLD" in "~/"*) OLD=$HOME/${OLD#"~/"} ;; esac
PARK=$HOME/.claude/projects-moved
LIST=$PARK/moved.tsv
as() { sudo -n -H -u "$U" "$@"; }
slug() { printf '%s' "$1" | sed 's/[^a-zA-Z0-9]/-/g'; }
db() { bash "$ROOT/ops/db.sh" "$1"; }
restart() { nohup sh -c "sleep 3; bash '$ROOT/ops/hub.sh' restart" >/dev/null 2>&1 & }

if [ "$mode" = --undo ]; then
  if [ -f "$LIST" ]; then
    while IFS=$'\t' read -r parked old new; do
      mkdir -p "$(dirname "$old")"
      mv "$parked" "$old" && as rm -f "$new"
    done < "$LIST"
    mv "$LIST" "$LIST.undone"
  fi
  "$ROOT/ops/py.sh" setup.py session-user --off
  restart
  echo "undone; the hub restarts in 3 s. Commits made in the clones stay there, on their th- branches"
  exit 0
fi

[ -n "$H" ] || { echo "no macOS user $U: create it first (SETUP.md)"; exit 1; }
as test -r "$H/.claude-token" 2>/dev/null || { echo "refused: run bash ops/session-user-prep.sh $U first (it needs sudo to $U and its Claude token)"; exit 1; }

# An approval wakes the session that raised it, so --go gives working
# sessions up to ten minutes to finish their turn before it refuses.
working=$(db "select count(*) from threads where status='running'")
if [ "$mode" = --go ]; then
  waited=0
  while [ "$working" != 0 ] && [ "$waited" -lt 600 ]; do
    sleep 5
    waited=$((waited + 5))
    working=$(db "select count(*) from threads where status='running'")
  done
fi
from=$OLD/$(slug "$ROOT")
moves=
for row in $(db "select id||'|'||claude_session_id from threads where status not in ('archived','done') and coalesce(claude_session_id,'')<>''"); do
  id=${row%%|*}
  cid=${row#*|}
  [ -f "$from/$cid.jsonl" ] && moves="$moves $id|$cid"
done
echo "working now: $working"
for m in $moves; do echo "move  ${m%%|*}"; done
echo "$(echo $moves | wc -w | tr -d ' ') session(s) to move to $U; others get a clone at their next start"
if [ "$mode" != --go ]; then
  echo "dry run: nothing changed. To move: bash ops/session-user-move.sh --go $U"
  exit 0
fi
[ "$working" = 0 ] || { echo "refused: $working session(s) still working"; exit 1; }

mkdir -p "$PARK/$(slug "$ROOT")"
for m in $moves; do
  id=${m%%|*}
  cid=${m#*|}
  new=$H/worktrees/th-$id
  if ! as test -d "$new"; then
    as git clone --quiet --no-hardlinks "$ROOT" "$new" || { echo "clone failed: $id"; exit 1; }
    as git -C "$new" checkout --quiet "th-$id" 2>/dev/null || as git -C "$new" checkout --quiet -b "th-$id" origin/main || { echo "branch failed: $id"; exit 1; }
    for p in data ops/hub.json; do
      [ -e "$ROOT/$p" ] && as ln -s "$ROOT/$p" "$new/$p"
    done
    as mkdir -p "$new/ops/logs"
  fi
  to=$H/.claude/projects/$(slug "$new")
  as mkdir -p "$to"
  as tee "$to/$cid.jsonl" < "$from/$cid.jsonl" >/dev/null || { echo "copy failed: $cid"; exit 1; }
  mv "$from/$cid.jsonl" "$PARK/$(slug "$ROOT")/"
  printf '%s\t%s\t%s\n' "$PARK/$(slug "$ROOT")/$cid.jsonl" "$from/$cid.jsonl" "$to/$cid.jsonl" >> "$LIST"
done
"$ROOT/ops/py.sh" setup.py session-user --user "$U"
restart
echo "moved; the hub restarts in 3 s. Then ask any session to run: ops/py.sh session-user-audit.py"
