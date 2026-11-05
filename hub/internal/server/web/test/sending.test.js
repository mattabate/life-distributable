// views/threads.js, the two counters under a chat (2026-10-14
// 23:12): "sending · N s" from the moment the owner's message leaves the box
// until it is in the chat, then the run block's "starting · N s" until its
// first step. threads.js needs a document and a few globals the page has;
// just enough is stubbed here to run the two paint functions. The fixtures
// hold no personal data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const els = {};
const el = id => els[id] || (els[id] = {
  id, hidden: false, innerHTML: '', textContent: '', dataset: {},
  querySelector: () => (els[id].innerHTML.includes('class="since"') ? {} : null),
  classList: { add() {}, remove() {}, toggle() {} },
});
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

const ago = s => new Date(Date.now() - s * 1000).toISOString();

test('a turn with no tool call yet counts "starting · N s" from its message', () => {
  const w = el('run-working');
  chatState.liveSince = Date.parse(ago(7));
  paintWorking(true, { status: 'running', turn_tools: 0 });
  assert.match(w.innerHTML, /starting · <span class="since" data-since="\d+">7 s<\/span>/);
  // Its message was queued 9 minutes before the turn opened (the previous
  // turn was still working): the clock counts from the hub's turn start.
  chatState.liveSince = Date.parse(ago(548));
  paintWorking(true, { status: 'running', turn_tools: 0, turn_at: ago(84) });
  assert.match(w.innerHTML, /starting · <span class="since" data-since="\d+">84 s<\/span>/);
  chatState.liveSince = Date.parse(ago(7));
  // A tool call landed: the count, never the clock.
  paintWorking(true, { status: 'running', turn_tools: 3 });
  assert.match(w.innerHTML, /working · <a href="#" class="chain-link" onclick="event.preventDefault\(\);toggleChain\(\)">3 tool calls<\/a>/);
  assert.doesNotMatch(w.innerHTML, /starting|since/);
  // A finished turn never counts.
  paintWorking(false, { status: 'idle' });
  assert.equal(w.hidden, true);
  chatState.liveSince = 0;
});

test('the working line reads "sending · N s" while the message is on its way', () => {
  chatState.id = 't1'; chatState.thread = { status: 'idle' }; chatState.running = false;
  const w = el('run-working');
  paintWorking(false, chatState.thread);
  assert.equal(w.hidden, true, 'idle and nothing sending: no line');
  // A Send from this box, three seconds ago, into a session that is not running.
  chatState.sendingSince = Date.now() - 3000; chatState.sendingAfter = 5;
  paintWorking(false, chatState.thread);
  assert.equal(w.hidden, false);
  assert.match(w.innerHTML, /<span class="dot pulse"><\/span> sending · <span class="since" data-since="\d+">3 s<\/span>/);
  chatState.sendingSince = 0;
  // Nothing on its way: the working line as before.
  paintWorking(true, { status: 'running', turn_tools: 2 });
  assert.match(w.innerHTML, /working · <a [^>]*>2 tool calls<\/a>/);
  assert.doesNotMatch(w.innerHTML, /sending/);
});

// Mock B3 (2026-10-25): every message is a row; the owner's grey, a reply
// with no text nothing at all, its dollars on the first card the turn raised.
test('msgHTML: the owner\'s message is a grey row, a reply is its cards', () => {
  const ts = new Date().toISOString();
  const mine = msgHTML({ id: 7, role: OWNER, author: OWNER, ts, text: 'trash the bad ones\nand unsubscribe', replies: [{ ref: 'ask:a1', label: 'Did it' }] }, [], [], [], {}, {});
  assert.match(mine, /^<details class="chat-row me" id="msg-7" data-k="msg:7" data-def="0">/);
  assert.match(mine, /<span class="k">You<\/span><span class="t">↩ Did it · trash the bad ones<\/span>/);
  assert.doesNotMatch(mine, /sent by/);
  chatState.costs = {};
  const card = { id: 'ask-1', kind: 'read', state: 'open', title: 'How the email pass went', created_at: ts };
  const reply = msgHTML({ id: 8, role: 'claude', ts, text: '', cost_usd: 0.6 }, [card], [], [], {}, {});
  assert.doesNotMatch(reply, /msg-8/, 'an empty reply draws no row');
  assert.match(reply, /\$0\.60/, 'its dollars ride on the card');
  assert.equal(msgHTML({ id: 9, role: 'claude', ts, text: '', cost_usd: 0.2 }, [], [], [], {}, {}), '');
  assert.match(msgHTML({ id: 10, role: 'claude', kind: 'schedule', ts, text: 'Back at 9:00 AM\n\nCheck the inbox' }, [], [], [], {}, {}),
    /class="chat-row sched".*<span class="k">⏰ Check-in<\/span><span class="t">Back at 9:00 AM<\/span>/s);
});

test('the empty chat draws no sending line with nothing on its way', () => {
  const n = el('new-sending');
  paintNewSending();
  assert.equal(n.hidden, true);
});

test('a turn that died unanswered is never in flight again', () => {
  // 2026-10-30 13:06: a relay and a plan-limit note from Tuesday came
  // back as soon as the session next worked.
  const ids = msgs => liveStarterMsgs(msgs).map(m => m.id);
  const dead = [
    { id: 1, ts: '2026-10-27T22:10:00Z', role: 'system', run_id: 'dead' },
    { id: 2, ts: '2026-10-27T22:10:05Z', role: 'system', run_id: 'dead' },
    { id: 3, ts: '2026-10-28T09:00:00Z', role: OWNER, run_id: 'r1' },
    { id: 4, ts: '2026-10-28T09:01:00Z', role: 'claude', run_id: 'r1' },
  ];
  assert.deepEqual(ids(dead), []);
  // A new message opens a new turn: only that one is live.
  const asked = [...dead, { id: 5, ts: '2026-10-30T13:06:03Z', role: OWNER, run_id: 'r2' }];
  assert.deepEqual(ids(asked), [5]);
  // A message queued during a turn keeps its place when that turn's reply lands
  // after it: the queued run started later than the answered one.
  const queued = [
    { id: 6, ts: '2026-10-30T13:00:00Z', role: OWNER, run_id: 'r3' },
    { id: 7, ts: '2026-10-30T13:00:10Z', role: OWNER, run_id: 'r4' },
    { id: 8, ts: '2026-10-30T13:00:20Z', role: 'claude', run_id: 'r3' },
  ];
  assert.deepEqual(ids(queued), [7]);
  // No answered turn at all: every starter still counts.
  assert.deepEqual(ids([{ id: 9, ts: '2026-10-30T13:00:00Z', role: OWNER, run_id: 'r5' }]), [9]);
});

test('open cards only: the chat draws what is the owner\'s to act on, the full chat is a toggle', () => {
  // 2026-10-26 22:48: "I only want to see the open messages for me…
  // these messages should only be available by toggle on".
  const asks = [{ id: 'ask-1', closed: false }, { id: 'ask-2', closed: true, folded: 'done' }];
  const acts = [{ id: 'act-1', open: true }, { id: 'act-2', open: false, state: 'approved' }];
  const recs = [{ id: 'rec-1', closed: false }, { id: 'rec-2', closed: true, status: 'accepted' }, { id: 'rec-3', closed: false, seen_at: '2026-11-05T00:00:00Z' }];
  assert.deepEqual(openCards(asks, acts, recs).map(l => l.map(c => c.id)), [['ask-1'], ['act-1'], ['rec-1']]);
  assert.deepEqual(openCards(undefined, undefined, undefined), [[], [], []]);
  // The button at the top names the other mode.
  assert.match(chatFullHTML(false), /class="fold small chat-mode" onclick="toggleFullChat\(\)">Show full chat</);
  assert.match(chatFullHTML(true), />Open cards only</);
  // A deep link to a closed card opens the full chat; to an open one, not.
  // A rec's links go to the Recs page, so a rec id changes nothing.
  const askById = Object.fromEntries(asks.map(a => [a.id, a]));
  assert.equal(focusNeedsFull('ask-2', askById, acts), true);
  assert.equal(focusNeedsFull('act-2', askById, acts), true);
  assert.equal(focusNeedsFull('rec-2', askById, acts), false);
  assert.equal(focusNeedsFull('ask-1', askById, acts), false);
  assert.equal(focusNeedsFull('act-1', askById, acts), false);
  assert.equal(focusNeedsFull('', askById, acts), false);
  assert.equal(focusNeedsFull('ask-9', askById, acts), false, 'a card not in the chat changes nothing');
  // A rec is one line in either view, with no Accept · Decline · Reply.
  const notice = recNoticeHTML({ id: 'rec-1', title: 'Try <b>', closed: false });
  assert.match(notice, /^<div class="rec-notice" id="rec-rec-1" data-card="rec-1">💡 <span class="muted">Recommendation filed:<\/span> <a href="#\/recs\/rec-1">Try &lt;b&gt;<\/a><\/div>$/);
  assert.doesNotMatch(notice, /Accept|Decline|Reply/);
  // Every chat opens on the open cards.
  assert.equal(chatState.full, false);
});

test('the tool chain: what the turn is doing, what it waits on, its helpers', () => {
  // 2026-10-27 21:57. Events as the hub stores them.
  const evs = [
    { id: 1, kind: 'tool_result', title: 'result', body: 'answers a call before the window' },
    { id: 2, kind: 'tool_use', title: 'Bash · make check', summary: 'Run the checks' },
    { id: 3, kind: 'tool_result', title: 'error', body: 'Exit code 1' },
    { id: 4, kind: 'tool_use', title: 'Agent · Literature search', summary: 'Delegate: Literature search' },
    { id: 5, kind: 'tool_result', title: 'result', body: 'Async agent launched successfully.' },
    { id: 6, kind: 'tool_use', title: 'Agent · Review the diff', summary: 'Delegate: Review the diff' },
    { id: 7, kind: 'thinking', body: 'hmm' },
    { id: 8, kind: 'tool_use', title: 'Read · DESIGN.md', summary: 'Read DESIGN.md' },
  ];
  const ch = toolChain(evs);
  assert.deepEqual(ch.steps.map(s => [s.label, s.open, s.error]), [
    ['Run the checks', false, true], ['Literature search', false, false],
    ['Review the diff', true, false], ['Read DESIGN.md', true, false]]);
  // No list from the hub: a launched helper runs for the rest of the turn.
  assert.deepEqual(ch.helpers, ['Literature search', 'Review the diff']);
  assert.deepEqual(ch.waiting, ['Read DESIGN.md']);
  // The hub's list (turn_tasks) says which async helpers are still going.
  assert.deepEqual(toolChain(evs, []).helpers, ['Review the diff']);
  assert.deepEqual(toolChain(evs, [{ type: 'local_agent', description: 'Literature search' }, { type: 'local_bash', description: 'make ship' }]).helpers,
    ['Literature search', 'Review the diff']);
  assert.equal(helpersWords(1), '1 subagent running');
  assert.equal(helpersWords(3), '3 subagents running');
  assert.equal(taskHelpers({ turn_tasks: [{ type: 'local_agent' }, { type: 'local_bash' }] }), 1);
  // Helpers then hub-run actions, turn or not: the hub lists helpers until
  // they end, past the reply that left them running (2026-11-05).
  const th = { status: 'idle', turn_tasks: [{ id: 'b', type: 'local_bash' }], running_actions: [{ id: 'act', type: 'action', description: 'Trash promo mail' }] };
  assert.deepEqual(liveTasks(th).map(k => k.id), ['b', 'act']);
  assert.deepEqual(liveTasks({ status: 'idle' }), []);
  assert.match(taskRowHTML(th.running_actions[0]), /<span class="verb">action<\/span>/);
  // On the Sessions card each job is a grey "running" cell with its bar,
  // ahead of the cards (2026-11-05, mock B).
  const jobs = jobCells({ running_actions: [{ id: 'act', description: 'Trash promo mail', doing: '280 trashed', done: 280, total: 400 }] });
  assert.deepEqual(jobs.map(c => [c.id, c.title]), [['job:act', 'Trash promo mail']]);
  const cell = miniCardHTML('staff', jobs[0]);
  assert.match(cell, /class="ask mini quiet job"/);
  assert.match(cell, /<span class="verb">running<\/span>Trash promo mail/);
  assert.match(cell, /280\/400/);
  assert.match(cell, /280 trashed/);
  // The dot follows the name, so the name never moves.
  assert.match(sessionCardHTML({ id: 'staff', title: 'Staff', status: 'done' }, { live: true, cells: '' }), /Staff<span class="sess-dot"><\/span><\/a>/);
  // Drawn (23:11): a row per worker, each opening on a second
  // click; no dot inside the chain.
  const tasks = [{ id: 'a1', type: 'local_agent', description: 'Literature search' }];
  const shut = chainHTML(ch, false, 4, { name: 'Research', tasks });
  assert.match(shut, /^<a [^>]*class="chain-w"[^>]*toggleChainMe\(\)"><span class="caret">›<\/span>Research · 2 tool calls<\/a>/);
  assert.match(shut, /<div class="chain-w"><span class="caret"><\/span>Literature search · subagent<\/div>/);
  assert.doesNotMatch(shut, /dot|Run the checks/);
  const me = chainHTML(ch, false, 4, { name: 'Research', me: true, tasks });
  assert.match(me, /<div class="chain-calls"><div class="chain-row err">Run the checks<\/div><div class="chain-row live">Read DESIGN.md<\/div><\/div>/);
  // A helper the hub can read opens to its own calls.
  const subs = { a1: { ok: true, open: true, evs: [{ id: 1, kind: 'tool_use', title: 'WebSearch · prior work', summary: 'Search prior work' }] } };
  assert.match(chainHTML(ch, false, 4, { name: 'Research', tasks, subs }),
    /class="chain-w on"[^>]*toggleChainSub\('a1'\)"><span class="caret">›<\/span>Literature search · 1 tool call<\/a><div class="chain-calls"><div class="chain-row live">Search prior work<\/div><\/div>/);
  // No helper: the calls straight away; more in the turn than loaded, the earlier ones one click away.
  const solo = toolChain(evs.filter(e => e.id < 4));
  assert.equal(chainHTML(solo, false, 1), '<div class="chain-row err">Run the checks</div>');
  assert.match(chainHTML(solo, false, 40), /onclick="event.preventDefault\(\);chainEarlier\(\)">39 earlier<\/a>/);
});

test('the working line counts the helpers the hub lists', () => {
  const w = el('run-working');
  chatState.liveSince = 0; chatState.sendingSince = 0;
  paintWorking(true, { status: 'running', turn_tools: 9, turn_tasks: [{ type: 'local_agent', description: 'a' }, { type: 'local_agent', description: 'b' }] });
  assert.match(w.innerHTML, />9 tool calls · 2 subagents running<\/a><\/span>$/);
});

test('tickSince moves every clock on the page', () => {
  const s = { dataset: { since: String(Date.now() - 12000) }, textContent: '' };
  const saved = document.querySelectorAll;
  document.querySelectorAll = () => [s];
  chatState.thread = null;
  tickSince();
  document.querySelectorAll = saved;
  assert.equal(s.textContent, '12 s');
});
