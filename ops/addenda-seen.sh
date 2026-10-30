#!/usr/bin/env bash
# The last ADDENDA entry the weekly check carded. No argument prints it ("none"
# before the first check); one argument, an entry id, records it. Nothing else:
# this is the only thing the addenda-check job can write.
set -euo pipefail
cd "$(dirname "$0")/.."
seen=data/addenda-seen.txt
if [ "$#" -eq 0 ]; then
  if [ -f "$seen" ]; then cat "$seen"; else echo none; fi
elif [ "$#" -eq 1 ] && [[ "$1" =~ ^add-[0-9]{4}$ ]]; then
  mkdir -p data
  echo "$1" > "$seen"
else
  echo "usage: addenda-seen.sh [add-NNNN]" >&2
  exit 2
fi
