<p align="center"><img src="docs/img/logo.svg" alt="life logo: a blue rounded square with a white dot, next to the word life" width="144"></p>

# life

**An AI personal assistant with editable apps on iPhone, Mac and web.**

*life* is a self-hosted AI assistant built on [Claude Code][claude-code].
It will help you meet your goals, whatever they are.

Your Mac (a Mac mini, or a laptop that stays on) runs the agents. It
connects to your iPhone over a private [Tailscale](https://tailscale.com)
VPN, and you talk to the agent from an iPhone app, a desktop app or a web
app. Nothing is on the public internet. Only your devices can reach it.

The assistant can change its apps to make you new pages, add
data sources, or modify itself and the way it interacts with you.
Nothing about the app you start with is set in stone; tell the agent
your desired changes through the app.

As you work with the assistant, it learns about you and your goals. 
It can make recommendations, or take actions on your behalf.
It connects safely to your data sources, and the data you share is 
saved on the computer.

**Setup takes about 2 hours,** but it is mostly following simple steps the
agent gives you. Your only job at the start is to open Claude Code and
give it the prompt below.

**You need:** a personal computer that is always on (a Mac), an
[Apple Developer membership][apple-dev] ($99 a year, for the iPhone and
desktop apps), and a [Claude subscription][claude-pricing] that works with
Claude Code (about $20 to $200 a month, depending on plan).

## Install: paste one sentence

The only step: open Claude Code on your Mac and give it this prompt.

```
Clone github.com/mattabate/life-distributable and set up my life agent. Follow its SETUP.md.
```

Most of the 2 hours is waiting on Apple and the Xcode download. The agent runs the Terminal for you. Your part is clicking where
it tells you. You never type a command.

⭐ **[Star it][repo]** to get developer addenda: new features and fixes land
in [ADDENDA.md](ADDENDA.md), and your agent brings each one to you.
**Check back often:** updates from the developer's own copy land here every week.

<p align="center"><img src="docs/img/sessions.png" alt="The life desktop app: the Sessions board with four cards waiting on the owner, an approval to buy $500 of VTI on Thursday, a honeymoon decision, an app build to install and today's tweet to pick, a Gmail expense scan the agent is running, and a new session ready to start" width="900"></p>

**Configuration.** What it is connected to, what it may spend, and what you are working toward: the Claude plan and how much of each limit is left, the model new sessions start on, your goals, and every data source under the goal it serves.

<p align="center"><img src="docs/img/configuration.png" alt="The life desktop app's Configuration page: a Powered by card with the Claude Max 20x plan, $728 spent this month, 60% of the 5 hour limit, 86% of the 7 day limit and 79% of the Fable limit left, and the model picker; a Goals card with seven goals; and data sources grouped under the goals Grow my audience, Make more money, Build agent and Make me healthier, most sources first, each goal on its own row in its colour, every source with its logo, row count and last sync" width="900"></p>

## Is this for me?

Yes, if you want an AI agent that actually does things, not a chat window
you have to babysit. You don't need to be technical. You need:

- **A Mac that stays on.** A Mac mini is ideal. A laptop works if it stays
  plugged in and awake.
- **A Claude subscription.** Pro works. Max is better for an agent that
  runs all day.
- **Claude Code installed.** [Here is how][claude-code-setup]. It's one
  line to paste, and it's the only one you paste yourself.

### Your first five minutes

1. Open **Terminal**: press **Cmd-Space**, type `Terminal`, press **Return**.
2. If you don't have Claude Code yet, install it with
   [Anthropic's guide][claude-code-setup].
3. In Terminal, type `claude` and press **Return**. Sign in with your
   Claude account if it asks.
4. Paste the sentence above and press **Return**.
5. From here, the agent leads. Do what it says, click where it points.

## What it does

- **Runs sessions for every part of your life.** Long-running Claude Code
  sessions, one per project, habit or question. They keep full memory and
  keep working while you sleep.
- **Puts a card on your phone when it needs you.** One clear thing to do,
  decide or read. Everything else it handles itself.
- **Asks before anything that matters.** Money, deleting, contacting
  people, sharing data and code commits wait for your approval, with a code
  only you hold.
- **Tracks your goals.** Sessions file what they learn under each goal, so
  every goal has a living "where this stands".
- **Recommends, with receipts.** Every suggestion goes in a ledger with its
  evidence and the price, so you can see later whether it worked.
- **Four pages, on iPhone, Mac and web.** Sessions, Recs and Calendar
  are the pages you live in. Configuration holds your goals, your spend
  limits and each data source, grouped under the goal it serves. You pick
  the apps you want at setup, and every change you ask for later is built
  for those. The others wait until you ask for them.
- **Grows with you.** Ask for a new data source, a page or an automation,
  and it builds it in your own copy of the code. A page for a part of your
  life (money, a training log, a reading list) is something your agent
  builds for a goal when that goal needs one. None ships in the box.

## Example app layout

### Desktop app

| **Approvals.** The agent proposes, you approve. | **Sessions.** It asks one clear question at a time. |
|---|---|
| ![The Weekly investing session in the life desktop app: the agent checks cash and price, then raises an approval card to buy $500 of VTI on Thursday, tagged money](docs/img/approval.png) | ![A session in the life desktop app planning a honeymoon: the agent asks which of six beach and spa plans to pick, St. Lucia and Costa Rica first, each with flight time and total](docs/img/chat.png) |

| **Recs.** Suggestions with evidence and a price. | **Calendar.** Your steps and the agent's runs. |
|---|---|
| ![The Recs page of the life desktop app: get to bed by 11:30 on weeknights, send the first newsletter, destroy an idle $6 a month server, import an art portfolio, each with evidence and Accept or Decline](docs/img/recs.png) | ![The life desktop app's Calendar: supplements as a daily chore, Fenaroli practice in the evenings, the Gmail expense scan and weekly agent runs, with due, do-soon and sessions-waiting lists](docs/img/calendar.png) |

### iPhone app

<table>
<tr>
<th align="left"><strong>An approval.</strong> In your pocket.</th>
<th align="left"><strong>Sessions.</strong> Cards waiting on you.</th>
<th align="left"><strong>Calendar.</strong> Your day at a glance.</th>
</tr>
<tr>
<td valign="top"><img src="docs/img/phone-approval.png" alt="The life iPhone app: a session chat with the approval card to buy $500 of VTI, Approve, Deny, Reply and Dismiss buttons"></td>
<td valign="top"><img src="docs/img/phone-sessions.png" alt="The life iPhone app Sessions tab: Your turn, 4 cards in 4 sessions, three red cards to approve or decide and one teal card to install an app build"></td>
<td valign="top"><img src="docs/img/phone-calendar.png" alt="The life iPhone app Calendar tab on the Day view: today's hours with a walk after lunch, the weekly recs score and the gym, the investing run, and the due and do-soon tray"></td>
</tr>
</table>

The screenshots are the maker's own hub, with private details changed.

## What it costs

| What | Cost | Needed for |
|---|---|---|
| [Claude Pro or Max][claude-pricing] | $20, $100 or $200 a month | Everything. Max is better for an always-on agent |
| [Apple Developer Program][apple-dev] | $99 a year | Only the iPhone app and the desktop app. Web only? Skip it |
| [Tailscale](https://tailscale.com/pricing) | free | Reaching your hub privately from your phone and laptops |
| [Backblaze B2](https://www.backblaze.com/cloud-storage/pricing) | cents a month | Nightly encrypted backup |
| A Mac | the one you own | The hub |

## Setup, in order

The agent follows [SETUP.md](SETUP.md). Here is the timeline.

1. **Pick your apps, see the costs** (2 min). iPhone, desktop, web, or all
   three. Later changes are built for the ones you picked.
2. **Start the slow things** (10 min, then waiting). Apple developer
   enrollment and the Xcode download start first. Apple usually answers
   within hours.
3. **Prepare the Mac** (10 min). Free tools, never sleep, disk encryption on.
4. **Your private copy on GitHub** (5 min). Your copy, your account, private.
5. **Tailscale** (10 min). A private network only your devices can join.
6. **The hub is up** (10 min). First milestone: the web console works.
7. **Backups** (15 min). Nightly, encrypted, to storage you own, tested
   with a real restore.
8. **Apple team and Xcode** (10 min). Once Apple says yes.
9. **The apps** (20 min). The desktop app opens, the iPhone app installs
   and buzzes.
10. **Your first goals** (15 min). The agent interviews you.
11. **Weekly updates and wrap-up** (5 min).

## How security works

Your data lives on your Mac. The hub listens only on your private
[Tailscale](https://tailscale.com) network, so nothing about it is public.

<p align="center"><img src="docs/img/security.png" alt="Security diagram for life: the iPhone app and a laptop or browser talk to the hub on your Mac inside your private Tailscale network, where your data lives. The hub reaches out to only four places: Anthropic for Claude sessions, Apple push for notifications, your Backblaze bucket for nightly backups encrypted on the Mac first, and an opt-in weekly usage heartbeat with counts only. The public internet has no way in." width="900"></p>

- **Nothing public.** No open ports, no public URL. Your phone reaches the
  hub through Tailscale.
- **Outbound, only three places:** Anthropic (your Claude sessions), Apple
  (push notifications), and an optional weekly heartbeat.
- **The heartbeat is off unless you say yes.** The agent asks once at
  setup, and the answer defaults to no. If you say yes, it sends a random
  install id, the version and a few counts. Never text, titles, names or
  amounts. You can switch it off in Settings.
- **Backups are yours.** Encrypted on your Mac before upload, so the
  storage only ever sees noise. The nightly key can't delete anything.
- **The gate.** Anything that spends money, deletes, contacts a person,
  shares data or commits code needs your approval plus a decider code that
  only you hold. No session can approve itself.

## Updates and addenda

- **The copy is yours.** Setup makes a private copy in your GitHub account.
  Change anything.
- **Upstream is read-only.** Nothing from this repo lands in your copy
  without your yes.
- **New every week.** The developer runs this same app daily. Each week the
  fixes and new pieces from that copy are ported here, so check back often.
- **Your app can drift.** Your copy will grow its own pages and look. Each
  update is written so your agent can fit it into what you have, not
  overwrite it. Optional add-ons (like voice) arrive the same way, off
  until you opt in.
- **Your agent checks weekly.** It reads [ADDENDA.md](ADDENDA.md) every
  Monday and shows each new entry as a card on your phone.
- **Nothing merges without you.** Say yes on a card and it applies that
  one update, tests it, and tells you what changed.

## What you need

- A Mac on macOS 15 or later that stays on
- A Claude Pro or Max subscription, with [Claude Code][claude-code-setup]
- An Apple Developer membership, for the iPhone and desktop apps
- An iPhone, if you want the phone app
- About 2 hours, mostly waiting

## FAQ

**Do I need to know how to code?**
No. The agent runs every command. You click, copy and paste where it tells you.

**Is my data sent anywhere?**
Your Claude sessions go to Anthropic, like any Claude Code session. Push
notifications go through Apple. Backups go to your own Backblaze bucket,
encrypted first. Nothing else, unless you opt in to the counts-only heartbeat.

**Can I skip the iPhone app?**
Yes. Pick web only and skip the Apple Developer membership. The console
works in any browser on your Tailscale network, phone included.

**Does it run on Windows or Linux?**
The hub needs a Mac. Once it runs, the web console works from any device on
your Tailscale network.

**Can the agent spend my money or email people on its own?**
No. Those actions are gated. It proposes, you approve with your decider code.

**Is this an Anthropic product?**
No. It's an independent open source project built on Claude Code.

**How do I get new features?**
They're announced in [ADDENDA.md](ADDENDA.md). Your agent brings each one
to you as a card, and you choose.

## Who made this

[Matt Abate](https://mattabate.com). Questions and ideas:
[open an issue][issues]. Want it set up for you or your team? Get in touch
through [mattabate.com](https://mattabate.com).

## License

MIT. See [LICENSE](LICENSE).

[repo]: https://github.com/mattabate/life-distributable
[issues]: https://github.com/mattabate/life-distributable/issues
[claude-code]: https://claude.com/claude-code
[claude-code-setup]: https://code.claude.com/docs/en/setup
[claude-pricing]: https://claude.com/pricing
[apple-dev]: https://developer.apple.com/programs/
