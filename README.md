# life

**A personal agent that works on your life around the clock, on a Mac you own.**
Claude Code sessions run on your Mac day and night; you steer them from your
iPhone, your desktop, or any browser.

## Install: one sentence

Open [Claude Code](https://claude.com/claude-code) on the Mac that will run it and paste:

```
Clone github.com/mattabate/life-distributable and set up my life agent. Follow its SETUP.md.
```

That's the whole install. Your Claude reads [SETUP.md](SETUP.md), asks which
apps you want (iPhone, desktop, web, or all three), tells you what it costs,
starts the slow Apple step first, and walks you through every click.

If it helps you, **[star the repo](https://github.com/mattabate/life-distributable)**.
A star is how a free project knows it's being used.

## What you get

- **Sessions**: long-running Claude sessions, one per piece of your life (a
  project, a habit, a question). Message them from any surface; they keep
  full memory.
- **Asks**: when a session is blocked on you, a card lands on your phone
  with exactly what it needs. Everything else it does itself.
- **Approvals**: money, deleting data, contacting people, sharing data and
  git commits are gated. A session proposes; you approve with a code only
  you hold.
- **Recs**: every recommendation goes in a ledger with its evidence and a
  way to tell later whether it worked.
- **Calendar**: dated steps for you, scheduled runs for the agent.
- **Goals**: what you're working toward. Sessions file what they learn
  there, and build you a page for a goal when you ask for one.
- **Spend**: what the agent costs, live, per model.

## Three apps, one hub

| App | What it is |
|---|---|
| **iPhone** | Cards, push, approvals, chat. Installed over the air from your own hub |
| **Desktop** | A native Mac app for the long sessions at your desk |
| **Web** | The console, in any browser on your private network |

Pick any combination at setup; all three is the default.

## Private by design

Your data stays on your Mac. The hub listens only on your private
[Tailscale](https://tailscale.com) network, so nothing is reachable from the
internet, and it backs up nightly, encrypted, to storage you own. The only
thing that ever leaves is an optional weekly heartbeat (counts only, off
unless you say yes at setup).

## What you need

- A Mac that stays on (a Mac mini is ideal), macOS 15 or later
- A Claude subscription (Pro works; Max is better for an always-on hub)
- For the iPhone app: an Apple Developer membership ($99/yr)

## Layout

| Path | What |
|---|---|
| `hub/` | Go backend: one binary, SQLite, serves the API and the web console |
| `app/` | SwiftUI: the iPhone app and the Mac desktop app (generated with xcodegen) |
| `shared/` | The API contract (`api.md`) and fixtures both sides test against |
| `ops/` | One-command scripts: hub lifecycle, backup, app shipping, setup |
| `data/` | Your data. Git-ignored, backed up, never leaves your machines |

`make check` is the gate for every change. See [CLAUDE.md](CLAUDE.md).

## Who made this

[Matt Abate](https://mattabate.com). Issues and ideas:
[open an issue](https://github.com/mattabate/life-distributable/issues).
Want it set up for you or your team? Get in touch through
[mattabate.com](https://mattabate.com).

## License

MIT. See [LICENSE](LICENSE).
