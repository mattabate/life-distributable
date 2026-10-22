// views/threads.js, the "Coming up" strip over a chat's composer (queuedHTML).
// Open, it draws the next FIVE wakes and an "N more" button; a wake in
// another year says the year. threads.js needs a document and a few globals
// the page has; just enough is stubbed here.
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

// 24 wakes a day apart from tomorrow, the last one a year and a half out.
const wakes = n => Array.from({ length: n }, (_, i) => ({
  id: 'p-' + i, state: 'queued', author: 'hub', target: 't-1', title: 'wake ' + i,
  not_before: new Date(Date.now() + (i < n - 1 ? (i + 1) * 86400000 : 540 * 86400000)).toISOString(),
}));

test('queuedHTML: open, the next five and a "19 more" button; the button draws the rest', () => {
  const html = queuedHTML(wakes(24));
  assert.match(html, /Coming up · 24/);
  assert.equal((html.match(/class="row qrow"/g) || []).length, 6, 'five rows and the button row');
  assert.match(html, /<button class="sm" onclick="queuedMore\(\)">19 more<\/button>/);
  assert.match(html, /wake 4/);
  assert.doesNotMatch(html, /wake 5/);
  const five = queuedHTML(wakes(5));
  assert.equal((five.match(/class="row qrow"/g) || []).length, 5);
  assert.doesNotMatch(five, / more</);
  chatState.queued = wakes(24);
  queuedMore();
  assert.equal((el('queued').outerHTML.match(/class="row qrow"/g) || []).length, 24);
  assert.doesNotMatch(el('queued').outerHTML, / more</);
  for (const f of onLeave) f();
  assert.match(queuedHTML(wakes(24)), /19 more/);
});

test('wakeWhen: the clock today, the day this year, the year once it differs', () => {
  const now = new Date(2026, 9, 21, 17, 0);
  assert.doesNotMatch(wakeWhen(new Date(2026, 9, 21, 23, 28).toISOString(), now), /Oct/);
  assert.doesNotMatch(wakeWhen(new Date(2026, 10, 5, 9, 0).toISOString(), now), /2026/);
  assert.match(wakeWhen(new Date(2027, 10, 5, 10, 30).toISOString(), now), /2027/);
  assert.equal(wakeWhen('', now), 'now');
});
