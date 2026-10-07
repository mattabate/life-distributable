// Tests for ui.js, the console's shared render helpers — the one file that
// runs without a browser. `ops/webcheck.sh` runs these (node --test); they
// are not part of `make check` (no JS lane in the gate, DESIGN.md). The
// fixtures are made up here and hold no personal data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// ui.js is a classic script: its top-level const/function declarations land
// in the global scope, which is exactly how the browser sees them.
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'ui.js'), 'utf8'), { filename: 'ui.js' });

test('esc: the five HTML characters, and null is empty', () => {
  assert.equal(esc(`<a href="x">&'</a>`), '&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;');
  assert.equal(esc(null), '');
  assert.equal(esc(42), '42');
});

test('makePager: pages, Show all, reset, and a route change starts it over', () => {
  let painted = 0;
  const p = makePager('tpager', 2, () => painted++);
  const rows = [1, 2, 3, 4, 5];
  assert.deepEqual(p.rows(rows), [1, 2]);
  assert.match(p.html(5, 2), /pagers\.tpager\.more\(\)/);
  p.more();
  assert.deepEqual(p.rows(rows), [1, 2, 3, 4]);
  p.more(true);
  assert.equal(p.rows(rows).length, 5);
  assert.equal(painted, 2);
  for (const f of onLeave) f();
  assert.deepEqual(p.rows(rows), [1, 2]);
});

test('act: toasts done or the error, repaints either way, says which', async () => {
  const el = { textContent: '', classList: { add() {}, remove() {} } };
  const oldDoc = globalThis.document;
  globalThis.document = { getElementById: () => el };
  try {
    let painted = 0;
    assert.equal(await act(async () => {}, 'saved', () => painted++), true);
    assert.equal(el.textContent, 'saved');
    assert.equal(await act(async () => { throw new Error('nope'); }, 'saved', () => painted++), false);
    assert.equal(el.textContent, 'nope');
    assert.equal(painted, 2);
    assert.equal(await act(async () => {}, 'x', null), true);
  } finally { clearTimeout(toastTimer); globalThis.document = oldDoc; }
});

// One table says where a ref opens; a bare id (a rec's `links`) is typed by
// its shape first, the same way store.KindOf does it — including a proposal's
// stamped id, which the old per-prefix check (`act-`) could never link.
test('refs: refHref is one lookup, refOf mirrors store.KindOf', () => {
  assert.equal(refHref('rec:rec-00a1b2c3'), '#/recs/rec-00a1b2c3');
  assert.equal(refHref('thread:hub'), '#/sessions/hub');
  assert.equal(refHref('cal:cal-9f51'), '#/open/cal-9f51');
  assert.equal(refHref('ask:ask-b755', 't-1'), '#/sessions/t-1/ask-b755');
  // No thread on the row: #/open asks the hub which session holds the card
  // (app.js openRef) and lands on it there.
  assert.equal(refHref('ask:ask-b755'), '#/open/ask-b755');
  assert.equal(refHref('action:20260828-150405-0a1b2c3d', 'hub'), '#/sessions/hub/20260828-150405-0a1b2c3d');
  assert.equal(refHref('job:statement-parse'), '');
  assert.equal(refHref(''), '');
  assert.equal(refHref('rec:<x>'), '#/recs/&lt;x&gt;');
  assert.deepEqual(splitRef('no-colon'), ['', '']);
  assert.equal(refOf('cal-9f51'), 'cal:cal-9f51');
  assert.equal(refOf('ask-1a2b3c4d'), 'ask:ask-1a2b3c4d');
  assert.equal(refOf('p-0a1b2c3d'), 'prompt:p-0a1b2c3d');
  assert.equal(refOf('20260828-150405-0a1b2c3d'), 'action:20260828-150405-0a1b2c3d');
  assert.equal(refOf('build-sections-2-and-3-6bae'), 'thread:build-sections-2-and-3-6bae');
  assert.equal(refHref(refOf('20260828-150405-0a1b2c3d')), '#/open/20260828-150405-0a1b2c3d');
});

// A bare id in card text is a link. The table is the phone's
// too (RefTests.swift): text → the ids that come out linked, in order.
test('mdInline: bare hub ids link, shared/ref-cases.json', () => {
  const cases = JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', '..', '..', '..', 'shared', 'ref-cases.json'), 'utf8'));
  for (const [src, want] of cases.links) {
    const got = [...mdInline(src).matchAll(/<a href="#\/(?:recs|open)\/([^"]+)">/g)].map(m => m[1]);
    assert.deepEqual(got, want, src);
  }
  assert.equal(mdInline('Decide rec-5e6f7a8b'), 'Decide <a href="#/recs/rec-5e6f7a8b">rec-5e6f7a8b</a>');
  assert.equal(mdInline('see `ask-955bfb8e`'), 'see <a href="#/open/ask-955bfb8e"><code>ask-955bfb8e</code></a>');
  // links=false (inside a clickable card) and a preview stay link-free
  assert.equal(mdInline('Decide rec-5e6f7a8b', false), 'Decide rec-5e6f7a8b');
  assert.equal(mdPreview('- Decide rec-5e6f7a8b'), '• Decide rec-5e6f7a8b');
});

// The table the hub (internal/format) and the phone (FormatTests.swift) read.
test('money and counts: shared/format-cases.json', () => {
  const cases = JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', '..', '..', '..', 'shared', 'format-cases.json'), 'utf8'));
  const fns = { usd, usdWhole: usd0, usdSigned, usdWholeSigned: usd0Signed, usdShort, usdPrice, commas: num };
  for (const [name, rows] of Object.entries(cases)) {
    if (name === '_') continue;
    assert.ok(fns[name], `no console formatter for ${name}`);
    for (const [v, want] of rows) assert.equal(fns[name](v), want, `${name}(${v})`);
  }
});

test('tokens', () => {
  assert.equal(tokens(950), '950 tok');
  assert.equal(tokens(12_400), '12k tok');
  assert.equal(tokens(3_400_000), '3.4M tok');
  assert.equal(tokens(21_000_000), '21M tok');
});

test('mdInline: bold, italic, code, links — and asterisks that are not syntax', () => {
  assert.equal(mdInline('**bold** and *it*'), '<strong>bold</strong> and <em>it</em>');
  assert.equal(mdInline('run `--host *` now'), 'run <code>--host *</code> now');
  assert.equal(mdInline('Bash(npm install:*) and Bash(npm run:*)'), 'Bash(npm install:*) and Bash(npm run:*)');
  assert.equal(mdInline('2**32 and 2**64'), '2**32 and 2**64');
  assert.equal(mdInline('[docs](https://ex.test/a)'), '<a href="https://ex.test/a" target="_blank" rel="noopener">docs</a>');
  assert.equal(mdInline('see https://ex.test/a'), 'see <a href="https://ex.test/a" target="_blank" rel="noopener">https://ex.test/a</a>');
  // links=false: inside a clickable card, so no nested anchors
  assert.equal(mdInline('[docs](https://ex.test/a) https://ex.test/b', false), 'docs https://ex.test/b');
  assert.equal(mdInline('<b>'), '&lt;b&gt;');
});

// GFM's rule: the sentence's trailing punctuation is not part of a bare URL —
// "…pull/82, merge when ready" linked the comma and 404ed.
test('mdInline: bare URL sheds trailing punctuation, keeps balanced parens', () => {
  assert.equal(mdInline('x https://a.com/1, y'),
    'x <a href="https://a.com/1" target="_blank" rel="noopener">https://a.com/1</a>, y');
  assert.equal(mdInline('x https://a.com/1. y'),
    'x <a href="https://a.com/1" target="_blank" rel="noopener">https://a.com/1</a>. y');
  assert.equal(mdInline('x (see https://a.com/1) y'),
    'x (see <a href="https://a.com/1" target="_blank" rel="noopener">https://a.com/1</a>) y');
  assert.equal(mdInline('x https://en.wikipedia.org/wiki/A_(b) y'),
    'x <a href="https://en.wikipedia.org/wiki/A_(b)" target="_blank" rel="noopener">https://en.wikipedia.org/wiki/A_(b)</a> y');
  // A quote after the URL is an entity by the time the pass runs.
  assert.equal(mdInline('see https://a.com/1".'),
    'see <a href="https://a.com/1" target="_blank" rel="noopener">https://a.com/1</a>&quot;.');
  // [text](url) keeps its exact URL, punctuation and all
  assert.equal(mdInline('[PR](https://g.test/pull/82), then'),
    '<a href="https://g.test/pull/82" target="_blank" rel="noopener">PR</a>, then');
});

test('md: bullets, numbers, headings, paragraphs', () => {
  assert.equal(md('- a\n- **b**\n\n1. one\n2) two\n# Head\nplain'),
    '<ul><li>a</li><li><strong>b</strong></li></ul><ol><li>one</li><li>two</li></ol><h3>Head</h3><p>plain</p>');
  assert.equal(md(''), '');
});

test('isLongText: a bubble folds past 18 non-blank lines or 1600 characters', () => {
  assert.equal(isLongText('short'), false);
  assert.equal(isLongText(Array(18).fill('a line').join('\n')), false);
  assert.equal(isLongText(Array(18).fill('a line').join('\n\n')), false); // blanks do not count
  assert.equal(isLongText(Array(19).fill('a line').join('\n')), true);
  assert.equal(isLongText('x'.repeat(1601)), true);
  assert.equal(longLineCount('a\n\n b \n'), 2);
  assert.equal(isLongText(null), false);
});

test('md: a pipe table is a table, not paragraphs of pipes', () => {
  assert.equal(md('Your buys\n| Date | Buy |\n|---|---:|\n| **Today** | 1 AMZN |\n| 09-24 | `a | b` |\nafter'),
    '<p>Your buys</p><table class="md-table"><thead><tr><th>Date</th><th style="text-align:right">Buy</th></tr></thead>' +
    '<tbody><tr><td><strong>Today</strong></td><td style="text-align:right">1 AMZN</td></tr>' +
    '<tr><td>09-24</td><td style="text-align:right"><code>a | b</code></td></tr></tbody></table><p>after</p>');
  // No separator: still a table, no header row.
  assert.equal(md('| a | b |\n| c | d |'),
    '<table class="md-table"><tbody><tr><td>a</td><td>b</td></tr><tr><td>c</td><td>d</td></tr></tbody></table>');
  // A lone pipe in prose is prose.
  assert.equal(md('a | b'), '<p>a | b</p>');
});

test('md: a fenced block keeps its text verbatim under a Copy button', () => {
  const box = (lang, text) => `<div class="md-code"><div class="md-code-bar"><span>${lang}</span>` +
    `<button type="button" class="md-copy" onclick="mdCopy(this)">Copy</button></div><pre><code>${text}</code></pre></div>`;
  // Indentation, blank lines and markdown-looking text all survive; the list
  // the block interrupted picks its numbering back up.
  assert.equal(md('1. Paste:\n```json\n{\n  "a": [\n    "**b** <c>"\n  ]\n}\n```\n2. Save'),
    '<ol><li>Paste:</li></ol>' + box('json', '{\n  &quot;a&quot;: [\n    &quot;**b** &lt;c&gt;&quot;\n  ]\n}') +
    '<ol start="2"><li>Save</li></ol>');
  // Indented under a list item: the fence's indent comes off every line.
  assert.equal(md('- run\n  ```\n  a\n    b\n  ```'), '<ul><li>run</li></ul>' + box('', 'a\n  b'));
  // Unclosed (still streaming): the block runs to the end.
  assert.equal(md('```sh\nls -la'), box('sh', 'ls -la'));
  // A preview drops the fence lines and keeps the words.
  assert.equal(mdPreview('Paste:\n```json\n{"a": 1}\n```'), 'Paste: {&quot;a&quot;: 1}');
});

test('mdPreview folds a reply into one link-free line', () => {
  assert.equal(mdPreview('- **Did:** x\n\n# Next\nsee https://ex.test'), '• <strong>Did:</strong> x Next see https://ex.test');
  assert.equal(mdPreview('| Date | Buy |\n|---|---|\n| 09-24 | 1 AMZN |'), 'Date · Buy 09-24 · 1 AMZN');
});

test('failedPreview keeps the first sentence', () => {
  assert.equal(failedPreview('Rate limited. Try again at https://status.claude.com'), '⚠ Session failed · Rate limited');
  assert.equal(failedPreview(''), '⚠ Session failed · no result');
});

test('card / pill', () => {
  assert.equal(card('x'), '<div class="card">x</div>');
  assert.equal(card('x', 'err-card', 'id="a"'), '<div class="card err-card" id="a">x</div>');
  assert.equal(failHTML('money'), '<div class="card err">Couldn\'t load money · <button class="sm" onclick="render()">Retry</button></div>');
  assert.equal(emptyHTML('<none>', 'card'), '<div class="card"><div class="empty">&lt;none&gt;</div></div>');
  assert.equal(pill('<b>'), '<span class="pill">&lt;b&gt;</span>');
  assert.equal(pill('approve?', 'needs'), '<span class="pill needs">approve?</span>');
  assert.equal(pill('money', undefined), '<span class="pill">money</span>');
});

test('statusPill', () => {
  assert.equal(statusPill('needs_you'), '<span class="pill needs">your turn</span>');
  assert.match(statusPill('running'), /dot pulse/);
});

// The hub's order (2026-09-18: decisive first, "Reply" last) — the card
// row and the composer strip both draw it as sent, so they cannot differ.
const outcomes = [{ value: 'done', label: 'Decided' }, { value: 'wont', label: 'Not deciding' }, { value: '', label: 'Reply' }];
const ask = { id: 'ask-1a2b3c4d', thread_id: 't-1a2b3c4d', thread_title: 'Plan lunches', title: 'Pick a **day**',
  detail: '- Mon\n- Tue', kind: 'decision', verb: 'decide', surface: 'any', state: 'open', open: true, closed: false, created_at: new Date().toISOString(), outcomes };
// Where a row stands is the hub's (store.AskStanding / ActionStanding /
// RecStanding): the fixtures carry it the way the wire does.
const WAITING = { open: false, closed: false }, CLOSED = { open: false, closed: true, reopen: true };
const DISMISSED = { ...CLOSED, folded: 'dismissed' };
const readAsk = { ...ask, kind: 'read', outcomes: [{ value: 'done', label: 'Read it' }] };

// One card shape: the row is the hub's words in the hub's order
// (decisive first and primary, "Reply" last), then Dismiss — each answer
// arms the session's composer with that pick (outcome, words, which session,
// when), and the strip's chips come in the same order as the card's buttons.
test('askHTML in the chat: bubble, id for deep links, the hub\'s words + Dismiss', () => {
  const h = askHTML(ask);
  assert.match(h, /^<div class="ask" id="ask-card-ask-1a2b3c4d">/);
  assert.match(h, /🔴 <span class="verb">[a-z ]+<\/span><\/div><div class="t">Pick a <strong>day<\/strong>/);
  assert.match(h, /<ul><li>Mon<\/li><li>Tue<\/li><\/ul>/);
  assert.match(h, /<div class="acts"><button class="sm primary" onclick="openRespond\('ask-1a2b3c4d','done'\)">Decided<\/button><button class="sm" onclick="openRespond\('ask-1a2b3c4d','wont'\)">Not deciding<\/button><button class="sm" onclick="openRespond\('ask-1a2b3c4d',''\)">Reply<\/button><button class="sm" onclick="cardFold\('ask','ask-1a2b3c4d'\)">Dismiss<\/button><\/div>/);
  // No Open page: the card in the chat IS the page.
  assert.doesNotMatch(h, /resolveAskGlobal|>Done<|>Open|target="_blank"|>Respond</);
  // A hub that sent no words: one Respond, the composer's default.
  assert.match(askHTML({ ...ask, outcomes: undefined }), /openRespond\('ask-1a2b3c4d'\)">Respond<[\s\S]*>Dismiss</);
});

// A read card is Respond (blue, arms the reply) + Read (white, the silent
// close: done with the hub's "Read it", never dismissed). A read card is read
// even when dismissed, so Respond takes Read's place and Read takes Dismiss's.
test('askHTML in the chat: a read card is Respond (arms the reply) + Read (closes, "Read it")', () => {
  const h = askHTML(readAsk);
  assert.match(h, /openRespond\('ask-1a2b3c4d'\)">Respond<[\s\S]*resolveAsk\('ask-1a2b3c4d','done','Read it'\)">Read</);
  assert.doesNotMatch(h, /resolveAskGlobal|>Done<|>Read it<|>Reply<|>Dismiss<|'dismissed'/);
  const other = askHTML(ask);
  // (">Reply<" is the hub's own third outcome since 09-19, asserted above.)
  assert.doesNotMatch(other, />Read<|>Done<|>Respond</);
  assert.match(other, />Decided<[\s\S]*>Dismiss</);
});

// Four cells: a read card is blue, never red — it is an
// answer to read, not a stopped session. Answered/closed drop the tint like
// any other card.
test('askHTML: a read card is blue', () => {
  const chat = askHTML({ ...ask, kind: 'read', verb: 'read' });
  assert.match(chat, /^<div class="ask read" id="ask-card-ask-1a2b3c4d">/);
  assert.match(chat, /🔵 <span class="verb">read<\/span><\/div><div class="t">Pick/);
  assert.doesNotMatch(chat, /🔴/);
  // Replying does not turn it red — it is still a read, now waiting on the
  // agent to close it.
  assert.match(askHTML({ ...readAsk, state: 'answered', ...WAITING }), /^<div class="ask read" id=/);
  // Closed by the owner's reply (resolution = the reply's first line): a full grey card.
  assert.match(askHTML({ ...readAsk, state: 'done', resolution: 'thanks', ...CLOSED }), /^<div class="ask answered" id=/);
});

// Once read, a card can be dismissed — every open card but a read carries
// Dismiss; a read's white Read button is its silent close.
test('askHTML: every open card but a read offers Dismiss', () => {
  assert.match(askHTML(ask), /Dismiss<\/button>/);
  assert.match(askHTML(ask), /cardFold\('ask','ask-1a2b3c4d'\)/);
  assert.doesNotMatch(askHTML({ ...ask, state: 'done', ...CLOSED }), /Dismiss<\/button>/);
  // Dismissed keeps a footprint: a folded grey line that
  // unfolds to the whole card and Reopen.
  const gone = askHTML({ ...readAsk, state: 'dismissed', ...DISMISSED });
  assert.match(gone, /^<details class="ask folded" id="ask-card-ask-1a2b3c4d" data-k="ask:ask-1a2b3c4d" data-def="0">/);
  assert.match(gone, /✕ dismissed ·/);
  assert.match(gone, /<ul><li>Mon<\/li><li>Tue<\/li><\/ul>/);
  assert.match(gone, /cardFold\('ask','ask-1a2b3c4d',true\)">Reopen</);
  assert.match(askHTML({ ...ask, state: 'dismissed', ...DISMISSED }, true), /data-def="0" open>/);
  assert.doesNotMatch(askHTML(readAsk), /Dismiss<\/button>/);
  // A read marked Read (done, "Read it") folds the same way, headed "read";
  // a decision closed done with any note does not fold.
  const readIt = askHTML({ ...readAsk, state: 'done', resolution: 'Read it', ...CLOSED, folded: 'read' });
  assert.match(readIt, /^<details class="ask folded" id="ask-card-ask-1a2b3c4d"/);
  assert.match(readIt, /✓ read ·/);
  assert.match(readIt, /Reopen</);
  assert.match(askHTML({ ...ask, state: 'done', resolution: 'Read it', ...CLOSED }), /^<div class="ask answered"/);
});

// Every closed card reopens but an install — the hub's `reopen` says which; a done card keeps its state word beside the button.
test('askHTML: a done card carries Reopen when the hub says reopen', () => {
  const done = askHTML({ ...ask, kind: 'physical', state: 'done', ...CLOSED });
  assert.match(done, /<div class="acts"><span class="small muted">done<\/span><button class="sm" onclick="cardFold\('ask','ask-1a2b3c4d',true\)">Reopen<\/button><\/div>/);
  assert.doesNotMatch(askHTML({ ...ask, kind: 'install', state: 'done', ...CLOSED, reopen: false }), /Reopen/);
});

// The install cell: title, description, one teal Install
// button — no Respond furniture. The button carries the OTA link itself, so
// the link line is stripped from the shown detail; Dismiss stays as the out.
test('askHTML: an install card is teal with one Install button, link line stripped', () => {
  const inst = { ...ask, kind: 'install', verb: 'install', title: 'Install app build 812 (tap the link)',
    detail: 'The install ask now draws as this teal cell.\n[Install build 812](https://hub.test/ota/tok/install.html)' };
  const chat = askHTML(inst);
  assert.match(chat, /^<div class="ask install" id="ask-card-ask-1a2b3c4d">/);
  assert.match(chat, /📲 <span class="verb">install<\/span><\/div><div class="t">Install app build 812/);
  assert.match(chat, /<a class="btn sm primary" href="https:\/\/hub\.test\/ota\/tok\/install\.html" target="_blank">Install<\/a>/);
  assert.match(chat, /teal cell/);
  assert.doesNotMatch(chat, /install\.html\)|>Respond<|>Done</);
  assert.match(chat, /Dismiss<\/button>/);
  // No link in the detail (old habit): fall back to Respond, still teal.
  const bare = askHTML({ ...inst, detail: 'New build is up.' });
  assert.match(bare, /class="ask install"/);
  assert.match(bare, />Respond</);
  assert.doesNotMatch(bare, /btn sm primary/);
  // Closed drops the button with the tint like any other card.
  assert.match(askHTML({ ...inst, state: 'done', ...CLOSED }), /^<div class="ask answered"/);
});

// The desktop app's install cell (one click builds in the background, then
// the app restarts): the same teal card, but its
// Install is a button on the hub's `mac` lane, not a link — nothing to open
// — and the detail stays whole. The hub's `target` says which device; an
// older hub's card is told by its title.
test('askHTML: a Mac install card has an Install button on the mac lane, no link', () => {
  const mac = { ...ask, kind: 'install', verb: 'install', target: 'mac', title: 'Install desktop build 1512',
    detail: 'The install cell for the desktop app.' };
  const chat = askHTML(mac);
  assert.match(chat, /^<div class="ask install" id="ask-card-ask-1a2b3c4d">/);
  assert.match(chat, /<button class="sm primary" onclick="macInstall\('ask-1a2b3c4d'\)">Install<\/button>/);
  assert.doesNotMatch(chat, /href=|>Respond</);
  assert.match(chat, /Dismiss<\/button>/);
  assert.match(chat, /install cell for the desktop app/);
  assert.equal(askInstallTarget(mac), 'mac');
  assert.equal(askInstallTarget({ ...mac, target: undefined }), 'mac');
  assert.equal(askInstallTarget({ title: 'Install app build 1512 (tap the link)' }), 'phone');
  assert.equal(askInstallTarget({ title: 'Install build 1512: the Mac bar' }), 'phone');
});

test('forYouPill: blue "to read" only when every item is a read', () => {
  assert.equal(forYouPill(0, 0), '');
  assert.equal(forYouPill(2, 2), '<span class="pill read">2 to read</span>');
  assert.equal(forYouPill(2, 1), '<span class="pill needs">2 for you</span>');
  assert.equal(forYouPill(1, 0), '<span class="pill needs">1 for you</span>');
  // Teal only when every card is an app build; one real ask beside it is red.
  assert.equal(forYouPill(1, 0, 1), '<span class="pill install">1 to install</span>');
  assert.equal(forYouPill(2, 0, 1), '<span class="pill needs">2 for you</span>');
  assert.equal(forYouPill(1, 1, 1), '<span class="pill read">1 to read</span>');
});

test('askHTML: answered keeps its buttons with ⏳; closed loses them', () => {
  const chat = askHTML({ ...ask, state: 'answered', ...WAITING });
  assert.match(chat, /class="ask" id=/);
  assert.match(chat, /⏳ <span class="verb">[a-z ]+<\/span><\/div><div class="t">Pick/);
  assert.match(chat, /Decided/);
  const done = askHTML({ ...ask, state: 'done', ...CLOSED, reopen: false });
  assert.match(done, /class="ask answered"/);
  assert.match(done, /✓ <span class="verb">decide<\/span><\/div><div class="t">Pick/);
  assert.match(done, /<span class="small muted">done<\/span>/);
  assert.doesNotMatch(done, /<button/);
});

test('askHTML: an error is one line and Restart', () => {
  const err = { ...ask, kind: 'error', verb: 'restart', outcomes: [], title: 'Run died' };
  const chat = askHTML(err);
  assert.match(chat, /class="ask err-card" id="ask-card-ask-1a2b3c4d"/);
  assert.match(chat, /⚠︎ <span class="verb">restart<\/span><\/div><div class="t">Run died/);
  assert.match(chat, /retryAsk\('ask-1a2b3c4d'\)">Restart/);
  assert.match(chat, /cardFold\('ask','ask-1a2b3c4d'\)">Dismiss/);
  assert.doesNotMatch(chat, /Done|Reply|<ul>/);
});

const actOutcomes = [{ value: 'approved', label: 'Approve' }, { value: 'denied', label: 'Deny' }, { value: '', label: 'Reply' }];
const action = { id: 'act-1a2b3c4d', thread_id: 't-1a2b3c4d', kind: 'commit', project: 'life', title: 'git commit',
  detail: 'one plain commit', exec_type: 'shell', exec_payload: 'git commit -m x', state: 'proposed', open: true, closed: false,
  created_at: new Date().toISOString(), outcomes: actOutcomes };

// The approval card is the ask card's shape: 🔴 title, the
// detail, the button row at the bottom — the hub's words ARM the composer
// (armActionReply), and Dismiss is the silent close.
test('actionHTML: the approval card in the chat', () => {
  const chat = actionHTML(action);
  assert.match(chat, /^<div class="ask" id="ask-card-act-1a2b3c4d">/);
  assert.match(chat, /<div class="card-h">🔴 <span class="verb">approve<\/span><\/div><div class="t">git commit<\/div>/);
  assert.match(chat, /<span class="pill purple">commit<\/span>/);
  assert.match(chat, /will run: shell/);
  assert.match(chat, /<div class="acts"><button class="sm primary" onclick="armActionReply\('act-1a2b3c4d','approved'\)">Approve<\/button><button class="sm" onclick="armActionReply\('act-1a2b3c4d','denied'\)">Deny<\/button><button class="sm" onclick="armActionReply\('act-1a2b3c4d',''\)">Reply<\/button><button class="sm" onclick="cardFold\('action','act-1a2b3c4d'\)">Dismiss<\/button><\/div>/);
  assert.doesNotMatch(chat, /textarea|anote|decideAction|approve\?|Open in session/);
  // Dismissed: folded like a dismissed ask, Reopen inside; the fold remembers.
  const gone = actionHTML({ ...action, state: 'dismissed', ...DISMISSED });
  assert.match(gone, /^<details class="ask folded" id="ask-card-act-1a2b3c4d" data-k="action:act-1a2b3c4d" data-def="0">/);
  assert.match(gone, /✕ dismissed ·/);
  assert.match(gone, /<div class="acts"><button class="sm" onclick="cardFold\('action','act-1a2b3c4d',true\)">Reopen<\/button><\/div>/);
  assert.match(actionHTML({ ...action, state: 'dismissed', ...DISMISSED }, true), /data-def="0" open>/);
  // What the push SPOKE, under the detail like an ask's; nothing drawn
  // when the row never pushed.
  assert.doesNotMatch(chat, /class="said"/);
  // The title line is dropped when the spoken line already says it (cd61d24).
  assert.match(actionHTML({ ...action, said: 'Hey Alex, I need your approval. git commit.' }),
    /<span class="verb">approve<\/span><\/div>\s*<div class="said">Hey Alex, I need your approval\. git commit\.<\/div>/);
});

test('actionHTML: a scheduled job\'s proposal names its job and is decided from the card', () => {
  const job = { ...action, thread_id: undefined, source: 'claude:job:daily-sweep', run_id: '150', outcomes: actOutcomes.slice(0, 2) };
  const h = actionHTML(job);
  assert.match(h, /<span class="pill">⚙ daily-sweep job<\/span>/);
  assert.match(h, /decideAction\('act-1a2b3c4d',true\)">Approve/);
  assert.match(h, /decideAction\('act-1a2b3c4d',false\)">Deny/);
  assert.match(h, /cardFold\('action','act-1a2b3c4d'\)">Dismiss/);
  assert.doesNotMatch(h, /sessions\/|anote|href=|armActionReply|>Reply</);
  assert.equal(actionJob(action), '');
  assert.equal(actionJob(job), 'daily-sweep');
});

// A rec's cell is the same card: violet, the hub's Accept · Decline · Just
// reply arming the composer, Dismiss (expired, note "dismissed") folding it.
const rec = { id: 'rec-1a2b3c4d', title: 'Try **oat** milk', domain: 'food', kind: 'try', status: 'proposed', open: true, closed: false, because: 'you liked it',
  created_at: new Date().toISOString(), outcomes: [{ value: 'accepted', label: 'Accept' }, { value: 'declined', label: 'Decline' }, { value: '', label: 'Reply' }] };
test('recHTML: the rec card in the chat', () => {
  const h = recHTML(rec);
  assert.match(h, /^<div class="ask rec" id="ask-card-rec-1a2b3c4d">/);
  assert.match(h, /💡 <span class="verb">rec<\/span><\/div><div class="t">Try <strong>oat<\/strong> milk<\/div>/);
  assert.match(h, /<a class="pill purple" href="#\/recs\/rec-1a2b3c4d">rec<\/a>/);
  assert.match(h, /<div class="acts"><button class="sm primary" onclick="armRecReply\('rec-1a2b3c4d','accepted'\)">Accept<\/button><button class="sm" onclick="armRecReply\('rec-1a2b3c4d','declined'\)">Decline<\/button><button class="sm" onclick="armRecReply\('rec-1a2b3c4d',''\)">Reply<\/button><button class="sm" onclick="cardFold\('rec','rec-1a2b3c4d'\)">Dismiss<\/button><\/div>/);
  assert.doesNotMatch(h, /Open in Recs|The record/);
  const done = recHTML({ ...rec, status: 'declined', decided_by: 'owner', decision_note: 'no', ...CLOSED, reopen: false });
  assert.match(done, /^<div class="ask rec answered"/);
  assert.match(done, /✕ <span class="verb">rec<\/span><\/div><div class="t">Try/);
  assert.match(done, /<div class="acts"><span class="small muted">declined by owner — no<\/span><\/div>/);
  const gone = recHTML({ ...rec, status: 'expired', decision_note: 'dismissed', ...DISMISSED });
  assert.match(gone, /^<details class="ask folded" id="ask-card-rec-1a2b3c4d" data-k="rec:rec-1a2b3c4d"/);
  assert.match(gone, /cardFold\('rec','rec-1a2b3c4d',true\)">Reopen</);
  // Expired by the hub, not by the owner: a closed card, not a fold.
  assert.match(recHTML({ ...rec, status: 'expired', ...CLOSED }), /^<div class="ask rec answered"/);
  // Parked (deferred): neither open nor closed — it keeps its buttons.
  assert.match(recHTML({ ...rec, status: 'deferred', ...WAITING }), />Accept<[\s\S]*>Dismiss</);
});

test('actionHTML: events[] draw only when the hub sent them', () => {
  assert.doesNotMatch(actionHTML(action), /approved by/);
  const h = actionHTML({ ...action, state: 'approved', ...CLOSED, reopen: false, events: [
    { id: 1, action_id: 'act-1a2b3c4d', ts: '2000-01-01T12:00:00Z', event: 'proposed', actor: 'claude:thread:t-1a2b3c4d' },
    { id: 2, action_id: 'act-1a2b3c4d', ts: '2000-01-01T12:02:00Z', event: 'approved', actor: 'app', note: 'ok' },
  ] });
  assert.match(h, /<div>proposed by claude:thread:t-1a2b3c4d · /);
  assert.match(h, /<div>approved by app · .* · ok<\/div>/);
  // decided: the card is grey, the state stands where the buttons were
  assert.match(h, /^<div class="ask answered"/);
  assert.match(h, /✓ <span class="verb">approve<\/span><\/div><div class="t">git commit<\/div>/);
  assert.match(h, /<div class="acts"><span class="small muted">approved<\/span><\/div>/);
  assert.doesNotMatch(h, /approve\?|decideAction|armActionReply|anote/);
  assert.match(actionHTML({ ...action, state: 'denied', ...CLOSED }), /✕ <span class="verb">approve<\/span><\/div><div class="t">git commit<\/div>/);
  assert.equal(eventsHTML([]), '');
  assert.equal(eventsHTML(undefined), '');
});

// Rounding, and the promotion at each ceiling: the phone's shortAgo floored
// until 2026-09-01, so 45 hours read "2d ago" here and "1d" there — the same
// instant with two different numbers, which showed up on the Sources line about
// how stale a connector is. Round without promotion prints "60m ago".
test('ago: rounds to the nearest unit and never prints a full bucket', () => {
  const back = s => new Date(Date.now() - s * 1000).toISOString();
  assert.equal(ago(back(100)), '2m ago');       // 1.67 min
  assert.equal(ago(back(3599)), '1h ago');      // not "60m ago"
  assert.equal(ago(back(5400)), '2h ago');      // 1.5 h
  assert.equal(ago(back(86399)), '1d ago');     // not "24h ago"
  assert.equal(ago(back(45 * 3600)), '2d ago'); // the Sources case: 1.875 days
});

test('when / ago', () => {
  assert.equal(when(''), '');
  assert.equal(ago(''), '');
  assert.equal(ago(new Date().toISOString()), 'just now');
});

test('select renders the current option selected', () => {
  assert.equal(select('k', 'Kind', ['a', 'b'], 'b'),
    '<label class="field" style="flex:1;min-width:150px"><span>Kind</span><select id="k">\n    <option value="a">a</option><option value="b" selected>b</option></select></label>');
});

// ---- views/sources.js + views/config.js: a card is a door, the page behind it says everything ----
// Loaded into this same context (classic scripts, like ui.js), which already
// declares `views` and `ago`; goals.js for the goal emblem config.js draws.
// Only the pure markup builders are exercised — the draw* need a browser.
for (const f of ['goals.js', 'sources.js', 'config.js']) {
  vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', f), 'utf8'), { filename: f });
}

test('sources: the source\'s own page carries the prose, under Configuration', () => {
  const g = { id: 'code', title: 'Code', tag: 'Code', color: '#64748B', sources: [
    { id: 'gh', title: 'GitHub', status: 'connected', total: 108, from: 'GitHub API daily',
      storage: 'observations table', summary: '2 handles',
      accounts: [{ label: '@handle', via: 'API', url: 'https://github.com/handle' }, { label: '@other' }],
      kinds: [{ kind: 'repo-stats', note: 'one row per sync', n: 27, first: '2026-08-23T05:58:27Z', last: '2026-09-26T04:05:11Z' }] },
  ] };
  const page = sourcePageHTML(g, g.sources[0]);
  assert.match(page, /<h2><a href="#\/config">Configuration<\/a> \/ GitHub<\/h2><span class="small muted">Code<\/span>/);
  assert.match(page, /GitHub API daily/);
  assert.match(page, /observations table/);
  assert.match(page, /<td class="mono">repo-stats<\/td><td class="small muted">one row per sync<\/td>/);
  assert.match(page, /2026-08-23 → 2026-09-26/);
  assert.match(page, /href="https:\/\/github.com\/handle"/);
  assert.equal(sourceHash(g, g.sources[0]), '#/sources/code/gh');
});

// The card has to contradict itself out loud: a connector can read
// "connected · 2m ago" while every call comes back refused, because `last`
// is the newest row of any kind — including rows the hub writes itself. The
// red line leads with how old the numbers are, not with the error.
test('sources: a failing source says how stale it is, on the card and its page', () => {
  const g = { id: 'code', title: 'Code', tag: 'Code', color: '#64748B', sources: [
    { id: 'gh', title: 'GitHub', status: 'failing', total: 150,
      error: 'gh: HTTP 402: credits depleted', fails: 4,
      failing_since: new Date(Date.now() - 16 * 3600e3).toISOString(),
      last_ok: new Date(Date.now() - 45 * 3600e3).toISOString(),
      last: new Date(Date.now() - 120e3).toISOString() },
  ] };
  const card = cfgSourceCard(g, g.sources[0]);
  assert.match(card, /<a class="scard bad" href="#\/sources\/code\/gh" style="--c:#64748B">/);
  assert.match(card, /sfail clamp1">4 tries failed · since 16h ago · last worked 2d ago</);
  assert.match(card, /150 rows · 2m ago/); // the contradiction, now visible
  // The error itself is on the source's page, under the same red line.
  const page = sourcePageHTML(g, g.sources[0]);
  assert.match(page, /Not syncing — 4 tries failed · since 16h ago · last worked 2d ago/);
  assert.match(page, /What the service said: <code>gh: HTTP 402: credits depleted<\/code>/);
  // One failure, and one that has never worked at all.
  assert.match(cfgSourceCard(g, { id: 'y', title: 'Y', status: 'failing' }), /1 try failed · never worked/);
  // Everything else keeps its quiet card: the summary line, no red.
  const ok = cfgSourceCard(g, { id: 'z', title: 'Z', status: 'connected', summary: '3 repos' });
  assert.doesNotMatch(ok, /sfail|scard bad/);
  assert.match(ok, /muted clamp1">3 repos</);
});

// A section per group, its name in the group's colour; groups that share a
// tag are one section; cards alphabetical inside it.
test('config: sources are sections of cards, by tag, each edged in its colour', () => {
  const groups = [
    { id: 'code', title: 'Code', tag: 'Code', color: '#181717', sources: [
      { id: 'yt', title: 'YouTube', status: 'connected', brand: { mark: '▶', color: '#FF0000', ink: '#FFFFFF' } },
      { id: 'gh', title: 'GitHub', status: 'connected', brand: { mark: 'GH', color: '#181717', ink: '#FFFFFF', logo: 'M12 .297c-6.63 0-12 5.373-12 12' } },
    ] },
    { id: 'repos', title: 'Repos', tag: 'Code', color: '#181717', sources: [{ id: 'gl', title: 'GitLab', status: 'connected' }] },
    { id: 'home', title: 'Home', sources: [{ id: 'w', title: 'Thermostat', status: 'failing' }] },
  ];
  const h = cfgSourcesHTML(groups);
  assert.equal(h.split('<section class="sgroup"').length - 1, 2);
  assert.match(h, /<section class="sgroup" style="--c:#181717;--n:3">\s*<h4>Code<span class="muted">3<\/span><\/h4>/);
  assert.ok(h.indexOf('>GitHub<') < h.indexOf('>GitLab<') && h.indexOf('>GitLab<') < h.indexOf('>YouTube<'));
  // No tag: the title on slate.
  assert.match(h, /<section class="sgroup" style="--c:#64748B;--n:1">\s*<h4>Home<span class="muted">1<\/span><\/h4>/);
  // The tile: letters on the provider's colour, or its logo path in its ink.
  assert.match(h, /<span class="bmark" style="background:#FF0000;color:#FFFFFF">▶<\/span>/);
  assert.match(h, /<span class="bmark logo" style="background:#181717;color:#FFFFFF"><svg viewBox="0 0 24 24" aria-label="GH"><path fill="currentColor" d="M12 .297c-6.63 0-12 5.373-12 12"\/><\/svg><\/span>/);
  assert.match(brandMark(null), /class="bmark" style="background:var\(--muted\);color:#fff">·</);
  assert.match(cfgSourcesHTML([]), /Nothing connected\./);
});

// Powered by: the plan, this month's spend, a meter per limit with its clock
// tick, a red alert for a limit nearly out, and the model ladder.
test('config: the plan block says what runs the sessions and how much is left', () => {
  const q = { available: true, next_model: 'claude-opus-5',
    plan: { name: 'Claude', via: 'Claude Code', usd: 100, period: 'monthly', charged_on: '2026-10-01', month_usd: 23.4,
      brand: { mark: 'A', color: '#D97757', ink: '#FFFFFF' } },
    windows: [
      { key: 'five_hour', label: 'All models · 5 hours', utilization: 18, elapsed_pct: 40, tone: '', foot: '3 h left · resets 4:00 PM' },
      { key: 'seven_day', label: 'All models · 7 days', utilization: 93, elapsed_pct: 55, tone: 'warn', foot: '3 d left · resets Oct 11 12:00 AM' },
    ] };
  const m = { default_model: 'claude-opus-5', starts_on: 'claude-opus-5', explicit: false,
    rungs: [{ model: 'claude-opus-5', open: true }, { model: 'claude-sonnet-5', open: false, why: 'day cap' }] };
  const h = cfgPlanHTML(q, m);
  assert.match(h, /<strong>Claude<\/strong><span class="small muted">Claude Code · \$100\/mo · charged Oct 1<\/span>/);
  assert.match(h, /<a class="cfg-month" href="#\/spend"><span class="n">\$23<\/span><span class="small muted">this month<\/span><\/a>/);
  assert.match(h, /<span class="small">All models · 5 hours<\/span>\s*<span class="small n">82% left<\/span>\s*<div class="meter "><div style="width:18%"><\/div><i class="tick" style="left:40%"><\/i><\/div>/);
  // 7% left: red, named at the top with the hub's own reset words.
  assert.match(h, /<div class="cfg-alert">\s*<strong>All models · 7 days nearly out<\/strong><span>7% left · resets Oct 11 12:00 AM<\/span>/);
  assert.match(h, /<div class="cfg-lim low">\s*<span class="small">All models · 7 days<\/span>\s*<span class="small n" style="color:var\(--red\)">7% left<\/span>\s*<div class="meter bad">/);
  // The ladder: the rung new sessions start on filled, the shut one struck through, auto on.
  assert.match(h, /<button class="sm on" onclick="setDefaultModel\('claude-opus-5'\)">opus 5<\/button>/);
  assert.match(h, /<button class="sm" disabled title="day cap" style="text-decoration:line-through;opacity:.5">sonnet 5<\/button>/);
  assert.match(h, /<button class="sm on" onclick="setDefaultModel\(''\)">auto<\/button>/);
  // No price known: the plan line is just what it runs through.
  const bare = cfgPlanHTML({ available: false, error: 'no token', plan: { name: 'Claude', via: 'Claude Code', month_usd: 0 } }, null);
  assert.match(bare, /<strong>Claude<\/strong><span class="small muted">Claude Code<\/span>/);
  assert.match(bare, /<div class="small err">no token<\/div>/);
  assert.doesNotMatch(bare, /cfg-model/);
  assert.match(cfgPlanHTML({}, null), /the plan/);
});

test('config: goals are the active ones as tiles, each the door to its page', () => {
  const h = cfgGoalsHTML([
    { id: 'run', title: 'Run a half marathon', status: 'active', emblem: { hue: 10, symbol: 'health' } },
    { id: 'old', title: 'Done with this', status: 'done', emblem: { hue: 100 } },
  ]);
  assert.match(h, /<a class="cfg-goal" href="#\/goals\/run" style="--h:10">\s*<span class="emblem" style="--h:10">/);
  assert.match(h, /Run a half marathon/);
  assert.doesNotMatch(h, /Done with this/);
  assert.match(cfgGoalsHTML([]), /No active goals\./);
});

// ---- composer.js: the one box every surface writes back through ----
// It is pure render on the way in (composerHTML/composerReHTML) and only
// touches the DOM when a box is on screen, so it loads here beside ui.js. The
// paste listener at the bottom needs a `document`; a stub with the one method
// it calls is enough, and no test exercises it.
globalThis.document = { addEventListener() {}, getElementById: () => null, querySelector: () => null, querySelectorAll: () => [] };
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'composer.js'), 'utf8'), { filename: 'composer.js' });

test('composerHTML: one box, its key on it, chips + card + words + bar', () => {
  const html = composerHTML('cal:cal-7f0e1d2c', {
    re: { ref: 'cal', id: 'cal-7f0e1d2c', title: 'Buy tranche 2', fixed: true, outcome: '',
      outcomes: [{ value: 'done', label: 'Done' }, { value: '', label: 'Reply' }],
      means: { '': 'the step stays open', done: 'the step closes as done' } },
    inline: true, placeholder: 'Say what happened', send: () => {},
  });
  // The key rides on the box (paste routing) and every element hangs off the
  // flattened form of it, because a colon is not a bare CSS selector.
  assert.match(html, /<div class="composer inline" id="c-cal-cal-7f0e1d2c" data-c="cal:cal-7f0e1d2c">/);
  assert.match(html, /<div class="chips" id="c-cal-cal-7f0e1d2c-chips">/);
  assert.match(html, /<textarea id="c-cal-cal-7f0e1d2c-draft" rows="1" placeholder="Say what happened">/);
  assert.match(html, /id="c-cal-cal-7f0e1d2c-file"/);
  assert.match(html, /composerSend\('cal:cal-7f0e1d2c'\)/);
  // A fixed card draws its chips alone: the title is the row above it, and
  // there is no × — the box belongs to that one card.
  assert.match(html, /composerPickOutcome\('cal:cal-7f0e1d2c','done',0\)">Done</);
  assert.doesNotMatch(html, /composerClearRe/);
  assert.match(html, /the step stays open/);
  // The lit chip survives the page's own redraw with the same card.
  composerPickOutcome('cal:cal-7f0e1d2c', 'done');
  assert.match(composerReHTML('cal:cal-7f0e1d2c'), /class="sm primary"[^>]*>Done</);
  assert.match(composerHTML('cal:cal-7f0e1d2c', { re: { ref: 'cal', id: 'cal-7f0e1d2c', fixed: true, outcome: '', outcomes: [{ value: 'done', label: 'Done' }] } }),
    /class="sm primary"[^>]*>Done</);
});

// A card that is armed, not fixed (the chat answering an ask), names what it
// answers and offers the way back to a plain message.
test('composerReHTML: an armed card carries its title and ×', () => {
  composerReset('chat');
  composerHTML('chat', { re: { ref: 'ask', id: 'ask-b755', title: 'Approve the tranche', label: 'Answering',
    href: '#/sessions/t-1/ask-b755', focus: 'focusAsk', outcome: 'done',
    outcomes: [{ value: 'done', label: 'I did this' }], means: { done: 'the card leaves your board' } } });
  const strip = composerReHTML('chat');
  assert.match(strip, /<span class="small muted">Answering<\/span>/);
  assert.match(strip, /href="#\/sessions\/t-1\/ask-b755"/);
  assert.match(strip, /composerDropRe\('chat',0\)/);
  assert.match(strip, /the card leaves your board/);
  composerReset('chat');
});

// Several cards on one Send: mark a read and accept a rec together, the
// agent gets both, and both close. Arming adds a strip
// with its own chips and ×; the first card stays `re` (every single-card
// reader), the rest ride in `also`; a pick lands on ITS card; one × drops one.
test('composerArm: a second card joins the strip, and Send carries them all', async () => {
  composerReset('chat');
  const sent = [];
  composerHTML('chat', { targets: [], when: false,send: p => { sent.push(p); } });
  const read = { ref: 'ask', id: 'ask-8d1a', title: 'All three yes', label: 'Replying to', bare: true, confirmEmpty: true,
    outcome: 'done', outcomes: [{ value: 'done', label: 'Read it' }] };
  const rec = { ref: 'rec', id: 'rec-62e5', title: 'Average $2,100 into AMZN', label: '💡 Rec', outcome: 'accepted',
    outcomes: [{ value: 'accepted', label: 'Accept' }, { value: 'declined', label: 'Decline' }, { value: '', label: 'Reply' }],
    means: { accepted: 'recorded as accepted', declined: 'recorded as declined', '': 'stays open' } };
  composerArm('chat', read);
  composerArm('chat', rec);
  const st = composerState('chat');
  assert.equal(st.re.id, 'ask-8d1a');
  assert.deepEqual(st.also.map(r => r.id), ['rec-62e5']);
  // The same card again only changes its pick.
  composerArm('chat', { ...rec, outcome: 'declined' });
  assert.equal(st.also.length, 1);
  assert.equal(st.also[0].outcome, 'declined');
  const strip = composerReHTML('chat');
  assert.equal((strip.match(/class="reply-strip"/g) || []).length, 2);
  assert.match(strip, /All three yes[\s\S]*composerDropRe\('chat',0\)[\s\S]*Average \$2,100[\s\S]*composerDropRe\('chat',1\)/);
  assert.match(strip, /composerPickOutcome\('chat','accepted',1\)/);
  assert.match(strip, /recorded as declined/);
  composerPickOutcome('chat', 'accepted', 1);
  assert.equal(st.also[0].outcome, 'accepted');
  assert.equal(st.re.outcome, 'done');
  // A read is armed, so an empty Send asks first; the second press sends every
  // card — words are optional because each has a pick.
  await composerSend('chat');
  assert.equal(sent.length, 0);
  await composerSend('chat');
  assert.equal(sent.length, 1);
  assert.equal(sent[0].re.id, 'ask-8d1a');
  assert.equal(sent[0].outcome, 'done');
  assert.deepEqual(sent[0].also.map(r => [r.id, r.outcome]), [['rec-62e5', 'accepted']]);
  assert.equal(st.re, null);
  assert.equal(st.also.length, 0);
  // One × drops one card: dropping the first promotes the next.
  composerArm('chat', read);
  composerArm('chat', rec);
  composerDropRe('chat', 0);
  assert.equal(st.re.id, 'rec-62e5');
  assert.equal(st.also.length, 0);
  composerArm('chat', read);
  composerDropRe('chat', 1);
  assert.equal(st.re.id, 'rec-62e5');
  assert.equal(st.also.length, 0);
  composerClearRe('chat');
  assert.equal(st.re, null);
  composerReset('chat');
});

// A read card armed in the chat is only the banner — "Replying to <title>" and
// its × — and an empty Send asks first ("Are you sure?" · "Send without").
// The second press sends; typing puts plain Send back.
test('composer: a read reply is a banner, and an empty Send asks "Are you sure?"', async () => {
  composerReset('chat');
  const sent = [];
  composerHTML('chat', { targets: [], when: false,send: p => { sent.push(p); },
    re: { ref: 'ask', id: 'ask-b755', title: 'Movie site shipped', label: 'Replying to', bare: true, confirmEmpty: true,
      outcome: 'done', outcomes: [{ value: 'done', label: 'Read it' }], means: { done: 'closes' } } });
  const strip = composerReHTML('chat');
  assert.match(strip, /Replying to<\/span>[\s\S]*Movie site shipped[\s\S]*composerDropRe/);
  assert.doesNotMatch(strip, /composerPickOutcome|closes/);
  await composerSend('chat');
  assert.equal(sent.length, 0);
  assert.match(composerActionsHTML('chat'), /Are you sure\?[\s\S]*>Send without</);
  await composerSend('chat');
  assert.equal(sent.length, 1);
  assert.equal(sent[0].outcome, 'done');
  assert.equal(sent[0].text, '');
  assert.match(composerActionsHTML('chat'), />Send</);
  composerReset('chat');
});

// A session's chat box has no route: the box the chat draws carries no
// session or time select, and Send goes now, to this session — the default
// box keeps both for the others.
test('composerHTML: the session chat has no route selects', async () => {
  composerReset('chat');
  const sent = [];
  const html = composerHTML('chat', { targets: [], when: false, send: p => { sent.push(p); } });
  assert.doesNotMatch(html, /-route|This session|New session|In an hour/);
  assert.match(composerHTML('other', { send: () => {} }), /This session[\s\S]*In an hour/);
  composerState('chat').text = 'go';
  await composerSend('chat');
  assert.equal(sent[0].target, 'this');
  assert.equal(sent[0].when, 'now');
  composerReset('chat');
});

globalThis.window = globalThis.window || { addEventListener() {} };

// The same message posted twice: a send held 21 s
// by a hub mid-restart, the button said nothing, ⌘↵ is not a button and was
// never disabled, so the second press posted the same words again.
test('composer: one message in flight per box — a second Send or ⌘↵ is dropped, the button says Sending…', async () => {
  composerReset('chat');
  const box = { value: 'hello there', style: {}, scrollHeight: 0, focus() {}, selectionStart: 0, selectionEnd: 0 };
  const send = { disabled: false, textContent: 'Send', dataset: {} };
  const doc = globalThis.document, oldGet = doc.getElementById, oldAll = doc.querySelectorAll;
  doc.getElementById = id => (id === 'c-chat-draft' ? box : id === 'c-chat-send' ? send : null);
  doc.querySelectorAll = sel => (sel === '#c-chat-acts button' ? [send] : []);
  let sent = 0, release = null;
  composerHTML('chat', { targets: [], when: false,send: () => new Promise(r => { sent++; release = r; }) });
  const first = composerSend('chat');
  assert.equal(sent, 1);
  assert.equal(send.disabled, true);
  assert.equal(send.textContent, 'Sending…');
  await composerSend('chat');
  await composerSend('chat');
  assert.equal(sent, 1);
  release(); await first;
  assert.equal(send.disabled, false);
  assert.equal(send.textContent, 'Send');
  assert.equal(box.value, '');
  // Landed: the next message goes.
  box.value = 'again';
  const second = composerSend('chat');
  assert.equal(sent, 2);
  release(); await second;
  doc.getElementById = oldGet; doc.querySelectorAll = oldAll; composerReset('chat');
});

// ---- views/calendar.js: a closed row looks closed, the way the phone draws it ----
// calgrid.js comes with it, in index.html's order: the row's kind pill takes
// its colour from the three lanes defined there (calLaneOf).
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'calendar.js'), 'utf8'), { filename: 'calendar.js' });
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'calgrid.js'), 'utf8'), { filename: 'calgrid.js' });

test('calEntryHTML: done is ✓ (never struck), dismissed ✕ + struck, open is ○ and keeps its buttons', () => {
  // The hub stamps `item`, `lane`, `closed` and `mark` on every row
  // (calendar.go stampEntry); they, not the id's prefix, say what the row is.
  const row = { id: 'cal-1a2b3c4d', ref: 'cal:cal-1a2b3c4d', kind: 'owner', title: 'Buy tranche 2', day: '2026-08-27', state: 'done', overdue: true,
    lane: 'mine', item: true, closed: true, mark: 'done' };
  const done = calEntryHTML(row);
  // A done step must not look open: it is muted, not bolded, and a closed
  // row is never drawn as overdue. But NOT struck — ✓ plus a line reads as
  // its own opposite; the line means won't-do, never done.
  assert.match(done, /<div class="card cal closed">/);
  assert.match(done, /<span class="tick">✓<\/span>Buy tranche 2/);
  assert.doesNotMatch(done, /<s>/);
  assert.match(done, /<span class="small muted">done<\/span>/);
  // A closed item keeps its undo for a stray tap — Reopen, and nothing else.
  assert.match(done, /resolveCal\('cal-1a2b3c4d','scheduled'\)"[^>]*>Reopen</);
  assert.doesNotMatch(done, /composerSend|'done'\)/);
  // The row's session, live (2026-09-26): the board's own pills beside
  // "open session", so the step you just answered from says its session is on
  // it — and nothing at all on a quiet session (a row never says "idle").
  const busy = calEntryHTML({ ...row, thread_id: 't-1', live: [{ word: 'speaking', tone: 'speaking' }, { word: 'running', tone: 'running' }] });
  assert.match(busy, /<a href="#\/sessions\/t-1">open session<\/a> <span class="pill speaking"><span class="dot pulse"><\/span>speaking<\/span><span class="pill running"><span class="dot pulse"><\/span>running<\/span>/);
  assert.doesNotMatch(calEntryHTML({ ...row, thread_id: 't-1' }), /class="pill (running|speaking|waiting)"/);
  const gone = calEntryHTML({ ...row, state: 'dismissed', mark: 'wont' });
  assert.match(gone, /card cal closed/);
  assert.match(gone, /<span class="tick">✕<\/span><s>Buy tranche 2<\/s>/);
  assert.match(gone, /Reopen</);
  // The hub stamps an open step's answers (store/close.go StepOutcomes).
  const open = calEntryHTML({ ...row, state: 'scheduled', closed: false, mark: 'todo',
    outcomes: [{ value: 'done', label: 'Done' }, { value: 'wont', label: "Won't do" }, { value: '', label: 'Reply' }] });
  assert.match(open, /<div class="card cal overdue">/);
  assert.match(open, /<span class="tick">○<\/span><strong>Buy tranche 2<\/strong>/);
  // Its minute passing never closes it — it says so, in red, until it is answered.
  assert.match(open, />overdue<\/span>/);
  // The owner's own open step closes like a rec — through THE composer: Done · Won't
  // do · Reply as chips over a box that takes a pasted picture. Never a
  // bare Done button.
  assert.doesNotMatch(open, /resolveCal\(/);
  assert.match(open, /<div class="composer inline" id="c-cal-cal-1a2b3c4d" data-c="cal:cal-1a2b3c4d">/);
  assert.match(open, /<textarea id="c-cal-cal-1a2b3c4d-draft"/);
  assert.match(open, /composerPickOutcome\('cal:cal-1a2b3c4d','done',0\)">Done</);
  assert.match(open, /composerPickOutcome\('cal:cal-1a2b3c4d','wont',0\)">Won&#39;t do</);
  assert.match(open, /composerPickOutcome\('cal:cal-1a2b3c4d','',0\)">Reply</);
  assert.match(open, /id="c-cal-cal-1a2b3c4d-file"/);      // Attach
  // Responding to a card means now, in this session (build 773): no pickers.
  assert.doesNotMatch(open, /id="c-cal-cal-1a2b3c4d-when"/);
  assert.match(open, /composerSend\('cal:cal-1a2b3c4d'\)/);
  // Only the owner's own steps are bold; an agent run is regular weight, as
  // on the phone — and ○ marks the owner's to-dos alone, so the grey lane never wears one.
  const agent = calEntryHTML({ ...row, kind: 'agent', state: 'scheduled', closed: false, lane: 'scheduled', mark: '' });
  assert.doesNotMatch(agent, /<strong>/);
  assert.doesNotMatch(agent, /○/);
  // An agent item keeps the plain pair: the words rule is the owner's alone.
  assert.match(agent, /resolveCal\('cal-1a2b3c4d','done'\)/);
  assert.doesNotMatch(agent, /class="composer/);
});
