// views/threads.js, the working line under a chat: "sending · N s" from the
// moment a message leaves the box until it is in the chat, then
// "starting · N s" until the turn's first tool call, then "working · N tool
// calls". And the rows the chat is drawn in (msgHTML). threads.js needs a
// document and a few globals the page has; just enough is stubbed here. The
// fixtures are synthetic.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const els = {};
const el = id => els[id] || (els[id] = {
  id, hidden: false, innerHTML: '', outerHTML: '', textContent: '', dataset: {},
  querySelector: () => null, addEventListener() {},
  classList: { add() {}, remove() {}, toggle() {} },
});
globalThis.document = { getElementById: el, querySelectorAll: () => [], addEventListener() {} };
globalThis.window = globalThis;
globalThis.location = { hash: '' };
globalThis.localStorage = { getItem: () => null, setItem() {} };
globalThis.setInterval = () => 0;
globalThis.setTimeout = () => 0;
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'ui.js'), 'utf8'), { filename: 'ui.js' });
globalThis.route = { gen: 0 };
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'threads.js'), 'utf8'), { filename: 'threads.js' });
globalThis.composerCards = () => [];

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
  paintWorking(true, { status: 'running', turn_tools: 1 });
  assert.match(w.innerHTML, /working · <a href="#" class="chain-link" onclick="event.preventDefault\(\);toggleChain\(\)">1 tool call<\/a>/);
  assert.doesNotMatch(w.innerHTML, /starting|since/);
  // A finished turn never counts.
  paintWorking(false, { status: 'idle' });
  assert.equal(w.hidden, true);
  chatState.liveSince = 0;
});

test('the working line reads "sending · N s" while a message is on its way', () => {
  chatState.id = 't1'; chatState.thread = { status: 'idle' }; chatState.running = false;
  const w = el('run-working');
  paintWorking(false, chatState.thread);
  assert.equal(w.hidden, true, 'idle and nothing sending: no line');
  // A Send from this box, three seconds ago, into a session that is not running.
  chatState.sendingSince = Date.now() - 3000; chatState.sendingAfter = 5;
  paintWorking(false, chatState.thread);
  assert.equal(w.hidden, false);
  assert.match(w.innerHTML, /^<span class="dot pulse"><\/span> sending · <span class="since" data-since="\d+">3 s<\/span>$/);
  // Into a session already at work: what it is doing rides behind.
  paintWorking(true, { status: 'running', turn_tools: 0 });
  assert.match(w.innerHTML, /sending · <span class="since" data-since="\d+">3 s<\/span> · starting$/);
  paintWorking(true, { status: 'running', turn_tools: 2 });
  assert.match(w.innerHTML, /sending · <span class="since" data-since="\d+">3 s<\/span> · working$/);
  chatState.sendingSince = 0;
  paintWorking(true, { status: 'running', turn_tools: 2 });
  assert.match(w.innerHTML, /working · <a [^>]*>2 tool calls<\/a>/);
  assert.doesNotMatch(w.innerHTML, /sending/);
});

test('the tool chain: what the turn is doing, what it waits on, its helpers', () => {
  // Events as the hub stores them.
  const evs = [
    { id: 1, kind: 'tool_result', title: 'result', body: 'answers a call before the window' },
    { id: 2, kind: 'tool_use', title: 'Bash · make check', summary: 'Run the checks' },
    { id: 3, kind: 'tool_result', title: 'error', body: 'Exit code 1' },
    { id: 4, kind: 'tool_use', title: 'Agent · Survey the literature', summary: 'Delegate: Survey the literature' },
    { id: 5, kind: 'tool_result', title: 'result', body: 'Async agent launched successfully.' },
    { id: 6, kind: 'tool_use', title: 'Agent · Review the diff', summary: 'Delegate: Review the diff' },
    { id: 7, kind: 'thinking', body: 'hmm' },
    { id: 8, kind: 'tool_use', title: 'Read · DESIGN.md', summary: 'Read DESIGN.md' },
  ];
  const ch = toolChain(evs);
  assert.deepEqual(ch.steps.map(s => [s.label, s.open, s.error]), [
    ['Run the checks', false, true], ['Survey the literature', false, false],
    ['Review the diff', true, false], ['Read DESIGN.md', true, false]]);
  // No list from the hub: a launched helper runs for the rest of the turn.
  assert.deepEqual(ch.helpers, ['Survey the literature', 'Review the diff']);
  assert.deepEqual(ch.waiting, ['Read DESIGN.md']);
  // The hub's list (turn_tasks) says which async helpers are still going.
  assert.deepEqual(toolChain(evs, []).helpers, ['Review the diff']);
  assert.deepEqual(toolChain(evs, [{ type: 'local_agent', description: 'Survey the literature' }, { type: 'local_bash', description: 'make ship' }]).helpers,
    ['Survey the literature', 'Review the diff']);
  assert.equal(helpersWords(1), '1 subagent running');
  assert.equal(helpersWords(3), '3 subagents running');
  assert.equal(taskHelpers({ turn_tasks: [{ type: 'local_agent' }, { type: 'local_bash' }] }), 1);
  // Drawn: a row per worker, each opening on a second click; no dot inside
  // the chain.
  const tasks = [{ id: 'a1', type: 'local_agent', description: 'Survey the literature' }];
  const shut = chainHTML(ch, false, 4, { name: 'Research', tasks });
  assert.match(shut, /^<a [^>]*class="chain-w"[^>]*toggleChainMe\(\)"><span class="caret">›<\/span>Research · 2 tool calls<\/a>/);
  assert.match(shut, /<div class="chain-w"><span class="caret"><\/span>Survey the literature · subagent<\/div>/);
  assert.doesNotMatch(shut, /dot|Run the checks/);
  const me = chainHTML(ch, false, 4, { name: 'Research', me: true, tasks });
  assert.match(me, /<div class="chain-calls"><div class="chain-row err">Run the checks<\/div><div class="chain-row live">Read DESIGN.md<\/div><\/div>/);
  // A helper the hub can read opens to its own calls.
  const subs = { a1: { ok: true, open: true, evs: [{ id: 1, kind: 'tool_use', title: 'WebSearch · sorting networks', summary: 'Search sorting networks' }] } };
  assert.match(chainHTML(ch, false, 4, { name: 'Research', tasks, subs }),
    /class="chain-w on"[^>]*toggleChainSub\('a1'\)"><span class="caret">›<\/span>Survey the literature · 1 tool call<\/a><div class="chain-calls"><div class="chain-row live">Search sorting networks<\/div><\/div>/);
  // No helper: the calls straight away; more in the turn than loaded, the
  // earlier ones one click away.
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

// Every message is a row: the owner's grey, a reply with no text nothing at
// all, its dollars on the first card the turn raised.
test('msgHTML: the owner\'s message is a grey row, a reply is its cards', () => {
  const ts = new Date().toISOString();
  const mine = msgHTML({ id: 7, role: 'owner', author: 'owner', ts, text: 'move the dentist\nto Friday', replies: [{ ref: 'ask:a1', label: 'Did it' }] }, [], [], [], {}, {});
  assert.match(mine, /^<details class="chat-row me" id="msg-7" data-k="msg:7" data-def="0">/);
  assert.match(mine, /<span class="k">You<\/span><span class="t">↩ Did it · move the dentist<\/span>/);
  assert.doesNotMatch(mine, /sent by/);
  chatState.costs = {};
  const card = { id: 'ask-1', kind: 'read', state: 'open', open: true, closed: false, title: 'How the inbox pass went', created_at: ts };
  const reply = msgHTML({ id: 8, role: 'claude', ts, text: '', cost_usd: 0.6 }, [card], [], [], {}, {});
  assert.doesNotMatch(reply, /msg-8/, 'an empty reply draws no row');
  assert.match(reply, /<span class="at">\$0\.60<\/span>/, 'its dollars ride on the card');
  assert.equal(msgHTML({ id: 9, role: 'claude', ts, text: '', cost_usd: 0.2 }, [], [], [], {}, {}), '');
  assert.match(msgHTML({ id: 11, role: 'claude', ts, text: 'All set.', cost_usd: 0.2 }, [], [], [], {}, {}),
    /<span class="k">Reply<\/span><span class="t">All set\.<\/span><span class="when">today [^<]* · \$0\.20<\/span>/);
  assert.match(msgHTML({ id: 10, role: 'claude', kind: 'schedule', ts, text: 'Back at 9:00 AM\n\nCheck the inbox' }, [], [], [], {}, {}),
    /class="chat-row sched".*<span class="k">⏰ Check-in<\/span><span class="t">Back at 9:00 AM<\/span>/s);
});

test('open cards only: the chat draws what is open, the full chat is a toggle', () => {
  const asks = [{ id: 'ask-1', closed: false }, { id: 'ask-2', closed: true, folded: 'done' }];
  const acts = [{ id: 'act-1', open: true }, { id: 'act-2', open: false, state: 'approved' }];
  const recs = [{ id: 'rec-1', closed: false }, { id: 'rec-2', closed: true, status: 'accepted' }];
  assert.deepEqual(openCards(asks, acts, recs).map(l => l.map(c => c.id)), [['ask-1'], ['act-1'], ['rec-1']]);
  assert.deepEqual(openCards(undefined, undefined, undefined), [[], [], []]);
  // The button at the top names the other mode.
  assert.match(chatFullHTML(false), /class="fold small chat-mode" onclick="toggleFullChat\(\)">Show full chat</);
  assert.match(chatFullHTML(true), />Open cards only</);
  // A deep link to a closed card opens the full chat; to an open one, not.
  const askById = Object.fromEntries(asks.map(a => [a.id, a])), recById = Object.fromEntries(recs.map(r => [r.id, r]));
  assert.equal(focusNeedsFull('ask-2', askById, recById, acts), true);
  assert.equal(focusNeedsFull('act-2', askById, recById, acts), true);
  assert.equal(focusNeedsFull('rec-2', askById, recById, acts), true);
  assert.equal(focusNeedsFull('ask-1', askById, recById, acts), false);
  assert.equal(focusNeedsFull('act-1', askById, recById, acts), false);
  assert.equal(focusNeedsFull('', askById, recById, acts), false);
  assert.equal(focusNeedsFull('ask-9', askById, recById, acts), false, 'a card not in the chat changes nothing');
  // Every chat opens on the open cards.
  assert.equal(chatState.full, false);
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
