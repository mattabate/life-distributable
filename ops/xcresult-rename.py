#!/usr/bin/env python3
"""Give exported .xcresult attachments their real names.

    xcrun xcresulttool export attachments --path X.xcresult --output-path dir
    ops/py.sh xcresult-rename.py dir

Xcode writes the files under opaque names plus a manifest.json that carries the
name the test asked for ("04-account.png"). This is the fallback lane of
ops/app-ui.sh: it only runs when the simulator could not write to the host
path, and without it the pictures keep names nothing else can find.
"""
import json
import os
import sys


def main() -> int:
    out = sys.argv[1] if len(sys.argv) > 1 else "."
    manifest = os.path.join(out, "manifest.json")
    if not os.path.exists(manifest):
        print("xcresult-rename: no manifest.json in %s, nothing to rename" % out)
        return 0
    with open(manifest) as fh:
        data = json.load(fh)
    entries = data if isinstance(data, list) else [data]
    renamed = 0
    for test in entries:
        for att in test.get("attachments", []):
            src = att.get("exportedFileName")
            dst = att.get("suggestedHumanReadableName") or src
            if not src or src == dst or os.path.basename(dst) != dst:
                continue
            s, d = os.path.join(out, src), os.path.join(out, dst)
            if os.path.exists(s) and not os.path.exists(d):
                os.rename(s, d)
                renamed += 1
    print("xcresult-rename: %d file(s)" % renamed)
    return 0


if __name__ == "__main__":
    sys.exit(main())
