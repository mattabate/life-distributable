<p align="center"><img src="docs/img/logo.svg" alt="life logo: the app icon, an L made of a mint bar and a sand bar on dark green, next to the word life" width="144"></p>

# life

**A self-hosted swarm of AI agents, with apps on iPhone, Mac and web.**

*life* is a self-hosted team of AI agents built on [Claude Code][claude-code].
It will help you meet your goals, whatever they are.

Your Mac runs the agents. It connects to your iPhone over a private
[Tailscale](https://tailscale.com) VPN, and you talk to the agents from an
iPhone app, a desktop app or a web app. Nothing is on the public internet.
Only your devices can reach it, and you can connect as many as you like: a
Mac mini, a laptop, a phone.

The agents learn about you and your goals as you work with them. They 
maintain their own code, and can change the UI or add data sources when asked. 
Dangerous actions, like sending emails, are safe behind a secure approval step.

**Setup takes about 2 hours,** mostly waiting on Apple and the Xcode
download. **You need** a Mac that stays on, a
[Claude subscription][claude-pricing] (about $20 to $200 a month), and an
[Apple Developer membership][apple-dev] ($99 a year, only for the iPhone and
desktop apps).

## Install with one prompt

Open Claude Code on your Mac and paste this:

```
Clone github.com/mattabate/life-distributable and set up my life agent. Follow its SETUP.md.
```

The agent walks you through setup from there.

⭐ **[Star it][repo]** to get developer addenda: new features and fixes land
in [ADDENDA.md](ADDENDA.md) every week, and your agent brings each one to you.

<p align="center"><img src="docs/img/sessions.png" alt="The life desktop app's Sessions board: Your turn, 5 cards in 4 sessions, one per agent: Sam's water filter to swap, Rachel's Lisbon hotels to read, Fred's sleep log and long run day, Charlotte's $500 VTI buy to approve, and Diane, still working, with a dentist time to pick; below, the team of five: chief of staff, health coach, home manager, money manager and travel planner" width="900"></p>

**Configuration.** Your team, goals, devices, and data sources.

<p align="center"><img src="docs/img/configuration.png" alt="The life desktop app's Configuration page: a Powered by card for Claude Code with the month's spend and how much of the 7 day limits is left, and a Goals card with five goals: Run a half marathon, Sleep seven hours, Invest every Thursday, Plan the Lisbon trip and Keep the apartment running" width="900"></p>

### Your first five minutes

You don't need to be technical.

1. Open **Terminal**: press **Cmd-Space**, type `Terminal`, press **Return**.
2. If you don't have Claude Code yet, install it with
   [Anthropic's guide][claude-code-setup].
3. In Terminal, type `claude` and press **Return**. Sign in with your
   Claude account if it asks.
4. Paste the sentence above and press **Return**.
5. From here, the agent leads. Do what it says, click where it points.

## How it works

- **Calendar.** The agents keep a calendar that stores what they need to do for you, and what you need to do for them.
- **Roles.** Each agent is a long-running Claude Code session with a role, like chief of staff or CTO.  They pass tasks between one another when some part of their task requires know-how from a different part of the swarm.
- **Cards, not chat.** By default the chat history isn't displayed. You can only see what the agent needs you to do or wants to tell you in that moment.

All of this is a starting point and subject to change. The agents maintain their own code, so if you don't like how something looks or works, just ask and they can change it from one prompt.

## Example app layout

### Desktop app

| **Approvals.** The agent proposes, you approve. | **Sessions.** It asks one clear question at a time. |
|---|---|
| ![Charlotte, the money manager, in the life desktop app: an approval card to buy $500 of VTI on Thursday, cash staying above a $2,000 floor, tagged money, with Approve, Deny, Reply and Dismiss, and the Thursday buy coming up below](docs/img/approval.png) | ![Fred, the health coach, in the life desktop app: two cards for the half marathon goal, a choice of a Saturday or Sunday long run and a step to log last night's sleep](docs/img/chat.png) |

| **Recs.** Suggestions with evidence and a price. | **Calendar.** Your steps and the agent's runs. |
|---|---|
| ![The Recs page of the life desktop app: try a meal kit for weeknights at $60 a month, move the trip fund to a high-yield account, blackout curtains for $89, each with its reason and Accept, Decline or Reply](docs/img/recs.png) | ![The life desktop app's Calendar on a week: an easy run, the farmers market, lunch, a spending review and dry cleaning on Sunday, then a tempo run, a hotel price check, the Thursday buy and a call home, with overdue, due, sessions waiting and anytime lists](docs/img/calendar.png) |

### iPhone app

<table>
<tr>
<th align="left"><strong>An approval.</strong> In your pocket.</th>
<th align="left"><strong>Sessions.</strong> Cards waiting on you.</th>
<th align="left"><strong>Calendar.</strong> Your day at a glance.</th>
</tr>
<tr>
<td valign="top"><img src="docs/img/phone-approval.png" alt="The life iPhone app: Charlotte, the money manager, in chat with an approval card to buy $500 of VTI on Thursday, cash staying above a $2,000 floor, with Approve, Deny, Reply and Dismiss buttons"></td>
<td valign="top"><img src="docs/img/phone-sessions.png" alt="The life iPhone app Sessions tab: Your turn, 5 cards in 4 sessions, one row per agent: Sam's water filter to swap, Rachel's Lisbon hotels to read, Fred's sleep log and long run day, Charlotte's buy to approve, and Diane booking a dentist cleaning"></td>
<td valign="top"><img src="docs/img/phone-calendar.png" alt="The life iPhone app Calendar tab on the Day view: an easy run at 7, the farmers market at 10, a water filter swap, reviewing the month's spending at 4 and picking up dry cleaning"></td>
</tr>
</table>

The screenshots are the real apps on a demo hub with made-up people and plans.

## What it costs

Every cost below goes to an outside service. Nothing goes to me.

| What | Cost | Needed for |
|---|---|---|
| [Claude Pro or Max][claude-pricing] | $20, $100 or $200 a month | Everything. Max is better for an always-on agent |
| [Apple Developer Program][apple-dev] | $99 a year | Only the iPhone app and the desktop app. Web only? Skip it |
| [Tailscale](https://tailscale.com/pricing) | free | Reaching your hub privately from your phone and laptops |
| [Backblaze B2](https://www.backblaze.com/cloud-storage/pricing) | cents a month | Nightly encrypted backup |
| A Mac on macOS 15 or later | the one you own | The hub |

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

<p align="center"><picture><source media="(prefers-color-scheme: dark)" srcset="docs/img/security-dark.png"><img src="docs/img/security.png" alt="Security diagram for life: the iPhone app, the Mac app on your laptop and any browser talk to the hub on a Mac you leave on, such as a Mac mini, inside your private Tailscale network, where your data lives. Only four things leave the hub's Mac: Claude sessions to Anthropic, push notifications to Apple, nightly backups encrypted on the Mac first to your Backblaze bucket, and an opt-in weekly usage heartbeat with counts only. The public internet has no way in." width="900"></picture></p>

- **Nothing public.** No open ports, no public URL. Your phone reaches the
  hub through Tailscale.
- **Outbound, only four places:** Anthropic (your Claude sessions), Apple
  (push notifications), your own backup bucket, and an optional weekly
  heartbeat to the project's [PostHog](https://posthog.com) analytics.
- **The heartbeat is off unless you say yes.** The agent asks once at
  setup, and the answer defaults to no. If you say yes, once a week it
  posts one event to `https://us.i.posthog.com/capture/` with a random
  install id and these fields only: `version`, `active_days`,
  `sessions_started`, `goals`, `pages` and `token_bucket` (a range such as
  "1-10M"), with GeoIP lookup disabled (`hub/internal/usage`). Never text,
  titles, names or amounts. Settings shows the payload and where it goes,
  and switches it off; `usage_host` in `ops/hub.json` sends it somewhere
  else.
- **Backups are yours.** Encrypted on your Mac before upload, so the
  storage only ever sees noise. The nightly key can't delete anything.
- **The gate.** Anything that spends money, deletes, contacts a person,
  shares data or pushes code needs your approval plus a decider code that
  only you hold. With sessions run as a separate user (SETUP.md, optional),
  no session can approve itself; without it a session runs as your account
  and could reach the code's hash, so the gate is a rule it follows, not a
  wall. A session can commit locally without asking; that commit stays on
  your Mac until a push is approved.

## Updates and addenda

- **The copy is yours.** Setup makes a private copy in your GitHub account.
  Change anything.
- **New every week.** The developer runs this same app daily, and each week
  its fixes and new pieces are ported here.
- **Your agent checks weekly.** It reads [ADDENDA.md](ADDENDA.md) every
  Monday and shows each new entry as a card on your phone.
- **Nothing lands without you.** Say yes and a new session applies that one
  update, fitted to your copy (which will have grown its own pages and look),
  tests it, and tells you what changed. Optional add-ons (like voice) arrive
  the same way, off until you opt in.

## FAQ

**Do I need to know how to code?**
No. The agent runs every command. You click, copy and paste where it tells you.

**Is my data sent anywhere?**
Your Claude sessions go to Anthropic, like any Claude Code session. Push
notifications go through Apple. Backups go to your own Backblaze bucket,
encrypted first. Nothing else, unless you opt in to the counts-only heartbeat
(to PostHog).

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

**What's in the folders?**
`hub/` is the server on your Mac and the web console it serves. `app/` is
the iPhone and desktop app. `shared/` is the contract between them, so the
two sides agree by test. `ops/` is the scripts that run one install: build
and start the hub, back it up nightly, install the apps, screenshot the
pages. Those scripts are the only commands a session may run, so that folder
is also the list of what the agent can do to your Mac.

## Who made this

[Matt Abate](https://mattabate.com), [@_laundry_matt_ on X](https://x.com/_laundry_matt_).
Questions and ideas: [open an issue][issues]. Want it set up for you or your
team? Get in touch through [mattabate.com](https://mattabate.com).

Found a security hole? [Report it privately](https://github.com/mattabate/life-distributable/security/advisories/new), not in an issue.

## License

MIT. See [LICENSE](LICENSE).

[repo]: https://github.com/mattabate/life-distributable
[issues]: https://github.com/mattabate/life-distributable/issues
[claude-code]: https://claude.com/claude-code
[claude-code-setup]: https://code.claude.com/docs/en/setup
[claude-pricing]: https://claude.com/pricing
[apple-dev]: https://developer.apple.com/programs/
