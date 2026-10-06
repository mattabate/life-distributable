// Tests for views/calgrid.js — the Calendar page drawn as a real calendar
// in the style of Google Calendar, with drag to change a time. What is
// actually worth pinning here is the arithmetic a grid gets wrong quietly:
// which local day a cell is, which range a mode fetches, which rows may be
// dragged at all, and what a drop rewrites. `ops/webcheck.sh` runs these
// (node --test). Every fixture below is invented and holds no personal data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// The same classic scripts the browser loads, in index.html's order: ui.js
// (esc/splitRef/mdInline), composer.js (the box a step's card carries),
// calendar.js (calEntryHTML — the row every mode draws), then the grid.
const load = f => vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', f), 'utf8'), { filename: f });
load('ui.js');
// composer.js binds one paste listener at load; calgrid.js binds none (its
// Escape key goes on in calWireKeys, which only a real draw calls). The stub
// is the one method that listener needs, and no test exercises it.
globalThis.document = { addEventListener() {}, getElementById: () => null, querySelector: () => null, querySelectorAll: () => [] };
// calStateSave writes the rail's ticks; a Set-and-forget stub is all it needs.
globalThis.localStorage = { store: {}, getItem(k) { return this.store[k] ?? null; }, setItem(k, v) { this.store[k] = String(v); } };
load('composer.js');
load('views/calendar.js');
load('views/calgrid.js');

// Read before any test mutates calState: the page opens with every lane on.
// There is no "show closed" any more — a closed row always shows: a
// calendar always shows what happened in the past.
const DEFAULT_OFF = [...calState.off];

test('a YYYY-MM-DD is a LOCAL day, never UTC', () => {
  // new Date('2026-08-31') is midnight UTC — west of Greenwich that renders as
  // Aug 30, which would put every event one column to the left.
  assert.equal(localDate('2026-08-31').getDate(), 31);
  assert.equal(ymd(localDate('2026-08-31')), '2026-08-31');
  assert.equal(addDays('2026-08-31', 1), '2026-09-01');
  assert.equal(addDays('2026-03-09', -1), '2026-03-08');      // across the DST jump
  assert.equal(daysBetween('2026-03-07', '2026-03-09'), 2);   // DST can't round a day away
  assert.equal(calMin('19:30'), 1170);
  assert.equal(calMin(''), null);
  assert.equal(calHHMM(1170), '19:30');
  assert.equal(calHHMM(0), '00:00');
});

test('the page is one week, and one fetch covers it and the rail\'s mini-month', () => {
  // Aug 31 2026 is a Monday; Aug 1 2026 is a Saturday.
  calState.anchor = '2026-08-31';
  assert.deepEqual(calVisible(), ['2026-08-30', '2026-09-05']);
  assert.deepEqual(calMonthGrid('2026-08-31'), ['2026-07-26', '2026-09-05']);
  assert.deepEqual(calFetchRange(), ['2026-07-26', '2026-09-05']);
  // Day, Month, Schedule and Just mine are gone.
  for (const f of ['calMonthHTML', 'calScheduleHTML', 'calSetMode', 'calSetMineOnly', 'calJumpDay'])
    assert.equal(typeof globalThis[f], 'undefined', f);
});

// Which rows drag, which lane a row is in, whether it is closed and the word
// for its kind are the hub's stamps (calendar.go `stampEntry`, pinned by
// TestEntryIsStampedOnceForBothSurfaces). Fixtures below carry the stamps a
// real response would; these tests pin what the page DRAWS from them.
test('the drag and the why are the hub\'s stamps, read as they are', () => {
  assert.equal(calMovable({ kind: 'owner', move: 'item' }), 'item');
  assert.equal(calMovable({ kind: 'run', move: 'run' }), 'run');
  assert.equal(calMovable({ kind: 'job' }), '');
  assert.equal(calWhyStuck({ kind: 'job', why: 'A job\'s cadence lives in ops/schedule.json — no API moves it.' }), 'A job\'s cadence lives in ops/schedule.json — no API moves it.');
  // A row the hub did not stamp is grey and closed only if it says so.
  assert.equal(calLaneOf({ kind: 'owner' }), CAL_AGENTS);
  assert.equal(calClosed({ kind: 'owner', state: 'done' }), false);
  assert.equal(calClosed({ kind: 'owner', closed: true }), true);
});

test('dropping a check-in rewrites the cadence; an all-day drop is refused', () => {
  assert.equal(calNewCadence('daily@05:00', '2026-09-03', '07:15'), 'daily@07:15');
  // Sep 3 2026 is a Thursday: a weekly cadence takes the dropped column's day.
  assert.equal(calNewCadence('weekly@Mon 09:00', '2026-09-03', '18:00'), 'weekly@Thu 18:00');
  // The all-day band has no time to give, so the move is refused, not guessed.
  assert.equal(calNewCadence('daily@05:00', '2026-09-03', ''), '');
  assert.equal(calNewCadence('every@6h', '2026-09-03', '07:15'), '');
  assert.equal(calNewCadence('', '2026-09-03', '07:15'), '');
});

test('one lane\'s pile collapses to one box; only different lanes sit side by side', () => {
  // Six agent events at one minute collapse into one box, not six slivers; an
  // owner's step at the same minute sits beside that box.
  const ev = (id, at, kind) => ({ id, at, title: id, kind, lane: kind === 'owner' ? 'mine' : 'agents' });
  // An overlapping run in ONE lane — 10:00/10:15/10:30 chain through the
  // 30-minute block — is one group spanning first to last.
  const runs = calLayout([ev('r2', '10:15', 'run'), ev('r1', '10:00', 'run'), ev('r3', '10:30', 'run')]);
  assert.equal(runs.length, 1);
  assert.deepEqual(runs[0].members.map(m => m.id), ['r1', 'r2', 'r3']);
  assert.equal(runs[0].s, 10 * 60);
  assert.equal(runs[0].n, 10 * 60 + 30 + CAL_SLOT_MIN);
  assert.equal(runs[0].cols, 1);            // one box, full width
  // The owner's step at the same minute keeps its own box beside the agents' pile.
  const mixed = calLayout([ev('a1', '10:00', 'run'), ev('a2', '10:00', 'run'), ev('m1', '10:00', 'owner')]);
  assert.equal(mixed.length, 2);
  assert.deepEqual(mixed.map(p => p.cols), [2, 2]);
  assert.notEqual(mixed[0].col, mixed[1].col);
  // Two members already collapse; clear of each other they don't.
  assert.equal(calLayout([ev('r1', '19:30', 'run'), ev('r2', '19:45', 'run')]).length, 1);
  const clear = calLayout([ev('r1', '19:30', 'run'), ev('r2', '21:00', 'run')]);
  assert.equal(clear.length, 2);
  assert.equal(clear[1].cols, 1);
  assert.equal(clear[0].n, 19 * 60 + 30 + CAL_SLOT_MIN);
});

test('a box fills every column that is empty beside it', () => {
  // A quarter-wide 09:30 under a four-wide 08:00 row: the row should always
  // be full when something is on at that time.
  const ev = (id, at, kind, lane) => ({ id, at, title: id, kind, lane: lane || (kind === 'owner' ? 'mine' : 'agents') });
  // Three lanes at 10:00 make three columns; the owner's 10:20 step chains the
  // first column to 10:50, so the 10:35 run lands in the second and the third
  // is empty beside it: two of three columns, not one.
  const laid = calLayout([ev('a', '10:00', 'owner'), ev('b', '10:00', 'chore', 'chores'), ev('c', '10:00', 'homework', 'homework'),
    ev('d', '10:20', 'owner'), ev('e', '10:35', 'run')]);
  const by = Object.fromEntries(laid.map(p => [p.e.id, p]));
  assert.deepEqual(laid.map(p => p.cols), [3, 3, 3, 3]);
  assert.equal(by.a.span, 1);                       // b and c are beside it
  assert.equal(by.b.span, 1);
  assert.equal(by.c.span, 1);
  assert.equal(by.e.col, 1);
  assert.equal(by.e.span, 2);                       // the third column is empty at 10:35
  assert.match(calBlockHTML(by.e.e, by.e), /left:calc\(33\.33\d*% \+ 1px\);width:calc\(66\.66\d*% - 3px\)/);
  assert.match(calBlockHTML(by.b.e, by.b), /width:calc\(33\.33\d*% - 3px\)/);
  // A box never grows over a column that is taken for any part of its height.
  // (a 08:00/08:20/08:40 pile reaches 09:10; the owner's 08:45 step covers
  // only its bottom, and that is enough to keep it one column wide.)
  const tall = calLayout([ev('r1', '08:00', 'run'), ev('r2', '08:20', 'run'), ev('r3', '08:40', 'run'), ev('m', '08:45', 'owner')]);
  const pile = tall.find(p => p.members.length === 3), step = tall.find(p => p.e.id === 'm');
  assert.equal(pile.n, 9 * 60 + 10);
  assert.equal(pile.span, 1);
  assert.equal(step.col, 1);
  assert.equal(step.span, 1);
  // A brush of a minute is not a stack: a 09:01 step reaches 09:31, and the
  // 09:30 run beside it still takes the whole row.
  const brush = calLayout([ev('x', '09:00', 'run'), ev('y', '09:01', 'owner'), ev('z', '09:30', 'run')]);
  const z = brush.find(p => p.e.id === 'z');
  assert.equal(z.col, 0);
  assert.equal(z.span, 2);
  assert.equal(brush.find(p => p.e.id === 'x').span, 1);
});

test('a collapsed box says +N and opens the popout listing every member', () => {
  calState.off = new Set();
  const run = (id, at, state) => ({
    id, ref: 'thread:' + id, kind: 'run', state: state || 'scheduled',
    day: '2026-09-08', at, title: 'check ' + id, thread_id: id, lane: 'agents', move: 'run',
  });
  const es = [run('t1', '09:15'), run('t2', '09:15'), run('t3', '09:20')];
  const [p] = calLayout(es);
  const html = calBlockHTML(p.e, p);
  assert.match(html, /\+2/);
  assert.match(html, /data-id="grp:t1,t2,t3"/);
  assert.doesNotMatch(html, /data-move/);    // moving three things at once is a guess
  assert.match(html, /check t1/);
  // Closed only when EVERY member is; the tooltip lists them all.
  assert.doesNotMatch(html, /closed/);
  const [pc] = calLayout(es.map(e => ({ ...e, state: 'done', closed: true })));
  assert.match(calBlockHTML(pc.e, pc), /closed/);
  // The popout: one row per member, each a real .cal-ev that opens its card.
  calState.open = 'grp:t1,t2,t3';
  const panel = calPanelHTML({ days: [{ day: '2026-09-08', entries: es }] });
  assert.match(panel, /3 recurring runs in one box/);
  assert.match(panel, /data-id="t3"/);
  assert.match(panel, /check t2/);
  calState.open = '';
});

test('a block carries what the drop needs, and an immovable row says why', () => {
  const e = {
    id: 'cal-9f51', ref: 'cal:cal-9f51', kind: 'owner', state: 'scheduled',
    day: '2026-09-03', at: '19:30', title: 'Drink the juice', goal_id: 'make-more-money',
    lane: 'mine', item: true, move: 'item', mark: 'todo',
  };
  calState.off = new Set();
  const b = calBlockHTML(e, { e, s: 1170, col: 0, cols: 1 });
  assert.match(b, /data-move="item"/);
  assert.match(b, /data-day="2026-09-03"/);
  assert.match(b, /data-at="19:30"/);
  assert.match(b, /--c:#d93025/);           // the owner's own step: red, whatever its goal
  // A row that cannot move says so on hover instead of dragging and failing.
  const job = { id: 'job:x:1', ref: 'job:x', kind: 'job', state: 'scheduled', day: '2026-09-03', at: '09:00', title: 'statement parse',
    lane: 'agents', why: 'A job\'s cadence lives in ops/schedule.json — no API moves it.' };
  const c = calChipHTML(job);
  assert.doesNotMatch(c, /data-move/);
  assert.match(c, /ops\/schedule\.json/);
  // Closed chips read closed, the way the agenda row does — and the line is
  // won't-do's alone (✓ + strikethrough reads as its own
  // opposite): done keeps its ✓ with no `wont` class, dismissed gets both.
  const doneChip = calChipHTML({ ...job, state: 'done', closed: true, mark: 'done', title: 'x' });
  assert.match(doneChip, /✓/);
  assert.doesNotMatch(doneChip, /wont/);
  const wontChip = calChipHTML({ ...job, state: 'dismissed', closed: true, mark: 'wont', title: 'x' });
  assert.match(wontChip, /✕/);
  assert.match(wontChip, /closed wont/);
  // An open step of the owner's wears ○ — still to do, however long past its minute —
  // on the block and the chip both; the agents' rows never do.
  assert.match(b, /○ Drink the juice/);
  assert.doesNotMatch(c, /○/);
});

// Red for the owner's tasks, grey for agent actions, purple for
// recommendations — and no goal colouring. A fourth, Homework (light blue):
// a practice round from Learn, still the owner's to do — it fires, nags and
// closes like a task. A fifth, Scheduled runs (its own grey, its own
// checkbox): the prompts agents write to be sent at a minute.
// The answers the hub stamps (store/close.go TickOutcomes / StepOutcomes).
const tick = [{ value: 'done', label: 'Did it' }, { value: 'wont', label: 'Skip' }, { value: '', label: 'Send' }];
const step = [{ value: 'done', label: 'Done' }, { value: 'wont', label: "Won't do" }, { value: '', label: 'Reply' }];
test('five lanes, five colours, and nothing else', () => {
  assert.equal(CAL_SCHEDULED.color, '#78909c');
  assert.ok(!CAL_SCHEDULED.owner && !calIsOwner({ kind: 'agent' }));
  assert.match(calRailHTML({ days: [] }), /One-off runs/);
  assert.equal(CAL_HOMEWORK.color, '#039be5');
  assert.ok(CAL_MINE.owner && CAL_HOMEWORK.owner && !CAL_AGENTS.owner && !CAL_RECS.owner);
  assert.ok(calIsOwner({ kind: 'homework' }) && calIsOwner({ kind: 'owner' }) && !calIsOwner({ kind: 'agent' }));
  // It is a plan row (a bar), never a strip mark, and wears the owner's ○ / bold.
  assert.equal(calIsPlan({ kind: 'homework', did: true, state: 'done' }), true);
  assert.equal(calGlyph({ kind: 'homework', state: 'scheduled', lane: 'homework', mark: 'todo' }), '○ ');
  const hw = { id: 'cal-hw', ref: 'cal:cal-hw', kind: 'homework', state: 'scheduled', day: '2026-09-12', title: 'Perfect pitch: one round',
    lane: 'homework', item: true, mark: 'todo', move: 'item', kind_label: 'homework', outcomes: tick };
  assert.match(calEntryHTML(hw, {}), /<span class="tick">○<\/span><strong>Perfect pitch: one round<\/strong>/);
  // Homework gets the Recs box: Did it · Skip · Send in
  // its footer, Did it twice over an empty box closes it with no session,
  // words start one. No small "talk" button beside it any more.
  const hwCell = calEntryHTML(hw, {});
  assert.match(hwCell, /data-c="cal:cal-hw"/);
  assert.match(hwCell, /composerSend\('cal:cal-hw','done'\)[^>]*>Did it</);
  assert.match(hwCell, /composerSend\('cal:cal-hw','wont'\)[^>]*>Skip</);
  assert.match(hwCell, /composerSend\('cal:cal-hw',''\)[^>]*>Send</);
  assert.doesNotMatch(hwCell, />talk</);
  assert.doesNotMatch(hwCell, /resolveCal\('cal-hw','done'\)/);
  const task = { ...hw, id: 'cal-t', ref: 'cal:cal-t', kind: 'owner', lane: 'mine', kind_label: 'your step', outcomes: step };
  assert.match(calEntryHTML(task, {}), /class="pill"[^>]*>your step</);
  assert.doesNotMatch(calEntryHTML(task, {}), /resolveCal\('cal-t','done'\)/);
  assert.match(calEntryHTML(task, {}), /data-c="cal:cal-t"/);
  // A chore no session is behind (the plants, hub `tick`) gets the tick box.
  assert.match(calEntryHTML({ ...task, tick: true, due: 'on', outcomes: tick }, {}), /composerSend\('cal:cal-t','done'\)[^>]*>Did it</);
  assert.doesNotMatch(calEntryHTML(task, {}), />Did it</);
  assert.match(calRailHTML({ days: [] }), /Homework/);
  // An install ask is the chat's teal cell on the calendar too, its own kind
  // of cell: one Install
  // button carrying the OTA link, Won't install as the out, never Done; the
  // link line leaves the detail. Still the owner's lane (Just mine keeps it), teal
  // only in the shade — and so is the record it leaves once tapped.
  const inst = { id: 'ask-9', ref: 'ask:ask-9', ask_id: 'ask-9', kind: 'ask', ask_kind: 'install', state: 'open', day: '',
    title: 'Install app build 881 (tap the link)', detail: 'Teal on the calendar.\n[Install](https://hub.test/ota/tok/install.html)',
    lane: 'mine', kind_label: 'app update' };
  const cell = calEntryHTML(inst, {});
  assert.match(cell, /^<div class="card cal install">/);
  assert.match(cell, /<a class="btn sm install" href="https:\/\/hub\.test\/ota\/tok\/install\.html" target="_blank"[^>]*>Install<\/a>/);
  assert.match(cell, /resolveAskGlobal\('ask-9','dismissed'\)[^>]*>Won't install</);
  assert.doesNotMatch(cell, />Done<|>Dismiss<|install\.html\)/);
  assert.match(cell, /Teal on the calendar/);
  assert.match(cell, /class="pill"[^>]*>app update</);
  assert.equal(calLaneOf(inst).key, 'mine');
  assert.equal(calShade(inst), CAL_INSTALL);
  assert.match(calRailRow(inst), new RegExp('--c:' + CAL_INSTALL));
  assert.match(calEntryHTML({ ...inst, detail: 'No link here.' }, {}), /openRespond\('ask-9'\)[^>]*>Respond</);
  // The desktop app's card (2026-09-30): Install is a button on the mac lane.
  const macCell = calEntryHTML({ ...inst, title: 'Install desktop build 1512', detail: 'The Mac build.' }, {});
  assert.match(macCell, /<button class="sm install" onclick="macInstall\('ask-9'\)"[^>]*>Install<\/button>/);
  assert.match(macCell, /resolveAskGlobal\('ask-9','dismissed'\)[^>]*>Won't install</);
  assert.doesNotMatch(macCell, /href=|>Respond</);
  const did = { id: 'did:ask:ask-9', ref: 'ask:ask-9', ask_id: 'ask-9', kind: 'ask', ask_kind: 'install', state: 'done', did: true,
    actor: 'owner', verb: 'Installed', day: '2026-09-12', at: '09:15', title: 'build 881', lane: 'mine', closed: true, mark: 'done', kind_label: 'app update' };
  assert.equal(calShade(did), CAL_INSTALL);
  assert.equal(calLaneOf(did).key, 'mine');
  assert.match(calStripHTML([did]), new RegExp('mark mine"[^>]*--c:' + CAL_INSTALL));
  assert.match(calEntryHTML(did, {}), /✓<\/span>Installed: build 881/);
  assert.doesNotMatch(calEntryHTML(did, {}), /btn sm install/);
  // Any other ask keeps its red and its Done/Dismiss.
  assert.equal(calShade({ ...inst, ask_kind: 'physical' }), CAL_MINE.color);
  assert.match(calEntryHTML({ ...inst, ask_kind: 'physical' }, {}), /resolveAskGlobal\('ask-9','done'\)[^>]*>Done</);
  assert.deepEqual(CAL_LANES.map(l => l.key), ['mine', 'chores', 'homework', 'scheduled', 'agents', 'recs']);
  assert.deepEqual(CAL_LANES.map(l => l.color), ['#d93025', '#e8710a', '#039be5', '#78909c', '#5f6368', '#8e24aa']);
  assert.equal(calColor({ kind: 'owner', goal_id: 'make-more-money', lane: 'mine' }), '#d93025');
  // A chore (the hub's lane: a dated step no session is behind) is orange, the owner's.
  assert.equal(calColor({ kind: 'owner', lane: 'chores' }), '#e8710a');
  assert.ok(calLaneOf({ lane: 'chores' }).owner);
  calState.off = new Set(['chores']);
  assert.equal(calShown({ kind: 'owner', lane: 'chores' }), false);
  assert.equal(calShown({ kind: 'owner', lane: 'mine' }), true);
  calState.off = new Set();
});

// Ahead of now the calendar is the plan; behind it, the record of steps done —
// reads, approvals, recs accepted — by the owner or by agents, each side
// hideable on its own.
test('a did row is a record: its doer\'s lane, closed, and named for its deed', () => {
  // The hub stamps a record's lane (its doer's) and closed; the row carries
  // the why a drag reads.
  const did = (id, at, verb, actor, extra) => ({
    id: 'did:ask:' + id, ref: 'ask:' + id, kind: 'ask', state: 'done', day: '2026-09-08', at,
    title: 't ' + id, did: true, verb, actor, thread_id: 'th1', thread_title: 'Migrate the movie site',
    lane: ['owner', 'app'].includes(actor) ? 'mine' : 'agents', closed: true, mark: 'done',
    why: 'A record — it sits at the minute it happened, and nothing moves it.', ...extra,
  });
  // Always shown: the past is the record and no tick hides it. Only its
  // lane's tick hides it.
  calState.off = new Set();
  assert.equal(calShown(did('a', '09:00', 'Read', 'owner')), true);
  calState.off = new Set(['mine']);
  assert.equal(calShown(did('a', '09:00', 'Read', 'owner')), false);
  assert.equal(calShown({ ...did('d', '09:00', 'Ran', 'auto'), kind: 'action' }), true);
  calState.off = new Set();
  // ✓ for what happened, ✕ + the line for what was refused or fell over.
  assert.equal(calGlyph(did('a', '09:00', 'Read', 'owner')), '✓ ');
  assert.equal(calGlyph({ ...did('c', '09:00', 'Declined', 'owner'), kind: 'rec', state: 'declined', mark: 'wont' }), '✕ ');
  assert.equal(calWont({ ...did('d', '09:00', 'Denied', 'app'), kind: 'action', state: 'denied', mark: 'wont' }), ' wont');
  assert.equal(calWont({ ...did('d', '09:00', 'Failed', 'auto'), kind: 'action', state: 'failed', mark: 'wont' }), ' wont');
  assert.equal(calWont(did('a', '09:00', 'Read', 'owner')), '');
  // The deed is the label, on the chip, the block and the agenda row alike;
  // nothing drags a record.
  const chip = calChipHTML(did('a', '09:00', 'Read', 'owner'));
  assert.match(chip, /✓ Read: t a/);
  assert.doesNotMatch(chip, /data-move/);
  assert.match(chip, /A record/);
  assert.match(calEntryHTML(did('a', '09:00', 'Read', 'owner')), /Read: t a/);
  assert.match(calEntryHTML(did('a', '09:00', 'Read', 'owner')), /by owner · <a href="#\/sessions\/th1">Migrate the movie site<\/a>/);
});

test('a session\'s run of record collapses into one box named for the session', () => {
  // A record reads at the scale of the work: one session's run of steps is
  // one box.
  const did = (id, at, thread, actor) => ({
    id: 'did:ask:' + id, ref: 'ask:' + id, kind: 'ask', state: 'done', day: '2026-09-08', at,
    title: 't ' + id, did: true, verb: 'Read', actor: actor || 'owner', thread_id: thread,
    thread_title: thread === 'th1' ? 'Migrate the movie site' : thread ? 'Finance agent' : '',
    lane: actor ? 'agents' : 'mine', closed: true, mark: 'done',
  });
  calState.off = new Set();
  // Reads spread over an afternoon — 13:00, 14:30, 16:00 — chain at two
  // hours' reach where a plan row chains at thirty minutes…
  const run = calLayout([did('a', '13:00', 'th1'), did('b', '14:30', 'th1'), did('c', '16:00', 'th1')]);
  assert.equal(run.length, 1);
  assert.deepEqual(run[0].members.map(m => m.id), ['did:ask:a', 'did:ask:b', 'did:ask:c']);
  assert.equal(run[0].n, 16 * 60 + CAL_SLOT_MIN);   // the box ends at the last step, not two hours on
  // …but never across sessions, and never across lanes: the agent's side of
  // the same session is its own box beside the owner's.
  const two = calLayout([did('a', '13:00', 'th1'), did('b', '13:10', 'th2')]);
  assert.equal(two.length, 2);
  const lanes = calLayout([did('a', '13:00', 'th1'), did('b', '13:10', 'th1', 'auto')]);
  assert.equal(lanes.length, 2);
  assert.deepEqual(lanes.map(p => p.cols), [2, 2]);
  // The box wears the session's title and the count of steps; the popout is
  // "N steps in <session>", one row per deed.
  const html = calBlockHTML(run[0].e, run[0]);
  assert.match(html, /Migrate the movie site/);
  assert.match(html, /3 steps/);
  assert.match(html, /closed/);
  assert.match(html, /data-id="grp:did:ask:a,did:ask:b,did:ask:c"/);
  // The box's glyph is the session's fate: one skipped read among done ones
  // is still ✓, and only a run refused end to end wears ✕.
  const skip = { ...did('a', '13:00', 'th1'), state: 'dismissed', verb: 'Skipped', mark: 'wont' };
  const oneSkip = calLayout([skip, did('b', '14:30', 'th1')]);
  assert.match(calBlockHTML(oneSkip[0].e, oneSkip[0]), /class="t">✓ Migrate/);
  const allSkip = calLayout([skip, { ...did('b', '14:30', 'th1'), state: 'dismissed', verb: 'Skipped', mark: 'wont' }]);
  assert.match(calBlockHTML(allSkip[0].e, allSkip[0]), /class="t">✕ Migrate/);
  calState.open = 'grp:did:ask:a,did:ask:b,did:ask:c';
  const panel = calPanelHTML({ days: [{ day: '2026-09-08', entries: [did('a', '13:00', 'th1'), did('b', '14:30', 'th1'), did('c', '16:00', 'th1')] }] });
  assert.match(panel, /3 steps in Migrate the movie site/);
  assert.match(panel, /Read: t b/);
  calState.open = '';
  // A pile with no one session behind it keeps the old label: first member, +N.
  const mixed = calLayout([did('a', '13:00', 'th1'), did('b', '13:10', '')]);
  assert.equal(mixed.length, 2);
  const plain = calLayout([did('a', '13:00', ''), did('b', '13:10', '')]);
  assert.equal(plain.length, 1);
  assert.match(calBlockHTML(plain[0].e, plain[0]), /\+1/);
});

// With the record on the grid, done steps and planned items looked alike.
// They need to be visually distinct, and less cluttered.
test('three forms, three places: bars are the plan, the record is a strip, a rec is a bar at its minute', () => {
  calState.off = new Set(); calState.anchor = '2026-09-08';
  const plan = { id: 'cal:1', ref: 'cal:1', kind: 'owner', state: 'scheduled', day: '2026-09-08', at: '13:00', title: 'Book a dentist', lane: 'mine' };
  const done = { ...plan, id: 'cal:2', ref: 'cal:2', state: 'done', did: true, verb: 'Did', actor: 'owner', at: '09:00', title: 'Water the plants', closed: true };
  const read = { id: 'did:ask:a', ref: 'ask:a', kind: 'ask', state: 'done', day: '2026-09-08', at: '00:20',
    title: 'The bank feed is $15.99/yr', did: true, verb: 'Read', actor: 'owner', thread_id: 'th1', thread_title: 'Statements', lane: 'mine', closed: true };
  const ran = { ...read, id: 'did:ask:b', at: '00:40', actor: 'auto', verb: 'Ran', lane: 'agents' };
  const filed = { id: 'filed:rec:r1', ref: 'rec:r1', kind: 'rec', state: 'proposed', day: '2026-09-08', at: '02:40', title: 'Try Kagi', verb: 'Filed', lane: 'recs' };
  const html = calTimeGridHTML({ days: [{ day: '2026-09-08', entries: [plan, done, read, ran, filed] }] }, '2026-09-08', '2026-09-08');
  // The plan — open AND closed — is what a bar means. A closed item keeps its
  // planned slot: its time is the time chosen, not the minute it was ticked.
  assert.match(html, /class="cal-ev block[^"]*"[^>]*data-id="cal:1"/);
  assert.match(html, /class="cal-ev block[^"]*closed[^"]*"[^>]*data-id="cal:2"/);
  // The record is not a bar at all: two marks in the strip, the owner's and the
  // agent's in their own halves of it, at the minute each happened.
  assert.doesNotMatch(html, /block[^>]*data-id="did:ask:/);
  assert.match(html, /class="cal-ev mark mine" data-id="did:ask:a"\n\s*style="--c:#d93025;top:14\.66[0-9]*px/);  // 00:20 of a 44px hour
  assert.match(html, /class="cal-ev mark agents"[^>]*data-id="did:ask:b"/);
  assert.match(html, /class="cal-plan inset"/);
  // A rec is a BAR in the clock at the minute it was filed,
  // never a strip mark and never a band above the clock.
  assert.match(html, /class="cal-ev block rec"[^>]*data-id="filed:rec:r1"[^>]*\n\s*style="--c:#8e24aa;top:117\.33[0-9]*px/);  // 02:40 of a 44px hour
  assert.doesNotMatch(html, /mark[^>]*data-id="filed:rec:r1"/);
  assert.doesNotMatch(html, /cal-cols sug/);
  // A rec with no minute — "Check back" on its review day — is an all-day chip.
  const back = { id: 'rec:r2', ref: 'rec:r2', kind: 'rec', state: 'deferred', day: '2026-09-08', title: 'Try Arc', verb: 'Check back', lane: 'recs' };
  const withBack = calTimeGridHTML({ days: [{ day: '2026-09-08', entries: [back] }] }, '2026-09-08', '2026-09-08');
  assert.match(withBack, /class="cal-allday"[^>]*>\s*<div class="cal-ev chip rec"[^>]*data-id="rec:r2"/);
  // With no record in range the strip's width is not reserved at all.
  const bare = calTimeGridHTML({ days: [{ day: '2026-09-08', entries: [plan] }] }, '2026-09-08', '2026-09-08');
  assert.doesNotMatch(bare, /cal-strip/);
});

// A rec is on the calendar twice: dark purple when filed, light purple when
// accepted or declined, and struck through when declined.
test('a rec wears its life: dark filed, light decided, struck declined — two bars, never one box', () => {
  calState.off = new Set();
  const rec = (id, extra) => ({ id, ref: 'rec:r', kind: 'rec', day: '2026-09-08', at: '02:40', title: 'Try Kagi', lane: 'recs', ...extra });
  const filed = rec('filed:rec:r', { state: 'proposed', verb: 'Filed' });
  const ok = rec('did:rec:r', { state: 'accepted', did: true, verb: 'Accepted', actor: 'owner', closed: true, mark: 'done' });
  const no = rec('did:rec:r', { state: 'declined', did: true, verb: 'Declined', actor: 'owner', closed: true, mark: 'wont' });
  const one = e => ({ e, s: calMin(e.at), n: calMin(e.at) + CAL_SLOT_MIN, col: 0, cols: 1, members: [e] });
  assert.match(calBlockHTML(filed, one(filed)), new RegExp('class="cal-ev block rec"[^>]*--c:' + CAL_RECS.color));
  assert.match(calBlockHTML(ok, one(ok)), new RegExp('block closed rec decided"[^>]*--c:' + CAL_REC_DONE));
  assert.match(calBlockHTML(ok, one(ok)), /✓ Accepted: Try Kagi/);
  assert.match(calBlockHTML(no, one(no)), /block closed rec decided wont/);        // .wont draws the line
  assert.match(calBlockHTML(no, one(no)), /✕ Declined: Try Kagi/);
  // The month cell's chip says the same in the same shade.
  assert.match(calChipHTML(ok), new RegExp('chip closed rec decided"[^>]*--c:' + CAL_REC_DONE));
  // The FILED bar carries the rec's current status, so an answered rec never
  // looks open on the calendar: still dark — it is
  // the minute it was filed — but faded with ✓ once accepted, ✕ + struck once
  // declined or expired. Only an unanswered rec's filed bar is full colour.
  const filedYes = rec('filed:rec:r', { state: 'accepted', verb: 'Filed', closed: true, mark: 'done' });
  const filedNo = rec('filed:rec:r', { state: 'declined', verb: 'Filed', closed: true, mark: 'wont' });
  const filedOld = rec('filed:rec:r', { state: 'expired', verb: 'Filed', closed: true, mark: 'wont' });
  assert.match(calBlockHTML(filedYes, one(filedYes)), new RegExp('class="cal-ev block closed rec"[^>]*--c:' + CAL_RECS.color));
  assert.match(calBlockHTML(filedYes, one(filedYes)), /✓ Filed: Try Kagi/);
  assert.match(calBlockHTML(filedNo, one(filedNo)), new RegExp('class="cal-ev block closed rec wont"[^>]*--c:' + CAL_RECS.color));
  assert.match(calBlockHTML(filedNo, one(filedNo)), /✕ Filed: Try Kagi/);
  assert.match(calBlockHTML(filedOld, one(filedOld)), /block closed rec wont/);
  assert.doesNotMatch(calBlockHTML(filed, one(filed)), /closed/);
  // The agenda row (Schedule mode) says the same: ✓ for accepted, ✕ + <s> for declined.
  assert.match(calEntryHTML(filedYes), /<span class="tick">✓<\/span>Filed: Try Kagi/);
  assert.match(calEntryHTML(filedNo), /<span class="tick">✕<\/span><s>Filed: Try Kagi<\/s>/);
  // Filed at 02:40 and answered at 02:50 are two bars side by side, not one
  // "+1" box: the two shades are two different things and never chain.
  const laid = calLayout([filed, { ...ok, at: '02:50' }]);
  assert.equal(laid.length, 2);
  assert.ok(laid.every(p => p.cols === 2 && p.members.length === 1));
  // A rec is a moment: seven answers over an hour and a half are NOT one box
  // from 00:25 to 02:00 (the record's two-hour chain) — only the ones inside
  // one slot pile, and the pile stays one slot tall. 00:25/00:26/00:40 share the 00:25 slot, 01:10/01:15
  // the 01:10 one, 01:50/02:00 the 01:50 one: three piles, each 30 min.
  const answers = ['00:25', '00:26', '00:40', '01:10', '01:15', '01:50', '02:00']
    .map((at, i) => ({ ...ok, id: 'did:rec:r' + i, at }));
  const piles = calLayout(answers);
  assert.deepEqual(piles.map(p => [p.s, p.n, p.members.length]), [
    [25, 55, 3], [70, 100, 2], [110, 140, 2],
  ]);
  // Same for the dark side: filed at 00:40 and 02:40 are two bars, not a run.
  const filedTwice = calLayout([filed, { ...filed, id: 'filed:rec:r9', at: '00:40' }]);
  assert.equal(filedTwice.length, 2);
  assert.ok(filedTwice.every(p => p.n - p.s === CAL_SLOT_MIN));
  // Both ends are on the calendar on their own days, and both hide with the
  // recs tick — never with the owner's own lane.
  assert.equal(calLaneOf(ok).key, 'recs');
  calState.off = new Set(['recs']);
  assert.equal(calShown(ok), false);
  assert.equal(calShown(filed), false);
  calState.off = new Set();
  // A decided rec is a record — and a record always shows.
  assert.equal(calShown(ok), true);
  assert.equal(calShown(filed), true);
});

test('the agenda row wears the same colour as its chip on the grid', () => {
  // Schedule is a mode of the same page, so a rec cannot be purple in the grid
  // and blue in the list — which is what a `.pill.purple` class would draw,
  // that class being the accent blue.
  const kindPill = e => calEntryHTML(e, {}).match(/<span class="pill" style="([^"]*)"/)[1];
  for (const e of [{ kind: 'owner', lane: 'mine' }, { kind: 'rec', lane: 'recs' }, { kind: 'agent', lane: 'scheduled' }, { kind: 'action', state: 'proposed', lane: 'mine' }]) {
    assert.match(kindPill({ id: 'x', title: 't', day: '2026-08-31', state: '', ...e }), new RegExp(calColor(e) + '$'));
  }
});

test('the rail hides by lane only — closed is a look, never a filter', () => {
  const e = { id: 'a', kind: 'agent', state: 'scheduled', goal_id: 'g1', lane: 'scheduled' };
  assert.deepEqual(DEFAULT_OFF, []);        // every lane on, first visit
  calState.off = new Set();
  assert.equal(calShown(e), true);
  assert.equal(calShown({ ...e, state: 'done', closed: true }), true);
  assert.equal(calShown({ ...e, state: 'dismissed', closed: true }), true);
  // An agent's cal item is a SCHEDULED run: its own tick hides it, the Agent
  // runs tick does not.
  const checkIn = { ...e, kind: 'run', ref: 'thread:t1', lane: 'agents' };
  calState.off = new Set(['scheduled']);
  assert.equal(calShown(e), false);
  assert.equal(calShown(checkIn), true);
  assert.equal(calShown({ ...e, kind: 'owner', lane: 'mine' }), true);   // the red lane is untouched
  assert.equal(calShown({ ...e, kind: 'homework', lane: 'homework' }), true);   // and the blue one
  calState.off = new Set(['agents']);
  assert.equal(calShown(e), true);
  assert.equal(calShown(checkIn), false);
  calState.off = new Set(['homework']);
  assert.equal(calShown({ ...e, kind: 'homework', lane: 'homework' }), false);
  assert.equal(calShown({ ...e, kind: 'owner', lane: 'mine' }), true);
  // The "Decided actions" calendar is gone: a decided action is just closed,
  // ✓ and muted on its day — and the "Done & decided" tick that could hide
  // it went the same way. A stale saved value is ignored.
  const act = st => ({ id: 'a', ref: 'action:a', kind: 'action', state: st, lane: st === 'proposed' ? 'mine' : 'agents', closed: st !== 'proposed' });
  calState.off = new Set();
  assert.equal(calShown(act('approved')), true);
  assert.equal(calShown(act('proposed')), true);
  localStorage.setItem('calState', JSON.stringify({ mode: 'week', off: [], showClosed: false }));
  calStateLoad();
  assert.equal(calState.showClosed, undefined);
  assert.equal(calShown(act('approved')), true);
  assert.doesNotMatch(calRailHTML({ days: [] }), /Done &amp; decided/);
  assert.equal(typeof globalThis.calToggleClosed, 'undefined');
});

test('the detail panel is the agenda row itself, not a second card', () => {
  const e = {
    id: 'cal-1a2b3c4d', ref: 'cal:cal-1a2b3c4d', kind: 'owner', state: 'scheduled',
    day: '2026-09-03', at: '19:30', title: 'Buy tranche 2', lane: 'mine', item: true, move: 'item',
  };
  calState.off = new Set();
  calState.open = 'cal-1a2b3c4d';
  const html = calPanelHTML({ days: [{ day: '2026-09-03', entries: [e] }] });
  // Acting on a calendar item needs its full text and its reply box, so the
  // panel carries calEntryHTML's composer verbatim.
  assert.match(html, /<div class="card cal">/);
  assert.match(html, /composerSend\('cal:cal-1a2b3c4d'\)/);
  assert.match(html, /· 19:30/);
  assert.match(html, /My tasks/);
  assert.doesNotMatch(html, /Drag it on the grid to move it/);
  // The panel finds a row wherever it lives in the response.
  assert.equal(calFind({ anytime: [e] }, 'cal-1a2b3c4d'), e);
  assert.equal(calFind({ overdue: [e] }, 'cal-1a2b3c4d'), e);
  assert.equal(calFind({ days: [] }, 'cal-1a2b3c4d'), null);
  calState.open = '';
  assert.equal(calPanelHTML({ days: [] }), '');
});

test('a browser that remembers the six calendars opens with all five lanes on', () => {
  // The old rail stored keys like 'log' / 'me' / 'checkins'. None of them is a
  // lane, so they are dropped rather than leaving a lane stuck invisible.
  const saved = { off: [...calState.off] };
  localStorage.setItem('calState', JSON.stringify({ mode: 'week', colorBy: 'goal', off: ['log', 'checkins'], goalsOff: ['g1'], showClosed: true }));
  calState.off = new Set(['agents']);
  calStateLoad();
  assert.deepEqual([...calState.off], []);
  assert.equal(calState.goalsOff, undefined);   // the goal filter is gone entirely
  assert.equal(calState.colorBy, undefined);
  // And a lane you really did untick survives the round trip.
  calState.off = new Set(['recs']); calStateSave(); calState.off = new Set();
  calStateLoad();
  assert.deepEqual([...calState.off], ['recs']);
  localStorage.setItem('calState', JSON.stringify(saved));
  calState.off = new Set(saved.off);
});

test('the rail trays: Overdue rows wear their day, Do soon its added day, Anytime none', () => {
  const past = addDays(todayYMD(), -3);
  const row = (id, day, extra) => ({ id, ref: 'cal:' + id, kind: 'owner', state: 'scheduled', day, title: id, lane: 'mine', item: true, ...extra });
  const cal = { overdue: [], anytime: [], days: [] };
  calState.off = new Set();
  // A row in the Overdue pile sits away from its day, so it says the day and
  // minute — "Tue 16 20:00" (headed "For you" until 2026-09-17).
  const late = row('late', past, { overdue: true, at: '20:00' });
  const when = calWhenShort(late);
  assert.match(when, /^[A-Z][a-z]{2} \d{1,2} 20:00$/);
  const rail = calRailHTML({ ...cal, overdue: [late], anytime: [row('whenever', '')] });
  assert.match(rail, new RegExp('Overdue <span class="cal-n">1</span>[\\s\\S]*<span class="at">' + when + '</span><span class="t">late</span>'));
  assert.doesNotMatch(rail, /For you/);
  // An Anytime row has no day to wear.
  assert.match(rail, /<span class="dot"><\/span><span class="t">whenever<\/span>/);
  // On its own day the row keeps the bare minute.
  assert.match(calEntryHTML(late), /overdue<\/span> · 20:00 · <span class="pill"/);
  // A do-soon to-do (2026-09-21): "added Sep 20" where a day would be.
  const todo = row('quartet', todayYMD(), { soon: true, at: '16:21', kind_label: 'do soon' });
  assert.match(calWhenShort(todo), /^added [A-Z][a-z]{2} \d{1,2}$/);
  assert.match(calRailHTML({ ...cal, soon: [todo] }), /Do soon <span class="cal-n">1<\/span>/);
  assert.equal(calFind({ soon: [todo] }, 'quartet'), todo);
});
