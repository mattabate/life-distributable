#!/usr/bin/env bash
# Get the session user ready and prove the wall, changing nothing a running
# session uses (SETUP.md "Run sessions as a separate user").
#   bash ops/session-user-prep.sh [user]    idempotent; run as the owner in a Terminal
# user defaults to session_user in ops/hub.json, else life-agent. Makes that
# user's folders and git settings, lets the owner's user (the hub) read its
# transcripts and clones, lets it read and write the hub's run files, then
# tries as it: each thing it must not do, a clone with one commit bundled, and
# one tiny Claude call in the clone. Prints PASS/FAIL per line; exits 1 on a FAIL.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OWNER=$(id -un)
cfg() { plutil -extract "$1" raw -o - "$ROOT/ops/hub.json" 2>/dev/null; }
U=${1:-$(cfg session_user)}
U=${U:-life-agent}
H=$(dscl . -read "/Users/$U" NFSHomeDirectory 2>/dev/null | awk '{print $2}')
[ -n "$H" ] || { echo "no macOS user $U: create it first (SETUP.md)"; exit 1; }
as() { sudo -n -H -u "$U" "$@"; }
as true 2>/dev/null || { echo "sudo cannot run commands as $U without a password: add the sudoers line (SETUP.md)"; exit 1; }
jobs=$(cfg jobs_dir)
jobs=${jobs:-$ROOT/data/jobs}
case "$jobs" in "~/"*) jobs=$HOME/${jobs#"~/"} ;; esac
RUNS=$jobs/threads
fails=0
ok() { echo "PASS  $1"; }
no() { echo "FAIL  $1"; fails=$((fails + 1)); }
refused() { if as /bin/bash -c "$2" >/dev/null 2>&1; then no "it could $1"; else ok "it cannot $1"; fi; }
grant() { chmod -a "$2" "$1" 2>/dev/null; chmod +a "$2" "$1"; }
grant_as() { as chmod -a "$2" "$1" 2>/dev/null; as chmod +a "$2" "$1"; }
READ="allow list,search,readattr,readextattr,readsecurity,read,file_inherit,directory_inherit"
RW="allow list,search,add_file,add_subdirectory,write,append,readattr,writeattr,readextattr,writeextattr,read,file_inherit,directory_inherit"

echo "== setting up $U (home $H) for $ROOT"
as mkdir -p "$H/worktrees" "$H/.claude/projects"
as git config --global --replace-all safe.directory "$ROOT"
as git config --global --add safe.directory "$ROOT/.git"
as git config --global user.name "$(git -C "$ROOT" config user.name)"
as git config --global user.email "$(git -C "$ROOT" config user.email)"
# The hub (the owner's user) reads that user's transcripts (the spend board)
# and checks its clones exist.
grant_as "$H" "$OWNER allow list,search,readattr"
grant_as "$H/.claude" "$OWNER allow list,search,readattr"
grant_as "$H/.claude/projects" "$OWNER $READ"
grant_as "$H/worktrees" "$OWNER $READ"
# The hub writes each run's prompt and output under the runs folder.
mkdir -p "$RUNS"
grant "$RUNS" "$U $RW"
# It must reach the checkout to clone it: a search right on each folder above it.
d=$(dirname "$ROOT")
while [ "$d" != / ]; do
  as test -x "$d" 2>/dev/null || grant "$d" "$U allow search"
  d=$(dirname "$d")
done

echo "== the wall"
refused "read the hub token" "cat '$ROOT/ops/secrets/hub.token'"
refused "write the database" "test -w '$ROOT/data/life.db'"
refused "replace the hub" "test -w '$ROOT/ops/bin/hub'"
refused "write the main checkout's git" "touch '$ROOT/.git/session-user-probe'"
refused "write the main checkout" "touch '$ROOT/hub/session-user-probe'"
refused "run a command as $OWNER" "sudo -n -u '$OWNER' true"
if as test -r "$H/.claude-token"; then ok "its Claude token is in place"; else no "no $H/.claude-token (claude setup-token, as $U)"; fi

echo "== the working copy"
WT=$H/worktrees/rehearsal
as rm -rf "$WT"
start=$(date +%s)
if as git clone --quiet --no-hardlinks "$ROOT" "$WT"; then ok "clone in $(($(date +%s) - start)) s"; else no "clone"; fi
as git -C "$WT" checkout --quiet -b th-rehearsal origin/main || no "branch"
if as /bin/bash -c "cd '$WT' && echo x > rehearsal.txt && git add rehearsal.txt && git commit --quiet --no-verify -m rehearsal"; then ok "commit"; else no "commit"; fi
if as git -C "$WT" bundle create --quiet "$H/rehearsal.bundle" origin/main..th-rehearsal; then ok "bundle"; else no "bundle"; fi
T=$(mktemp -d)
if git clone --quiet --no-hardlinks "$ROOT" "$T/c" && as cat "$H/rehearsal.bundle" > "$T/b" && git -C "$T/c" fetch --quiet "$T/b" th-rehearsal:refs/incoming/rehearsal; then ok "$OWNER's user reads the bundle"; else no "$OWNER's user reads the bundle"; fi
rm -rf "$T"
if [ -r "$WT/hub/go.mod" ]; then ok "$OWNER's user reads the clone"; else no "$OWNER's user reads the clone"; fi
if as test -w "$RUNS"; then ok "it writes run files"; else no "it writes run files"; fi

echo "== a run"
bin=$(cfg session_claude_bin)
bin=${bin:-$H/.local/bin/claude}
out=$(as /bin/bash -c "cd '$WT' && CLAUDE_CODE_OAUTH_TOKEN=\"\$(cat \$HOME/.claude-token)\" '$bin' -p --model haiku --max-turns 1 'Reply READY and nothing else.' </dev/null" 2>&1)
case "$out" in
*READY*) ok "Claude answered in the clone" ;;
*) no "Claude in the clone: ${out:0:200}" ;;
esac
slug=$(printf '%s' "$WT" | sed 's/[^a-zA-Z0-9]/-/g')
if ls "$H/.claude/projects/$slug/"*.jsonl >/dev/null 2>&1; then ok "$OWNER's user reads its transcript"; else no "$OWNER's user reads its transcript"; fi

as rm -rf "$WT" "$H/rehearsal.bundle" "$H/.claude/projects/$slug"
echo "== $fails failed"
[ "$fails" = 0 ]
