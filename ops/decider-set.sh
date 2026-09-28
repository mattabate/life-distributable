#!/usr/bin/env bash
# RUN BY THE OWNER IN A TERMINAL. Sets (or rotates) the decider code — the
# second credential that Approve/Deny needs on top of the hub token.
#
# Why it exists: every session holds the hub token, so without it a session
# could propose a gated action (money, delete, contact, share, commit) and
# approve its own proposal in the next tool call. The hub also demands a code
# it has never seen: it stores only the SHA-256, in ops/secrets/decider.hash.
#
# The code is printed ONCE, here, to you. Type it into:
#   - the phone: Settings -> Decider code (stored in the iOS Keychain)
#   - the console: it asks the first time you approve something
# Nothing else ever needs it. Rotating = running this again and retyping it in
# both places; until you do, Approve stops working there.
set -euo pipefail
cd "$(dirname "$0")"

if [[ ! -t 0 || ! -r /dev/tty ]]; then
  echo "decider-set.sh is interactive on purpose: the code must be seen by you" >&2
  echo "and by nothing else. No session or cron job can run it." >&2
  exit 2
fi

HASH=secrets/decider.hash
if [[ -f "$HASH" ]]; then
  read -r -p "A decider code is already set. Replace it? [y/N] " yn < /dev/tty
  [[ "$yn" == [yY]* ]] || { echo "unchanged"; exit 0; }
fi
mkdir -p secrets

python3 - <<'PY'
import hashlib, os, secrets

ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"   # no I, L, O, U — typed by hand
groups = ["".join(secrets.choice(ALPHABET) for _ in range(5)) for _ in range(4)]
code = "-".join(groups)

path = "secrets/decider.hash"
fd = os.open(path + ".new", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w") as f:
    f.write(hashlib.sha256(code.encode()).hexdigest() + "\n")
os.replace(path + ".new", path)

print()
print("  Your decider code:  " + code)
print()
print("  Save it in Apple Passwords now (title: life decider code). Then type it")
print("  into the phone (Settings -> Decider code) and into the console the next")
print("  time it asks. It is not stored anywhere else.")
print()
print("  The hub keeps only ops/secrets/decider.hash. From now on Approve/Deny")
print("  needs this code: the hub token alone cannot approve anything.")
PY
