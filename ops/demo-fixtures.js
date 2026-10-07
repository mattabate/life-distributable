// Demo world for the README screenshots: a fictional owner, "Sam", with four
// goals, five sessions, cards, recs and a week of calendar. Loaded by
// ops/web-preview.html only when the URL has &demo=1 (ops/demo-shots.sh).
// Everything here is invented. Nothing ships in the hub binary.
'use strict';
(function () {
  const now = new Date();
  const iso = m => new Date(now.getTime() - m * 60000).toISOString();
  const pad = n => String(n).padStart(2, '0');
  const ymd = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const day = n => ymd(new Date(now.getFullYear(), now.getMonth(), now.getDate() + n));
  const today = day(0);

  const RUN = 'half-marathon-plan-a1c3';
  const INS = 'car-insurance-renewal-b7d2';
  const ESP = 'spanish-every-day-c4e9';
  const HOUSE = 'house-fund-d2f8';
  const BDAY = 'moms-birthday-dinner-e5a1';
  const MODEL = 'claude-opus-5';

  const outs = {
    approve: [{ value: 'approved', label: 'Approve' }, { value: 'denied', label: 'Deny' }, { value: '', label: 'Reply' }],
    ask: [{ value: 'done', label: 'I did this' }, { value: 'wont', label: "Won't do" }, { value: '', label: 'Reply' }],
    decide: [{ value: '', label: 'Reply' }, { value: 'done', label: 'Decided' }, { value: 'wont', label: 'Not deciding' }],
    read: [{ value: 'done', label: 'Read it' }],
    rec: [{ value: 'accepted', label: 'Accept' }, { value: 'declined', label: 'Decline' }, { value: '', label: 'Reply' }],
    step: [{ value: 'done', label: 'Done' }, { value: 'wont', label: "Won't do" }, { value: '', label: 'Reply' }],
  };

  const threads = [
    { id: INS, title: 'Car insurance renews in 3 weeks', status: 'needs_you', goal_id: 'save-for-a-house', model: MODEL,
      last_message: 'Found the same coverage for $26 a month less.', last_message_at: iso(12), cost_usd: 2.14 },
    { id: BDAY, title: "Mom's birthday dinner", status: 'needs_you', model: MODEL,
      last_message: 'Two places have a table for 6 on Saturday.', last_message_at: iso(34), cost_usd: 0.88 },
    { id: RUN, title: 'Half marathon training plan', status: 'running', goal_id: 'run-a-half-marathon', model: MODEL,
      activity: 'Reading last week of runs from Apple Health', last_message: 'Adjusting week 6.', last_message_at: iso(1),
      cost_usd: 3.40, schedule: 'weekly@Sun 18:00', schedule_label: 'Sundays 6 PM' },
    { id: ESP, title: 'Spanish, 15 minutes a day', status: 'needs_you', goal_id: 'learn-spanish', model: MODEL,
      last_message: 'Your streak is 23 days.', last_message_at: iso(180), cost_usd: 1.05, schedule: 'daily@07:30', schedule_label: 'daily 7:30 AM' },
    { id: HOUSE, title: 'Where to keep the down payment', status: 'idle', goal_id: 'save-for-a-house', model: MODEL,
      last_message: 'Compared four high-yield savings accounts.', last_message_at: iso(1440), cost_usd: 1.72 },
  ];

  const actions = [
    { id: 'act-ins-1', created_at: iso(12), project: 'life', kind: 'money', gated: true, state: 'proposed', thread_id: INS,
      outcomes: outs.approve, title: 'Switch car insurance: save $312 a year',
      detail: 'Same coverage, same deductible, new policy starts the day the old one ends.\n\n- Old: $148/mo\n- New: $122/mo\n- No cancellation fee',
      exec_type: 'claude', exec_payload: { project: 'life', prompt: 'Start the new policy on the renewal date and cancel the old one.' } },
    { id: 'act-bday-1', created_at: iso(30), project: 'life', kind: 'contact', gated: true, state: 'proposed', thread_id: BDAY,
      outcomes: outs.approve, title: 'Email the restaurant to book a table for 6',
      detail: 'Saturday 7 PM, a quiet table, one guest is gluten free. The email goes from your address.',
      exec_type: 'claude', exec_payload: { project: 'life', prompt: 'Send the booking email and add the dinner to the calendar.' } },
  ];

  const asks = [
    { id: 'ask-esp-1', title: '15 minutes of Spanish today', state: 'active', created_at: iso(180), kind: 'physical', surface: 'mobile',
      thread_id: ESP, thread_title: 'Spanish, 15 minutes a day', detail: 'Lesson 24: ordering at a restaurant.', outcomes: outs.ask, class: 'unblock', open: true },
    { id: 'ask-bday-1', title: 'Which place for Saturday?', state: 'active', created_at: iso(33), kind: 'decision', surface: 'any',
      thread_id: BDAY, thread_title: "Mom's birthday dinner",
      detail: '- **Olive & Fig**: Mediterranean, $45 a head, private corner\n- **Hearth**: Italian, $38 a head, louder', outcomes: outs.decide, class: 'unblock', open: true },
    { id: 'ask-run-1', title: 'Week 5 done: 31 miles, longest run 9', state: 'active', created_at: iso(240), kind: 'read', surface: 'any',
      thread_id: RUN, thread_title: 'Half marathon training plan',
      detail: 'All four runs logged. Your easy pace dropped 12 seconds a mile. Week 6 adds one mile to the long run.', outcomes: outs.read, class: 'read', open: true },
  ];

  const recs = [
    { id: 'rec-hysa', status: 'proposed', open: true, title: 'Move the house fund to a 4.4% savings account', thread_id: HOUSE, goal_id: 'save-for-a-house',
      because: 'It earns **0.5%** where it is now. On $18,000 that is about **$700 a year** more.', expect: 'Interest above $55 a month by spring',
      domain: 'money', kind: 'switch', cost_label: 'free', effort: 'low', confidence: 80, model: MODEL, created_at: iso(1400),
      dates_label: 'act by Friday', outcomes: outs.rec },
    { id: 'rec-shoes', status: 'proposed', open: true, title: 'New running shoes before week 8', thread_id: RUN, goal_id: 'run-a-half-marathon',
      because: 'Your pair has about **380 miles** on it. Most last 300 to 500.', expect: 'No shin pain on the long runs',
      domain: 'health', kind: 'buy', cost_label: '$140', cost_cents: 14000, effort: 'low', confidence: 65, model: MODEL, created_at: iso(300),
      dates_label: 'act by Nov 1', outcomes: outs.rec },
    { id: 'rec-tutor', status: 'proposed', open: true, title: 'One 30 minute Spanish conversation class a week', thread_id: ESP, goal_id: 'learn-spanish',
      because: 'The app covers reading. Speaking is the gap you named.', expect: 'Order dinner in Spanish by December',
      domain: 'other', kind: 'try', cost_label: '$15/wk', cost_cents: 1500, cost_period: 'weekly', effort: 'medium', confidence: 60, model: MODEL, created_at: iso(2000),
      dates_label: 'review in 6 weeks', outcomes: outs.rec },
    { id: 'rec-stream', status: 'accepted', open: false, closed: true, title: 'Cancel the second streaming service', domain: 'money', kind: 'stop',
      cost_label: '$16/mo saved', created_at: iso(9000), decision_note: 'yes, never watch it', outcomes: outs.rec },
  ];

  const goals = [
    { id: 'run-a-half-marathon', title: 'Run a half marathon', status: 'active', horizon: 'quarter', cadence: 'weekly', emblem: { symbol: 'health', hue: 10 },
      statement: 'Finish the city half in March, under 2:10, without getting hurt.', note_count: 14, last_reviewed_at: iso(240), digest_at: iso(240),
      digest: '**Week 5 of 16.** 31 miles this week, long run 9 miles. Easy pace is 10:40 a mile, down from 11:05 in week 1.\n\n- On plan, no missed runs\n- Shoes near the end of their life (a rec is open)\n- Next: week 6 long run, 10 miles on Sunday' },
    { id: 'save-for-a-house', title: 'Save for a house', status: 'active', horizon: 'year', cadence: 'monthly', emblem: { symbol: 'money', hue: 145 },
      statement: '$40,000 down payment by next fall.', note_count: 9, last_reviewed_at: iso(1440) },
    { id: 'learn-spanish', title: 'Learn Spanish', status: 'active', horizon: 'year', cadence: 'weekly', emblem: { symbol: 'learn', hue: 210 },
      statement: 'Hold a 10 minute conversation by summer.', note_count: 6, last_reviewed_at: iso(3000) },
    { id: 'see-friends-more', title: 'See friends more', status: 'active', horizon: 'ongoing', cadence: 'monthly', emblem: { symbol: 'audience', hue: 280 },
      statement: 'One plan with friends every week.', note_count: 3, last_reviewed_at: iso(9000) },
  ];
  const runNotes = [
    { id: 4, goal_id: 'run-a-half-marathon', created_at: iso(240), author: 'Half marathon training plan', thread_id: RUN, kind: 'evidence',
      text: 'Week 5: **31 miles**, all four runs. Long run 9 miles at 10:52.' },
    { id: 3, goal_id: 'run-a-half-marathon', created_at: iso(5000), author: 'owner', kind: 'decision', text: 'Long runs on Sunday mornings, not Saturday.' },
    { id: 2, goal_id: 'run-a-half-marathon', created_at: iso(12000), author: 'Half marathon training plan', thread_id: RUN, kind: 'evidence',
      text: 'Signed up for the March race. Bib pickup the Friday before.' },
  ];

  const messages = {
    [INS]: [
      { id: 1, role: 'claude', ts: iso(2000), run_id: 'r1', text: 'Your car insurance renews on the 28th at **$148 a month**, up from $131. I will get quotes for the same coverage.' },
      { id: 2, role: 'owner', author: 'owner', ts: iso(1900), run_id: 'r1', text: 'Yes please. Keep the same deductible.' },
      { id: 3, role: 'claude', ts: iso(12), run_id: 'r2', cost_usd: 2.14,
        text: 'Got three quotes with the same coverage and the $500 deductible.\n\n| Insurer | Monthly |\n|---|---|\n| Current | $148 |\n| Quote A | $122 |\n| Quote B | $129 |\n\nQuote A saves **$312 a year**. Switching is a money step, so it is waiting for your approval.' },
    ],
    [BDAY]: [
      { id: 1, role: 'owner', author: 'owner', ts: iso(60), run_id: 'r1', text: "Find a place for mom's birthday dinner on Saturday, 6 of us, one is gluten free." },
      { id: 2, role: 'claude', ts: iso(34), run_id: 'r1', cost_usd: 0.88, text: 'Two places have a table for 6 at 7 PM and a gluten free menu. Pick one and I will draft the booking email.' },
    ],
    [RUN]: [
      { id: 1, role: 'claude', ts: iso(240), run_id: 'r1', text: 'Week 5 is done. Summary is on your board.' },
      { id: 2, role: 'owner', author: 'owner', ts: iso(5), run_id: 'r2', text: 'My left calf was tight on Sunday. Adjust if needed.' },
    ],
    [ESP]: [{ id: 1, role: 'claude', ts: iso(180), run_id: 'r1', text: 'Your streak is 23 days. Today is lesson 24.' }],
    [HOUSE]: [{ id: 1, role: 'claude', ts: iso(1440), run_id: 'r1', text: 'Compared four high-yield savings accounts. One rec is on your Recs page.' }],
  };

  const pill = (word, tone) => ({ word, tone });
  const board = {
    surface: 'web', count: 5, working: 1,
    for_you: { [INS]: 1, [BDAY]: 2, [ESP]: 1, [RUN]: 1 },
    reads: { [RUN]: 1 }, installs: {}, open: { [INS]: 1, [BDAY]: 2, [ESP]: 1, [RUN]: 1 },
    first: { [INS]: 'act-ins-1', [BDAY]: 'ask-bday-1', [ESP]: 'ask-esp-1', [RUN]: 'ask-run-1' },
    sessions: [
      { id: INS, title: threads[0].title, n: 1, first: 'act-ins-1', running: false, actions: [actions[0]], asks: [], steps: [] },
      { id: BDAY, title: threads[1].title, n: 2, first: 'ask-bday-1', running: false, actions: [actions[1]], asks: [asks[1]], steps: [] },
      { id: ESP, title: threads[3].title, n: 1, first: 'ask-esp-1', running: false, actions: [], asks: [asks[0]], steps: [] },
      { id: RUN, title: threads[2].title, n: 1, first: 'ask-run-1', running: true, actions: [], asks: [asks[2]], steps: [] },
    ],
    headings: [
      { key: 'your_turn', label: 'Your turn', count: '5 in 4 sessions', n: 3 },
      { key: 'working', label: 'Working', count: '1', n: 1 },
    ],
    section: { [INS]: 'your_turn', [BDAY]: 'your_turn', [ESP]: 'your_turn', [RUN]: 'working' },
    pills: { [INS]: [pill('1 for you', 'needs')], [BDAY]: [pill('2 for you', 'needs')], [ESP]: [pill('1 for you', 'needs')],
      [RUN]: [pill('running', 'running'), pill('1 to read', 'read')] },
    badges: { your_turn: 5, calendar: 2, recs: 3 },
  };

  // A week of calendar around today: Sam's steps (red), agent runs (grey).
  const e = (d, at, kind, title, extra) => Object.assign({ id: `cal-${d}-${at}`, ref: `cal:cal-${d}-${at}`, day: day(d), at, kind, title,
    state: 'scheduled', lane: kind === 'agent' ? 'scheduled' : 'mine', open: true, item: true, kind_label: kind === 'agent' ? 'one-off run' : 'your step' }, extra || {});
  // w(k): day k of this week (0 = Sunday), so every row lands in the visible week.
  const w = k => k - now.getDay();
  const entries = [
    e(w(0), '08:30', 'owner', 'Long run, 9 miles', { goal_id: 'run-a-half-marathon' }),
    e(w(0), '18:00', 'agent', 'Plan the week', { thread_id: RUN }),
    e(w(1), '08:00', 'owner', 'Spanish, lesson 23', { goal_id: 'learn-spanish' }),
    e(w(1), '18:00', 'owner', 'Easy run, 4 miles', { goal_id: 'run-a-half-marathon' }),
    e(w(2), '08:00', 'owner', 'Spanish, lesson 24', { goal_id: 'learn-spanish' }),
    e(w(2), '09:00', 'agent', 'Check the savings rates', { thread_id: HOUSE }),
    e(w(2), '12:30', 'owner', 'Call the dentist to reschedule'),
    e(w(3), '08:00', 'owner', 'Spanish, lesson 25', { goal_id: 'learn-spanish' }),
    e(w(3), '18:00', 'owner', 'Tempo run, 5 miles', { goal_id: 'run-a-half-marathon' }),
    e(w(4), '10:00', 'agent', 'Draft the monthly savings note', { thread_id: HOUSE }),
    e(w(4), '19:00', 'owner', 'Dinner with Alex and Jo', { goal_id: 'see-friends-more' }),
    e(w(5), '08:00', 'owner', 'Spanish, lesson 26', { goal_id: 'learn-spanish' }),
    e(w(5), '14:00', 'agent', 'Compare car insurance quotes', { thread_id: INS }),
    e(w(6), '09:00', 'owner', 'Easy run, 5 miles', { goal_id: 'run-a-half-marathon' }),
    e(w(6), '19:00', 'owner', "Mom's birthday dinner", { thread_id: BDAY }),
  ];
  const byDay = {};
  for (const x of entries) (byDay[x.day] = byDay[x.day] || []).push(x);
  const calendar = {
    from: day(-30), to: day(60), today,
    overdue: [Object.assign(e(-2, '', 'owner', 'Renew the passport'), { overdue: true, due: 'by', detail: 'Form and photo are in the drawer.' })],
    due: entries.filter(x => x.day === today && x.kind === 'owner'),
    soon: [Object.assign(e(0, '', 'owner', 'Pick up the dry cleaning'), { soon: true, window: 'soon', kind_label: 'anytime' })],
    anytime: [{ id: 'ask:ask-bday-1', day: '', kind: 'ask', state: 'open', title: 'Which place for Saturday?', ask_id: 'ask-bday-1', thread_id: BDAY, lane: 'mine', open: true }],
    days: Object.keys(byDay).sort().map(d => ({ day: d, entries: byDay[d] })),
  };

  // Configuration: the plan Sam's sessions run on and its two limits, the
  // model new sessions start on, and what the hub is connected to. The
  // plan's price is the card charge the hub found; the windows are the
  // plan's own, as Anthropic reports them.
  const quota = {
    generated_at: iso(0), fetched_at: iso(1), available: true, next_model: MODEL, error: '',
    plan: { name: 'Claude Max', via: 'Claude Code', usd: 100, period: 'monthly', charged_on: day(-6), month_usd: 23.4,
      brand: { mark: 'A', color: '#D97757', ink: '#FFFFFF' } },
    windows: [
      { key: 'five_hour', label: '5 hours', utilization: 31, elapsed_pct: 58, tone: 'ok', foot: '31% used · resets 4:00 PM' },
      { key: 'seven_day', label: '7 days · all models', utilization: 64, elapsed_pct: 71, tone: 'ok', foot: '64% used · resets Sun 12:00 AM' },
    ],
  };
  const model = { default_model: '', explicit: false, options: [MODEL, 'claude-sonnet-5'], reason: '', starts_on: MODEL,
    rungs: [{ model: MODEL, open: true, why: '' }, { model: 'claude-sonnet-5', open: true, why: '' }] };

  // One card per source, under its group's tag. `brand` is the provider's
  // tile; the heart is the Health app's own glyph as a path.
  const HEART = 'M12 21.6 10.5 20.2C5.2 15.4 1.7 12.2 1.7 8.3 1.7 5.1 4.2 2.6 7.4 2.6c1.8 0 3.5.8 4.6 2.2 1.1-1.4 2.8-2.2 4.6-2.2 3.2 0 5.7 2.5 5.7 5.7 0 3.9-3.5 7.1-8.8 11.9z';
  const src = (id, title, brand, o) => Object.assign({ id, title, status: 'live', kinds: [], accounts: [], brand,
    from: `${title}, read by the hub on its own tick.`, storage: 'observations table in data/life.db (SQLite, append-only)' }, o);
  const sources = { groups: [
    { id: 'health', title: 'Health', blurb: 'Apple Health syncs itself from the phone.', tag: 'Health', color: '#DC2626', sources: [
      src('health', 'Apple Health', { mark: '♥', color: '#FF2D55', ink: '#FFFFFF', logo: HEART },
        { total: 1842, last: iso(40), summary: 'iPhone (Life app)', accounts: [{ label: 'iPhone (Life app)', via: 'phone', last: iso(40) }],
          kinds: [{ kind: 'steps', n: 412, first: iso(600000), last: iso(40), note: 'one row per day' }, { kind: 'workout', n: 96, first: iso(600000), last: iso(1500) },
            { kind: 'sleep', n: 398, first: iso(600000), last: iso(300) }] }),
    ] },
    { id: 'calendar', title: 'Calendar and mail', blurb: 'Read only. The agent never sends or deletes.', tag: 'Calendar and mail', color: '#1A73E8', sources: [
      src('gcal', 'Google Calendar', { mark: '31', color: '#1A73E8', ink: '#FFFFFF' },
        { total: 412, last: iso(15), summary: '2 calendars', accounts: [{ label: 'Personal', via: 'google', last: iso(15) }, { label: 'Running club', via: 'google', last: iso(90) }] }),
      src('mail', 'Mail', { mark: 'M', color: '#EA4335', ink: '#FFFFFF' },
        { total: 980, last: iso(8), summary: 'inbox, read only', accounts: [{ label: 'inbox', via: 'google', last: iso(8) }] }),
    ] },
    { id: 'money', title: 'Money', blurb: 'Balances and transactions, through SimpleFIN.', tag: 'Money', color: '#16A34A', sources: [
      src('simplefin', 'Bank (SimpleFIN)', { mark: 'SF', color: '#0F766E', ink: '#FFFFFF' },
        { total: 1306, last: iso(300), summary: '2 accounts', accounts: [{ label: 'Checking', via: 'simplefin', last: iso(300) }, { label: 'House fund (savings)', via: 'simplefin', last: iso(300) }] }),
    ] },
    { id: 'code', title: 'Code', blurb: 'The repos the agent works in.', tag: 'Code', color: '#181717', sources: [
      src('github', 'GitHub', { mark: 'GH', color: '#181717', ink: '#FFFFFF' },
        { total: 220, last: iso(55), summary: '3 repos', accounts: [{ label: 'life', via: 'github', last: iso(55) }, { label: 'running-log', via: 'github', last: iso(4000) }, { label: 'recipes', via: 'github', last: iso(9000) }] }),
    ] },
    { id: 'phone', title: 'From the phone', blurb: 'What you send the app directly.', tag: 'Phone', color: '#0D9488', sources: [
      src('app', 'Life app', { mark: 'L', color: '#16A34A', ink: '#FFFFFF' },
        { total: 57, last: iso(12), summary: 'photos, notes, voice', from: 'What Sam sends the app: a photo, a note, a voice line.' }),
    ] },
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
    if (p === '/status') return { ok: true, jobs: 1, pending_actions: 2, quiet: true };
    const g = p.match(/^\/goals\/([^/]+)(\/notes)?$/);
    if (g) return g[2] ? (g[1] === 'run-a-half-marathon' ? runNotes : []) : goals.find(x => x.id === g[1]);
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
