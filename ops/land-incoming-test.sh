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
echo "land-incoming-test: OK"
