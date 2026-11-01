#!/usr/bin/env bash
# Put a session's commits on main when sessions run as their own user
# (session_user in ops/hub.json, SETUP.md "Run sessions as a separate user").
#   bash ops/land.sh       from the session's clone, after ops/commit.sh
# That user cannot write the main checkout, so it works in a clone whose origin
# is the main checkout. This refuses uncommitted tracked edits, rebases the
# branch on main (on a conflict the rebase is left open: resolve, git add,
# git rebase --continue, rerun), sends the new commits to the hub as a git
# bundle (POST /api/v1/land), and waits while the hub fast-forwards main as
# the owner's user on the standing approval for a local commit. In the main
# checkout itself there is nothing to land: its commits are already on main.
set -euo pipefail
die() { echo "land.sh: $*" >&2; exit 2; }

top=$(git rev-parse --show-toplevel)
origin=$(git remote get-url origin 2>/dev/null || true)
if [ ! -d "$origin/.git" ] || [ "$(cd "$origin" && pwd -P)" = "$(cd "$top" && pwd -P)" ]; then
  echo "land.sh: not a session clone; commits here are already on main"
  exit 0
fi
git diff --quiet HEAD -- || die "uncommitted edits: commit them (ops/commit.sh) or restore them"
branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" != HEAD ] || die "detached HEAD: finish the rebase first"
git fetch --quiet origin main
git rebase origin/main || die "rebase on main stopped on a conflict: resolve, git add, git rebase --continue, then rerun ops/land.sh"
if git merge-base --is-ancestor HEAD origin/main; then
  echo "land.sh: $branch is already on main"
  exit 0
fi
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
git bundle create --quiet "$dir/b" "origin/main..$branch"
printf '{"branch":"%s","bundle_b64":"%s"}' "$branch" "$(base64 -i "$dir/b" | tr -d '\n')" > "$dir/body.json"
if ! lifectl api POST /api/v1/land "@$dir/body.json" > "$dir/out.json"; then
  cat "$dir/out.json" >&2
  die "the hub refused the landing"
fi
"$(dirname "$0")/py.sh" action-wait.py "$dir/out.json"
