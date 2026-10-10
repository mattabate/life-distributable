#!/usr/bin/env bash
# Nightly encrypted backup of the repo's data/ via restic. The repository (bucket),
# its credentials and the encryption password all come from
# ops/secrets/restic.env (see ops/restic.env.example).
# First run (after filling ops/secrets/restic.env): ./backup.sh init
set -euo pipefail
# launchd runs with a minimal PATH; make sure Homebrew's restic/sqlite3 are found.
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "$(dirname "$0")"
ROOT=$(cd .. && pwd)
mkdir -p logs
LIFECTL="$ROOT/ops/bin/lifectl"

# A failed night reaches the owner as an ask, not a line in logs/backup.log
# nobody reads. The hub raises it on its own "hub" thread; if the hub itself
# is down the trap only logs — and the next run tries again.
on_fail() {
  local rc=$?
  echo "backup FAILED (exit $rc) $(date '+%F %T')" >&2
  "$LIFECTL" ask add "Backup failed last night (exit $rc)" \
    --thread hub --kind other \
    --detail "ops/backup.sh exited $rc at $(date '+%F %T'). See ops/logs/backup.log." \
    2>/dev/null || echo "backup: could not raise the ask (hub down?)" >&2
}
trap on_fail ERR
# After the trap, so a missing or broken restic.env raises the ask too.
source secrets/restic.env

if [[ "${1:-}" == "init" ]]; then
  restic init
  exit 0
fi

# Consistent SQLite snapshot first (WAL mode: never copy the raw file).
DB="$ROOT/data/life.db"
if [[ -f "$DB" ]]; then
  mkdir -p "$ROOT/data/snapshots"
  sqlite3 "$DB" ".backup $ROOT/data/snapshots/life.db"
fi

# A bundle of every ref of the repo rides the encrypted backup too, so a dead
# disk never costs local commits. Restore: git clone data/mirrors/life.bundle life
mkdir -p "$ROOT/data/mirrors"
git -C "$ROOT" bundle create "$ROOT/data/mirrors/life.bundle" --all 2>/dev/null || \
  echo "bundle: git bundle failed" >&2

restic backup "$ROOT/data" --exclude "$DB" --exclude "$DB-wal" --exclude "$DB-shm" --tag nightly
restic check --read-data-subset=5%

# No prune here, deliberately: forget/prune is the only step that deletes, so
# it is the only step that needs a key with delete rights — and any key on this
# Mac is a key a rogue session can use. Prune by hand, with a separate key
# (ops/b2.sh prune). A key that could delete fails the run, so it gets an ask.
if [[ "$RESTIC_REPOSITORY" == b2:* ]]; then ./b2.sh check; fi
