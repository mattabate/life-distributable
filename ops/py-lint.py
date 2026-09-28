#!/usr/bin/env python3
"""Lint every Python tool in ops/ (part of `make check`). Reads files only.

    ops/py.sh py-lint.py [file.py …]     default: every ops/*.py

Fails on:
  - a syntax error (ast.parse — nothing is executed or written)
  - sqlite3.connect(...) without mode=ro: life.db is read-only outside the hub;
    use hublib.db()
  - TLS verification switched off (CERT_NONE / check_hostname = False)
  - the hub's hostname written into a script: it lives in ops/hub.json
    `public_host`, read through hublib (the Mac mini move is one line)
  - a fixed Eastern offset (timedelta(hours=-4/-5)): DST moves it; use hublib.ET
  - a secrets token read by hand (github.token / hub.token): use hublib

The review behind each rule: docs/history/infra.md (2026-09-26, one hub client).
"""
import ast
import glob
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
RULES = [
    (re.compile(r"sqlite3\.connect\((?![^\n]*mode=ro)"), "sqlite3.connect without mode=ro — use hublib.db()"),
    (re.compile(r"CERT_NONE|check_hostname\s*=\s*False|_create_unverified_context"), "TLS verification off — use hublib.tls()"),
    (re.compile(r"[a-z0-9-]+\.[a-z0-9-]+\.ts\.net"), "hub host hard-coded — read hublib.host() (ops/hub.json public_host)"),
    (re.compile(r"timedelta\(hours=-?[45]\)"), "fixed Eastern offset — use hublib.ET / today_et()"),
    (re.compile(r"secrets/(github|hub)\.token"), "token read by hand — use hublib.gh() / hublib.call()"),
]

files = sys.argv[1:] or sorted(glob.glob(os.path.join(HERE, "*.py")))
bad = 0
for f in files:
    name = os.path.basename(f)
    src = open(f, encoding="utf-8").read()
    try:
        ast.parse(src, filename=name)
    except SyntaxError as e:
        print("%s:%s: syntax: %s" % (name, e.lineno, e.msg))
        bad += 1
        continue
    if name in ("hublib.py", "py-lint.py"):
        continue
    for i, line in enumerate(src.splitlines(), 1):
        for pat, why in RULES:
            if pat.search(line):
                print("%s:%d: %s" % (name, i, why))
                bad += 1
if bad:
    sys.exit("py-lint: %d problem(s)" % bad)
print("py-lint: %d files ok" % len(files))
