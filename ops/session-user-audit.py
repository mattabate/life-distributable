"""What a session can still reach as the session user (session_user in ops/hub.json).

    ops/py.sh session-user-audit.py        # from a session: one PASS/FAIL line per row
    ops/py.sh session-user-audit.py --wall # the path rows only, from the main checkout (ops/wall-audit.sh, nightly)
    ops/py.sh session-user-audit.py --test # shut_dir and shut_write_tree against folders in each mode

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


def shut_dir(p):
    # No list, no write and no search: a session that cannot enter the folder
    # reaches no file in it by name, whatever that file's own mode (#153).
    row(not any(os.access(p, m) for m in (os.R_OK, os.W_OK, os.X_OK)), "shut-dir " + p, "listable, writable or enterable")


def shut_write_tree(p, skip=()):
    # The owner's checkout is what the hub, the installers and every build run
    # as the owner (.git hooks and the Xcode project included): a session that
    # can write any file or folder in it can run code as the owner.
    if not os.path.exists(p):
        return row(True, "shut-write " + p, "absent")
    # A folder it cannot list is still checked: write without read is a mode.
    hid, bad = [], []
    for d, dirs, files in os.walk(p, onerror=lambda e: hid.append(e.filename)):
        dirs[:] = [x for x in dirs if os.path.join(d, x) not in skip]
        bad += [q for q in [d] + [os.path.join(d, f) for f in files] if os.access(q, os.W_OK)]
    bad += [q for q in hid if os.access(q, os.W_OK)]
    if bad:
        # One line per place, the folder that holds them, with a count and modes.
        by = {}
        for q in bad:
            k = os.path.dirname(os.path.relpath(q, p)) or "."
            by.setdefault(k, set()).add("%o" % (os.lstat(q).st_mode & 0o7777))
        return row(False, "shut-write " + p, "%d writable in %d folders:\n%s" % (len(bad), len(by), "\n".join(
            "      %s (%s)" % (k, ",".join(sorted(v))) for k, v in sorted(by.items()))))
    row(True, "shut-write " + p)


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


def wall(main):
    """The path rows: what the session user must not reach in the main checkout."""
    # The folder is the wall; the files are what is behind it if it is not.
    shut_dir(os.path.join(main, "ops", "secrets"))
    for n in ("hub" + ".token", "decider.hash"):
        shut(os.path.join(main, "ops", "secrets", n))
    # A free ls may name the intake in-box and follows a link in it at run
    # time (actions.lsInRoots checks links when proposed): a session must not
    # plant one there.
    for p in ("data/life.db", "data/life.db-wal", "data/life.db-shm", "data/imports/intake", "ops/bin/hub", ".git", "hub", "ops/hub.json"):
        no_write(os.path.join(main, p))
    row(os.access(os.path.join(main, "data"), os.R_OK), "read " + os.path.join(main, "data"), "not readable")
    # The whole checkout but data/ (its own rows above). Xcode makes its
    # project's swiftpm configuration folder 777; ops/xcode-shut.sh closes it
    # before each build, and this row shows any that are open now.
    shut_write_tree(main, skip=(os.path.join(main, "data"),))


def selftest():
    # As the folder's owner the mode bits are what os.access answers from.
    global fails
    bad = 0
    with tempfile.TemporaryDirectory() as t:
        d = os.path.join(t, "secrets")
        os.mkdir(d)
        for mode, want in ((0o000, 0), (0o100, 1), (0o400, 1), (0o200, 1), (0o700, 1)):
            os.chmod(d, mode)
            fails = 0
            shut_dir(d)
            bad += fails != want
        os.chmod(d, 0o700)
        # shut_write_tree: a read-only build passes; a writable file deep in
        # it, or a writable folder it cannot list, fails.
        b = os.path.join(t, "build")
        deep = os.path.join(b, "life.app", "Contents", "MacOS")
        os.makedirs(deep)
        exe = os.path.join(deep, "life")
        open(exe, "w").close()
        cases = ((0o555, 0o555, 0o555, 0), (0o555, 0o555, 0o755, 1), (0o555, 0o300, 0o555, 1))
        for top, mid, f, want in cases:
            os.chmod(exe, f)
            for x in (deep, os.path.dirname(deep), os.path.dirname(os.path.dirname(deep))):
                os.chmod(x, mid if x == deep else 0o555)
            os.chmod(b, top)
            fails = 0
            shut_write_tree(b)
            bad += fails != want
            for x in (b, os.path.dirname(os.path.dirname(deep)), os.path.dirname(deep), deep):
                os.chmod(x, 0o755)
    print("session-user-audit selftest " + ("ok" if not bad else "FAILED"))
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    args = sys.argv[1:]
    if args == ["--test"]:
        selftest()
    if args == ["--wall"]:
        # Run as the session user from the main checkout (ops/wall-audit.sh):
        # this checkout is the main one, and the clone-only rows are left out.
        wall(REPO)
        print("session-user-audit: %d failed" % fails)
        sys.exit(1 if fails else 0)
    main = main_checkout()
    if not os.path.isdir(os.path.join(main, ".git")):
        sys.exit("session-user-audit: run it from a session clone (its origin is the main checkout)")
    wall(main)
    writes(os.path.join(REPO, "ops", "logs"))
    row(bool(os.environ.get("LIFE_SESSION_TOKEN")), "its own session token", "no LIFE_SESSION_TOKEN")
    probe("db.sh", ["bash", "ops/db.sh", "select count(*) from threads"])
    probe("lifectl threads", ["lifectl", "threads"])
    probe("py-lint.py", ["ops/py.sh", "py-lint.py"])
    probe("git fetch origin", ["git", "fetch", "--quiet", "--dry-run", "origin", "main"])
    print("session-user-audit: %d failed" % fails)
    sys.exit(1 if fails else 0)
