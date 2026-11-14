#!/usr/bin/env bash
# Land a session's incoming commits on main, run by the hub's land action
# (hub/internal/server/land.go).
#   bash ops/land-incoming.sh <sha>
# Tests <sha> first, on every path (#208: a landing used to fast-forward main
# and restart the hub with only a build, no tests). When main has moved since
# the landing was filed, the commits main lacks are rebased onto main first.
# All of it in a throwaway worktree: make test runs there, then main
# fast-forwards to what was tested. A conflict or a failing test stops it with
# main untouched.
set -euo pipefail
die() { echo "land-incoming.sh: $*" >&2; exit 2; }
sha=${1:-}
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die "usage: bash ops/land-incoming.sh <40-hex sha>"
cd "$(git rev-parse --show-toplevel)"
# An uncommitted edit here that is byte for byte what the landing brings (a
# build's regenerate of a generated file, the same regenerate committed by a
# session) goes back to HEAD first: git refuses to fast-forward over a dirty
# file even when the result is identical. Any other dirty file is untouched
# and still stops the fast-forward.
settle() {
  local f
  git diff --name-only -z | while IFS= read -r -d '' f; do
    [ -f "$f" ] || continue
    if [ "$(git hash-object -- "$f")" = "$(git rev-parse -q --verify "$1:$f" 2>/dev/null)" ]; then
      git checkout -- "$f"
    fi
  done
}
base=$(git merge-base main "$sha")
tmp="$(mktemp -d)/land"
git worktree add --detach "$tmp" "$sha" >/dev/null
trap 'git worktree remove --force "$tmp" >/dev/null 2>&1 || true' EXIT
rebased=
if ! git merge-base --is-ancestor main "$sha"; then
  rebased=1
  if ! git -C "$tmp" rebase --onto main "$base"; then
    git -C "$tmp" rebase --abort || true
    die "the commits no longer apply on main: the session rebases and lands again"
  fi
fi
# The tree is a session's code: its tests run as the session user, where the
# wall holds, never first as the owner. A snapshot in that user's own temp
# dir, since it cannot read this worktree; the build and restart after the
# landing run as the owner by design.
user=$(python3 - ops/hub.json <<'PY'
import json, sys
print(json.load(open(sys.argv[1])).get("session_user", ""))
PY
)
if [ -n "$user" ]; then
  as_session() { sudo -n -H -u "$user" "$@"; }
  snap=$(as_session mktemp -d /tmp/life-land.XXXXXX) || die "cannot run as $user: main untouched"
  trap 'as_session rm -rf "$snap" >/dev/null 2>&1 || true; git worktree remove --force "$tmp" >/dev/null 2>&1 || true' EXIT
  git -C "$tmp" archive HEAD | as_session tar -x -C "$snap"
  as_session env PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin make -s -C "$snap" test \
    || die "make test (as $user) failed on the landing commits: main untouched"
else
  make -s -C "$tmp" test || die "make test failed on the landing commits: main untouched"
fi
settle "$(git -C "$tmp" rev-parse HEAD)"
git merge --ff-only "$(git -C "$tmp" rev-parse HEAD)"
[ -n "$rebased" ] || exit 0
echo "== landed on main (rebased from ${sha:0:8}):"
git log --oneline "$base..main" | head -40
