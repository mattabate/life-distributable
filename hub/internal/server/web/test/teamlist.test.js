// views/threads.js, the Sessions page's team list: one list with no
// headings under a "Your team" band, rows in status order, one pill each.
// The page's fetches and board are stubbed; the fixtures hold no personal
// data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const els = {};
const el = id => els[id] || (els[id] = { id, hidden: false, innerHTML: '', textContent: '', dataset: {}, querySelector: () => null, classList: { add() {}, remove() {}, toggle() {} } });
globalThis.document = { getElementById: el, querySelectorAll: () => [], addEventListener() {} };
globalThis.window = globalThis;
globalThis.location = { hash: '' };
globalThis.setInterval = () => 0;
globalThis.setTimeout = () => 0;
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'ui.js'), 'utf8'), { filename: 'ui.js' });
globalThis.route = { gen: 0 };
globalThis.onLeave = globalThis.onLeave || [];
globalThis.composerCards = () => [];
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'threads.js'), 'utf8'), { filename: 'threads.js' });

const p = (id, title, role, extra) => ({ id, title, role, persona: id, status: 'idle', ...extra });
const team = [
  p('idle1', 'Quinn', 'Archivist'),
  p('todo1', 'Ada', 'Builder', { todos: [{ id: 1, text: 'Wire the export', state: 'open' }] }),
  p('recs1', 'Bo', 'Scout'),
  p('work1', 'Cy', 'Analyst', { status: 'running' }),
  p('turn1', 'Dee', 'Planner', { status: 'running' }),
  p('read1', 'Eve', 'Editor'),
  p('wait1', 'Fay', 'Writer'),
  p('spk1', 'Gus', 'Chief of staff'),
  p('ins1', 'Ivy', 'Fixer', { status: 'running' }),
];
const board = {
  count: 2, for_you: { turn1: 1, spk1: 1 }, first: {}, home: {}, open: {}, todo: {},
  section: { turn1: 'working', work1: 'working', ins1: 'working' },
  pills: { spk1: [{ tone: 'speaking', word: 'speaking' }], wait1: [{ tone: 'waiting', word: 'waiting to speak' }] },
  sessions: [
    { id: 'turn1', asks: [{ id: 'ask-1', title: 'Pick a date', kind: 'decision' }] },
    { id: 'read1', asks: [{ id: 'ask-2', title: 'The draft is up', kind: 'read' }] },
    { id: 'spk1', asks: [{ id: 'ask-3', title: 'Pick a venue', kind: 'decision', said: 'Hey, one card.', speaking: true }] },
    { id: 'ins1', asks: [{ id: 'ask-4', title: 'Install the build', kind: 'install' }] },
  ],
};
const recs = [{ id: 'rec-1', title: 'Try a <b>timer</b>', thread_id: 'recs1', closed: false }, { id: 'rec-2', title: 'Old', thread_id: 'todo1', closed: true }];
// What the stubs serve: the people, the live sessions, the board, the recs.
const first = { team, live: team.filter(t => t.status === 'running'), board, recs };
let cur = first;
globalThis.board = null;
globalThis.EMPTY_BOARD = board;
globalThis.loadBoard = async () => cur.board;
globalThis.setBadge = () => {};
globalThis.get = async u => {
  if (u.startsWith('/threads?live=1')) return cur.live;
  if (u === '/threads?persona=1') return cur.team;
  if (u === '/recs') return { recs: cur.recs };
  throw new Error('unexpected ' + u);
};

test('one list, no headings: rows in status order, one pill each', async () => {
  await drawList('', false);
  const html = el('list').innerHTML;
  assert.match(html, /<div class="side-top">Your team<\/div>/);
  assert.doesNotMatch(html, /side-h\b/);
  const order = [...html.matchAll(/class="side-row st-(\w+)[^]*?class="side-t[^"]*" href="[^"]*">(\w+)</g)].map(m => [m[2], m[1]]);
  // An install card is the top stack, and Working never covers it. Speaking
  // is a pill only: Gus keeps his Your turn place and edge. Dee is mid-turn
  // with an open card: she wears Working and sorts with the working rows,
  // after the card to read, the hub's order among them. Recs sit above the
  // idle to-do list.
  assert.deepEqual(order, [['Ivy', 'install'], ['Gus', 'turn'], ['Eve', 'read'], ['Cy', 'work'], ['Dee', 'work'], ['Bo', 'recs'], ['Ada', 'todo']]);
  const row = name => html.split('class="side-row').find(s => s.includes(`">${name}</a>`));
  assert.equal(row('Dee').match(/class="pill /g).length, 1);
  assert.match(row('Dee'), /<span class="pill running">Working<\/span>/);
  assert.match(row('Ivy'), /<span class="pill install">Install<\/span>/);
  assert.doesNotMatch(row('Ivy'), /Working/);
  assert.match(row('Gus'), /class="side-audio">[^]*speaking/);
  assert.doesNotMatch(row('Gus'), /Your turn<\/span>/);
  // Waiting with nothing else is not a row: Fay is an idle pair, her pill on it.
  assert.ok(!row('Fay'));
  assert.match(html, /<a class="idle-cell" href="#\/sessions\/wait1"><span class="side-name"><strong>Fay<\/strong><span class="side-audio">[^]*Waiting/);
  assert.doesNotMatch(html, /waiting to speak/);
  assert.match(row('Ada'), /<span class="pill idle">Idle<\/span>[^]*Wire the export/);
  // An open rec is a violet line under its person; a closed one is not.
  assert.match(row('Bo'), /class="side-item rec"[^]*Try a/);
  // Its title is text, never markup.
  assert.match(row('Bo'), /Try a &lt;b&gt;timer&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<b>timer/);
  assert.doesNotMatch(html, />Old</);
  // The role has its own column; the plain idle are pairs at the foot.
  assert.match(row('Gus'), /class="side-g3">[^]*<span class="side-role">Chief of staff<\/span><span class="side-st">/);
  assert.match(html, /<div class="idle-grid"><a class="idle-cell" href="#\/sessions\/idle1"><span class="side-name"><strong>Quinn<\/strong>/);
});

test('a sub-session counts on its person; old recs by source; each person once', async () => {
  const sub = { id: 'sub1', title: 'Survey the papers', status: 'running', parent_id: 'hal1' };
  cur = {
    // A second session of the same role is not a second row.
    team: [p('hal1', 'Hal', 'Researcher'), p('hal2', 'Hal', 'Researcher', { persona: 'hal1' }), p('idle1', 'Quinn', 'Archivist')],
    live: [sub],
    board: {
      count: 1, for_you: { sub1: 1 }, first: {}, open: {}, todo: {},
      home: { sub1: 'hal1' },
      // The hub's rollup: the running sub-session files its person under Working.
      section: { sub1: 'working', hal1: 'working' },
      pills: {},
      sessions: [{ id: 'sub1', asks: [{ id: 'ask-9', title: 'Pick a venue', kind: 'decision' }] }],
    },
    // An older rec names its session only in `source`.
    recs: [{ id: 'rec-9', title: 'Read the survey', thread_id: '', source: 'claude:thread:sub1', closed: false }],
  };
  try {
    await drawList('', false);
  } finally {
    cur = first;
  }
  const html = el('list').innerHTML;
  const rows = [...html.matchAll(/class="side-row st-(\w+)[^]*?class="side-t[^"]*" href="[^"]*">(\w+)</g)].map(m => [m[2], m[1]]);
  // The sub-session's decision makes the row Your turn, not the rollup's Working.
  assert.deepEqual(rows, [['Hal', 'turn']]);
  assert.match(html, /<span class="pill needs">Your turn<\/span>/);
  assert.match(html, /class="side-item rec"[^]*Read the survey/);
  // Hal once: no idle pair for the role's second session.
  assert.equal((html.match(/>Hal</g) || []).length, 1);
  assert.match(html, /<strong>Quinn<\/strong>/);
});

test("a person's row is the session with the highest own status, sorted by its pill", async () => {
  cur = {
    // Kim's running session holds a decision; an older one holds a card to read.
    team: [p('kim1', 'Kim', 'Pilot', { status: 'running' }), p('kim2', 'Kim', 'Pilot', { persona: 'kim1' }), p('read1', 'Eve', 'Editor')],
    live: [p('kim1', 'Kim', 'Pilot', { status: 'running' })],
    board: {
      count: 1, for_you: { kim1: 1 }, first: {}, home: {}, open: {}, todo: {},
      section: { kim1: 'working' }, pills: {},
      sessions: [
        { id: 'kim1', asks: [{ id: 'ask-5', title: 'Pick a route', kind: 'decision' }] },
        { id: 'kim2', asks: [{ id: 'ask-6', title: 'The notes are up', kind: 'read' }] },
        { id: 'read1', asks: [{ id: 'ask-7', title: 'The draft is up', kind: 'read' }] },
      ],
    },
    recs: [],
  };
  try {
    await drawList('', false);
  } finally {
    cur = first;
  }
  const html = el('list').innerHTML;
  const rows = [...html.matchAll(/class="side-row st-(\w+)[^]*?class="side-t[^"]*" href="([^"]*)">(\w+)</g)].map(m => [m[3], m[1], m[2]]);
  // The running session with the decision is Kim's row, not the idle one
  // with the card to read; wearing Working, it sorts after Eve's card.
  assert.deepEqual(rows, [['Eve', 'read', '#/sessions/read1'], ['Kim', 'work', '#/sessions/kim1']]);
});

test('the chat head shows no speaking pill; running stays', () => {
  const b = { pills: { s1: [{ tone: 'speaking', word: 'speaking' }, { tone: 'running', word: 'running' }] } };
  const h = headPills(b, { id: 's1', status: 'running' });
  assert.doesNotMatch(h, /speaking/);
  assert.match(h, /class="pill running/);
  assert.doesNotMatch(headPills({ pills: { s1: [{ tone: 'speaking', word: 'speaking' }] } }, { id: 's1', status: 'idle' }), /speaking/);
});
