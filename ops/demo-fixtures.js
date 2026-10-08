// Demo world for the README screenshots: the maker's own hub, scrubbed. Real
// goals, plan numbers, sources, sessions, recs and calendar items, with
// anything private (accounts, amounts, names, profile ids) rewritten.
// Loaded by ops/web-preview.html only when the URL has &demo=1
// (ops/demo-shots.sh) and served by ops/demo-hub.js for the phone shots.
// Nothing ships in the hub binary.
'use strict';
(function () {
  const now = new Date();
  const iso = m => new Date(now.getTime() - m * 60000).toISOString();
  const pad = n => String(n).padStart(2, '0');
  const ymd = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const day = n => ymd(new Date(now.getFullYear(), now.getMonth(), now.getDate() + n));
  const today = day(0);

  const DIST = 'weekly-investing-a4d1';
  const HONEY = 'honeymoon-destinations-c7a2';
  const BUILD = 'chat-rows-metadata-d3f9';
  const EXP = 'gmail-expense-scan-e2b8';
  const TWEET = 'daily-tweet-options-f5c1';
  const MODEL = 'claude-opus-5-5';

  const outs = {
    approve: [{ value: 'approved', label: 'Approve' }, { value: 'denied', label: 'Deny' }, { value: '', label: 'Reply' }],
    ask: [{ value: 'done', label: 'I did this' }, { value: 'wont', label: "Won't do" }, { value: '', label: 'Reply' }],
    decide: [{ value: '', label: 'Reply' }, { value: 'done', label: 'Decided' }, { value: 'wont', label: 'Not deciding' }],
    read: [{ value: 'done', label: 'Read it' }],
    rec: [{ value: 'accepted', label: 'Accept' }, { value: 'declined', label: 'Decline' }, { value: '', label: 'Reply' }],
    step: [{ value: 'done', label: 'Done' }, { value: 'wont', label: "Won't do" }, { value: '', label: 'Reply' }],
  };

  const threads = [
    { id: DIST, title: 'Weekly investing', status: 'needs_you', goal_id: 'make-more-money', model: MODEL,
      last_message: "This week's buy is ready. Spending money waits for you.", last_message_at: iso(9), cost_usd: 1.84,
      schedule: 'weekly@thu', schedule_label: 'Thursdays' },
    { id: HONEY, title: 'Honeymoon destinations', status: 'needs_you', goal_id: 'plan-the-wedding', model: MODEL,
      last_message: 'Draft 3: Bali, but closer. Six plans compared.', last_message_at: iso(41), cost_usd: 4.15 },
    { id: BUILD, title: 'Chat rows metadata', status: 'needs_you', goal_id: 'build-a-compelling-life-agent', model: MODEL,
      last_message: 'Build 1671 is ready to install.', last_message_at: iso(75), cost_usd: 3.27 },
    { id: EXP, title: 'Gmail expense scan', status: 'running', goal_id: 'make-more-money', model: MODEL,
      activity: 'Reading receipts from the last 48 hours', last_message: 'Filed 6 receipts.', last_message_at: iso(1),
      cost_usd: 0.92, schedule: 'every@48h', schedule_label: 'every 48 hours' },
    { id: TWEET, title: 'Daily tweet options', status: 'idle', goal_id: 'grow-my-audience', model: MODEL,
      last_message: 'Three options for today are on your board.', last_message_at: iso(600), cost_usd: 0.61,
      schedule: 'daily@06:00', schedule_label: 'daily 6 AM' },
  ];

  const actions = [
    { id: 'act-dist-1', created_at: iso(9), project: 'life', kind: 'money', gated: true, state: 'proposed', thread_id: DIST,
      outcomes: outs.approve, title: 'Buy $500 of VTI on Thursday',
      detail: "This week's buy from your plan: $500 a week into a total market fund.\n\n- VTI is 4% under its 50-day average\n- Cash after the buy: $2,140, above your $2,000 floor\n- A market order at the open, in the brokerage app",
      exec_type: 'claude', exec_payload: { project: 'life', prompt: 'Place the $500 VTI market order at the open and confirm the fill.' } },
  ];

  const asks = [
    { id: 'ask-honey-1', title: 'Honeymoon draft 3: Bali, but closer', state: 'active', created_at: iso(41), kind: 'decision', surface: 'any',
      thread_id: HONEY, thread_title: 'Honeymoon destinations',
      detail: 'A week of beach and spa, under 6 hours from New York.\n\n- **St. Lucia**: Ladera, about 4h45, $10,650\n- **Costa Rica**: Nayara, about 5h, $10,400\n- **Hawaii, Belize, Puerto Rico**: on the comparison slide',
      outcomes: outs.decide, class: 'unblock', open: true },
    { id: 'ask-build-1', title: 'Install app build 1671', state: 'active', created_at: iso(75), kind: 'install', surface: 'mobile', target: 'phone',
      thread_id: BUILD, thread_title: 'Chat rows metadata', detail: 'Each chat row now shows its tool calls, clock and dollars.\n\nhttps://hub.example.ts.net/app/install/1671',
      outcomes: [{ value: 'done', label: 'Install' }, { value: 'dismissed', label: 'Dismiss' }], class: 'unblock', open: true },
    { id: 'ask-tweet-1', title: "Pick today's tweet", state: 'active', created_at: iso(600), kind: 'decision', surface: 'any',
      thread_id: TWEET, thread_title: 'Daily tweet options', detail: 'Three drafts, each under 200 characters. Reply with a number or your own edit.',
      outcomes: outs.decide, class: 'unblock', open: true },
  ];

  const recs = [
    { id: 'rec-mini', status: 'proposed', open: true, title: 'Get to bed by 11:30 on weeknights for two weeks', goal_id: 'make-me-healthier',
      because: 'Apple Health has you at **6h 10m a night** on weeknights, against **7h 40m** on weekends. The late nights are the gap.', expect: 'Weeknight sleep above 7 hours',
      domain: 'health', kind: 'try', cost_label: 'free', effort: 'medium', confidence: 75, model: MODEL, created_at: iso(1400),
      dates_label: 'review in 2 weeks', outcomes: outs.rec },
    { id: 'rec-newsletter', status: 'proposed', open: true, title: 'Send the first issue of the newsletter people already signed up for', thread_id: TWEET, goal_id: 'grow-my-audience',
      because: 'People signed up and **have not heard from you yet**. A first issue keeps them.', expect: 'Open rate above 40%',
      domain: 'other', kind: 'try', cost_label: 'free', effort: 'medium', confidence: 70, model: MODEL, created_at: iso(300),
      dates_label: 'act by Nov 1', outcomes: outs.rec },
    { id: 'rec-droplet', status: 'proposed', open: true, title: 'Destroy the idle DigitalOcean droplet', goal_id: 'make-more-money',
      because: '**$6 a month** for a box nothing has used in 90 days.', expect: '$72 a year saved',
      domain: 'money', kind: 'stop', cost_label: '$6/mo saved', effort: 'low', confidence: 90, model: MODEL, created_at: iso(2000),
      dates_label: 'act this week', outcomes: outs.rec },
    { id: 'rec-art', status: 'proposed', open: true, title: 'Import your existing art portfolio into the hub', goal_id: 'make-more-art',
      because: 'Your site lists the pieces; the hub has none, so there is **no baseline** to count new work against.', expect: 'Every piece on one list',
      domain: 'other', kind: 'try', cost_label: 'free', effort: 'low', confidence: 75, model: MODEL, created_at: iso(2600),
      dates_label: 'review in 2 weeks', outcomes: outs.rec },
    { id: 'rec-puzzle', status: 'accepted', open: false, closed: true, title: "Kill the puzzle site's 30 to 60 second cold start: Render Starter", domain: 'other', kind: 'buy',
      cost_label: '$7/mo', created_at: iso(9000), decision_note: 'yes, it loads instantly now', outcomes: outs.rec },
  ];

  // The maker's goals as they read on his Configuration page.
  const goals = [
    { id: 'make-me-healthier', title: 'Make me healthier', status: 'active', horizon: 'ongoing', cadence: 'weekly', emblem: { symbol: 'health', hue: 350 },
      statement: 'Better sleep, activity, nutrition and cardio fitness, measured from Apple Health.', note_count: 76, last_reviewed_at: iso(1440) },
    { id: 'make-more-money', title: 'Make more money', status: 'active', horizon: 'year', cadence: 'weekly', emblem: { symbol: 'money', hue: 150 },
      statement: 'Grow income and net worth: spend, saving and investing from the bank feeds and statements.', note_count: 225, last_reviewed_at: iso(600) },
    { id: 'build-a-compelling-life-agent', title: 'Build agent', status: 'active', horizon: 'year', cadence: 'weekly', emblem: { symbol: 'agent', hue: 220 },
      statement: 'Make this hub genuinely useful: plain-language intentions in, long-running work, a card only when it is my turn.', note_count: 493, last_reviewed_at: iso(240),
      digest_at: iso(240), digest: '**Three surfaces at parity.** iPhone, desktop and web read the same API.\n\n- Spoken cards in the car\n- Chat rows show time and cost\n- Next: a weekly review page' },
    { id: 'grow-my-audience', title: 'Grow my audience', status: 'active', horizon: 'year', cadence: 'weekly', emblem: { symbol: 'audience', hue: 25 },
      statement: 'A public following around my work: followers, site visits, citations, talk invitations.', note_count: 273, last_reviewed_at: iso(900) },
    { id: 'get-smarter', title: 'Get smarter', status: 'active', horizon: 'ongoing', cadence: 'weekly', emblem: { symbol: 'learn', hue: 195 },
      statement: 'Curriculums with ordered, checkable steps. First: Fenaroli partimento, a video every day or two.', note_count: 42, last_reviewed_at: iso(3000) },
    { id: 'make-more-art', title: 'Make more art', status: 'active', horizon: 'ongoing', cadence: 'weekly', emblem: { symbol: 'art', hue: 320 },
      statement: 'Make and publish more compositions, poetry and film, and keep a record of every piece.', note_count: 8, last_reviewed_at: iso(5000) },
    { id: 'plan-the-wedding', title: 'Plan the wedding', status: 'active', horizon: 'year', cadence: 'weekly', emblem: { symbol: 'goal', hue: 333 },
      statement: 'Every wedding step in one place: vendors, music, the honeymoon.', note_count: 4, last_reviewed_at: iso(41) },
  ];
  const agentNotes = [
    { id: 4, goal_id: 'build-a-compelling-life-agent', created_at: iso(240), author: 'Chat rows metadata', thread_id: BUILD, kind: 'evidence',
      text: 'Every chat row now shows **tool calls, clock and dollars**, on all three surfaces.' },
    { id: 3, goal_id: 'build-a-compelling-life-agent', created_at: iso(5000), author: 'owner', kind: 'decision', text: 'Spoken cards only when headphones are in.' },
    { id: 2, goal_id: 'build-a-compelling-life-agent', created_at: iso(12000), author: 'Chat rows metadata', thread_id: BUILD, kind: 'evidence',
      text: 'The desktop app now rebuilds each view from the web console, not the phone.' },
  ];

  const messages = {
    [DIST]: [
      { id: 1, role: 'owner', author: 'owner', ts: iso(200), run_id: 'r1', text: 'Every Thursday, line up my $500 buy. Skip a week if cash would drop under $2,000.' },
      { id: 2, role: 'claude', ts: iso(120), run_id: 'r1', text: 'Set: every Thursday before the open I check **cash, price and the plan**, then ask you to approve the buy.' },
      { id: 3, role: 'claude', ts: iso(9), run_id: 'r2', cost_usd: 1.84,
        text: "This week's buy.\n\n| Check | Result |\n|---|---|\n| Cash after | $2,140 |\n| VTI vs 50-day | 4% under |\n| Plan | $500 a week |\n\nSpending money waits for your approval." },
    ],
    [HONEY]: [
      { id: 1, role: 'owner', author: 'owner', ts: iso(90), run_id: 'r1', text: 'Bali is too far. Something like it but closer: beach, spa, relaxing.' },
      { id: 2, role: 'claude', ts: iso(41), run_id: 'r1', cost_usd: 4.15, text: 'Six plans under 6 hours from New York, each with flights, a resort and a total. St. Lucia and Costa Rica are the closest to Bali. Pick one, or reply with what to change.' },
    ],
    [BUILD]: [
      { id: 1, role: 'owner', author: 'owner', ts: iso(300), run_id: 'r1', text: 'Each chat row should say how long the turn took and what it cost.' },
      { id: 2, role: 'claude', ts: iso(75), run_id: 'r1', cost_usd: 3.27, text: 'Done on all three surfaces. Build 1671 is ready to install on your phone.' },
    ],
    [EXP]: [{ id: 1, role: 'claude', ts: iso(1), run_id: 'r1', text: 'Filed 6 receipts from the last 48 hours.' }],
    [TWEET]: [{ id: 1, role: 'claude', ts: iso(600), run_id: 'r1', text: 'Three options for today are on your board.' }],
  };

  const pill = (word, tone) => ({ word, tone });
  const board = {
    surface: 'web', count: 4, working: 1,
    for_you: { [DIST]: 1, [HONEY]: 1, [BUILD]: 1, [TWEET]: 1 },
    reads: {}, installs: { [BUILD]: 1 }, open: { [DIST]: 1, [HONEY]: 1, [BUILD]: 1, [TWEET]: 1 },
    first: { [DIST]: 'act-dist-1', [HONEY]: 'ask-honey-1', [BUILD]: 'ask-build-1', [TWEET]: 'ask-tweet-1' },
    sessions: [
      { id: DIST, title: threads[0].title, n: 1, first: 'act-dist-1', running: false, actions: [actions[0]], asks: [], steps: [] },
      { id: HONEY, title: threads[1].title, n: 1, first: 'ask-honey-1', running: false, actions: [], asks: [asks[0]], steps: [] },
      { id: BUILD, title: threads[2].title, n: 1, first: 'ask-build-1', running: false, actions: [], asks: [asks[1]], steps: [] },
      { id: TWEET, title: threads[4].title, n: 1, first: 'ask-tweet-1', running: false, actions: [], asks: [asks[2]], steps: [] },
      { id: EXP, title: threads[3].title, n: 0, first: '', running: true, actions: [], asks: [], steps: [] },
    ],
    headings: [
      { key: 'your_turn', label: 'Your turn', count: '4 in 4 sessions', n: 4 },
      { key: 'working', label: 'Working', count: '1', n: 1 },
    ],
    section: { [DIST]: 'your_turn', [HONEY]: 'your_turn', [BUILD]: 'your_turn', [TWEET]: 'your_turn', [EXP]: 'working' },
    pills: { [DIST]: [pill('1 for you', 'needs')], [HONEY]: [pill('1 for you', 'needs')], [BUILD]: [pill('1 for you', 'needs')],
      [TWEET]: [pill('1 for you', 'needs')], [EXP]: [pill('running', 'running')] },
    badges: { your_turn: 4, calendar: 3, recs: 4 },
  };

  // A week of calendar around today: the owner's steps (red), agent runs (grey).
  const e = (d, at, kind, title, extra) => Object.assign({ id: `cal-${d}-${at}-${title.length}`, ref: `cal:cal-${d}-${at}`, day: day(d), at, kind, title,
    state: 'scheduled', lane: kind === 'agent' ? 'scheduled' : 'mine', open: true, item: true, kind_label: kind === 'agent' ? 'one-off run' : 'your step' }, extra || {});
  // w(k): day k of this week (0 = Sunday), so every row lands in the visible week.
  const w = k => k - now.getDay();
  const daily = { kind_label: 'daily', lane: 'chores' };
  const runs = (o) => Object.assign({ lane: 'agents', kind_label: 'recurring run' }, o);
  const fen = { goal_id: 'get-smarter', kind: 'homework', lane: 'homework', kind_label: 'practice', tick: true };
  const entries = [];
  for (let k = 0; k < 7; k++) {
    entries.push(e(w(k), '06:00', 'agent', 'Daily tweet options', runs({ thread_id: TWEET })));
    entries.push(e(w(k), '08:00', 'owner', 'Take your supplements', Object.assign({ goal_id: 'make-me-healthier' }, daily)));
  }
  entries.push(
    e(w(0), '17:00', 'agent', 'Weekly investing check-in', runs({ goal_id: 'make-more-money' })),
    e(w(0), '17:30', 'agent', 'Weekly recs score', runs()),
    e(w(1), '04:00', 'agent', 'Weekly statements import', runs({ goal_id: 'make-more-money' })),
    e(w(1), '20:00', 'owner', 'Fenaroli: watch the next video', fen),
    e(w(2), '09:00', 'agent', 'Gmail expense scan', runs({ thread_id: EXP })),
    e(w(3), '20:00', 'owner', 'Fenaroli: watch the next video', fen),
    e(w(4), '09:00', 'agent', 'Gmail expense scan', runs({ thread_id: EXP })),
    e(w(5), '20:00', 'owner', 'Fenaroli: watch the next video', fen),
    e(w(6), '09:00', 'agent', 'Gmail expense scan', runs({ thread_id: EXP })),
    // Today, as the Day grid opens on it: the weekly buy, a walk, the gym,
    // a wind-down, and a practice round with no time of its own.
    e(0, '', 'owner', 'Ear training: one round (20 notes)', fen),
    e(0, '09:30', 'agent', 'Weekly investing: line up the buy', runs({ thread_id: DIST, goal_id: 'make-more-money' })),
    e(0, '12:30', 'owner', 'Walk after lunch, 20 minutes', { goal_id: 'make-me-healthier' }),
    e(0, '14:00', 'agent', 'Weekly recs score', runs()),
    e(0, '18:00', 'owner', 'Gym: upper body', { goal_id: 'make-me-healthier' }),
  );
  const byDay = {};
  for (const x of entries) (byDay[x.day] = byDay[x.day] || []).push(x);
  for (const d in byDay) byDay[d].sort((a, b) => a.at.localeCompare(b.at));
  const chores = ['Water the plants', 'Wash your face'].map(t => Object.assign(e(0, '', 'owner', t), daily, { goal_id: 'make-me-healthier' }));
  const calendar = {
    from: day(-30), to: day(60), today,
    overdue: [],
    due: entries.filter(x => x.day === today && x.kind !== 'agent').concat(chores),
    soon: [
      Object.assign(e(0, '', 'owner', 'Book the string quartet for the wedding'), { soon: true, window: 'soon', kind_label: 'anytime', goal_id: 'plan-the-wedding' }),
      Object.assign(e(0, '', 'owner', 'Finish Notes of 2024'), { soon: true, window: 'soon', kind_label: 'anytime', goal_id: 'make-more-art' }),
    ],
    anytime: [{ id: 'ask:ask-honey-1', day: '', kind: 'ask', state: 'open', title: 'Honeymoon draft 3: Bali, but closer', ask_id: 'ask-honey-1', thread_id: HONEY, lane: 'mine', open: true }],
    days: Object.keys(byDay).sort().map(d => ({ day: d, entries: byDay[d] })),
  };

  // Configuration: the plan the sessions run on and its limits as Anthropic
  // reports them, the model new sessions start on, and the data sources
  // grouped under the goals that read them.
  const ANTHROPIC = 'M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 20.459h3.7442l1.3693-3.5527h7.0052l1.3693 3.5528h3.7442L10.5363 3.5409Zm-.3712 10.2232 2.2914-5.9456 2.2914 5.9456Z';
  const quota = {
    generated_at: iso(0), fetched_at: iso(1), available: true, next_model: MODEL, error: '',
    plan: { name: 'Claude Max 20x', via: 'Claude Code', usd: 200, period: 'monthly', charged_on: day(-17), month_usd: 727.96,
      brand: { mark: 'A', color: '#D97757', ink: '#FFFFFF', logo: ANTHROPIC } },
    windows: [
      { key: 'five_hour', label: 'All models · 5 hours', utilization: 40, elapsed_pct: 50, tone: 'ok', foot: '2 h 30 min left · resets 2:00 AM' },
      { key: 'seven_day', label: 'All models · 7 days', utilization: 14, elapsed_pct: 14, tone: 'ok', foot: '6 d left · resets Tue 12:00 AM' },
      { key: 'seven_day_fable', label: 'Fable · 7 days', utilization: 21, elapsed_pct: 14, tone: 'warn', scope_model: 'fable', foot: '6 d left · resets Tue 12:00 AM' },
    ],
  };
  const model = { default_model: 'claude-fable-5-1', explicit: true, options: ['claude-fable-5-1', MODEL, 'claude-sonnet-5-5'],
    reason: 'fable $280 of $250 today, back at midnight', starts_on: MODEL,
    rungs: [{ model: 'claude-fable-5-1', open: false, why: 'fable $280 of $250 today, back at midnight' }, { model: MODEL, open: true, why: '' }, { model: 'claude-sonnet-5-5', open: true, why: '' }] };

  // Each source's tile, as the hub's brand table draws it.
  const B = {
    health: { mark: '♥', color: '#FF2D55', ink: '#FFFFFF', logo: 'M12 21.6 10.5 20.2C5.2 15.4 1.7 12.2 1.7 8.3 1.7 5.1 4.2 2.6 7.4 2.6c1.8 0 3.5.8 4.6 2.2 1.1-1.4 2.8-2.2 4.6-2.2 3.2 0 5.7 2.5 5.7 5.7 0 3.9-3.5 7.1-8.8 11.9z' },
    amazon: { mark: 'a', color: '#232F3E', ink: '#FF9900', logo: 'M.045 18.02c.072-.116.187-.124.348-.022 3.636 2.11 7.594 3.166 11.87 3.166 2.852 0 5.668-.533 8.447-1.595l.315-.14c.138-.06.234-.1.293-.13.226-.088.39-.046.525.13.12.174.09.336-.12.48-.256.19-.6.41-1.006.654-1.244.743-2.64 1.316-4.185 1.726a17.617 17.617 0 01-10.951-.577 17.88 17.88 0 01-5.43-3.35c-.1-.074-.151-.15-.151-.22 0-.047.021-.09.051-.13zm6.565-6.218c0-1.005.247-1.863.743-2.577.495-.71 1.17-1.25 2.04-1.615.796-.335 1.756-.575 2.912-.72.39-.046 1.033-.103 1.92-.174v-.37c0-.93-.105-1.558-.3-1.875-.302-.43-.78-.65-1.44-.65h-.182c-.48.046-.896.196-1.246.46-.35.27-.575.63-.675 1.096-.06.3-.206.465-.435.51l-2.52-.315c-.248-.06-.372-.18-.372-.39 0-.046.007-.09.022-.15.247-1.29.855-2.25 1.820-2.88.976-.616 2.1-.975 3.39-1.05h.54c1.65 0 2.957.434 3.888 1.29.135.15.27.3.405.48.12.165.224.314.283.45.075.134.15.33.195.57.06.254.105.42.135.51.03.104.062.3.076.615.01.313.02.493.02.553v5.28c0 .376.06.72.165 1.036.105.313.21.54.315.674l.51.674c.09.136.136.256.136.36 0 .12-.06.226-.18.314-1.2 1.05-1.86 1.62-1.963 1.71-.165.135-.375.15-.63.045a6.062 6.062 0 01-.526-.496l-.31-.347a9.391 9.391 0 01-.317-.42l-.3-.435c-.81.886-1.603 1.44-2.4 1.665-.494.15-1.093.227-1.83.227-1.11 0-2.04-.343-2.76-1.034-.72-.69-1.080-1.665-1.080-2.94l-.05-.076zm3.753-.438c0 .566.14 1.02.425 1.364.285.34.675.512 1.155.512.045 0 .106-.007.195-.02.09-.016.134-.023.166-.023.614-.16 1.080-.553 1.424-1.178.165-.28.285-.58.36-.91.09-.32.12-.59.135-.8.015-.195.015-.54.015-1.005v-.54c-.84 0-1.484.06-1.92.18-1.275.36-1.92 1.17-1.92 2.43l-.035-.02zm9.162 7.027c.03-.06.075-.11.132-.17.362-.243.714-.41 1.05-.5a8.094 8.094 0 011.612-.24c.14-.012.28 0 .41.03.65.06 1.050.168 1.172.33.063.09.099.228.099.39v.15c0 .51-.149 1.11-.424 1.8-.278.69-.664 1.248-1.156 1.68-.073.06-.14.09-.197.09-.03 0-.06 0-.09-.012-.09-.044-.107-.12-.064-.24.54-1.26.806-2.143.806-2.64 0-.15-.03-.27-.087-.344-.145-.166-.55-.257-1.224-.257-.243 0-.533.016-.87.046-.363.045-.7.09-1 .135-.09 0-.148-.014-.18-.044-.03-.03-.036-.047-.02-.077 0-.017.006-.03.02-.063v-.06z' },
    craigslist: { mark: 'CL', color: '#5B2A86', ink: '#FFFFFF', logo: 'M12 2a10 10 0 1 1 0 20a10 10 0 1 1 0-20zm0 2a8 8 0 1 0 0 16a8 8 0 1 0 0-16zM11 4h2v16h-2zM11.29 11.29 12.71 12.71 7.05 18.37 5.63 16.95zM12.71 11.29 18.37 16.95 16.95 18.37 11.29 12.71z' },
    mail: { mark: 'M', color: '#EA4335', ink: '#FFFFFF', logo: 'M24 5.457v13.909c0 .904-.732 1.636-1.636 1.636h-3.819V11.73L12 16.64l-6.545-4.91v9.273H1.636A1.636 1.636 0 0 1 0 19.366V5.457c0-2.023 2.309-3.178 3.927-1.964L5.455 4.64 12 9.548l6.545-4.91 1.528-1.145C21.69 2.28 24 3.434 24 5.457z' },
    drive: { mark: 'GD', color: '#FFBA00', ink: '#1F2937', logo: 'M12.01 1.485c-2.082 0-3.754.02-3.743.047.01.02 1.708 3.001 3.774 6.62l3.76 6.574h3.76c2.081 0 3.753-.02 3.742-.047-.005-.02-1.708-3.001-3.775-6.62l-3.76-6.574zm-4.76 1.73a789.828 789.861 0 0 0-3.63 6.319L0 15.868l1.89 3.298 1.885 3.297 3.62-6.335 3.618-6.33-1.88-3.287C8.1 4.704 7.255 3.22 7.25 3.214zm2.259 12.653-.203.348c-.114.198-.96 1.672-1.88 3.287a423.93 423.948 0 0 1-1.698 2.97c-.01.026 3.24.042 7.222.042h7.244l1.796-3.157c.992-1.734 1.85-3.23 1.906-3.323l.104-.167h-7.249z' },
    simplefin: { mark: 'SF', color: '#1B5E20', ink: '#FFFFFF' },
    car: { mark: 'car', color: '#0EA5E9', ink: '#FFFFFF', logo: 'M12 1a4 4 0 0 0-4 4v6a4 4 0 0 0 8 0V5a4 4 0 0 0-4-4zM5 10v1a7 7 0 0 0 6 6.93V21H8v2h8v-2h-3v-3.07A7 7 0 0 0 19 11v-1h-2v1a5 5 0 0 1-10 0v-1z' },
    console: { mark: '⌘', color: '#334155', ink: '#FFFFFF' },
    gcal: { mark: '31', color: '#1A73E8', ink: '#FFFFFF', logo: 'M18.316 5.684H24v12.632h-5.684V5.684zM5.684 24h12.632v-5.684H5.684V24zM18.316 5.684V0H1.895A1.894 1.894 0 0 0 0 1.895v16.421h5.684V5.684h12.632zm-7.207 6.25v-.065c.272-.144.5-.349.687-.617s.279-.595.279-.982c0-.379-.099-.72-.3-1.025a2.05 2.05 0 0 0-.832-.714 2.703 2.703 0 0 0-1.197-.257c-.6 0-1.094.156-1.481.467-.386.311-.65.671-.793 1.078l1.085.452c.086-.249.224-.461.413-.633.189-.172.445-.257.767-.257.33 0 .602.088.816.264a.86.86 0 0 1 .322.703c0 .33-.12.589-.36.778-.24.19-.535.284-.886.284h-.567v1.085h.633c.407 0 .748.109 1.02.327.272.218.407.499.407.843 0 .336-.129.614-.387.832s-.565.327-.924.327c-.351 0-.651-.103-.897-.311-.248-.208-.422-.502-.521-.881l-1.096.452c.178.616.505 1.082.977 1.401.472.319.984.478 1.538.477a2.84 2.84 0 0 0 1.293-.291c.382-.193.684-.458.902-.794.218-.336.327-.72.327-1.149 0-.429-.115-.797-.344-1.105a2.067 2.067 0 0 0-.881-.689zm2.093-1.931l.602.913L15 10.045v5.744h1.187V8.446h-.827l-2.158 1.557zM22.105 0h-3.289v5.184H24V1.895A1.894 1.894 0 0 0 22.105 0zm-3.289 23.5l4.684-4.684h-4.684V23.5zM0 22.105C0 23.152.848 24 1.895 24h3.289v-5.184H0v3.289z' },
    lchan: { mark: 'lc', color: '#111827', ink: '#A3E635' },
    app: { mark: 'L', color: '#16A34A', ink: '#FFFFFF' },
    github: { mark: 'GH', color: '#181717', ink: '#FFFFFF', logo: 'M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12' },
    scholar: { mark: 'GS', color: '#4285F4', ink: '#FFFFFF', logo: 'M5.242 13.769L0 9.5 12 0l12 9.5-5.242 4.269C17.548 11.249 14.978 9.5 12 9.5c-2.977 0-5.548 1.748-6.758 4.269zM12 10a7 7 0 1 0 0 14 7 7 0 0 0 0-14z' },
    searchconsole: { mark: 'SC', color: '#34A853', ink: '#FFFFFF' },
    openalex: { mark: 'OA', color: '#C2410C', ink: '#FFFFFF' },
    posthog: { mark: 'PH', color: '#1D4AFF', ink: '#F9BD2B' },
    semanticscholar: { mark: 'S2', color: '#1857B6', ink: '#F7C948' },
    x: { mark: 'X', color: '#000000', ink: '#FFFFFF', logo: 'M14.234 10.162 22.977 0h-2.072l-7.591 8.824L7.251 0H.258l9.168 13.343L.258 24H2.33l8.016-9.318L16.749 24h6.993zm-2.837 3.299-.929-1.329L3.076 1.56h3.182l5.965 8.532.929 1.329 7.754 11.09h-3.182z' },
    youtube: { mark: '▶', color: '#FF0000', ink: '#FFFFFF', logo: 'M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.070 0 12 0 12s0 3.930.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.930 24 12 24 12s0-3.930-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z' },
  };
  const src = (id, title, total, lastMin, summary, o) => Object.assign({ id, title, status: 'connected', kinds: [], accounts: [], brand: B[id],
    total, last: iso(lastMin), last_ok: iso(lastMin), summary,
    from: `${title}, read by the hub on its own tick.`, storage: 'observations table in data/life.db (SQLite, append-only)' }, o || {});
  // Each goal's section in its own colour (sources.go goalHex of the goal's
  // hue), the goal with the most sources first, as byGoal orders them.
  const GOAL_HEX = { 'make-me-healthier': '#AB2B40', 'make-more-money': '#2BAB6B', 'build-a-compelling-life-agent': '#2B56AB', 'grow-my-audience': '#AB602B' };
  const G = (id, title, sources) => ({ id, title, blurb: (goals.find(g => g.id === id) || {}).statement || '', tag: title, color: GOAL_HEX[id], sources });
  const sources = { groups: [
    G('grow-my-audience', 'Grow my audience', [
      src('github', 'GitHub', 1300, 55, '21 repos'),
      src('scholar', 'Google Scholar', 223, 60 * 20, 'Scholar profile'),
      src('searchconsole', 'Google Search Console', 346, 60 * 8, '3 properties'),
      src('openalex', 'OpenAlex', 1677, 60 * 20, 'every paper on the Scholar profile'),
      src('posthog', 'Personal site (PostHog)', 14051, 25, '4 projects'),
      src('semanticscholar', 'Semantic Scholar', 969, 60 * 20, 'citation context'),
      src('x', 'X / Twitter', 270, 60 * 6, '1 account'),
      src('youtube', 'YouTube', 1346, 60 * 3, '2 channels + Analytics'),
    ]),
    G('make-more-money', 'Make more money', [
      src('amazon', 'Amazon order history', 144, 60 * 24 * 12, 'order export'),
      src('craigslist', 'Craigslist', 0, 60 * 30, 'saved searches'),
      src('mail', 'Gmail receipts', 435, 300, 'receipts, read only'),
      src('drive', 'Google Drive · statements', 11002, 600, '16 folders'),
      src('simplefin', 'SimpleFIN', 1455, 200, '7 accounts'),
    ]),
    G('build-a-compelling-life-agent', 'Build agent', [
      src('car', 'Car recorder', 4816, 30, 'desktop app'),
      src('console', 'Console trail', 19029, 2, 'web console'),
      src('gcal', 'Google Calendar', 0, 15, '2 calendars'),
      src('lchan', 'lchan', 5, 360, 'agent board'),
      src('app', 'Life app', 1425, 12, 'photos, notes, voice'),
    ]),
    G('make-me-healthier', 'Make me healthier', [
      src('health', 'Apple Health', 22755, 40, 'iPhone (Life app)', {
        accounts: [{ label: 'iPhone (Life app)', via: 'phone', last: iso(40) }],
        kinds: [{ kind: 'steps', n: 1412, first: iso(900000), last: iso(40), note: 'one row per day' }, { kind: 'sleep', n: 1398, first: iso(900000), last: iso(300) },
          { kind: 'workout', n: 296, first: iso(900000), last: iso(1500) }] }),
    ]),
  ] };

  const recFull = id => Object.assign({}, recs.find(r => r.id === id));

  // Answer by full path (query included). undefined = fall through to FIX.
  window.DEMO = function (path) {
    const [p, q] = path.split('?');
    const qs = new URLSearchParams(q || '');
    if (p === '/threads') return threads;
    if (p === '/board') return board;
    if (p === '/goals') return goals;
    if (p === '/recs') return { recs: qs.get('thread') ? recs.filter(r => r.thread_id === qs.get('thread')) : recs };
    if (p === '/recs/stats') return undefined;
    if (p.startsWith('/recs/')) return recFull(p.split('/')[2]);
    if (p === '/calendar') return calendar;
    if (p === '/actions') return actions;
    if (p.startsWith('/actions/')) return actions.find(a => a.id === p.split('/')[2]);
    if (p === '/asks') return qs.get('thread') ? asks.filter(a => a.thread_id === qs.get('thread')) : asks;
    if (p === '/prompts') return [];
    if (p === '/spend/quota') return quota;
    if (p === '/spend/model') return model;
    if (p === '/sources') return sources;
    if (p === '/status') return { ok: true, jobs: 1, pending_actions: 1, quiet: true };
    const g = p.match(/^\/goals\/([^/]+)(\/notes)?$/);
    if (g) return g[2] ? (g[1] === 'build-a-compelling-life-agent' ? agentNotes : []) : goals.find(x => x.id === g[1]);
    const t = p.match(/^\/threads\/([^/]+)(\/[a-z]+)?$/);
    if (t) {
      const th = threads.find(x => x.id === t[1]);
      if (!t[2]) return Object.assign({ tokens: 380000 }, th);
      if (t[2] === '/messages') return (messages[t[1]] || []).map(m => Object.assign({ thread_id: t[1] }, m));
      return [];
    }
    return undefined;
  };
})();
