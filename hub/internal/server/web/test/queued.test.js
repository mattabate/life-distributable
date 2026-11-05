// views/threads.js, the "Coming up" strip over a chat's composer (queuedHTML).
// Open, it draws the next FIVE wakes and a "N more" button (2026-10-21,
// a phone screenshot of a chat with 24 wakes out to Nov 2027 filling the
// screen: "can we limit it to next 5?"); a wake in another year says the
// year. threads.js needs a document and a few globals the page has; just
// enough is stubbed here. The fixtures hold no personal data.
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
globalThis.onLeave = globalThis.onLeave || [];
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'threads.js'), 'utf8'), { filename: 'threads.js' });

// threads.js is loaded alone and needs no other view.
test('threads.js runs on its own', () => {
  assert.equal(typeof views.sessions.draw, 'function');
  assert.equal(newSendingSince(), 0);
  assert.equal(sendingSince(), 0);
});

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
  // Five or fewer: every row, no button.
  const five = queuedHTML(wakes(5));
  assert.equal((five.match(/class="row qrow"/g) || []).length, 5);
  assert.doesNotMatch(five, / more</);
  // The button: every row this visit, five again after leaving the chat.
  chatState.queued = wakes(24);
  queuedMore();
  assert.equal((el('queued').outerHTML.match(/class="row qrow"/g) || []).length, 24);
  assert.doesNotMatch(el('queued').outerHTML, / more</);
  for (const f of onLeave) f();
  assert.match(queuedHTML(wakes(24)), /19 more/);
});

test('a standing wake is one pill: its period and its next minute', () => {
  const html = queuedHTML([{ id: 'p-1', state: 'queued', author: 'hub', target: 't-1', title: 'check',
    repeat: 'weekly@Wed 09:30', not_before: new Date(Date.now() + 86400000).toISOString() }]);
  assert.match(html, />weekly · next [^<]+</);
  assert.doesNotMatch(html, />weekly Wed 09:30</);
  assert.equal((html.match(/class="pill/g) || []).length, 1);
});

test('wakeWhen: the clock today, the day this year, the year once it differs', () => {
  const now = new Date(2026, 9, 21, 17, 0);
  assert.equal(wakeWhen(new Date(2026, 9, 21, 23, 28).toISOString(), now), '11:28 PM');
  assert.equal(wakeWhen(new Date(2026, 10, 5, 9, 0).toISOString(), now), 'Nov 5 9:00 AM');
  assert.equal(wakeWhen(new Date(2027, 10, 5, 10, 30).toISOString(), now), 'Nov 5, 2027 10:30 AM');
  assert.equal(wakeWhen('', now), 'now');
});
