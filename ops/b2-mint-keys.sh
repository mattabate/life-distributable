#!/usr/bin/env bash
# ONE-TIME, RUN BY THE OWNER IN A TERMINAL (after `ops/py.sh setup.py backup`
# wrote ops/secrets/restic.env with the bucket name). Mints two keys scoped to
# the backup bucket:
#
#   life-nightly  listBuckets, listFiles, readFiles, writeFiles
#                 -> written into ops/secrets/restic.env, used by the nightly
#                    backup. Cannot delete a byte, cannot touch bucket settings.
#   life-prune    the same + deleteFiles
#                 -> PRINTED ONCE for Apple Passwords. Never stored here.
#                    ops/b2-prune.sh asks for it monthly.
#
# It also generates the restic encryption passphrase if restic.env still has
# the placeholder, and prints it once — lose it and every backup is unreadable.
#
# The Backblaze web UI only offers coarse "Read and Write" (which includes
# delete), so this mints the keys through the API where capabilities are exact.
# Your master key is typed in, held in memory for this run, and never written.
set -euo pipefail
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "$(dirname "$0")"

if [[ ! -t 0 || ! -r /dev/tty ]]; then
  echo "b2-mint-keys.sh is interactive on purpose: it needs your master key" >&2
  echo "typed by hand. No session or cron job can run it." >&2
  exit 2
fi

echo "Backblaze -> Application Keys -> Master Application Key -> Generate New Master Application Key."
echo "(It is shown once; you only need it for this run. Generating a new one"
echo " invalidates only the old master key, not bucket keys.)"
read -r  -p "  master keyID: " B2_MASTER_ID  < /dev/tty
read -rs -p "  master key:   " B2_MASTER_KEY < /dev/tty; echo
export B2_MASTER_ID B2_MASTER_KEY

python3 - <<'PY'
import base64, json, os, re, secrets, sys, urllib.request, urllib.error

ENV = "secrets/restic.env"
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
