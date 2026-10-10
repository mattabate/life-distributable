#!/usr/bin/env bash
# The Backblaze side of the backup. Three verbs, one file:
#
#   b2.sh check   What may the key in ops/secrets/restic.env actually do?
#                 Exit 0 = SAFE (write-only). Exit 1 = it can destroy backups,
#                 or it no longer authenticates, which means NO backup is
#                 running. ops/backup.sh runs this at the end of every night.
#                 Never prints the key.
#   b2.sh mint    ONE-TIME, RUN BY THE OWNER IN A TERMINAL (after
#                 `ops/py.sh setup.py backup` wrote restic.env with the bucket).
#                 Mints two keys scoped to the bucket:
#                   life-nightly  listBuckets, listFiles, readFiles, writeFiles
#                                 -> written into restic.env for the nightly
#                                    backup. Cannot delete a byte.
#                   life-prune    the same + deleteFiles -> PRINTED ONCE for
#                                 Apple Passwords, never stored on this Mac.
#                 Also generates the restic passphrase if restic.env still
#                 has the placeholder, and prints it once. The web UI only
#                 offers coarse "Read and Write" (which includes delete), so
#                 the keys are minted through the API where capabilities are
#                 exact. The master key is typed in, held in memory for this
#                 run, and never written.
#   b2.sh prune   MONTHLY, RUN BY THE OWNER IN A TERMINAL. Trims old snapshots.
#                 Pruning is the only part of the backup that deletes, so it
#                 is the only part that needs the delete-capable key: it is
#                 pasted in from Apple Passwords and never written to disk.
#
# The split is the point: anything running on this Mac, including a session
# that followed an injected instruction, holds a key that cannot erase a
# backup. mint and prune refuse to run without a terminal.
set -euo pipefail
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "$(dirname "$0")"
ENV=secrets/restic.env

need_tty() {
  if [[ ! -t 0 || ! -r /dev/tty ]]; then
    echo "b2.sh $1 is interactive on purpose: it needs a key typed by hand." >&2
    echo "No session or cron job can run it." >&2
    exit 2
  fi
}

check() {
  set -a; source "$ENV"; set +a
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
        print("         -> NO BACKUP IS RUNNING. Fix: ops/b2.sh mint (needs the")
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
    print("         fix: ops/b2.sh mint mints a bucket-scoped key with")
    print("         listBuckets, listFiles, readFiles, writeFiles only.")
    sys.exit(1)
print("SAFE:    write-only key — nothing on this Mac can delete a backup")
PY
}

mint() {
  need_tty mint
  echo "Backblaze -> Application Keys -> Master Application Key -> Generate New Master Application Key."
  echo "(It is shown once; you only need it for this run. Generating a new one"
  echo " invalidates only the old master key, not bucket keys.)"
  read -r  -p "  master keyID: " B2_MASTER_ID  < /dev/tty
  read -rs -p "  master key:   " B2_MASTER_KEY < /dev/tty; echo
  export B2_MASTER_ID B2_MASTER_KEY ENV
  python3 - <<'PY'
import base64, json, os, re, secrets, sys, urllib.request, urllib.error

ENV = os.environ["ENV"]
NIGHTLY = ["listBuckets", "listFiles", "readFiles", "writeFiles"]
PRUNE = NIGHTLY + ["deleteFiles"]


def api(url, token=None, body=None, basic=None):
    headers = {}
    if basic:
        headers["Authorization"] = "Basic " + base64.b64encode(basic.encode()).decode()
    if token:
        headers["Authorization"] = token
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    try:
        with urllib.request.urlopen(urllib.request.Request(url, data=data, headers=headers), timeout=30) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        sys.exit(f"Backblaze said no ({e.code}): {e.read().decode()[:300]}")


if not os.path.exists(ENV):
    sys.exit("no ops/secrets/restic.env yet — run: ops/py.sh setup.py backup --bucket <name>")
env_text = open(ENV).read()
cur = dict(re.findall(r"(?m)^\s*(?:export\s+)?([A-Z0-9_]+)=(.*)$", env_text))
repo = cur.get("RESTIC_REPOSITORY", "").strip().strip('"').strip("'")
bucket = repo.split(":")[1] if repo.startswith("b2:") else ""
if not bucket or bucket.startswith("<"):
    sys.exit(f"cannot read the bucket name out of RESTIC_REPOSITORY={repo!r}")

mid, mkey = os.environ["B2_MASTER_ID"], os.environ["B2_MASTER_KEY"]
auth = api("https://api.backblazeb2.com/b2api/v2/b2_authorize_account", basic=f"{mid}:{mkey}")
if "writeKeys" not in auth.get("allowed", {}).get("capabilities", []):
    sys.exit("that key cannot create keys — it is not the master application key")

buckets = api(auth["apiUrl"] + "/b2api/v2/b2_list_buckets", token=auth["authorizationToken"],
              body={"accountId": auth["accountId"], "bucketName": bucket})["buckets"]
if not buckets:
    sys.exit(f"no bucket named {bucket}")
bucket_id = buckets[0]["bucketId"]


def mint(name, caps):
    k = api(auth["apiUrl"] + "/b2api/v2/b2_create_key", token=auth["authorizationToken"],
            body={"accountId": auth["accountId"], "capabilities": caps,
                  "keyName": name, "bucketId": bucket_id})
    return k["applicationKeyId"], k["applicationKey"]

nid, nkey = mint("life-nightly", NIGHTLY)
pid, pkey = mint("life-prune", PRUNE)

# Prove the nightly key has exactly what it needs BEFORE it is installed.
check = api("https://api.backblazeb2.com/b2api/v2/b2_authorize_account", basic=f"{nid}:{nkey}")
caps = set(check.get("allowed", {}).get("capabilities", []))
if "deleteFiles" in caps or not set(NIGHTLY) <= caps:
    sys.exit(f"refusing to install a key with the wrong capabilities: {sorted(caps)}")


def put(text, var, val):
    line = f'export {var}="{val}"'
    if re.search(rf"(?m)^\s*(?:export\s+)?{var}=", text):
        return re.sub(rf"(?m)^\s*(?:export\s+)?{var}=.*$", line.replace("\\", "\\\\"), text)
    return text.rstrip("\n") + "\n" + line + "\n"

new_text = put(put(env_text, "B2_ACCOUNT_ID", nid), "B2_ACCOUNT_KEY", nkey)
passphrase = ""
if cur.get("RESTIC_PASSWORD", "").strip().strip('"').startswith("<"):
    passphrase = "-".join(secrets.token_urlsafe(6) for _ in range(5))
    new_text = put(new_text, "RESTIC_PASSWORD", passphrase)
tmp = ENV + ".new"
fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w") as f:
    f.write(new_text)
os.replace(tmp, ENV)

print()
print("ops/secrets/restic.env now holds life-nightly (no delete, no bucket settings).")
print()
print("SAVE IN APPLE PASSWORDS NOW — shown once, not stored on this Mac:")
print(f"  entry 'life backup prune key':  keyID {pid}   key {pkey}")
if passphrase:
    print(f"  entry 'life backup passphrase': {passphrase}")
    print("  (the passphrase IS stored in restic.env too — but if this Mac dies,")
    print("   Apple Passwords is the only copy, and without it the backup is noise)")
print()
print("Then tell your Claude session: done.")
PY
}

prune() {
  need_tty prune
  set -a; source "$ENV"; set +a   # repository + encryption password
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
}

case "${1:-}" in
  check) check ;;
  mint)  mint ;;
  prune) prune ;;
  *) sed -n '2,30p' "$0"; exit 1 ;;
esac
