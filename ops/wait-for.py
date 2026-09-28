#!/usr/bin/env python3
"""Block until a file has a line matching a regex, then print that line.

    ops/py.sh wait-for.py <file> <regex> [timeout-seconds=540]

For a build another lane started (the hub's OTA lane writes
ops/logs/app-install.log and ends with "exit=N"): a session cannot sleep in
its shell, so it waits here, in the foreground, and reads the result.
Exit 0 with the line; exit 1 on timeout (the last line is printed).
"""
import re
import sys
import time


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__)
        return 2
    path, pattern = sys.argv[1], re.compile(sys.argv[2])
    limit = float(sys.argv[3]) if len(sys.argv) > 3 else 540.0
    start = time.time()
    last = ""
    while time.time() - start < limit:
        try:
            with open(path, encoding="utf-8", errors="replace") as f:
                lines = f.read().splitlines()
        except FileNotFoundError:
            lines = []
        if lines:
            last = lines[-1]
        for line in lines:
            if pattern.search(line):
                print(line)
                return 0
        time.sleep(3)
    print(f"timeout after {int(limit)}s; last line: {last}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
