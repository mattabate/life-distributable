# ops/ — the commands that run this install

`hub/` is the server, `app/` the phone and desktop apps, `shared/` the
contract between them. `ops/` is everything else it takes to *operate* one
install on one Mac: build it, start it, back it up, ship the app, look at
the UI, and give unattended agent sessions a safe way to touch any of that.

The rule behind the folder (CLAUDE.md, convention 3): **a thing you do
twice is a script, and a session may run a script but not improvise the
command.** Sessions here have no `curl`, `sqlite3` or `python3 -c`; they
get `ops/db.sh "select …"` (read-only), `ops/py.sh <tool>.py` (only a `.py`
inside this folder) and the one-command scripts below. So the folder is
also the permission boundary: what is in here is what an agent can do to
your machine.

## What is in it

| Group | Files | Why |
|---|---|---|
| Hub lifecycle | `hub.sh`, `com.life.*.plist`, `renew-cert.sh`, `hub.json.example` | `make install` builds the binary into `ops/bin/` and loads the launchd agents (hub, keep-awake, cert renewal). `hub.json` is this install's config, written by setup. |
| Setup | `setup.py`, `decider-set.sh`, `app-identity.sh`, `app.env.example` | Writes this Mac's config files from what SETUP.md asks; sets the decider code the owner holds for Approve/Deny. |
| Backup | `backup.sh`, `b2-*.sh`, `restic.env.example` | Nightly encrypted restic backup of `data/` to a B2 bucket. Pruning (the only step that deletes) is a separate script the owner runs by hand. |
| Shipping the apps | `install-phone.sh`, `ota.sh`, `install-mac.sh`, `mac-apply.sh`, `mac-swap.sh` | Build, sign and install the iPhone app (cable or over the air) and the desktop app. |
| Looking at the UI | `screens.sh`, `app-ui.sh`, `mac-screens.sh`, `web-smoke.sh`, `web-widths.sh`, `browse.js`, `flows/`, `web-preview.html`, `web-check.html` | Screenshot or drive every surface so a change is looked at before it ships. Output lands in `ops/logs/` (git-ignored: it shows your data). |
| Demo world | `demo-hub.js`, `demo-fixtures.js`, `demo-screens.sh`, `demo-mac-screens.sh` | A stand-in hub with scrubbed data, used only for the README pictures. |
| Checks | `webcheck.sh`, `py-lint.py`, `mac-keychain-probe.sh` | Parts of `make check`. |
| For sessions | `db.sh`, `py.sh`, `commit.sh`, `hublib.py`, `blob-put.py`, `wait-for.py`, `sim-pick.py`, `schedule.json` | The narrow tools an agent is allowed: read the database, run a vetted script, commit only named paths, store a file on the hub. `schedule.json` is the standing session roster. |

## What is not committed

`ops/secrets/`, `ops/hub.json`, `ops/app.env`, `ops/bin/` and `ops/logs/`
are written on this machine at setup or by the scripts, and are
git-ignored. Nothing in them should ever appear in a chat, a commit or a
screenshot you share.

## Adding one

New code goes in a script here, with a usage comment at the top (every file
in this folder has one; `head -5 ops/<file>` tells you what it is for) and,
if it is something you run, a `make` target that calls it. If a session
needs it, add the script, not a broader tool grant.
