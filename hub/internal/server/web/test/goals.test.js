// Tests for views/goals.js — one goal's page, whose head panel Edit swaps for
// a form in place. What is worth pinning is the form's way out: the page's
// redraw keeps a half-typed edit while #g-title is in the DOM, so Cancel and
// a landed Save must take the form out BEFORE they redraw (daily bug sweep
// 2026-10-25: Cancel drew the form again). `ops/webcheck.sh` runs these
// (node --test). The goal below is invented and holds no personal data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const load = f => vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', f), 'utf8'), { filename: f });
load('ui.js');

// A page-sized fake DOM: `page` is the HTML the last paint wrote, `head` the
// head panel's innerHTML inside it (the template's own marker lines bound
// it). An id exists when the page holds id="<id>"; the form's boxes answer
// with their value attribute.
const HEAD_OPEN = 'id="ghead">', HEAD_CLOSE = '\n\n      <h3>Where this stands';
let page = '';
const headOf = () => { const a = page.indexOf(HEAD_OPEN) + HEAD_OPEN.length; return page.slice(a, page.indexOf(HEAD_CLOSE, a)); };
const setHead = html => { const a = page.indexOf(HEAD_OPEN) + HEAD_OPEN.length; page = page.slice(0, a) + html + page.slice(page.indexOf(HEAD_CLOSE, a)); };
const attr = (id, name) => { const m = page.match(new RegExp(`<[^>]*id="${id}"[^>]*${name}="([^"]*)"`)); return m ? m[1] : ''; };
const ghead = { get innerHTML() { return headOf(); }, set innerHTML(v) { setHead(v); }, querySelector: () => ({ textContent: 'Make widgets' }) };
globalThis.document = {
  addEventListener() {}, querySelector: () => null, querySelectorAll: () => [],
  getElementById: id => {
    if (id === 'ghead') return page.includes(HEAD_OPEN) ? ghead : null;
    if (id === 'toast') return { textContent: '', classList: { add() {}, remove() {} } };
    if (!page.includes(`id="${id}"`)) return null;
    if (id === 'g-title') return { value: attr(id, 'value'), focus() {} };
    if (id === 'g-statement') { const m = page.match(/<textarea id="g-statement"[^>]*>([\s\S]*?)<\/textarea>/); return { value: m ? m[1] : '' }; }
    return {};
  },
};
globalThis.location = { hash: '#/goals/make-widgets' };
const view = { isConnected: true, set innerHTML(v) { page = v; } };

const goal = { id: 'make-widgets', title: 'Make widgets', statement: 'Ship one widget a week.', status: 'active', horizon: 'year', cadence: 'weekly', emblem: { symbol: 'agent', hue: 220 } };
let patched = null;
globalThis.get = p => Promise.resolve(p.endsWith('/notes?limit=30') ? [] : goal);
globalThis.patch = (p, b) => { patched = { p, b }; return Promise.resolve({}); };
load('views/goals.js');

const tick = () => new Promise(r => setTimeout(r, 0));

test('goal page: Edit opens the form in the head panel; Cancel puts the panel back', async () => {
  await views.goals.draw(view, ['make-widgets']);
  assert.ok(page.includes('onclick="editGoal()"'), 'the head panel has Edit');
  assert.ok(!page.includes('id="g-title"'), 'no form yet');

  editGoal();
  await tick();
  assert.ok(page.includes('id="g-title"'), 'Edit drew the form');
  assert.equal(attr('g-title', 'value'), 'Make widgets');

  await closeGoalForm();
  assert.ok(!page.includes('id="g-title"'), 'Cancel took the form out');
  assert.ok(page.includes('onclick="editGoal()"'), 'and the head panel is back');
  assert.ok(page.includes('Ship one widget a week.'), 'with the statement as the hub has it');
});

test('goal page: a landed Save closes the form; a nameless one keeps it open', async () => {
  await views.goals.draw(view, ['make-widgets']);
  editGoal();
  await tick();
  page = page.replace('value="Make widgets"', 'value="Make more widgets"');

  await saveGoal('make-widgets');
  assert.deepEqual(patched, { p: '/goals/make-widgets', b: { title: 'Make more widgets', statement: 'Ship one widget a week.' } });
  assert.ok(!page.includes('id="g-title"'), 'the form is gone after the save');
  assert.ok(page.includes('onclick="editGoal()"'), 'the head panel is back');

  editGoal();
  await tick();
  page = page.replace('value="Make widgets"', 'value=""');
  patched = null;
  await saveGoal('make-widgets');
  assert.equal(patched, null, 'nothing sent without a name');
  assert.ok(page.includes('id="g-title"'), 'the form stays for him to fill in');
  clearTimeout(toastTimer);
});

// A live redraw (the hub said something moved) keeps what he typed — that is
// the rule Cancel has to step around, so it is pinned beside it.
test('goal page: a live redraw mid-edit keeps the typed words', async () => {
  await views.goals.draw(view, ['make-widgets']);
  editGoal();
  await tick();
  page = page.replace('value="Make widgets"', 'value="Half typed"');
  await pageRedraw();
  assert.equal(attr('g-title', 'value'), 'Half typed');
});
