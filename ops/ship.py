"""Build the phone or desktop app from a session, as the owner's user, with no card.
Usage: ship.py phone|mac "<what it fixes>"

With session_user set, a session cannot read the signing setup, so `make ship`
and `make mac` from its clone come here (ops/ota.sh, ops/install-mac.sh): the
hub runs the build from main on the standing approval (POST /api/v1/app/ship)
and this waits for it. Land first (ops/land.sh): the build is main's. After a
phone build, raise the one install ask with the link the output ends with."""
import os
import subprocess
import sys

import hublib

if len(sys.argv) != 3 or sys.argv[1] not in ("phone", "mac") or not sys.argv[2].strip():
    sys.exit(__doc__)
a = hublib.call("POST", "/api/v1/app/ship", {"target": sys.argv[1], "why": sys.argv[2].strip()})
print(a.get("title", ""), a["id"])
here = os.path.dirname(os.path.abspath(__file__))
sys.exit(subprocess.call([sys.executable, os.path.join(here, "action-wait.py"), a["id"]]))
