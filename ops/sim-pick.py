#!/usr/bin/env python3
"""Pick a simulator UDID by device name, from `xcrun simctl list devices -j` on stdin.

    xcrun simctl list devices available -j | ops/py.sh sim-pick.py "iPhone 17"

An already-booted device wins (booting costs ~20 s), then an exact name match,
then a prefix match — so "iPhone 17" never silently picks "iPhone 17 Pro Max"
when the plain one exists. Prints nothing and exits 1 if there is no match.
"""
import json
import sys


def main() -> int:
    want = (sys.argv[1] if len(sys.argv) > 1 else "iPhone").strip().lower()
    data = json.load(sys.stdin)["devices"]
    exact, prefix = [], []
    for runtime, devices in data.items():
        if "iOS" not in runtime:
            continue
        for d in devices:
            if d.get("isAvailable") is False:
                continue
            name = d["name"].strip().lower()
            row = (d.get("state") == "Booted", d["udid"])
            if name == want:
                exact.append(row)
            elif name.startswith(want):
                prefix.append(row)
    for pool in (exact, prefix):
        if pool:
            pool.sort(key=lambda r: not r[0])  # booted first
            print(pool[0][1])
            return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
