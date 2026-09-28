#!/usr/bin/env bash
# What is the B2 key in ops/secrets/restic.env actually allowed to do?
#
# The nightly backup only ever needs to LIST and WRITE. A key that can also
# delete (or rewrite bucket settings) means anything running on this Mac —
# including a session that followed an injected instruction — can erase every
# backup.
#
# Exit 0 = safe (write-only). Exit 1 = the key can destroy backups, or it no
# longer authenticates at all — a deleted/rotated key means the nightly backup
# is silently not running, which is worse than a key with too much power.
# Never prints the key itself.
set -euo pipefail
cd "$(dirname "$0")"
set -a; source secrets/restic.env; set +a

python3 - <<'PY'
import base64, json, os, sys, urllib.error, urllib.request

kid, key = os.environ["B2_ACCOUNT_ID"], os.environ["B2_ACCOUNT_KEY"]
req = urllib.request.Request(
    "https://api.backblazeb2.com/b2api/v2/b2_authorize_account",
    headers={"Authorization": "Basic " + base64.b64encode(f"{kid}:{key}".encode()).decode()})
try:
    with urllib.request.urlopen(req, timeout=20) as r:
        auth = json.load(r)
except urllib.error.HTTPError as e:
    if e.code in (401, 403):
        print(f"DEAD:    the key in secrets/restic.env ({kid}) no longer authenticates")
        print("         -> NO BACKUP IS RUNNING. Fix: ops/b2-mint-keys.sh (needs the")
        print("            master key) installs a fresh nightly key.")
        sys.exit(1)
    raise
except urllib.error.URLError as e:
    print(f"UNKNOWN: could not reach Backblaze ({e.reason}) — key not checked")
    sys.exit(1)

allowed = auth.get("allowed", {})
caps = set(allowed.get("capabilities", []))
bucket = allowed.get("bucketName") or "ALL BUCKETS"
master = auth.get("accountId") == kid

# Everything the nightly run needs, and nothing else.
NEEDED = {"listBuckets", "listFiles", "readFiles", "writeFiles"}
# Capabilities that let the holder destroy or silently expire the backups.
DANGEROUS = {"deleteFiles", "deleteBuckets", "deleteKeys", "writeKeys",
             "writeBuckets", "writeBucketLifecycleRules", "writeBucketReplications"}

print(f"bucket:  {bucket}{' (MASTER KEY)' if master else ''}")
print(f"needed:  {', '.join(sorted(NEEDED))} -> {'all present' if NEEDED <= caps else 'MISSING ' + ', '.join(sorted(NEEDED - caps))}")
held = sorted(caps & DANGEROUS)
if held or master:
    print(f"UNSAFE:  this key can destroy backups: {', '.join(held) or 'master key, every capability'}")
    print("         fix: ops/b2-mint-keys.sh mints a bucket-scoped key with")
    print("         listBuckets, listFiles, readFiles, writeFiles only.")
    sys.exit(1)
print("SAFE:    write-only key — nothing on this Mac can delete a backup")
PY
