#!/usr/bin/env python3
"""Convert an image the Read tool can't open (HEIC, HEIF, AVIF) to a JPEG.

Usage: ops/py.sh img-to-jpeg.py <src> [out.jpg] [max_edge=1600]

Prints the JPEG's path; Read that. With no out, it goes to the system temp
dir as <src name>.jpg. The hub's prompt names this line for every such
attachment (threads/runner.go): Read takes PNG, JPEG, GIF and WebP only, and a
bare `sips` call is not on the pre-approved list. sips ships with macOS; no
Pillow needed.
"""
import os
import subprocess
import sys
import tempfile


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__)
        return 2
    src = sys.argv[1]
    base = os.path.splitext(os.path.basename(src))[0] + ".jpg"
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(tempfile.gettempdir(), base)
    max_edge = sys.argv[3] if len(sys.argv) > 3 else "1600"
    r = subprocess.run(
        ["sips", "-s", "format", "jpeg", "-s", "formatOptions", "85",
         "-Z", max_edge, src, "--out", out],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        print(f"img-to-jpeg: {(r.stderr or r.stdout).strip()[:300]}", file=sys.stderr)
        return 1
    print(out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
