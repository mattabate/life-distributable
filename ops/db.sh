#!/usr/bin/env bash
# Read-only query access to data/life.db, for sessions.
#
#   ops/db.sh "select count(*) from observations"
#   ops/db.sh -json "select id,title from cal_items limit 5"
#
# Why this exists: `sqlite3:*` used to be pre-approved for phone sessions, and
# that is a write handle on the whole database — it walks straight past the
# append-only model and past the `delete` gate. Reading is the part sessions actually needed.
#
# Writes go through the hub (lifectl / `lifectl api`), which owns the rules.
set -euo pipefail
DB="$(cd "$(dirname "$0")/.." && pwd)/data/life.db"

if [[ $# -eq 0 ]]; then
  echo "usage: ops/db.sh [-json|-line|-csv] \"select …\"" >&2
  echo "       ops/db.sh tables" >&2
  exit 2
fi

fmt=()
case "${1:-}" in
  -json|-line|-csv) fmt=("$1"); shift ;;
  # Any other flag (-cmd, -init, -bail …) is a way to run dot-commands such
  # as .shell or .output, i.e. a write handle again.
  -*) echo "ops/db.sh: only -json, -line or -csv" >&2; exit 2 ;;
esac

if [[ "${1:-}" == "tables" ]]; then
  exec sqlite3 -readonly -safe "$DB" ".tables"
fi

sql="$*"
# An argument starting with "." is a dot-command (.shell, .output, .import).
if [[ "$sql" =~ ^[[:space:]]*\. ]]; then
  echo "ops/db.sh: dot-commands are not allowed — SQL only" >&2
  exit 2
fi
# -readonly protects the main database, but ATTACH would open a second one
# read-write and hand back the write handle this script exists to remove.
if grep -qiE '(^|[^a-z_])attach([^a-z_]|$)' <<<"$sql"; then
  echo "ops/db.sh: ATTACH is not allowed — this is a read-only view of life.db" >&2
  exit 2
fi

# ${fmt[@]+…}: macOS ships bash 3.2, where an empty array trips `set -u`.
# -safe also turns off writefile(), edit() and load_extension(), which write
# files or run code from plain SELECTs.
exec sqlite3 -readonly -safe ${fmt[@]+"${fmt[@]}"} "$DB" "$sql"
