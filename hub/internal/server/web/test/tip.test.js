// Tests for tip.js — the one tooltip every plot on the console draws into.
// `ops/webcheck.sh` runs these (node --test); they are not part of `make check`
// (no JS lane in the gate, DESIGN.md). Nothing here is real data.
//
// What these pin is the CONTRACT the eight charts
// share: a card that escapes what it is handed, prints the value before the
// name, keys a series by colour, survives the trip through an HTML attribute,
// and never leaves the window.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const web = path.join(__dirname, '..');
// tip.js hangs two document listeners at load and creates its card lazily, so
// the browser bits it touches have to exist before it is evaluated.
const nodes = {};
const on = {};
globalThis.document = {
  addEventListener: (k, fn) => { on[k] = fn; },
  getElementById: id => nodes[id] || null,
  createElement: () => ({ id: '', className: '', hidden: false, innerHTML: '', style: {},
    offsetWidth: 220, offsetHeight: 90 }),
  body: { appendChild: n => { nodes[n.id] = n; } },
};
const onWin = {};
globalThis.window = { addEventListener: (k, fn) => { onWin[k] = fn; }, innerWidth: 1000, innerHeight: 700 };
vm.runInThisContext(fs.readFileSync(path.join(web, 'ui.js'), 'utf8'), { filename: 'ui.js' });
vm.runInThisContext(fs.readFileSync(path.join(web, 'tip.js'), 'utf8'), { filename: 'tip.js' });

const unesc = s => s.replace(/&quot;/g, '"').replace(/&#39;/g, "'")
  .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');

test('every field is escaped — a series name is data, not markup', () => {
  const html = tipHTML({
    title: '<script>x</script>', sub: 'a & b',
    rows: [{ c: 'var(--s1)', v: '1', k: '<img onerror=boom>' }],
    foot: '"quoted"',
  });
  assert.ok(!html.includes('<script>') && !html.includes('<img'));
  assert.ok(html.includes('&lt;script&gt;') && html.includes('a &amp; b'));
});

test('the value leads and the name follows it', () => {
  // In a legend the reader has the number and wants the series; in a tooltip
  // they have the series and want the number. So: bold value, then muted name.
  const html = tipHTML({ rows: [{ c: 'var(--accent)', v: '$1,283', k: 'SNOW', t: '65%' }] });
  assert.ok(html.indexOf('<b>$1,283</b>') < html.indexOf('class="k">SNOW'));
  assert.ok(html.includes('background:var(--accent)'));
  assert.ok(html.includes('class="t">65%'));
});

test('rows keep their order, and a keyless row keeps its indent', () => {
  const html = tipHTML({ rows: [{ v: 'a', k: 'first' }, { c: 'red', v: 'b', k: 'second' }] });
  assert.ok(html.indexOf('first') < html.indexOf('second'));
  assert.ok(html.includes('<i class="none">'), 'a row with no colour still needs the key column');
});

test('the hovered row is marked and the context rows are dimmed', () => {
  const html = tipHTML({ rows: [{ v: '1', k: 'a', on: true }, { v: '2', k: 'b', dim: true }] });
  assert.ok(/tip-r on"/.test(html) && /tip-r dim"/.test(html));
});

test('lines are whole sentences under the rows, escaped, in order', () => {
  // The P&L rows (2026-09-23): the working behind a number is prose, not a
  // series key, so it goes in its own block and never through a tip-r.
  const html = tipHTML({ title: 'Gold → AI sleeve swap', lines: ['SNOW 5 sh → $1,683', 'vs <keeping> GLD', null] });
  assert.ok(html.includes('tip-lines') && !html.includes('tip-rows'));
  assert.ok(html.indexOf('SNOW') < html.indexOf('keeping'));
  assert.ok(html.includes('&lt;keeping&gt;') && !html.includes('<keeping>'));
  assert.ok(!html.includes('null'));
});

test('nothing is drawn for the parts a plot did not fill in', () => {
  const html = tipHTML({ title: 'Aug 31' });
  assert.ok(html.includes('tip-h') && !html.includes('tip-s') && !html.includes('tip-rows') && !html.includes('tip-f'));
  // A null row is how a chart says "no comparison here" (the first bar).
  assert.ok(!tipHTML({ rows: [null, { v: '1', k: 'a' }] }).includes('undefined'));
});

test('a spec survives the round trip through an HTML attribute', () => {
  const spec = { title: 'He said "hi" & <left>', rows: [{ v: '2', k: "o'clock" }] };
  const attr = tipAttr(spec);
  assert.ok(attr.startsWith('data-tip="') && !attr.slice(10, -1).includes('"'));
  assert.deepEqual(JSON.parse(unesc(attr.slice('data-tip="'.length, -1))), spec);
});

test('the card sits above the mark, flips under it, and stays in the window', () => {
  // 220×90 card, 1000×700 window (the stubs above), 10px gap.
  tipShow({ title: 'x' }, { left: 500, right: 520, top: 300, bottom: 320 });
  const el = nodes.tipcard;
  assert.equal(el.hidden, false);
  assert.equal(el.style.top, '200px');            // 300 − 90 − 10
  assert.equal(el.style.left, '400px');           // centred on the mark
  // No room above: under the mark instead.
  tipShow({ title: 'x' }, { left: 500, right: 520, top: 12, bottom: 30 });
  assert.equal(el.style.top, '40px');
  // A mark at either edge still gets a whole card.
  tipShow({ title: 'x' }, { left: 0, right: 8, top: 300, bottom: 320 });
  assert.equal(el.style.left, '8px');
  tipShow({ title: 'x' }, { left: 992, right: 1000, top: 300, bottom: 320 });
  assert.equal(el.style.left, '772px');           // 1000 − 220 − 8
});

test('a scroll or resize re-measures a mark\'s card, and kills an orphan', () => {
  // Reading a tooltip and nudging the trackpad should not delete the answer —
  // the mark is still on screen, so the card is re-measured against it.
  const mark = { isConnected: true, getBoundingClientRect: () => ({ left: 500, right: 520, top: 200, bottom: 220 }) };
  for (const fire of [onWin.scroll, onWin.resize]) {
    tipShow({ title: 'x' }, { left: 0, right: 0, top: 600, bottom: 600 }, mark);
    fire();
    assert.equal(nodes.tipcard.hidden, false);
    assert.equal(nodes.tipcard.style.top, '100px');   // 200 − 90 − 10, from the MARK
    assert.equal(nodes.tipcard.style.left, '400px');
  }
  // A card with no anchor left — a redraw dropped the mark — goes down.
  tipShow({ title: 'x' }, mark.getBoundingClientRect(), { isConnected: false });
  onWin.scroll();
  assert.equal(nodes.tipcard.hidden, true);
});

test('hide empties the card as well as hiding it', () => {
  tipShow({ title: 'secret-ish' }, { left: 10, right: 20, top: 300, bottom: 320 });
  tipHide();
  assert.equal(nodes.tipcard.hidden, true);
  assert.equal(nodes.tipcard.innerHTML, '');
});

test('one delegated listener serves every mark on the page', () => {
  // 365 heat cells add 365 attributes and no listeners — that is the whole
  // reason the spec travels as JSON in the DOM.
  assert.equal(typeof on.pointerover, 'function');
  assert.equal(typeof on.pointerout, 'function');
  const mark = { dataset: { tip: JSON.stringify({ title: 'Aug 31', rows: [{ v: '4', k: 'contributions' }] }) },
    getBoundingClientRect: () => ({ left: 100, right: 111, top: 400, bottom: 411 }) };
  on.pointerover({ target: { closest: () => mark } });
  assert.ok(nodes.tipcard.innerHTML.includes('contributions'));
  assert.equal(nodes.tipcard.hidden, false);
  on.pointerout({ target: { closest: () => ({ ...mark, contains: () => false }) }, relatedTarget: null });
  assert.equal(nodes.tipcard.hidden, true);
  // A mark with an unreadable spec shows nothing rather than throwing.
  on.pointerover({ target: { closest: () => ({ dataset: { tip: '{oops' } }) } });
  assert.equal(nodes.tipcard.hidden, true);
});
