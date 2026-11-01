"""Wait for an action the hub runs on the standing approval and print how it ended.
Usage: action-wait.py <file with the POST's JSON answer> | <action id>

With session_user set, ops/land.sh and ops/ship.py hand the hub a landing or an
app build, which it decides at once with no card (server/land.go standing). The
session still has to know whether main moved: this polls the action until it is
done, failed or uncertain, prints its result, and exits 0 only for done. A
landing that touches hub/ restarts the hub after it finishes, so a failed poll is
retried."""
import json
import os
import sys
import time
import urllib.parse

import hublib

if len(sys.argv) != 2:
    sys.exit(__doc__)
arg = sys.argv[1]
if os.path.exists(arg):
    with open(arg) as f:
        a = json.load(f)
    if "id" not in a:
        sys.exit("hub: %s" % a.get("error", a))
    print(a.get("title", ""), a["id"])
    aid = a["id"]
else:
    aid = arg
deadline = time.time() + 35 * 60
while True:
    try:
        st, out = hublib.request("GET", "/api/v1/actions/" + urllib.parse.quote(aid))
        a = json.loads(out.decode() or "null") if st == 200 else {}
    except Exception:  # the hub restarting after a hub/ landing
        a = {}
    state = a.get("state", "")
    if state in ("done", "failed", "uncertain"):
        break
    if state == "proposed":
        print("waiting on the owner's card (this one is not on the standing approval)")
        sys.exit(3)
    if time.time() > deadline:
        sys.exit("still %s after 35 min: lifectl api GET /api/v1/actions/%s" % (state or "unreachable", aid))
    time.sleep(3)
print((a.get("result") or "").rstrip()[-3000:])
if state != "done":
    print("%s: %s" % (state, a.get("error", "")))
    sys.exit(1)
print("== done")
