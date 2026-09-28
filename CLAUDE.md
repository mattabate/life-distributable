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
- Gate (hub-enforced): money, delete, contact, share, commit →
  `lifectl propose`. Approval needs the decider code, which only the owner
  holds (`ops/decider-set.sh`).
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
5. **Surface parity.** Every console page has a phone screen with the same
   API, numbers and words, or says why not in `web/places.js`;
   `parity_test.go` fails on an unpaired page.
6. **Look at what you changed.** Any UI change is rendered and looked at
   before it ships: `make browse` / `make web-shot` for the console,
   `make screens` for the app.
7. **Commits**: `ops/commit.sh -m "<subject>" <path>…` — stages only the
   paths given (parallel sessions share the tree), refuses `data/`.
8. **Shipping**: hub changed → `ops/hub.sh restart`. App changed →
   `make ship`, then one ask with the install link.

## Recipe: a page for a goal

Pages are made for a goal when it truly needs one — they don't ship. Data
comes first; a page only when the owner asks for one. Example: the owner's
goal is *"Get stronger and eat better"* and they want to log workouts and
meals.

1. **Data first (always).** A package `hub/internal/fitness` with a
   `Schema` of append-only tables — `workouts(id, day, kind, minutes,
   notes, source, created_at)` and `meals(id, day, meal, protein_g, kcal,
   notes, source, created_at)` — handed to `db.Migrate("fitness", Schema)`
   in its constructor, wired in `hub/cmd/hub/main.go`. Add both tables to
   `TestFreshSchema`'s inventory.
2. **Write path**: `POST /api/v1/fitness/workouts` and `…/meals` (routes.go
   row + api.md section + handler + a `server_test.go` test), and
   `lifectl fitness log workout|meal …` so any session can record one.
3. **The goal reads it**: set the goal's sources
   (`lifectl goal new … --sources fitness`) and have its check-in session
   summarise the week into the goal digest. Many goals stop here: the data,
   a weekly digest, and cards when something needs the owner.
4. **Only if the owner asks for a page**: `GET /api/v1/fitness/summary`
   (contract + test + `make generate` for its fixture), then
   - console: `hub/internal/server/web/views/fitness.js` registering
     `views.fitness`, a nav entry, a test in `web/test/`;
   - phone: `app/Life/Sources/FitnessView.swift` + its model in
     `Models.swift` + a row under **More**, and a decode test against the
     fixture;
   - `web/places.js`: `fitness: { label: 'Fitness', view: 'views/fitness.js', phone: 'FitnessView.swift' }`.
5. `make check`, screenshots of both surfaces (look at them), commit,
   `ops/hub.sh restart`, `make ship`.

## Pulling updates

`upstream` is the public life-distributable repo; `origin` is the owner's
private copy. Pull upstream only when the owner asks:
`git fetch upstream && git merge upstream/main`, resolve, `make check`.
