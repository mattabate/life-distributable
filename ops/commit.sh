#!/usr/bin/env bash
# One-command commit for the shared repo tree (other sessions edit it too).
#   ops/commit.sh -m "<subject>" [-m "<body>"]… [--] <path>…
# Prints `git status --short`, stages EXACTLY the given paths, commits only
# those paths (anything else already staged by another session is left alone),
# adds the Co-Authored-By trailer if the message has none (a session on another
# model passes its own as the last -m), then prints the new short hash
# and the remaining `git status --short`.
# Refuses: no -m, no paths, any path under data/, the repo root ("." etc.),
# and paths with "..". Never uses -a / -A. Retries while index.lock is held.
set -uo pipefail

TRAILER="Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
die() { echo "commit.sh: $*" >&2; exit 2; }

msgs=()
paths=()
while [ $# -gt 0 ]; do
  case "$1" in
    -m) [ $# -ge 2 ] || die "-m needs a message"; msgs+=("$2"); shift 2 ;;
    -m*) msgs+=("${1#-m}"); shift ;;
    --) shift; paths+=("$@"); break ;;
    -a|-A|--all|-am|--amend) die "refusing $1 — name the paths" ;;
    -*) die "unknown flag $1" ;;
    *) paths+=("$1"); shift ;;
  esac
done
[ ${#msgs[@]} -gt 0 ] || die 'usage: ops/commit.sh -m "<subject>" [-m "<body>"]… <path>…'
[ ${#paths[@]} -gt 0 ] || die "no paths given — refusing to commit without explicit paths"

top=$(git rev-parse --show-toplevel 2>/dev/null) || die "not inside a git repo"
prefix=$(git rev-parse --show-prefix)

for p in "${paths[@]}"; do
  rel="$p"
  case "$rel" in "$top"/*) rel="${rel#"$top"/}" ;; /*) die "$p is outside the repo" ;; *) rel="$prefix$rel" ;; esac
  while [ "${rel#./}" != "$rel" ]; do rel="${rel#./}"; done
  rel="${rel//\/.\//\/}"
  rel="${rel%/}"
  case "/$rel/" in */../*) die "$p: use a repo-relative path without '..'" ;; esac
  case "$rel" in ""|"."|"*") die "$p is the whole tree — name the files" ;; esac
  case "$rel" in data|data/*) die "$p is under data/ — your personal data is never committed" ;; esac
done

echo "== git status --short (before)"
git status --short

retry() {
  local out i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    if out=$("$@" 2>&1); then [ -n "$out" ] && echo "$out"; return 0; fi
    case "$out" in
      *index.lock*) echo "commit.sh: index.lock held, retry $i" >&2; sleep 1 ;;
      *) echo "$out" >&2; return 1 ;;
    esac
  done
  echo "$out" >&2; return 1
}

# --literal-pathspecs: a path like ':/' or '*.go' is pathspec magic to git —
# the whole tree or a glob — which walks straight past the checks above.
# A deletion already staged (`git rm`, e.g. run by an approved proposal) is in
# neither the tree nor the index, so `git add` refuses it; commit still takes it.
addpaths=()
for p in "${paths[@]}"; do
  if [ -e "$p" ] || [ -n "$(git --literal-pathspecs ls-files -- "$p")" ]; then addpaths+=("$p"); fi
done
if [ ${#addpaths[@]} -gt 0 ]; then
  retry git --literal-pathspecs add -- "${addpaths[@]}" || die "git add failed"
fi

has_trailer=0
for m in "${msgs[@]}"; do
  case "$m" in *"Co-Authored-By: Claude "*) has_trailer=1 ;; esac
done
args=()
for m in "${msgs[@]}"; do args+=(-m "$m"); done
[ $has_trailer -eq 1 ] || args+=(-m "$TRAILER")

retry git --literal-pathspecs commit "${args[@]}" -- "${paths[@]}" || die "git commit failed"

echo "== committed $(git rev-parse --short HEAD)"
echo "== git status --short (after)"
git status --short
