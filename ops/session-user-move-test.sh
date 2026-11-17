#!/usr/bin/env bash
# session-user-move.sh --undo brings back the transcript the session kept
# writing after the move, not the parked copy from before it. Runs undo_transcripts alone, in a temp folder,
# with no other user: nothing in the real tree is touched.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
as() { "$@"; }
eval "$(sed -n '/^undo_transcripts() {/,/^}/p' "$here/session-user-move.sh")"
mkdir -p "$d/park" "$d/new"
echo before > "$d/park/a.jsonl"
printf 'before\nturn after the move\n' > "$d/new/a.jsonl"
echo parked-only > "$d/park/b.jsonl"
printf '%s\t%s\t%s\n' "$d/park/a.jsonl" "$d/old/a.jsonl" "$d/new/a.jsonl" \
  "$d/park/b.jsonl" "$d/old/b.jsonl" "$d/new/b.jsonl" > "$d/list"
undo_transcripts "$d/list"
fail() { echo "session-user-move-test: $*" >&2; exit 1; }
grep -q 'turn after the move' "$d/old/a.jsonl" || fail "the newer transcript did not come back"
[ ! -e "$d/new/a.jsonl" ] || fail "the moved copy stayed (it would count twice on spend)"
[ "$(cat "$d/old/b.jsonl")" = parked-only ] || fail "a transcript with no moved copy did not come back"
[ -f "$d/park/a.jsonl" ] || fail "the parked copy went"
echo "session-user-move-test: OK"
