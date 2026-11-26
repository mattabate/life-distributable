#!/usr/bin/env bash
# Test for ops/land-incoming.sh's dirty-file rule, in a scratch repo:
#   bash ops/land-incoming-test.sh
# A dirty file identical to what lands does not stop the fast-forward; a dirty
# file that differs does, and keeps its edit. A commit whose make test fails
# does not land, on the plain fast-forward either.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"
git init -q -b main .
git config user.email t@t
git config user.name t
mkdir ops
cp "$here/land-incoming.sh" ops/
echo one > gen.txt
echo one > other.txt
printf 'test:\n\t@! test -f red.txt\n' > Makefile
echo '{}' > ops/hub.json
git add . && git commit -qm base
git checkout -q -b work
echo two > gen.txt
echo two > other.txt
git commit -qam change
sha=$(git rev-parse HEAD)
git checkout -q main

echo two > gen.txt
bash ops/land-incoming.sh "$sha" >/dev/null
[ "$(git rev-parse HEAD)" = "$sha" ] && [ -z "$(git status --porcelain)" ] || { echo "FAIL: identical dirty file blocked the landing"; exit 1; }

git reset -q --hard HEAD~1
echo mine > other.txt
if bash ops/land-incoming.sh "$sha" >/dev/null 2>&1; then echo "FAIL: landed over a differing dirty file"; exit 1; fi
[ "$(cat other.txt)" = mine ] || { echo "FAIL: the dirty file's edit was lost"; exit 1; }

# A red test stops a clean fast-forward too, with main where it was.
git checkout -q -- other.txt
git checkout -q work
touch red.txt && git add red.txt && git commit -qm red
red=$(git rev-parse HEAD)
git checkout -q main
was=$(git rev-parse HEAD)
if bash ops/land-incoming.sh "$red" >/dev/null 2>&1; then echo "FAIL: landed a failing test"; exit 1; fi
[ "$(git rev-parse HEAD)" = "$was" ] || { echo "FAIL: main moved under a failing test"; exit 1; }

# No GIT_* variable from the caller reaches the landing's git: a GIT_DIR naming
# another repo would land (or fail) there, and a GIT_CONFIG_* core.hooksPath
# would run its hooks as the owner.
git init -q "$dir/decoy"
mkdir -p "$dir/hooks"
for h in post-checkout post-merge post-rewrite; do
  printf '#!/bin/sh\ntouch "%s/hooked"\n' "$dir" > "$dir/hooks/$h"
  chmod +x "$dir/hooks/$h"
done
GIT_DIR="$dir/decoy/.git" GIT_WORK_TREE="$dir/decoy" \
  GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0="$dir/hooks" \
  bash ops/land-incoming.sh "$sha" >/dev/null 2>&1 || { echo "FAIL: a GIT_* variable in the environment stopped the landing"; exit 1; }
[ "$(git rev-parse HEAD)" = "$sha" ] && [ "$(cat other.txt)" = two ] || { echo "FAIL: a GIT_DIR in the environment steered the landing"; exit 1; }
[ ! -e "$dir/hooked" ] || { echo "FAIL: a GIT_CONFIG_* hooksPath ran its hooks in the landing"; exit 1; }
echo "land-incoming-test: OK"
