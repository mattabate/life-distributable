# Sourced by ops/backup.sh: the nightly bundle of every ref in the repo.
#   bundle_snap <repo> <dest.bundle>
# Writes to <dest>.tmp, runs `git bundle verify`, then moves it into place.
# On any failure the last good bundle is renamed <dest>.stale, so restic never
# backs up last week's code under the name that means "tonight".
# Self-test: bash ops/git-bundle-snap.sh --test (make check runs it).
# The repo must be its own top level: git searches upward, so a checkout with
# no .git of its own bundled an enclosing repo and the night looked clean (#169).
bundle_snap() {
  local repo=$1 dest=$2 err top here
  rm -f "$dest.tmp"
  top=$(git -C "$repo" rev-parse --show-toplevel 2>/dev/null) && top=$(cd "$top" && pwd -P) || top=""
  here=$(cd "$repo" 2>/dev/null && pwd -P) || here=""
  if [[ -z "$top" || "$top" != "$here" ]]; then
    [[ -f "$dest" ]] && mv -f "$dest" "$dest.stale"
    echo "bundle: $repo is not a repository root (git found ${top:-none})" >&2
    return 1
  fi
  if err=$(git -C "$repo" bundle create "$dest.tmp" --all 2>&1) &&
     err=$(git -C "$repo" bundle verify "$dest.tmp" 2>&1); then
    mv -f "$dest.tmp" "$dest"
    rm -f "$dest.stale"
    return 0
  fi
  rm -f "$dest.tmp"
  [[ -f "$dest" ]] && mv -f "$dest" "$dest.stale"
  echo "bundle: git bundle failed for $repo: $(tail -1 <<<"$err")" >&2
  return 1
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${1:-}" == "--test" ]]; then
  set -euo pipefail
  t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
  git init -q "$t/repo"
  git -C "$t/repo" -c user.name=t -c user.email=t@t commit -q --allow-empty -m one
  bundle_snap "$t/repo" "$t/life.bundle" 2>/dev/null || { echo "FAIL: a good repo did not bundle"; exit 1; }
  git bundle verify "$t/life.bundle" >/dev/null 2>&1 || { echo "FAIL: the bundle does not verify"; exit 1; }
  # A failed night: the old bundle must not stay at the current name.
  mkdir "$t/notrepo"
  if bundle_snap "$t/notrepo" "$t/life.bundle" 2>/dev/null; then echo "FAIL: a non-repo bundled"; exit 1; fi
  [[ ! -e "$t/life.bundle" && -f "$t/life.bundle.stale" && ! -e "$t/life.bundle.tmp" ]] ||
    { echo "FAIL: after a failure the stale bundle is still current"; exit 1; }
  # The next good night clears the stale marker.
  bundle_snap "$t/repo" "$t/life.bundle" 2>/dev/null
  [[ -f "$t/life.bundle" && ! -e "$t/life.bundle.stale" ]] || { echo "FAIL: recovery left .stale"; exit 1; }
  # A plain directory inside a repo: git would answer with the enclosing one.
  mkdir "$t/repo/life"
  if bundle_snap "$t/repo/life" "$t/life.bundle" 2>/dev/null; then echo "FAIL: an enclosing repo bundled"; exit 1; fi
  [[ ! -e "$t/life.bundle" && -f "$t/life.bundle.stale" ]] || { echo "FAIL: the enclosing-repo night left a current bundle"; exit 1; }
  echo "git-bundle-snap: OK"
fi
