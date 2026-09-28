#!/usr/bin/env python3
"""Store a file on the hub as a photo observation and print its blob ref.

The app does this for every photo the owner attaches (multipart POST /observations,
kind photo, blob content-addressed under data/blobs/sha256/). A session has no
way to attach a file to its own reply, so this is how a picture a session made
reaches them: the blob's URL opens in the console (cookie auth) and
`GET /api/v1/blobs/<ref>` serves it. Address, token and TLS come from
hublib (ops/hub.json); only the hub on the tailnet is contacted.

  ops/py.sh blob-put.py <file> [note]     -> prints ref and the console URL
"""
import json
import mimetypes
import os
import sys
import uuid

import hublib


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__)
        return 2
    path = sys.argv[1]
    note = sys.argv[2] if len(sys.argv) > 2 else ""
    name = os.path.basename(path)
    ctype = mimetypes.guess_type(name)[0] or "application/octet-stream"
    boundary = "----life" + uuid.uuid4().hex
    payload = json.dumps({"via": "session", "note": note})
    fields = [("source", "claude"), ("kind", "photo"), ("payload", payload)]
    body = b""
    for k, v in fields:
        body += (f"--{boundary}\r\nContent-Disposition: form-data; name=\"{k}\"\r\n\r\n{v}\r\n").encode()
    body += (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{name}\"\r\n"
             f"Content-Type: {ctype}\r\n\r\n").encode()
    body += open(path, "rb").read() + b"\r\n"
    body += f"--{boundary}--\r\n".encode()

    st, out = hublib.request("POST", "/api/v1/observations", body,
                             {"Content-Type": f"multipart/form-data; boundary={boundary}"})
    if st >= 300:
        print(f"HTTP {st}: {out.decode('utf-8', 'replace')[:300]}")
        return 1
    ref = json.loads(out).get("blob_ref", "")
    print(f"{name}: {ref}")
    print(hublib.url(f"/api/v1/blobs/{ref}"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
