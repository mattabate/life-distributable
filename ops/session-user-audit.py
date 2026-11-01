"""What a session can still reach as the session user (session_user in ops/hub.json).

    ops/py.sh session-user-audit.py        # from a session: one PASS/FAIL line per row

Read-only: every probe is a read, a self-test or a lookup; nothing is written
but a temp file it removes. A FAIL on a `shut` or `no-write` row is a hole in
the wall; a FAIL on any other row is something a session can no longer do.
"""
import os
import subprocess
import sys
import tempfile

import hublib

REPO = hublib.REPO  # the session's clone
fails = 0


def row(ok, name, why=""):
    global fails
    fails += not ok
    print("%-5s %-40s %s" % ("PASS" if ok else "FAIL", name, why))


def main_checkout():
    """The clone's origin: the owner's checkout."""
    p = subprocess.run(["git", "-C", REPO, "remote", "get-url", "origin"], capture_output=True, text=True)
    return p.stdout.strip()


def shut(p):
    row(not (os.access(p, os.R_OK) or os.access(p, os.W_OK)), "shut " + p, "readable or writable")


def no_write(p):
    row(not os.access(p, os.W_OK), "no-write " + p, "writable")


def writes(d):
    try:
        fd, t = tempfile.mkstemp(prefix=".audit.", dir=d)
        os.close(fd)
        os.unlink(t)
        row(True, "write " + d)
    except OSError as e:
        row(False, "write " + d, e.strerror)


def probe(name, argv):
    try:
        p = subprocess.run(argv, cwd=REPO, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=90)
    except (OSError, subprocess.TimeoutExpired) as e:
        return row(False, name, str(e)[:110])
    lines = [l for l in (p.stdout + p.stderr).splitlines() if l.strip()]
    row(p.returncode == 0, name, (lines[-1][:110] if lines else "exit %d" % p.returncode))


if __name__ == "__main__":
    main = main_checkout()
    if not os.path.isdir(os.path.join(main, ".git")):
        sys.exit("session-user-audit: run it from a session clone (its origin is the main checkout)")
    for n in ("hub" + ".token", "decider.hash"):
        shut(os.path.join(main, "ops", "secrets", n))
    for p in ("data/life.db", "ops/bin/hub", ".git", "hub", "ops/hub.json"):
        no_write(os.path.join(main, p))
    row(os.access(os.path.join(main, "data"), os.R_OK), "read " + os.path.join(main, "data"), "not readable")
    writes(os.path.join(REPO, "ops", "logs"))
    row(bool(os.environ.get("LIFE_SESSION_TOKEN")), "its own session token", "no LIFE_SESSION_TOKEN")
    probe("db.sh", ["bash", "ops/db.sh", "select count(*) from threads"])
    probe("lifectl threads", ["lifectl", "threads"])
    probe("py-lint.py", ["ops/py.sh", "py-lint.py"])
    probe("git fetch origin", ["git", "fetch", "--quiet", "--dry-run", "origin", "main"])
    print("session-user-audit: %d failed" % fails)
    sys.exit(1 if fails else 0)
