Clone github.com/mattabate/life-distributable and set up my life agent — follow its SETUP.md.

That sentence, pasted into Claude Code on a Mac, is the whole install. The
agent reads [SETUP.md](SETUP.md) and walks you through it: what it costs, what
you click, and what it runs for you.

# life

A personal hub that runs Claude Code sessions for you around the clock on a
Mac you own, and an iPhone app to steer them.

- **Sessions**: long-running Claude sessions, one per piece of your life
  (a project, a habit, a question). Message them from the phone or the web
  console; they keep full memory.
- **Asks**: when a session is blocked on you, it raises a card on your phone
  with exactly what it needs. Everything else it does itself.
- **Approvals**: money, deleting data, contacting people, sharing data and git
  commits are gated: a session proposes, you approve with a code only you hold.
- **Recs**: every recommendation goes in a ledger with its evidence and a way
  to tell later whether it worked.
- **Calendar**: dated steps for you, scheduled runs for the agent.
- **Goals**: what you're working toward; sessions file what they learn there.
- **Spend**: what the agent costs, live, per model.

Your data stays on your Mac. The hub listens only on your private
[Tailscale](https://tailscale.com) network — nothing is reachable from the
internet — and backs up nightly, encrypted, to storage you own.

## What you need

- A Mac that stays on (a Mac mini is ideal) with macOS 15 or later.
- A Claude subscription (Pro works; Max is better for an always-on hub).
- An iPhone, and an Apple Developer membership ($99/yr) for the app with push.

## Layout

| Path | What |
|---|---|
| `hub/` | Go backend: one binary, SQLite, the web console embedded |
| `app/` | SwiftUI iPhone app (generated with xcodegen) |
| `shared/` | The API contract (`api.md`) and fixtures both sides test against |
| `ops/` | One-command scripts: hub lifecycle, backup, app shipping, setup |
| `data/` | Your data. Git-ignored, backed up, never leaves your machines |

`make check` is the gate for every change. See [CLAUDE.md](CLAUDE.md).

## License

MIT — see [LICENSE](LICENSE).
