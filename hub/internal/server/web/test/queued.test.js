// views/threads.js, a chat's Coming up page (queuedHTML): the second word by
// the session's name, every wake listed in fire order, one row each; a wake
// in another year says the year. threads.js needs a document and a few
// globals the page has; just enough is stubbed here. The fixtures hold no
// personal data.
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

test('queuedHTML: the page lists every wake, soonest first, each with Cancel', () => {
  const html = queuedHTML(wakes(24).reverse());
  assert.equal((html.match(/class="row qrow"/g) || []).length, 24);
  assert.equal((html.match(/>Cancel</g) || []).length, 24);
  assert.ok(html.indexOf('wake 0') < html.indexOf('wake 23'));
  assert.match(queuedHTML([]), /Nothing coming up/);
  assert.match(queuedHTML([{ ...wakes(1)[0], state: 'done' }]), /Nothing coming up/);
});

test('queuedSoonHTML: only wakes within 15 minutes go back on the bar', () => {
  const at = mins => ({ id: 'p-' + mins, state: 'queued', author: 'hub', target: 't-1', title: 'in ' + mins,
    not_before: new Date(Date.now() + mins * 60e3).toISOString() });
  const html = queuedSoonHTML([at(40), at(10), at(14)]);
  assert.equal((html.match(/class="row qrow"/g) || []).length, 2);
  assert.ok(html.indexOf('in 10') < html.indexOf('in 14'));
  assert.doesNotMatch(html, /in 40/);
  assert.equal(queuedSoonHTML([at(40)]), '');
});

test('a standing wake is one pill: its period and its next minute', () => {
  const html = queuedHTML([{ id: 'p-1', state: 'queued', author: 'hub', target: 't-1', title: 'check',
    repeat: 'weekly@Wed 09:30', not_before: new Date(Date.now() + 86400000).toISOString() }]);
  assert.match(html, />weekly · next [^<]+</);
  assert.doesNotMatch(html, />weekly Wed 09:30</);
  assert.equal((html.match(/class="pill/g) || []).length, 1);
  assert.match(html, />Turn off</);
});

test('wakeWhen: the clock today, the day this year, the year once it differs', () => {
  const now = new Date(2026, 9, 21, 17, 0);
  assert.equal(wakeWhen(new Date(2026, 9, 21, 23, 28).toISOString(), now), '11:28 PM');
  assert.equal(wakeWhen(new Date(2026, 10, 5, 9, 0).toISOString(), now), 'Nov 5 9:00 AM');
  assert.equal(wakeWhen(new Date(2027, 10, 5, 10, 30).toISOString(), now), 'Nov 5, 2027 10:30 AM');
  assert.equal(wakeWhen('', now), 'now');
});
