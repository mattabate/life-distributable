#!/usr/bin/env python3
"""What is the B2 key in ops/secrets/restic.env allowed to do, and will the
bucket keep what it writes? Run through `ops/b2.sh check` (it loads the env).

The nightly backup only ever needs to LIST and WRITE. A key that can also
delete (or rewrite bucket settings) means anything running on this Mac -
including a session that followed an injected instruction - can erase every
backup.

The key is half of it. A bucket lifecycle rule deletes on its own: one with
daysFromUploadingToHiding expires every backup after N days, and one with
daysFromHidingToDeleting turns a write-only key into a delete key, because
writeFiles may hide a file and the rule then erases it. So SAFE also needs
the bucket's rules read and none that remove files.

Exit 0 = safe. Exit 1 = the key or the bucket can lose backups, the key no
longer authenticates, or the rules could not be read. Never prints the key.
  ops/py.sh b2-key-check.py --test   the verdicts, offline (make check)
"""
import base64, json, os, sys, urllib.error, urllib.request

# Everything the nightly run needs, and nothing else.
NEEDED = {"listBuckets", "listFiles", "readFiles", "writeFiles"}
# Capabilities that let the holder destroy or silently expire the backups.
DANGEROUS = {"deleteFiles", "deleteBuckets", "deleteKeys", "writeKeys",
             "writeBuckets", "writeBucketLifecycleRules", "writeBucketReplications"}


def rule_risks(rules):
    """Words for each lifecycle rule that can remove a backup; [] = none."""
    out = []
    for r in rules or []:
        where = f"prefix '{r.get('fileNamePrefix', '')}'"
        if r.get("daysFromUploadingToHiding") is not None:
            out.append(f"{where} expires every file {r['daysFromUploadingToHiding']} days after upload")
        if r.get("daysFromHidingToDeleting") is not None:
            out.append(f"{where} deletes hidden files after {r['daysFromHidingToDeleting']} days"
                       " (a write-only key can hide, so it can delete)")
    return out


def verdict(auth, kid, bucket_rules):
    """(lines, ok). bucket_rules: the bucket's lifecycleRules, or None = unread."""
    allowed = auth.get("allowed", {})
    caps = set(allowed.get("capabilities", []))
    master = auth.get("accountId") == kid
    lines = [f"bucket:  {allowed.get('bucketName') or 'ALL BUCKETS'}{' (MASTER KEY)' if master else ''}",
             f"needed:  {', '.join(sorted(NEEDED))} -> "
             + ('all present' if NEEDED <= caps else 'MISSING ' + ', '.join(sorted(NEEDED - caps)))]
    ok = True
    held = sorted(caps & DANGEROUS)
    if held or master:
        ok = False
        lines += [f"UNSAFE:  this key can destroy backups: {', '.join(held) or 'master key, every capability'}",
                  "         fix: ops/b2.sh mint mints a bucket-scoped key with",
                  "         listBuckets, listFiles, readFiles, writeFiles only."]
    if bucket_rules is None:
        ok = False
        lines.append("UNKNOWN: could not read the bucket's lifecycle rules - not SAFE")
    else:
        risks = rule_risks(bucket_rules)
        if risks:
            ok = False
            lines += [f"UNSAFE:  lifecycle rule: {r}" for r in risks]
            lines.append("         fix: Backblaze -> Buckets -> Lifecycle Settings -> Keep all versions of the file")
        else:
            lines.append("rules:   no lifecycle rule removes files")
    if ok:
        lines.append("SAFE:    write-only key, keep-all bucket - nothing on this Mac can delete a backup")
    return lines, ok


def post(url, token, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, headers={"Authorization": token,
                                                          **({"Content-Type": "application/json"} if data else {})})
    with urllib.request.urlopen(req, timeout=20) as r:
        return json.load(r)


def main():
    kid, key = os.environ["B2_ACCOUNT_ID"], os.environ["B2_ACCOUNT_KEY"]
    try:
        auth = post("https://api.backblazeb2.com/b2api/v2/b2_authorize_account",
                    "Basic " + base64.b64encode(f"{kid}:{key}".encode()).decode())
    except urllib.error.HTTPError as e:
        if e.code in (401, 403):
            print(f"DEAD:    the key in secrets/restic.env ({kid}) no longer authenticates")
            print("         -> NO BACKUP IS RUNNING. Fix: ops/b2.sh mint (needs the")
            print("            master key) installs a fresh nightly key.")
            sys.exit(1)
        raise
    except urllib.error.URLError as e:
        print(f"UNKNOWN: could not reach Backblaze ({e.reason}) - key not checked")
        sys.exit(1)

    # The bucket restic writes to: b2:<bucket>:<path>.
    repo = os.environ.get("RESTIC_REPOSITORY", "")
    name = auth.get("allowed", {}).get("bucketName") or (repo.split(":")[1] if repo.count(":") >= 2 else None)
    rules = None
    try:
        body = {"accountId": auth["accountId"]}
        if auth.get("allowed", {}).get("bucketId"):
            body["bucketId"] = auth["allowed"]["bucketId"]
        for b in post(auth["apiUrl"] + "/b2api/v2/b2_list_buckets", auth["authorizationToken"], body)["buckets"]:
            if b.get("bucketName") == name:
                rules = b.get("lifecycleRules") or []
    except (urllib.error.URLError, KeyError) as e:
        print(f"         b2_list_buckets: {e}")
    lines, ok = verdict(auth, kid, rules)
    print("\n".join(lines))
    sys.exit(0 if ok else 1)


def test():
    scoped = {"accountId": "acct", "allowed": {"bucketName": "b", "capabilities": sorted(NEEDED)}}
    _, ok = verdict(scoped, "k", [])
    assert ok, "write-only key + keep-all bucket is SAFE"
    _, ok = verdict(scoped, "k", None)
    assert not ok, "unread rules must not be SAFE"
    lines, ok = verdict(scoped, "k", [{"fileNamePrefix": "", "daysFromHidingToDeleting": 1,
                                       "daysFromUploadingToHiding": None}])
    assert not ok and any("hidden" in l for l in lines), "keep-only-last-version is UNSAFE"
    _, ok = verdict(scoped, "k", [{"fileNamePrefix": "life/", "daysFromHidingToDeleting": None,
                                   "daysFromUploadingToHiding": 30}])
    assert not ok, "an expiring rule is UNSAFE"
    deleter = {"accountId": "acct", "allowed": {"capabilities": sorted(NEEDED | {"deleteFiles"})}}
    _, ok = verdict(deleter, "k", [])
    assert not ok, "deleteFiles is UNSAFE"
    print("b2-key-check: OK")


if __name__ == "__main__":
    test() if "--test" in sys.argv[1:] else main()
