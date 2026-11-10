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

*Note: everything below is a starting point and subject to change. The agents
maintain their own code, so they can change the app's front end or back end in
whichever way suits you.*

## Install with one prompt

Open Claude Code on your Mac and paste this:

```
Clone github.com/mattabate/life-distributable and set up my life agent. Follow its SETUP.md.
```

The agent walks you through setup from there.

**Use it at your own risk.** life is free, open source software, provided as
is with no warranty (see the [LICENSE](LICENSE)). The agents act for you, so
they can spend money, send messages, delete files and share data. The approval
step is there to catch that, and keeping it on is up to you. You are
responsible for what your copy does, and nothing it tells you is legal,
financial or medical advice.

⭐ **[Star it][repo]** to get developer addenda: new features and fixes land
in [ADDENDA.md](ADDENDA.md) every week, and your agent brings each one to you.

<table>
<tr>
<td width="50%" valign="top"><img src="docs/img/sessions.png" alt="The life desktop app's Sessions list: under Your turn, one row per agent with its open card: Charlotte, marketing manager, a build thread to post on X; Diane, executive assistant, dinner with friends on Friday or Saturday; Jules, head of AI, a local model that can route messages; Grace, chief technology officer, passing on work Tim finished; Fred, chief financial officer, two subscriptions for the same thing; Rachel, head of wellness, a sleep log. Under Working, Randy, whose chat is open on the right with a photo backup running and its progress bar. Under Idle, Herbert, chief of staff, and the rest of the team"></td>
<td width="50%" valign="top"><img src="docs/img/configuration.png" alt="The life desktop app's Configuration page: Powered by Claude Code with how much of the 7 day limits is left; Devices, the Mac hub and a phone; the Team org chart under you: Herbert, chief of staff, Rachel, head of wellness, and Fred, chief financial officer; then Grace, chief technology officer, over Randy, Owen and Tim, Jules, head of AI, over Brahms the composer and Lothar the mathematician, and Diane, executive assistant, over Choppy the wedding planner; then Charlotte, marketing manager, and Justin, head of strategy; each card marked with the goals it owns; seven goals in Work, Social and Lifestyle; and the data sources for each goal with their logos"></td>
</tr>
<tr>
<td align="center"><strong>Sessions.</strong> Agents tell you when they need you.</td>
<td align="center"><strong>Configuration.</strong> Your team, goals, devices, and data.</td>
</tr>
</table>

### Your first five minutes

You don't need to be technical.

1. If you don't have Claude Code yet, install it with
   [Anthropic's guide][claude-code-setup].
2. Open **Terminal**: press **Cmd-Space**, type `Terminal`, press **Return**.
3. In Terminal, type `claude` and press **Return**. Sign in with your
   Claude account if it asks.
4. Give Claude the following prompt and press **Return**:
   ```
   Clone github.com/mattabate/life-distributable and set up my life agent. Follow its SETUP.md.
   ```
5. Follow the instructions the agent gives you. It will ask what features
   you want at the start.

## How it works

- **Calendar.** The agents keep a calendar of what they need to do for you, and you for them.
- **Roles.** Each agent is a long-running Claude Code session with a role, like chief of staff or CTO.  They pass tasks between one another when some part of their task requires know-how from a different part of the swarm.
- **Cards, not chat.** By default the chat history isn't displayed. You can only see what the agent needs you to do or wants to tell you in that moment.

*All of this is a starting point and subject to change. The agents maintain their own code, so if you don't like how something looks or works, just ask and they can change it from one prompt.*

## Example app layout

### Desktop app

| **Calendar.** Your steps and the agents' runs. | **Roles.** Agents hand work to each other. |
|---|---|
| ![The life desktop app's Calendar on the week of November 8: a morning routine every day, piano harmony homework in the evening, a proof to read, plants to water, a build thread to post, lunch with a friend, a demo to record, the weekly team review, the app update Grace ships on Monday, her release notes to read on Tuesday, the music service to pick and gym days, with overdue, due, do soon and sessions waiting lists](docs/img/calendar.png) | ![Grace, the chief technology officer, in the life desktop app, speaking aloud: a card telling you Tim, one of her engineers, finished the bigger calendar text and it is in tomorrow's update, with the 5 PM app update coming up below](docs/img/chat.png) |

| **Cards.** The agent proposes, you approve. | **Cards.** A running job shows its progress. |
|---|---|
| ![Charlotte, the marketing manager, in the life desktop app: an approval card to post this week's build thread on X, with the first post quoted, tagged share, with Approve, Deny, Reply and Dismiss, a rec she filed, and a search rankings check coming up below](docs/img/approval.png) | ![The life desktop app's Sessions list with Randy's chat open: an approved backup of 4,210 photos to cloud storage, running, its progress bar past halfway](docs/img/sessions.png) |

### iPhone app

<table>
<tr>
<th align="left"><strong>An approval.</strong> In your pocket.</th>
<th align="left"><strong>Sessions.</strong> Cards waiting on you.</th>
<th align="left"><strong>Calendar.</strong> Your day at a glance.</th>
</tr>
<tr>
<td valign="top"><img src="docs/img/phone-approval.png" alt="The life iPhone app: Charlotte, the marketing manager, in chat with an approval card to post this week's build thread on X, the first post quoted, tagged share, with Approve, Deny, Reply and Dismiss buttons, a rec she filed, and a search rankings check coming up"></td>
<td valign="top"><img src="docs/img/phone-sessions.png" alt="The life iPhone app Sessions tab: Your turn, 6 cards in 6 sessions, one row per agent: Charlotte's build thread to approve, Diane's dinner night to decide, Jules's local model to read, Grace speaking Tim's finished work aloud, and Fred's two subscriptions below"></td>
<td valign="top"><img src="docs/img/phone-calendar.png" alt="The life iPhone app Calendar tab on November 8: the passport to renew, overdue, then seven items due today: the morning routine, plants to water at 9, the build thread to post at 11, lunch with a friend at 12:30, the demo to record at 3, a piano harmony exercise at 7 and lights out at 11"></td>
</tr>
</table>

The screenshots are the real apps on a demo hub with made-up people and plans.

## What it costs

Every cost below goes to an outside service. Nothing goes to me.

| What | Necessary? | Cost | Needed for |
|---|---|---|---|
| [Claude Pro or Max][claude-pricing] | Required | $20, $100 or $200 a month | Everything. Max is better for an always-on agent |
| A Mac on macOS 15 or later | Required | the one you own | The hub |
| [Tailscale](https://tailscale.com/pricing) | Required | free | Reaching your hub privately from your phone and laptops |
| [Apple Developer Program][apple-dev] | Optional | $99 a year | Only the iPhone app and the desktop app. Web only? Skip it |
| [Backblaze B2](https://www.backblaze.com/cloud-storage/pricing) | Recommended | cents a month | Nightly encrypted backup |

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
  a session can't approve its own card; without it a session runs as your
  account and could reach the code's hash, so the gate is a rule it follows,
  not a wall. Either way, a session lands commits on your local main and
  builds the app without asking, and a hub change is rebuilt and restarted
  as your account, so code a session lands runs with your rights: the
  separate user guards the card, not landed code. Landed commits stay on
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
