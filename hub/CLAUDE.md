# hub/ — Go backend

Stdlib only, one binary, config in `ops/hub.json`. Run `make check` from
the repo root before declaring anything done. Lifecycle: `ops/hub.sh
install|restart|stop|status|logs|url`.

Layout: `cmd/hub` (main, TLS, token) · `internal/config` · `internal/spend`
(JSONL parser + pricing + summary) · `internal/server` (routes, auth, embedded
console in `web/`) · `internal/store` (SQLite + the migrator) ·
`internal/threads` (sessions + asks) · `internal/actions` (proposals) ·
`internal/calendar` (dated items) · `internal/recs` (recommendations +
`flow.go`, what happens around a decision) · `internal/attention` (the board:
ONE "your turn" for both surfaces) · `internal/deliver` (the one way a hub
event reaches a session).

## Recipe: add an endpoint
1. Add one line to the table in `server/routes.go` (`open: true` only for
   token-less pages/callbacks) and a `### METHOD /path` section to
   `shared/api.md` (the contract test fails if they drift).
2. Handler in the domain file it belongs to (`server/board.go`, `recs`,
   `asks`… — `server.go` is the shared plumbing, not a dumping ground). A
   handler decodes, calls the package, answers; rules live in the package.
3. Test in `server_test.go` via `newTest(t)` (tmux calls are stubbed).
4. `make check`, then `ops/hub.sh restart`.

## Recipe: add a table or column
1. Every package owns its DDL in one ordered `var Schema = []string{…}` and
   hands it to `db.Migrate("<pkg>", Schema)` from its constructor
   (`store/migrate.go`). Statements are keyed by their text and recorded in
   `schema_migrations`; a "duplicate column"/"already exists" error on an
   unrecorded statement is tolerated (the live DB predates the migrator).
2. New column: **append** an `ALTER TABLE … ADD COLUMN …` to that package's
   `Schema`. Never edit an existing statement — edited text is a new statement.
   New table: append its `CREATE TABLE IF NOT EXISTS` (+ indices).
3. A one-time backfill pairs with the fresh list `Migrate` returns, or is a
   plain idempotent `UPDATE … WHERE col=''` after it (recs.go does the latter).
4. Update the pinned inventory in `server_test.go` `TestFreshSchema` (a fresh
   DB must end up with exactly the live DB's tables and indices), `make check`.

## Recipe: change model pricing
Edit `internal/spend/prices.json` (the hub embeds it, `ops/hublib.py` reads
it, so the scripts' dollars match the board); add a row only if the dashboard
shows a model under "Unpriced models".

## Permission model for unattended Claude (ops/schedule.json)
- `default_tools` — scheduled jobs: read-only shell + lifectl. Never build/commit.
- `prompt_tools` — sessions (threads) the owner starts from the app: may edit, `make`,
  `ops/install-phone.sh`, `ops/hub.sh restart`, `git commit`. The session's
  preamble (threads.go `systemPreamble`) tells it so.
- Neither uses bypassPermissions. Gate-list actions go via `lifectl propose`.
- **No `python3`, `sqlite3` or `curl` on either list**: each is
  one line past every other control. Use `lifectl api
  METHOD /api/v1/…` (hub only), `ops/db.sh "select …"` (`sqlite3 -readonly`,
  refuses ATTACH), `ops/py.sh <script.py>` (a `.py` inside ops/, no `-c`/`-m`).
  `TestNoUnrestrictedShellInAnyToolGrant` (internal/sched) fails if one comes
  back — it checks the live schedule.json **and** both fallback lists in
  `LoadTable`, because `"default_tools": []` means "use the default". A job's
  own `allowed_tools` is held to the same rule.

## Gotchas (learned 2026-08-20)
- launchd PATH lacks nvm: `claude_bin` in hub.json must be absolute.
- Without `LANG=en_US.UTF-8` tmux mangles tabs in `-F` formats; we use `|`.
- `tmux capture-pane -t =name` fails; use `name:`.
