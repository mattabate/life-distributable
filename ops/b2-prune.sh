#!/usr/bin/env bash
# MONTHLY, RUN BY THE OWNER IN A TERMINAL. Trims old snapshots and reclaims space.
#
# Pruning is the only part of the backup that deletes anything, so it is the
# only part that needs a B2 key with deleteFiles. That key deliberately does
# NOT live on this Mac: it stays in Apple Passwords and is pasted in here,
# held in memory for one run, and never written to disk. The nightly job
# (ops/backup.sh) runs with a write-only key and cannot delete a byte.
set -euo pipefail
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "$(dirname "$0")"

if [[ ! -t 0 || ! -r /dev/tty ]]; then
  echo "b2-prune.sh is interactive on purpose: it needs the delete-capable key"
  echo "typed in by hand. No session or cron job can run it." >&2
  exit 2
fi

set -a; source secrets/restic.env; set +a   # repository + encryption password
NIGHTLY_KEY="$B2_ACCOUNT_KEY"

echo "Repository: $RESTIC_REPOSITORY"
echo "Paste the PRUNE key from Apple Passwords ('life backup prune key')."
read -r  -p "  keyID: "     B2_ACCOUNT_ID  < /dev/tty
read -rs -p "  key:   "     B2_ACCOUNT_KEY < /dev/tty; echo
export B2_ACCOUNT_ID B2_ACCOUNT_KEY

if [[ -z "$B2_ACCOUNT_KEY" ]]; then echo "no key given, nothing done" >&2; exit 2; fi
if [[ "$B2_ACCOUNT_KEY" == "$NIGHTLY_KEY" ]]; then
  echo "That is the nightly key from restic.env. If it can prune, it can also" >&2
  echo "erase the backups from this Mac — which is the thing this split prevents." >&2
  exit 2
fi

restic forget --prune --keep-daily 14 --keep-weekly 8 --keep-monthly 24
restic check --read-data-subset=5%
echo
echo "Done. Tell any session it ran, so it can note it."
