# life — a personal hub, for one owner

One person's hub: a Go backend on their Mac (`hub/`), a web console it
serves, and a SwiftUI iPhone app (`app/`). Hub sessions get the operating
model (asks, the gate, cards, calendar, recs) in their preamble; this file
is for changing the code. New install? Read `SETUP.md` instead.

Context is cost: every turn re-reads everything loaded. Open only the file
your task needs — `hub/CLAUDE.md` for backend recipes, `shared/api.md` for
one endpoint's contract.

## Hard rules

- `data/` holds the owner's personal data: never commit it, never send it to
  any external service. Besides Anthropic (plan usage) and Apple (push), the
  hub's only outbound call is the opt-in weekly heartbeat
  (`hub/internal/usage`, off unless the owner said yes at setup; the app's
  Settings switches it), which carries counts only.
- The hub binds only to the Tailscale interface. Nothing public-facing, ever.
- Gate (hub-enforced): money, delete, contact, share, push →
  `lifectl propose`. Approval needs the decider code, which only the owner
  holds (`ops/decider-set.sh`). A local `git commit` is not gated.
- `ops/secrets/`, `ops/hub.json`, `ops/app.env`, `app/local.xcconfig` are
  written at setup, git-ignored, and never printed into a chat.

## Conventions

1. **Contract first.** An endpoint is a row in `hub/internal/server/routes.go`
   and a `### METHOD /path` section in `shared/api.md`; a test fails if they
   drift. `make generate` rewrites `shared/fixtures/` from a synthetic DB, and
   the app's tests decode those same files, so both sides agree by test.
2. **`make check` is the gate**: gofmt, vet, Go tests, web tests, script
   lint, the hub build and a simulator build of the app. Nothing is done
   until it passes.
3. **One command per operation, in `ops/`.** A thing you do twice is a
   script. `python3 -c`, `sqlite3` and `curl` are not in session tool grants
   on purpose: new code goes in a script under `ops/` (`ops/py.sh x.py`),
   reads go through `ops/db.sh "select …"`, writes through the hub.
4. **Append-only data.** Tables record events and observations; state is
   derived. A migration appends to its package's `Schema`, never edits one.
5. **Surface parity, on the owner's surfaces.** Every console page has a
   phone screen with the same API, numbers and words, or says why not in
   `web/places.js`; `parity_test.go` fails on an unpaired page. But a change
   the owner asks for is built for the apps they chose at setup (`surfaces`
   in `ops/hub.json`: phone, desktop, web; the session preamble names
   them) and shipped there; an app they did not pick waits until they ask
   for it. Three apps per change is a lot of tool calls for a page nobody
   opens.
6. **Look at what you changed.** Any UI change is rendered and looked at
   before it ships: `make browse` / `make web-shot` for the console,
   `make screens` for the app.
7. **Commits**: `ops/commit.sh -m "<subject>" <path>…` — stages only the
   paths given (parallel sessions share the tree), refuses `data/`.
8. **Shipping**: hub changed → `ops/hub.sh restart`. App changed →
   `make ship`, then one ask with the install link.

## The pages that ship

Four tabs, the same on the console, the phone and the desktop app:
**Sessions · Recs · Calendar · Configuration**. Sessions, Recs and
Calendar are the defaults the owner lives in. Configuration holds the
goals, the spend limits (the plan, its windows, the model new sessions
start on) and the data sources; Goals (`#/goals`), Spend (`#/spend`) and
one source's page sit behind it and light its tab. Domain pages (money, a
training log, a reading list) are goal pages an agent builds for the owner
when a goal needs one; none ships.

Building a page for a goal (data first, a page only when the owner asks):
the recipe is in `hub/CLAUDE.md`, read it when that is the task.

## Team mode

Optional, off by default (`team_mode` in `ops/hub.json`;
`ops/py.sh setup.py hub --team yes|no`). On, some sessions take a standing
role: a `personas` row in `hub/internal/threads/persona.go` (Role, Covers,
Who, Lane, Brief), a lane in `routeLanes` (`route.go`), and
`lifectl thread <id> persona <key>` to give a session the role. The router
(a local Ollama model, else the cloud's small one) sends each new message
from the app or console to the role whose lane it is, or starts a session as
before; `POST /api/v1/route` is the dry run. A role's own files live in
`data/people/<key>/` (`lifectl workspace`); only that session or the owner
writes them. One role ships, the executive assistant who owns the calendar
session. Adding a role is those three steps; every brief is written for this
owner, never copied from anyone's. It was offered once at setup; if the
owner said no, you may offer it once more after two weeks, in the same words
(SETUP.md step 10), and never again after that.

## Giving back

When you build or fix something any owner would want (not a page for one
goal), offer the owner an issue on the project, the turn you finish it: the title, the body and, if useful, a branch
with the change as a reference. The owner approves each one
(`lifectl propose --kind share`); before it goes, scrub the body and the
diff of names, paths, tokens, amounts and anything from `data/`. Nothing
from outside is merged as it comes: the maintainer rebuilds what fits and
ships it as an addendum, so an issue is enough and a PR is only a pointer.

## Pulling updates

`upstream` is the public repo, github.com/mattabate/life-distributable; `origin`
is the owner's private copy. The copy is theirs: upstream is read-only, and
nothing from it lands without the owner's yes.

The weekly check is the read-only `addenda-check` job; how to apply an entry
is in the header of `ADDENDA.md`. When the owner just asks to update: `git merge upstream/main`,
conflicts resolved in favour of their own changes, then `make check`.
