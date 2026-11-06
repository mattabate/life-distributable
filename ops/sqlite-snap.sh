# Sourced by ops/backup.sh: one way to copy a live WAL-mode SQLite file.
#   snap <src.db> <dest.db>
# VACUUM INTO writes a consistent copy in one pass (.backup restarts from
# page 1 whenever the hub writes; lyshr-1024/friday memory/db.ts), then
# PRAGMA integrity_check must say ok or snap fails, so a corrupt copy never
# becomes the night's backup (mikeadolan/claude-brain).
# A missing source fails too, after the old copy is removed: the caller's
# ERR trap raises the card, and last week's copy never rides tonight's
# backup under the name that means "tonight" (life-distributable #127,
# vixkosla: `if [[ -f "$DB" ]]` skipped the snapshot silently).
# (sqlite3 on a missing path would create an empty database, so the check
# comes before it.)
# Self-test: bash ops/sqlite-snap.sh --test (make check runs it).
snap() {
  rm -f "$2"
  [[ -f "$1" ]] || { echo "snap: $1 is missing, no snapshot taken" >&2; return 1; }
  sqlite3 "$1" "VACUUM INTO '$2'" || return 1
  [[ "$(sqlite3 "$2" 'PRAGMA integrity_check')" == ok ]] || { echo "snap: integrity_check failed on $2" >&2; return 1; }
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${1:-}" == "--test" ]]; then
  set -euo pipefail
  t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
  sqlite3 "$t/live.db" "CREATE TABLE x (a); INSERT INTO x VALUES (1);"
  snap "$t/live.db" "$t/snap.db" || { echo "FAIL: a good database did not snapshot"; exit 1; }
  [[ "$(sqlite3 "$t/snap.db" 'SELECT a FROM x')" == 1 ]] || { echo "FAIL: the snapshot lost its row"; exit 1; }
  # The live database is gone, an old snapshot is there: fail, and the old
  # copy must not stay at the current name.
  rm -f "$t/live.db"
  if snap "$t/live.db" "$t/snap.db" 2>/dev/null; then echo "FAIL: a missing database snapshotted"; exit 1; fi
  [[ ! -e "$t/snap.db" ]] || { echo "FAIL: the old snapshot is still current"; exit 1; }
  [[ ! -e "$t/live.db" ]] || { echo "FAIL: snap created the missing database"; exit 1; }
  # No live database and no old snapshot: still a failure.
  if snap "$t/live.db" "$t/snap.db" 2>/dev/null; then echo "FAIL: a missing database with no old copy passed"; exit 1; fi
  echo "sqlite-snap: OK"
fi
