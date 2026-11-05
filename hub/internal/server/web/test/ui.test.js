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
  assert.equal(refHref('rec:rec-34a2a000'), '#/recs/rec-34a2a000');
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

// A bare id in card text is a link (rec-caa3e181). The table is the phone's
// too (RefTests.swift): text → the ids that come out linked, in order.
test('mdInline: bare hub ids link, shared/ref-cases.json', () => {
  const cases = JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', '..', '..', '..', 'shared', 'ref-cases.json'), 'utf8'));
  for (const [src, want] of cases.links) {
    const got = [...mdInline(src).matchAll(/<a href="#\/(?:recs|open)\/([^"]+)">/g)].map(m => m[1]);
    assert.deepEqual(got, want, src);
  }
  assert.equal(mdInline('Decide rec-120a5f85'), 'Decide <a href="#/recs/rec-120a5f85">rec-120a5f85</a>');
  assert.equal(mdInline('see `ask-955bfb8e`'), 'see <a href="#/open/ask-955bfb8e"><code>ask-955bfb8e</code></a>');
  // links=false (inside a clickable card) and a preview stay link-free
  assert.equal(mdInline('Decide rec-120a5f85', false), 'Decide rec-120a5f85');
  assert.equal(mdPreview('- Decide rec-120a5f85'), '• Decide rec-120a5f85');
});

// The table the hub (internal/format) and the phone (FormatTests.swift) read:
// money and counts take a number, days, months and clocks a hub string.
test('money, counts, days, months and clocks: shared/format-cases.json', () => {
  const cases = JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', '..', '..', '..', 'shared', 'format-cases.json'), 'utf8'));
  const fns = { usd, usdWhole: usd0, usdSigned, usdWholeSigned: usd0Signed, usdShort, usdPrice, usdCents, commas: num,
    day: shortDay, dayLabel, fullDay, longDay, weekDay, month: monthName, monthLong, monthWord, monthShort, monthYY, clock };
  for (const [name, rows] of Object.entries(cases)) {
    if (name === '_') continue;
    assert.ok(fns[name], `no console formatter for ${name}`);
    for (const [v, want] of rows) assert.equal(fns[name](v), want, `${name}(${v})`);
  }
});

// A day's year is noise until it changes (the phone's dayWords says the
// same, FormatTests); a month always carries its year (10-12).
test('dayWords: the year only when it is not this one', () => {
  const now = new Date(2026, 9, 11, 12);
  assert.equal(dayWords('2026-10-03', now), 'Oct 3');
  assert.equal(dayWords('2025-10-03', now), 'Oct 3, 2025');
  assert.equal(dayWords('2027-01-01', now), 'Jan 1, 2027');
  assert.equal(monthName('2026-08'), 'Aug 2026');
  assert.equal(clock(new Date(2026, 9, 11, 15, 5)), '3:05 PM');
  assert.equal(clock('2026-10-11T00:53:00'), '12:53 AM');
  assert.equal(shortDay(new Date(2026, 9, 11, 15, 5)), 'Oct 11, 2026');
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
  assert.equal(mdInline('[docs](https://x.test/a)'), '<a href="https://x.test/a" target="_blank" rel="noopener">docs</a>');
  assert.equal(mdInline('see https://x.test/a'), 'see <a href="https://x.test/a" target="_blank" rel="noopener">https://x.test/a</a>');
  // links=false: inside a clickable card, so no nested anchors
  assert.equal(mdInline('[docs](https://x.test/a) https://x.test/b', false), 'docs https://x.test/b');
  assert.equal(mdInline('<b>'), '&lt;b&gt;');
});

// GFM's rule: the sentence's trailing punctuation is not part of a bare URL —
// "…pull/82, merge when ready" linked the comma and 404ed (2026-09-06).
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

// A long detail pops open and shut (2026-10-26: the in-card
// pagination is gone, "put it back to the way that it was"): the clamped box
// and one button, hidden until fitFolds counts the lines; open, Show less.
test('askBody: a long detail folds with one open/shut button, no paging', () => {
  const long = Array(80).fill('a line of the log').join('\n');
  const h = askBody(long, 'ask:ask-1a2b3c4d');
  assert.match(h, /^<div class="ask-long"><p>a line of the log/);
  assert.match(h, /<button type="button" class="fold small" hidden onclick="toggleCardLong\(this,'ask:ask-1a2b3c4d'\)">Show 0 more lines<\/button>/);
  assert.doesNotMatch(h, /Back up|Show more<|--off|overflow|scroll/);
  // A short detail is just its markdown.
  assert.equal(askBody('- Mon\n- Tue', 'ask:x'), '<ul><li>Mon</li><li>Tue</li></ul>');
  assert.equal(foldLabel(false, 42), 'Show 42 more lines');
  assert.equal(foldLabel(true, 42), 'Show less');
  // Open stays open across the redraw.
  cardLongOpen.add('ask:ask-1a2b3c4d');
  assert.match(askBody(long, 'ask:ask-1a2b3c4d'), /^<div class="ask-long open">[\s\S]*>Show less<\/button>/);
  cardLongOpen.delete('ask:ask-1a2b3c4d');
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

test('md: a line that is only a hub file draws the file; anywhere else it is a link', () => {
  const ref = 'sha256/a6/a620d8.png';
  for (const line of [`![before](https://my-mac.tail0000.ts.net:8443/api/v1/blobs/${ref})`,
                      `[shot](https://host:8443/api/v1/blobs/${ref})`,
                      `https://host:8443/api/v1/blobs/${ref}`, `  /api/v1/blobs/${ref}  `]) {
    assert.equal(blobLineRef(line), ref, line);
  }
  const html = md(`**Now**\n\n![now](https://h:8443/api/v1/blobs/${ref})\n\nafter`);
  assert.ok(html.startsWith('<p><strong>Now</strong></p><div class="md-files"><figure><a href="/api/v1/blobs/sha256/a6/a620d8.png"'), html);
  assert.ok(html.includes('<img src="/api/v1/blobs/sha256/a6/a620d8.png"') && html.includes('<figcaption>now</figcaption>') && html.endsWith('<p>after</p>'), html);
  // Files on lines in a row, blank lines between, are one strip of tiles.
  const two = md(`![before](/api/v1/blobs/sha256/00/b.png)\n\n![after](/api/v1/blobs/sha256/00/a.png)\nend`);
  assert.equal((two.match(/class="md-files"/g) || []).length, 1, two);
  assert.equal((two.match(/<figure>/g) || []).length, 2, two);
  assert.ok(two.endsWith('<p>end</p>'), two);
  // Not a picture: a 📎 chip.
  assert.ok(md('[bill](https://h/api/v1/blobs/sha256/00/abc.pdf)').includes('📎 abc.pdf'));
  // A drawn SVG is a picture (2026-10-20: "Svgs don't open").
  assert.ok(md('![grid](https://h/api/v1/blobs/sha256/00/g.svg)').includes('<img src="/api/v1/blobs/sha256/00/g.svg"'));
  // A sound: a player in place.
  assert.ok(md('[af_heart](https://h/api/v1/blobs/sha256/00/v.m4a)').includes('<audio controls preload="none" src="/api/v1/blobs/sha256/00/v.m4a"></audio>'));
  // An .html page is a file to download or open in a browser, never a viewer
  // in the app (2026-10-26): a thumbnail of its top, Download named by
  // the link's text, Open in browser in its own tab.
  const page = md('[life, day by day](https://h/api/v1/blobs/sha256/28/28bf.html)');
  assert.ok(page.includes('<div class="att-thumb"><iframe src="/api/v1/blobs/sha256/28/28bf.html" sandbox="" scrolling="no" tabindex="-1"'), page);
  assert.ok(page.includes('<a href="/api/v1/blobs/sha256/28/28bf.html" download="life, day by day.html" class="small att-file">Download</a>'), page);
  assert.ok(page.includes('<a href="/api/v1/blobs/sha256/28/28bf.html" target="_blank" class="small att-file">Open in browser</a>'), page);
  assert.ok(page.includes('<figcaption>life, day by day</figcaption>') && !page.includes('📎'), page);
  // No link text: the blob's own name. A message attachment has no caption.
  assert.equal(htmlFileName('sha256/28/28bf.html', ''), '28bf.html');
  assert.equal(htmlFileName('sha256/28/28bf.html', 'a/b: c'), 'a-b- c.html');
  assert.ok(attachHTML('sha256/28/28bf.html').includes('download="28bf.html"'));
  // A broken picture's fallback never carries the ref in its handler: the
  // browser decodes &#39; to a quote before it runs one (GHSA-98ww-qvvc-42wq).
  const evil = attachHTML(`x'+alert(1)+'.png`);
  assert.ok(evil.includes('onerror="attFallback(this)"') && !/onerror="[^"]*alert/.test(evil), evil);
  // Inside a sentence, in a list, or climbing out of the blob route: not a file.
  assert.equal(blobLineRef(`see ![x](https://h/api/v1/blobs/${ref}) here`), null);
  assert.equal(blobLineRef(`- ![x](https://h/api/v1/blobs/${ref})`), null);
  assert.equal(blobLineRef('/api/v1/blobs/../secret'), null);
  assert.equal(mdInline('see ![x](https://h/a.png) here'), 'see <a href="https://h/a.png" target="_blank" rel="noopener">x</a> here');
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
  assert.equal(mdPreview('- **Did:** x\n\n# Next\nsee https://x.test'), '• <strong>Did:</strong> x Next see https://x.test');
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
const outcomes = [{ value: 'done', label: 'Decided' }, { value: '', label: 'Reply' }];
const ask = { id: 'ask-1a2b3c4d', thread_id: 't-1a2b3c4d', thread_title: 'Plan lunches', title: 'Pick a **day**',
  detail: '- Mon\n- Tue', kind: 'decision', verb: 'decide', surface: 'any', state: 'open', open: true, closed: false, created_at: new Date().toISOString(), outcomes };
// Where a row stands is the hub's (store.AskStanding / ActionStanding /
// RecStanding): the fixtures carry it the way the wire does.
const WAITING = { open: false, closed: false }, CLOSED = { open: false, closed: true, reopen: true };
const DISMISSED = { ...CLOSED, folded: 'dismissed' };
const readAsk = { ...ask, kind: 'read', outcomes: [{ value: 'done', label: 'Read it' }] };

// One card shape (2026-09-18): the row is the hub's words in the hub's order
// (decisive first and primary, "Reply" last), then Dismiss — each answer
// arms the session's composer with that pick (outcome, words, which session,
// when), and the strip's chips come in the same order ("Buttons are in
// a different order on the bar than on the card").
test('askHTML in the chat: bubble, id for deep links, the hub\'s words + Dismiss', () => {
  const h = askHTML(ask);
  assert.match(h, /^<div class="ask" id="ask-card-ask-1a2b3c4d">/);
  assert.match(h, /🔴 <span class="verb">[a-z ]+<\/span><span class="at">[^<]*<\/span><\/div><div class="t">Pick a <strong>day<\/strong>/);
  assert.match(h, /<ul><li>Mon<\/li><li>Tue<\/li><\/ul>/);
  assert.match(h, /<div class="acts"><button class="sm suggested" onclick="openRespond\('ask-1a2b3c4d','done'\)">Decided<\/button><button class="sm" onclick="openRespond\('ask-1a2b3c4d',''\)">Reply<\/button><button class="sm" onclick="cardFold\('ask','ask-1a2b3c4d'\)">Dismiss<\/button><\/div>/);
  // No Open page: the card in the chat IS the page (2026-09-15).
  assert.doesNotMatch(h, /resolveAskGlobal|>Done<|>Open|target="_blank"|>Respond</);
  // A hub that sent no words: one Respond, the composer's default.
  assert.match(askHTML({ ...ask, outcomes: undefined }), /openRespond\('ask-1a2b3c4d'\)">Respond<[\s\S]*>Dismiss</);
});

// The pick on the bar is the FILLED button, and only that one; with nothing
// armed the first pick is outlined (suggested), never filled, so arming it
// shows (2026-10-11: "if you hit accept… the 'accept' doesn't highlight
// at all"). '' (Reply) is a real pick.
test('askHTML / actionHTML: the armed pick is primary, the rest plain', () => {
  const wont = askHTML(ask, false, 'done');
  assert.match(wont, /<button class="sm primary" onclick="openRespond\('ask-1a2b3c4d','done'\)">Decided<\/button><button class="sm" onclick="openRespond\('ask-1a2b3c4d',''\)">Reply</);
  assert.doesNotMatch(wont, /suggested/);
  const reply = askHTML(ask, false, '');
  assert.match(reply, /<button class="sm primary" onclick="openRespond\('ask-1a2b3c4d',''\)">Reply</);
  assert.doesNotMatch(reply, /suggested|sm primary" onclick="openRespond\('ask-1a2b3c4d','done'\)/);
  // A read's Respond: outlined until it is on the bar, filled once it is.
  assert.match(askHTML(readAsk), /<button class="sm suggested" onclick="openRespond\('ask-1a2b3c4d'\)">Respond</);
  assert.match(askHTML(readAsk, false, 'done'), /<button class="sm primary" onclick="openRespond\('ask-1a2b3c4d'\)">Respond</);
  assert.match(askHTML({ ...ask, outcomes: undefined }, false, ''), /<button class="sm primary" onclick="openRespond\('ask-1a2b3c4d'\)">Respond</);
  // Install, Restart: one-shot acts, filled as ever.
  assert.match(askHTML({ ...ask, kind: 'error', detail: 'boom' }), /<button class="sm primary" onclick="retryAsk\('ask-1a2b3c4d'\)">Restart</);
});

// A read card is Respond (blue, arms the reply) + Read (white, the silent
// close: done with the hub's "Read it", never dismissed). 2026-09-17:
// "even when I dismiss it, I will have had to read it… respond… takes the
// place of read, and read effectively takes the place of dismiss".
test('askHTML in the chat: a read card is Respond (arms the reply) + Read (closes, "Read it")', () => {
  const h = askHTML(readAsk);
  assert.match(h, /openRespond\('ask-1a2b3c4d'\)">Respond<[\s\S]*resolveAsk\('ask-1a2b3c4d','done','Read it'\)">Read</);
  assert.doesNotMatch(h, /resolveAskGlobal|>Done<|>Read it<|>Reply<|>Dismiss<|'dismissed'/);
  const other = askHTML(ask);
  // (">Reply<" is the hub's own third outcome since 09-19, asserted above.)
  assert.doesNotMatch(other, />Read<|>Done<|>Respond</);
  assert.match(other, />Decided<[\s\S]*>Dismiss</);
});

// Four cells (2026-08-28): a read card is blue, never red — it is an
// answer to read, not a stopped session. Answered/closed drop the tint like
// any other card.
test('askHTML: a read card is blue', () => {
  const chat = askHTML({ ...ask, kind: 'read', verb: 'read' });
  assert.match(chat, /^<div class="ask read" id="ask-card-ask-1a2b3c4d">/);
  assert.match(chat, /🔵 <span class="verb">read<\/span><span class="at">[^<]*<\/span><\/div><div class="t">Pick/);
  assert.doesNotMatch(chat, /🔴/);
  // Replying does not turn it red (2026-08-28: "after I responded, they
  // turn red") — it is still a read, now waiting on the agent to close it.
  assert.match(askHTML({ ...readAsk, state: 'answered', ...WAITING }), /^<div class="ask read" id=/);
  // Closed by his reply (resolution = the reply's first line): one row.
  assert.match(askHTML({ ...readAsk, state: 'done', resolution: 'thanks', ...CLOSED }), /^<details class="chat-row" id=/);
});

// "Once I read it, I can just dismiss it" (2026-08-28) — every open card
// but a read carries it; a read's white Read button is its silent close
// (2026-09-17).
test('askHTML: every open card but a read offers Dismiss', () => {
  assert.match(askHTML(ask), /Dismiss<\/button>/);
  assert.match(askHTML(ask), /cardFold\('ask','ask-1a2b3c4d'\)/);
  assert.doesNotMatch(askHTML({ ...ask, state: 'done', ...CLOSED }), /Dismiss<\/button>/);
  // Dismissed keeps a footprint (2026-09-15): one row that opens to the
  // whole card and Reopen (mock B3, 2026-10-25: every closed card is a row).
  const gone = askHTML({ ...readAsk, state: 'dismissed', ...DISMISSED });
  assert.match(gone, /^<details class="chat-row" id="ask-card-ask-1a2b3c4d" data-k="ask:ask-1a2b3c4d" data-def="0">/);
  assert.match(gone, /<span class="k">✕ dismissed<\/span><span class="t">Pick a <strong>day<\/strong><\/span><span class="when">today /);
  assert.match(gone, /<ul><li>Mon<\/li><li>Tue<\/li><\/ul>/);
  assert.match(gone, /cardFold\('ask','ask-1a2b3c4d',true\)">Reopen</);
  assert.match(askHTML({ ...ask, state: 'dismissed', ...DISMISSED }, true), /data-def="0" open>/);
  assert.doesNotMatch(askHTML(readAsk), /Dismiss<\/button>/);
  // A read he marked Read (done, "Read it") is headed "read"; any other close
  // by its state.
  const readIt = askHTML({ ...readAsk, state: 'done', resolution: 'Read it', ...CLOSED, folded: 'read' });
  assert.match(readIt, /^<details class="chat-row" id="ask-card-ask-1a2b3c4d"/);
  assert.match(readIt, /<span class="k">✓ read<\/span>/);
  assert.match(readIt, /Reopen</);
  assert.match(askHTML({ ...ask, state: 'done', resolution: 'Read it', ...CLOSED }), /<span class="k">✓ done<\/span>/);
});

// Every closed card reopens but an install (2026-09-29: "every kind of
// cell should be reopenable except for the install cells") — the hub's
// `reopen` says which; the row's label carries the state word.
test('askHTML: a done card carries Reopen when the hub says reopen', () => {
  const done = askHTML({ ...ask, kind: 'physical', state: 'done', ...CLOSED });
  assert.match(done, /<div class="acts"><button class="sm" onclick="cardFold\('ask','ask-1a2b3c4d',true\)">Reopen<\/button><\/div>/);
  assert.doesNotMatch(askHTML({ ...ask, kind: 'install', state: 'done', ...CLOSED, reopen: false }), /Reopen/);
});

// The install cell (2026-09-08): title, description, one teal Install
// button — no Respond furniture. The button carries the OTA link itself, so
// the link line is stripped from the shown detail; Dismiss stays as the out.
test('askHTML: an install card is teal with one Install button, link line stripped', () => {
  const inst = { ...ask, kind: 'install', verb: 'install', title: 'Install app build 812 (tap the link)',
    detail: 'The install ask now draws as this teal cell.\n[Install build 812](https://hub.test/ota/tok/install.html)' };
  const chat = askHTML(inst);
  assert.match(chat, /^<div class="ask install" id="ask-card-ask-1a2b3c4d">/);
  assert.match(chat, /📲 <span class="verb">install<\/span><span class="at">[^<]*<\/span><\/div><div class="t">Install app build 812/);
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
  assert.match(askHTML({ ...inst, state: 'done', ...CLOSED }), /^<details class="chat-row"/);
});

// The desktop app's install cell (2026-09-30: "an install cell with the
// word install, and myself clicking it should do the thing where it kind of
// builds… in the background and then restart"): the same teal card, but its
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
  assert.match(chat, /⏳ <span class="verb">[a-z ]+<\/span><span class="at">[^<]*<\/span><\/div><div class="t">Pick/);
  assert.match(chat, /Decided/);
  const done = askHTML({ ...ask, state: 'done', ...CLOSED });
  assert.match(done, /<summary><span class="k">✓ done<\/span><span class="t">Pick/);
  // Reopen is the one button a closed card keeps (2026-09-29).
  assert.match(done, /<div class="acts"><button class="sm" onclick="cardFold\('ask','ask-1a2b3c4d',true\)">Reopen<\/button><\/div>/);
  assert.doesNotMatch(done, /Decided|Respond|Dismiss/);
});

test('askHTML: an error is one line and Restart', () => {
  const err = { ...ask, kind: 'error', verb: 'restart', outcomes: [], title: 'Run died' };
  const chat = askHTML(err);
  assert.match(chat, /class="ask err-card" id="ask-card-ask-1a2b3c4d"/);
  assert.match(chat, /⚠︎ <span class="verb">restart<\/span><span class="at">[^<]*<\/span><\/div><div class="t">Run died/);
  assert.match(chat, /retryAsk\('ask-1a2b3c4d'\)">Restart/);
  assert.match(chat, /cardFold\('ask','ask-1a2b3c4d'\)">Dismiss/);
  assert.doesNotMatch(chat, /Done|Reply|<ul>/);
});

const actOutcomes = [{ value: 'approved', label: 'Approve' }, { value: 'denied', label: 'Deny' }, { value: '', label: 'Reply' }];
const action = { id: 'act-1a2b3c4d', thread_id: 't-1a2b3c4d', kind: 'commit', project: 'life', title: 'git commit',
  detail: 'one plain commit', exec_type: 'shell', exec_payload: 'git commit -m x', state: 'proposed', open: true, closed: false,
  created_at: new Date().toISOString(), outcomes: actOutcomes };

// The approval card is the ask card's shape (2026-09-17): 🔴 title, the
// detail, the button row at the bottom — the hub's words ARM the composer
// (armActionReply), and Dismiss is the silent close (2026-09-18:
// "approval cells should also have a dismiss button").
test('actionHTML: the approval card in the chat', () => {
  const chat = actionHTML(action);
  assert.match(chat, /^<div class="ask" id="ask-card-act-1a2b3c4d">/);
  // One time on the head line: when it was raised, the chat's own clock.
  assert.match(chat, /<div class="card-h">🔴 <span class="verb">approve<\/span><span class="at">today [^<]*<\/span><\/div><div class="t">git commit<\/div>/);
  assert.doesNotMatch(chat, /ago</);
  assert.match(chat, /<span class="pill purple">commit<\/span>/);
  assert.match(chat, /will run: shell/);
  assert.match(chat, /<div class="acts"><button class="sm suggested" onclick="armActionReply\('act-1a2b3c4d','approved'\)">Approve<\/button><button class="sm" onclick="armActionReply\('act-1a2b3c4d','denied'\)">Deny<\/button><button class="sm" onclick="armActionReply\('act-1a2b3c4d',''\)">Reply<\/button><button class="sm" onclick="cardFold\('action','act-1a2b3c4d'\)">Dismiss<\/button><\/div>/);
  assert.doesNotMatch(chat, /textarea|anote|decideAction|approve\?|Open in session/);
  // Deny on the bar: Deny filled, Approve plain.
  assert.match(actionHTML(action, false, 'denied'), /<button class="sm" onclick="armActionReply\('act-1a2b3c4d','approved'\)">Approve<\/button><button class="sm primary" onclick="armActionReply\('act-1a2b3c4d','denied'\)">Deny</);
  // Dismissed: a row like a dismissed ask, Reopen inside; the row remembers.
  const gone = actionHTML({ ...action, state: 'dismissed', ...DISMISSED });
  assert.match(gone, /^<details class="chat-row" id="ask-card-act-1a2b3c4d" data-k="action:act-1a2b3c4d" data-def="0">/);
  assert.match(gone, /<span class="k">✕ dismissed<\/span><span class="t">git commit<\/span>/);
  assert.match(gone, /<div class="acts"><button class="sm" onclick="cardFold\('action','act-1a2b3c4d',true\)">Reopen<\/button><\/div>/);
  assert.match(actionHTML({ ...action, state: 'dismissed', ...DISMISSED }, true), /data-def="0" open>/);
  // What the push SPOKE, under the detail like an ask's (2026-09-22:
  // "approve cell not showing notification sounds text"); nothing drawn
  // when the row never pushed.
  assert.doesNotMatch(chat, /class="said"/);
  // A click on it reads it again (2026-09-23: "read out on command");
  // the title line is dropped when the spoken line already says it (cd61d24).
  assert.match(actionHTML({ ...action, said: 'Hey, I need your approval. git commit.' }),
    /<button class="say-btn" data-ref="act-1a2b3c4d" data-say="Hey, I need your approval\. git commit\." data-thread="t-1a2b3c4d" onclick="sayAgain\(this\)" title="Play again"><\/button><\/div>\s*<div class="said">Hey, I need your approval\. git commit\.<\/div>/);
});

test('askHTML: a line still queued wears "Waiting to speak"; a double click drops the line, not the card', () => {
  const a = { ...ask, kind: 'read', verb: 'read', said: 'Hey, one card.', waiting_to_speak: true };
  const html = askHTML(a);
  assert.match(html, /<button class="say-btn waiting" data-ref="ask-1a2b3c4d" [^>]*ondblclick="sayHushCard\(this\)"/);
  assert.doesNotMatch(html, /onclick="sayAgain/);
  assert.match(askHTML({ ...a, waiting_to_speak: false }), /class="say-btn" [^>]*onclick="sayAgain\(this\)"/);
});

test('askHTML: a line in his ears wears "Speaking"; a double click stops it, not the card', () => {
  const html = askHTML({ ...ask, kind: 'read', verb: 'read', said: 'Hey, one card.', speaking: true });
  assert.match(html, /<button class="say-btn speaking" data-ref="ask-1a2b3c4d" [^>]*ondblclick="sayHushCard\(this\)"/);
  assert.doesNotMatch(html, /onclick="sayAgain/);
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

// A decided approval (2026-10-22: "This proposed by who business is
// probably not necessary"): no audit trail, the state where the buttons were,
// and the head's one time is when it was decided, not when it was raised.
test('actionHTML: a decided approval has no trail, and its time is the decision', () => {
  const h = actionHTML({ ...action, state: 'approved', ...CLOSED, created_at: '2000-01-01T12:00:00Z', decided_at: new Date().toISOString(), decided_via: 'app', events: [
    { id: 1, action_id: 'act-1a2b3c4d', ts: '2000-01-01T12:00:00Z', event: 'proposed', actor: 'claude:thread:t-1a2b3c4d' },
  ] });
  assert.doesNotMatch(h, /proposed by|via app|claude:thread/);
  assert.match(h, /^<details class="chat-row"/);
  assert.match(h, /<span class="k">✓ approved<\/span><span class="t">git commit<\/span><span class="when">today /);
  assert.match(h, /<div class="acts"><button class="sm" onclick="cardFold\('action','act-1a2b3c4d',true\)">Reopen<\/button><\/div>/);
  assert.doesNotMatch(h, /approve\?|decideAction|armActionReply|anote/);
  assert.match(actionHTML({ ...action, state: 'denied', ...CLOSED }), /<span class="k">✕ denied<\/span>/);
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

// The chat row's clock always says the day (2026-10-07: "this is the
// time, but not the day"); the phone's dayClock prints the same words.
test('dayWhen: today, yesterday, the weekday inside a week, the date past it', () => {
  const now = new Date(2026, 9, 7, 21, 16); // Wed Oct 7 2026, 9:16 PM local
  const at = (y, mo, d, h, mi) => new Date(y, mo, d, h, mi).toISOString();
  assert.equal(dayWhen('', now), '');
  assert.equal(dayWhen(at(2026, 9, 7, 0, 53), now), 'today 12:53 AM');
  assert.equal(dayWhen(at(2026, 9, 7, 15, 15), now), 'today 3:15 PM');
  assert.equal(dayWhen(at(2026, 9, 6, 23, 59), now), 'yesterday 11:59 PM');
  assert.equal(dayWhen(at(2026, 9, 5, 9, 15), now), 'Mon 9:15 AM');
  assert.equal(dayWhen(at(2026, 9, 1, 16, 2), now), 'Thu 4:02 PM');
  assert.equal(dayWhen(at(2026, 8, 30, 16, 2), now), 'Sep 30 4:02 PM');
  assert.equal(dayWhen(at(2025, 9, 1, 16, 2), now), 'Oct 1, 2025 4:02 PM');
});

test('turnFacts: the turn in flight in a finished turn\'s words', () => {
  const at = new Date(); at.setHours(22, 9, 0, 0);
  const t = { turn_tools: 23, turn_cost_usd: 1.12, turn_at: at.toISOString() };
  assert.equal(turnFacts(t), '23 tool calls · today 10:09 PM · $1.12');
  assert.equal(turnFacts(t, false), 'today 10:09 PM · $1.12');
  assert.equal(turnFacts(t, true, false), '23 tool calls · today 10:09 PM');
  assert.equal(turnFacts({ turn_tools: 1 }), '1 tool call');
  assert.equal(turnFacts({}), '');
  assert.equal(turnFacts(null), '');
});

test('taskRowHTML: a helper\'s kind, name, age, newest line and bar', () => {
  const now = Date.parse('2026-11-04T03:30:00Z');
  assert.equal(taskAge({ started_at: '2026-11-04T03:26:10Z' }, now), '3m');
  assert.equal(taskAge({ started_at: '2026-11-04T02:25:00Z' }, now), '1h 05m');
  assert.equal(taskAge({}, now), '');
  assert.equal(taskCount({ done: 3, total: 10 }), '3/10');
  assert.equal(taskCount({ done: 45, total: 100 }), '45%');
  assert.equal(taskCount({}), '');
  assert.equal(taskKind({ type: 'local_bash' }), 'shell');
  assert.equal(taskKind({ type: 'local_agent' }), 'subagent');
  const html = taskRowHTML({ id: 'b1', type: 'local_bash', description: 'Post <the> issues', doing: 'posted a/b', done: 3, total: 10 }, { click: "toggleTask('b1')" });
  assert.match(html, /<span class="verb">shell<\/span><span class="task-d">Post &lt;the&gt; issues<\/span>/);
  assert.match(html, /width:30%/);
  assert.match(html, /tbar-n">3\/10</);
  assert.match(html, /task-doing">posted a\/b</);
  assert.match(html, /onclick="event.preventDefault\(\);toggleTask\('b1'\)"/);
  assert.doesNotMatch(taskRowHTML({ type: 'local_agent', description: 'x' }), /tbar|task-doing|<a /);
});

test('select renders the current option selected', () => {
  assert.equal(select('k', 'Kind', ['a', 'b'], 'b'),
    '<label class="field" style="flex:1;min-width:150px"><span>Kind</span><select id="k">\n    <option value="a">a</option><option value="b" selected>b</option></select></label>');
});

// ---- views/sources.js: a row is a door, the page behind it says everything ----
// Loaded into this same context (a classic script, like ui.js), which already
// declares `views` and `ago`. Only the pure markup builders are exercised —
// drawSources needs a browser.
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'sources.js'), 'utf8'), { filename: 'sources.js' });

// ---- views/config.js: the source cards and the plan block ----
// Goal tiles need goals.js (emblem SVGs) and are not exercised here.
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'config.js'), 'utf8'), { filename: 'config.js' });

test('sources: a card opens the source\'s own page, which carries the prose', () => {
  const g = { id: 'grow-my-audience', title: 'Grow my audience', tag: 'Grow my audience', color: '#2563EB', sources: [
    { id: 'x', title: 'X / Twitter', status: 'connected', total: 108, from: 'X API v2 daily',
      storage: 'observations table', summary: '2 handles', brand: { mark: 'X', color: '#000000', ink: '#FFFFFF' },
      last: '2025-09-26T04:05:11Z',
      accounts: [{ label: '@handle', via: 'API', url: 'https://x.com/handle' }, { label: '@other' }],
      kinds: [{ kind: 'post-stats', note: 'one row per sync', n: 27, first: '2026-08-23T05:58:27Z', last: '2026-09-26T04:05:11Z' }] },
  ] };
  // A card (2026-10-06): the brand tile, the name, the summary; the
  // group's colour on its edge; rows and the newest row's date.
  const card = cfgSourceCard(g, g.sources[0]);
  assert.match(card, /^<a class="scard" href="#\/sources\/grow-my-audience\/x" style="--c:#2563EB">/);
  assert.match(card, /<span class="bmark" style="background:#000000;color:#FFFFFF">X<\/span>/);
  assert.match(card, /<span class="nm">X \/ Twitter<\/span><span class="small muted clamp1">2 handles<\/span>/);
  assert.match(card, /<span class="bot"><span class="small muted">108 rows · Sep 26, 2025<\/span>/);
  assert.doesNotMatch(card, /X API v2 daily/);
  const page = sourcePageHTML(g, g.sources[0]);
  assert.match(page, /<h2><a href="#\/config">Configuration<\/a> \/ X \/ Twitter<\/h2><span class="small muted">Grow my audience<\/span>/);
  assert.match(page, /X API v2 daily/);
  assert.match(page, /observations table/);
  assert.match(page, /<td class="mono">post-stats<\/td><td class="small muted">one row per sync<\/td>/);
  assert.match(page, /2026-08-23 → 2026-09-26/);
  assert.match(page, /href="https:\/\/x.com\/handle"/);
});

// The row has to contradict itself out loud (2026-09-01): X read "connected ·
// 2m ago" for two days while every call came back 402, because `last` is the
// newest row of any kind — including the follow batches the hub writes itself.
// The Newest column still says 2m; the red line under the name is what makes
// that legible, and it leads with how old the numbers are, not with the error.
test('sources: a failing source says how stale it is, on the row', () => {
  const g = { id: 'audience', title: 'Audience', sources: [
    { id: 'x', title: 'X / Twitter', status: 'failing', total: 150,
      error: 'x: HTTP 402: credits depleted', fails: 4,
      failing_since: new Date(Date.now() - 16 * 3600e3).toISOString(),
      last_ok: new Date(Date.now() - 45 * 3600e3).toISOString(),
      last: new Date(Date.now() - 120e3).toISOString() },
  ] };
  const h = cfgSourceCard(g, g.sources[0]);
  assert.match(h, /<a class="scard bad"/);
  // The red line takes the summary's place and says how stale.
  assert.match(h, /sfail clamp1">4 tries failed · since 16h ago · last worked 2d ago/);
  assert.match(h, /150 rows · 2m ago<\/span>/); // the contradiction, now visible
  // The error itself is on the source's page, under the same red line.
  const page = sourcePageHTML(g, g.sources[0]);
  assert.match(page, /Not syncing — 4 tries failed · since 16h ago · last worked 2d ago/);
  assert.match(page, /What the service said: <code>x: HTTP 402: credits depleted<\/code>/);
  // One failure, and one that has never worked at all.
  const once = cfgSourceCard(g, { id: 'y', title: 'Y', status: 'failing' });
  assert.match(once, /sfail clamp1">1 try failed · never worked/);
  // Everything else keeps its quiet card.
  assert.doesNotMatch(cfgSourceCard(g, { id: 'z', title: 'Z', status: 'connected' }), /sfail|scard bad/);
});

// Grouped by goal (2026-10-07: "instead of audience here, it should say
// grow my audience"): the hub's group order, the goal's title in the hub's
// colour for it over the goal's first card only, by name inside it, all in
// one left-to-right grid (10-16: "I don't want discrete columns"); a failing
// source stays put.
test('config: source cards run in goal order, by name inside a goal', () => {
  const groups = [
    { id: 'grow-my-audience', tag: 'Grow my audience', color: '#AB6B2B', sources: [
      { id: 'yt', title: 'YouTube', status: 'connected' },
      { id: 'x', title: 'X / Twitter', status: 'failing' },
      { id: 'gh', title: 'GitHub', status: 'connected' },
    ] },
    { id: 'none', tag: 'No goal', color: '#64748B', sources: [{ id: 'w', title: 'Weather', status: 'connected' }] },
  ];
  const h = cfgSourcesHTML(groups);
  const at = s => h.indexOf(`>${s}<`);
  assert.ok(at('GitHub') < at('X / Twitter') && at('X / Twitter') < at('YouTube') && at('YouTube') < at('Weather'));
  assert.match(h, /^<div class="sflow"><div class="scell" style="--c:#AB6B2B"><h4>Grow my audience<span class="muted">3<\/span><\/h4><a class="scard"/);
  assert.equal(h.split('<h4 class="ghost"').length - 1, 2);
  assert.doesNotMatch(h, /--n:|<section/);
  assert.match(h, /<h4>No goal<span class="muted">1<\/span><\/h4>/);
  assert.match(h, /href="#\/sources\/grow-my-audience\/gh"/);
  assert.doesNotMatch(h, /<h3>/);
  assert.equal(h.split('<a class="scard').length - 1, 4);
});

test('config: the plan block names the plan, its price and each limit\'s headroom', () => {
  const q = { available: true, next_model: 'claude-opus-5-5',
    plan: { name: 'Claude Max 20x', via: 'Claude Code', usd: 200, period: 'monthly', charged_on: '2026-09-20', month_usd: 1234.5,
      brand: { mark: 'A', color: '#D97757', ink: '#FFFFFF' } },
    windows: [{ label: 'All models · 7 days', utilization: 38.4, tone: '', elapsed_pct: 50 },
      { label: 'Fable · 7 days', utilization: 91, tone: 'bad' }] };
  const h = cfgPlanHTML(q, null);
  assert.match(h, /<strong>Claude Max 20x<\/strong><span class="small muted">Claude Code · \$200\/mo · charged Sep 20<\/span>/);
  assert.match(h, /<span class="n">\$1,235<\/span><span class="small muted">this month<\/span>/);
  assert.match(h, /All models · 7 days<\/span>\s*<span class="small n">62% left<\/span>/);
  assert.match(h, /style="color:var\(--red\)">9% left/);
  assert.match(h, /<i class="tick" style="left:50%"><\/i>/);
  // A plan with no charge found yet still says what runs the sessions.
  assert.match(cfgPlanHTML({ ...q, plan: { name: 'Claude', via: 'Claude Code', month_usd: 0 } }, null),
    /<strong>Claude<\/strong><span class="small muted">Claude Code<\/span>/);
});

// ---- composer.js: the one box every surface writes back through ----
// It is pure render on the way in (composerHTML/composerReHTML) and only
// touches the DOM when a box is on screen, so it loads here beside ui.js. The
// paste listener at the bottom needs a `document`; a stub with the one method
// it calls is enough, and no test exercises it.
globalThis.document = { addEventListener() {}, getElementById: () => null, querySelector: () => null, querySelectorAll: () => [] };
globalThis.window = globalThis.window || { addEventListener() {}, innerHeight: 900 };
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

// Several cards on one Send (2026-09-17: "click read and accept
// recommendation at the same time… two things on the chat bar… the agent
// gets those things, and then those things are closed"). Arming adds a strip
// with its own chips and ×; the first card stays `re` (every single-card
// reader), the rest ride in `also`; a pick lands on ITS card; one × drops one.
test('composerArm: a second card joins the strip, and Send carries them all', async () => {
  composerReset('chat');
  const sent = [];
  composerHTML('chat', { targets: [], when: false, send: p => { sent.push(p); } });
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
// its × — and an empty Send asks first (2026-09-13: "If you don't want
// anything, you can send it, but then it'll say 'Are you sure?'… 'Send
// without'"). The second press sends; typing puts plain Send back.
test('composer: a read reply is a banner, and an empty Send asks "Are you sure?"', async () => {
  composerReset('chat');
  const sent = [];
  composerHTML('chat', { targets: [], when: false, send: p => { sent.push(p); },
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

// A session's chat box has no route (2026-09-28: "we don't need these
// dropdowns"): the box the chat draws carries no session or time select, and
// Send goes now, to this session — the default box keeps both for the others.
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

// ---- mic.js: Send while the microphone is on ----
// The composer marks the take as Send begins and tells the mic when the hub
// took it: only what was said after the mark is in the box from then on — not
// on the next poll, not after a hop to another session and back, not when a
// send that was in flight as the mic was tapped on lands.
globalThis.window = globalThis.window || { addEventListener() {} };
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'mic.js'), 'utf8'), { filename: 'mic.js' });

test('mic: a send while listening keeps only what was said after it', async () => {
  composerReset('chat');
  const box = { value: '', style: {}, scrollHeight: 0, focus() {}, selectionStart: 0, selectionEnd: 0 };
  const doc = globalThis.document, oldGet = doc.getElementById;
  doc.getElementById = id => (id === 'c-chat-draft' ? box : null);
  globalThis.chatState = { id: 't-1' };
  let taken = null, release = null;
  composerHTML('chat', { targets: [], when: false, send: p => new Promise(r => { taken = p.text; release = r; }) });
  const fresh = () => Object.assign(mic, { on: true, key: 'chat', thread: 't-1', base: '', inserted: '', finalsBase: 0, finalsSeen: '',
    volShown: '', sentBase: 0, skipFinal: false, mark: null });
  fresh();
  // Two phrases final, a third still volatile, and Send is pressed.
  micApply({ seq: 1, on: true, text: 'open the goals page. then the recs.', volatile: 'and send' });
  assert.equal(box.value, 'open the goals page. then the recs. and send');
  const sending = composerSend('chat');
  assert.equal(taken, 'open the goals page. then the recs. and send');
  // While the hub takes it, the talking goes on: the box still shows the take.
  micApply({ seq: 2, on: true, text: 'open the goals page. then the recs. and send it.', volatile: 'now the' });
  release(); await sending;
  // The hub took it: the sent words are gone, the volatile phrase went WITH the
  // message (its final is not shown again), what came after stays.
  assert.equal(box.value, 'now the');
  micApply({ seq: 3, on: true, text: 'open the goals page. then the recs. and send it. now the calendar.', volatile: 'please' });
  assert.equal(box.value, 'now the calendar. please');
  assert.equal(composerState('chat').text, 'now the calendar. please');
  // A hop to another session: its box is the one on the screen now, so the
  // words go there from this moment (micSync at the next poll); the sent
  // words never come back anywhere, and coming back to the first session
  // starts that box from what it holds, not from the take.
  chatState.id = 't-2'; box.value = ''; composerState('chat').text = '';
  micSync();
  assert.equal(mic.thread, 't-2');
  micApply({ seq: 4, on: true, text: 'open the goals page. then the recs. and send it. now the calendar. please do.', volatile: '' });
  assert.equal(box.value, 'please do.');
  chatState.id = 't-1'; box.value = ''; composerState('chat').text = '';
  micSync();
  micApply({ seq: 5, on: true, text: 'open the goals page. then the recs. and send it. now the calendar. please do. back.', volatile: '' });
  assert.equal(box.value, 'back.');

  // Send with nothing volatile: the first final after it is a new phrase and shows.
  fresh(); box.value = ''; composerState('chat').text = '';
  micApply({ seq: 6, on: true, text: 'first phrase.', volatile: '' });
  const s2 = composerSend('chat'); release(); await s2;
  assert.equal(taken, 'first phrase.');
  assert.equal(box.value, '');
  micApply({ seq: 7, on: true, text: 'first phrase. second phrase.', volatile: '' });
  assert.equal(box.value, 'second phrase.');

  // Tapped on while a typed message was still being sent: the take began over
  // words that then left with the send; every phrase of it stays in the box.
  mic.on = false; box.value = 'typed words'; composerState('chat').text = 'typed words';
  const s3 = composerSend('chat');
  assert.equal(taken, 'typed words');
  fresh(); mic.base = box.value;
  micApply({ seq: 8, on: true, text: 'said meanwhile.', volatile: '' });
  assert.equal(box.value, 'typed words said meanwhile.');
  release(); await s3;
  assert.equal(box.value, 'said meanwhile.');
  micApply({ seq: 9, on: true, text: 'said meanwhile. and after.', volatile: '' });
  assert.equal(box.value, 'said meanwhile. and after.');

  mic.on = false; mic.key = null; doc.getElementById = oldGet; composerReset('chat');
});

// The mic is a sensor: the words go to the box ON THE SCREEN — the sessions
// page's own box without a click, the box with the caret elsewhere, none on a
// page without one — and a hub that comes back from a restart with no take
// never takes words out of a box.
test('mic: the words go to the box on the screen, and a hub restart takes nothing back', () => {
  composerReset('chat'); composerReset('new');
  const boxes = {};
  const mk = value => ({ value, style: {}, scrollHeight: 0, focus() {}, selectionStart: 0, selectionEnd: 0 });
  const doc = globalThis.document, oldGet = doc.getElementById, oldActive = doc.activeElement;
  doc.getElementById = id => boxes[id] || null;
  doc.activeElement = null;
  globalThis.chatState = { id: 't-1' };
  composerHTML('chat', { send: () => {} });
  composerHTML('new', { send: () => {} });
  // On from the bar on a page with no box (Goals, Recs): rows only, the
  // counters move.
  Object.assign(mic, { on: true, key: null, thread: '', base: '', inserted: '', finalsBase: 0, finalsSeen: '', volShown: '',
    sentBase: 0, skipFinal: false, mark: null });
  micSync();
  assert.equal(mic.key, null);
  micApply({ seq: 1, on: true, text: 'said on the goals page.', volatile: 'and then' });
  assert.equal(mic.finalsSeen, 'said on the goals page.');
  // A session opens: its box is on the screen, no click needed, and it takes
  // the phrases from here on — never the ones said on Goals.
  boxes['c-chat-draft'] = mk('');
  micSync();
  assert.equal(mic.key, 'chat'); assert.equal(mic.thread, 't-1');
  micApply({ seq: 2, on: true, text: 'said on the goals page. and then this.', volatile: 'more' });
  assert.equal(boxes['c-chat-draft'].value, 'and then this. more');
  // Off to Recs: the box is gone, the words are rows again; the take keeps
  // going and nothing waits for the box.
  delete boxes['c-chat-draft'];
  micSync();
  assert.equal(mic.key, null); assert.equal(mic.thread, '');
  micApply({ seq: 3, on: true, text: 'said on the goals page. and then this. more here on recs.', volatile: '' });
  // Back to the session: the box was rebuilt from its draft; nothing said on
  // Recs is dumped into it, only what comes next.
  boxes['c-chat-draft'] = mk(composerState('chat').text);
  micSync();
  assert.equal(boxes['c-chat-draft'].value, 'and then this.');
  micApply({ seq: 4, on: true, text: 'said on the goals page. and then this. more here on recs. back now.', volatile: '' });
  assert.equal(boxes['c-chat-draft'].value, 'and then this. back now.');
  // The box is cleared by hand: the mic stays on and the next phrase lands alone.
  boxes['c-chat-draft'].value = ''; composerState('chat').text = '';
  micApply({ seq: 5, on: true, text: 'said on the goals page. and then this. more here on recs. back now. fresh start.', volatile: '' });
  assert.equal(mic.on, true);
  assert.equal(boxes['c-chat-draft'].value, 'fresh start.');
  // + New session: a blank box on the screen, and the caret in another box
  // (a calendar step's) beats the page's own.
  delete boxes['c-chat-draft']; boxes['c-new-draft'] = mk('typed first');
  micSync();
  assert.equal(mic.key, 'new');
  micApply({ seq: 6, on: true, text: 'said on the goals page. and then this. more here on recs. back now. fresh start. more here.', volatile: '' });
  assert.equal(boxes['c-new-draft'].value, 'typed first more here.');
  assert.equal(composerState('new').text, 'typed first more here.');
  boxes['c-step-draft'] = mk('');
  doc.activeElement = { tagName: 'TEXTAREA', closest: () => ({ dataset: { c: 'step' } }) };
  micSync();
  assert.equal(mic.key, 'step');
  doc.activeElement = null; delete boxes['c-step-draft'];
  micSync();
  assert.equal(mic.key, 'new');
  // The hub restarted underneath: its take is empty. The box keeps every word
  // and the new take lands after them.
  micApply({ seq: 0, on: true, text: '', volatile: '' });
  assert.equal(boxes['c-new-draft'].value, 'typed first more here.');
  micApply({ seq: 1, on: true, text: 'after the restart.', volatile: '' });
  assert.equal(boxes['c-new-draft'].value, 'typed first more here. after the restart.');
  // Same restart while no box is the target: nothing to keep, counters reset.
  mic.key = null; mic.thread = '';
  micApply({ seq: 0, on: true, text: '', volatile: '' });
  assert.equal(mic.finalsSeen, ''); assert.equal(mic.finalsBase, 0);
  mic.on = false; mic.key = null; doc.getElementById = oldGet; doc.activeElement = oldActive;
  composerReset('chat'); composerReset('new');
});

// A tap off right after a short phrase: its final lands during the hub's
// drain (`stopping`), and a poll that stopped following at `on:false` would
// leave the final no box (rows only). The take is followed until it has ended.
test('mic: a phrase finalized while the take is stopping still lands in the box', async () => {
  composerReset('chat');
  const box = { value: '', style: {}, scrollHeight: 0, focus() {}, selectionStart: 0, selectionEnd: 0 };
  const doc = globalThis.document, oldGet = doc.getElementById, oldHubGet = globalThis.get;
  doc.getElementById = id => (id === 'c-chat-draft' ? box : null);
  globalThis.chatState = { id: 't-1' };
  composerHTML('chat', { send: () => {} });
  const views = [
    { seq: 1, on: true, since: 's', text: '', volatile: 'For example, did you' },
    { seq: 1, on: false, since: 's', stopping: true, status: 'stopping', text: '', volatile: 'For example, did you hear this?' },
    { seq: 2, on: false, since: 's', text: 'For example, did you hear this?', volatile: '' },
  ];
  globalThis.get = async () => views.shift();
  Object.assign(mic, { on: true, seq: -1, key: 'chat', thread: 't-1', base: '', inserted: '', finalsBase: 0, finalsSeen: '',
    volShown: '', sentBase: 0, skipFinal: false, mark: null });
  await micPoll();
  assert.equal(box.value, 'For example, did you');
  await micPoll();
  assert.equal(mic.on, true); assert.equal(mic.key, 'chat');
  assert.equal(box.value, 'For example, did you hear this?');
  await micPoll();
  assert.equal(box.value, 'For example, did you hear this?');
  assert.equal(composerState('chat').text, 'For example, did you hear this?');
  assert.equal(mic.on, false); assert.equal(mic.key, null);
  globalThis.get = oldHubGet; doc.getElementById = oldGet; composerReset('chat');
});

// A take switched on in another app (the desktop app's box) while this page
// was open: the watch finds it. The switch reads on, no box here takes the
// words and none is named to the hub, one tap turns it off, and a hub that
// comes back without the take ends it.
test('mic: a take switched on elsewhere shows on an open page as a guest', async () => {
  composerReset('chat');
  const box = { value: 'my draft', style: {}, scrollHeight: 0, focus() {}, selectionStart: 0, selectionEnd: 0 };
  const doc = globalThis.document, oldGet = doc.getElementById, oldHubGet = globalThis.get, oldPost = globalThis.post;
  doc.getElementById = id => (id === 'c-chat-draft' ? box : null);
  globalThis.chatState = { id: 't-1' };
  const posts = [];
  globalThis.post = async (p, b) => { posts.push(b); return { on: false, seq: 9, text: '' }; };
  let view = { seq: 3, on: true, since: 's', status: 'listening', box: 'new', text: 'said in the desktop app.', volatile: '' };
  globalThis.get = async () => view;
  Object.assign(mic, { on: false, guest: false, key: null, thread: '', busy: false });
  try {
    await micWatch();
    assert.equal(mic.on, true); assert.equal(mic.guest, true);
    assert.equal(micLabel(), '● Listening · other window');
    view = { ...view, seq: 4, text: 'said in the desktop app. And more.' };
    await micPoll();
    assert.equal(mic.key, null); assert.equal(box.value, 'my draft'); assert.deepEqual(posts, []);
    // The hub restarted and did not bring the take back: not this window's to switch on.
    view = { seq: 0, on: false };
    await micPoll();
    assert.equal(mic.on, false); assert.equal(mic.guest, false); assert.deepEqual(posts, []);
    // Found again, a tap on the bar turns it off — and sends nothing as a
    // new session, whatever page this window is on: the words are not its own.
    view = { seq: 1, on: true, since: 's', text: '', volatile: '' };
    await micWatch();
    assert.equal(mic.guest, true);
    await micToggle();
    assert.equal(mic.on, false); assert.equal(posts.length, 1);
    assert.equal(posts[0].on, false); assert.equal(posts[0].send, null);
    // A switch on here is never a guest, and the watch leaves a live switch alone.
    mic.on = true; view = { seq: 2, on: true, since: 's', text: '' };
    assert.equal(micWatch(), undefined);
  } finally {
    micStop();
    globalThis.get = oldHubGet; globalThis.post = oldPost; doc.getElementById = oldGet; composerReset('chat');
  }
});

// The tap on posts the box and the origin; a tap off away from Sessions
// posts `send` with the page (the hub starts one session with the words that
// went into no box), and the switch reads off before the hub answers — its
// off waits for the helper's last word. On Sessions the tap off sends nothing.
test('mic: a tap off away from Sessions sends the page; the switch reads off at once', async () => {
  composerReset('chat');
  const doc = globalThis.document, oldGet = doc.getElementById, oldPost = globalThis.post, oldLoc = globalThis.location;
  const toastEl = { textContent: '', classList: { add() {}, remove() {} } };
  doc.getElementById = id => (id === 'toast' ? toastEl : null);
  const posts = [];
  globalThis.location = { hash: '#/goals' };
  // `views` is ui.js's own const (the pages register on it); Goals must be a
  // page for micSendFrom to send from it.
  const hadGoals = 'goals' in views;
  views.goals = views.goals || {};
  try {
    Object.assign(mic, { on: false, busy: false, guest: false, key: null, thread: '', origin: 0, timer: null });
    globalThis.post = async (p, body) => { posts.push(body); return { on: true, seq: 1, origin_ms: 5, text: '', volatile: '' }; };
    await micToggle();
    assert.equal(posts.length, 1); assert.equal(posts[0].on, true); assert.equal(posts[0].box, '');
    assert.equal(mic.on, true);
    let onDuringPost = null;
    globalThis.post = async (p, body) => { posts.push(body); onDuringPost = mic.on; return { on: false, seq: 2, text: 'said on goals.', volatile: '', sent: 't-9' }; };
    await micToggle();
    assert.equal(posts.length, 2); assert.equal(posts[1].on, false); assert.deepEqual(posts[1].send, { page: '#/goals' });
    assert.equal(posts[1].origin_ms, 5);
    assert.equal(onDuringPost, false); assert.equal(mic.on, false);
    // On Sessions the tap off sends nothing.
    globalThis.location = { hash: '#/sessions' };
    globalThis.post = async (p, body) => { posts.push(body); return { on: true, seq: 3, origin_ms: 7, text: '', volatile: '' }; };
    await micToggle();
    globalThis.post = async (p, body) => { posts.push(body); return { on: false, seq: 4, text: '', volatile: '' }; };
    await micToggle();
    assert.equal(posts[3].on, false); assert.equal(posts[3].send, null);
  } finally {
    micStop();
    globalThis.location = oldLoc; globalThis.post = oldPost; doc.getElementById = oldGet;
    if (!hadGoals) delete views.goals;
    composerReset('chat');
  }
});

// 2026-09-25 (screenshot: the same message twice): a send held 21 s
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
  composerHTML('chat', { targets: [], when: false, send: () => new Promise(r => { sent++; release = r; }) });
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
  const row = { id: 'cal-1a2b3c4d', ref: 'cal:cal-1a2b3c4d', kind: OWNER, title: 'Buy tranche 2', day: '2026-08-27', state: 'done', overdue: true,
    lane: 'mine', item: true, closed: true, mark: 'done' };
  const done = calEntryHTML(row);
  // 2026-08-27: "both of these are already completed… right now it makes
  // it look like these are still open" — a done step is muted, not bolded,
  // and a closed row is never drawn as overdue. But NOT struck
  // (2026-09-03: "It looks like 'water the plants' is crossed out with a check
  // mark, so is it complete or not?" — the line means won't-do, never done).
  assert.match(done, /<div class="card cal closed">/);
  assert.match(done, /<span class="tick">✓<\/span>Buy tranche 2/);
  assert.doesNotMatch(done, /<s>/);
  assert.match(done, /<span class="small muted">done<\/span>/);
  // A closed item keeps its undo (2026-08-29, after a stray tap: "there's
  // no way to get it back") — Reopen, and nothing else.
  assert.match(done, /resolveCal\('cal-1a2b3c4d','scheduled'\)"[^>]*>Reopen</);
  assert.doesNotMatch(done, /composerSend|'done'\)/);
  // The row's session, live (2026-09-26): the board's own pills beside
  // "open session", so the step he just answered from says its session is on
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
  // Its minute passing never closes it — it says so, in red, until he answers.
  assert.match(open, />overdue<\/span>/);
  // His own open step closes like a rec — through THE composer: Done · Won't
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
  // Only his own steps are bold; an agent run is regular weight, as on the
  // phone — and ○ marks HIS to-dos alone, so the grey lane never wears one.
  const agent = calEntryHTML({ ...row, kind: 'agent', state: 'scheduled', closed: false, lane: 'scheduled', mark: '' });
  assert.doesNotMatch(agent, /<strong>/);
  assert.doesNotMatch(agent, /○/);
  // An agent item keeps the plain pair: the words rule is his alone.
  assert.match(agent, /resolveCal\('cal-1a2b3c4d','done'\)/);
  assert.doesNotMatch(agent, /class="composer/);
});

// A record's "by …": the session that did it is a link named for it (hub
// `actor_title`), and the owning session links beside it — the way back into
// a closed chat from its step (2026-10-09: "why isn't there a button to
// click into its session? even though it's closed? I'm trying to get back in").
test('calEntryHTML: a did row links the doing session by name and the owning session', () => {
  const did = { id: 'did:cal:cal-1a2b3c4d', ref: 'cal:cal-1a2b3c4d', kind: OWNER, title: 'Buy 2 AMZN', day: '2026-10-08', state: 'done',
    lane: 'mine', item: true, closed: true, mark: 'done', did: true, verb: 'Did',
    actor: 'claude:thread:t-doer', actor_title: 'Calendar routing', thread_id: 't-owner', thread_title: 'Income averaging plan' };
  const h = calEntryHTML(did);
  assert.match(h, /by <a href="#\/sessions\/t-doer">Calendar routing<\/a> · <a href="#\/sessions\/t-owner">Income averaging plan<\/a>/);
  assert.doesNotMatch(h, /claude:thread:/);
  // No title known → the id is the link's text, still a link.
  assert.match(calEntryHTML({ ...did, actor_title: undefined }), /by <a href="#\/sessions\/t-doer">t-doer<\/a> · /);
  // The owner is a word, not a link; one session named once when it did its own step.
  assert.match(calEntryHTML({ ...did, actor: OWNER, actor_title: undefined }), /by owner · <a href="#\/sessions\/t-owner">Income averaging plan<\/a>/);
  const own = calEntryHTML({ ...did, actor: 'claude:thread:t-owner', actor_title: 'Income averaging plan' });
  assert.match(own, /by <a href="#\/sessions\/t-owner">Income averaging plan<\/a> /);
  assert.equal((own.match(/Income averaging plan/g) || []).length, 1);
});
