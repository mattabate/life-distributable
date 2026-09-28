// Tests for what the Spend page's by-day chart puts in the shared tooltip.
// tip.test.js pins the card; this pins what the chart says into it. Every
// fixture here is invented. `ops/webcheck.sh` runs it (node --test).
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const web = path.join(__dirname, '..');
globalThis.localStorage = { getItem: () => null, setItem: () => {} };
globalThis.document = {
  addEventListener: () => {}, getElementById: () => null,
  createElement: () => ({ style: {}, offsetWidth: 200, offsetHeight: 80 }),
  body: { appendChild: () => {} },
};
globalThis.window = { addEventListener: () => {}, innerWidth: 1000, innerHeight: 700 };
globalThis.views = globalThis.views || {};
for (const f of ['ui.js', 'tip.js', path.join('views', 'spend.js')]) {
  vm.runInThisContext(fs.readFileSync(path.join(web, f), 'utf8'), { filename: f });
}

// Every mark carries its card as JSON in a data-tip attribute; this is how a
// test reads back what the chart decided to say.
const unesc = s => s.replace(/&quot;/g, '"').replace(/&#39;/g, "'")
  .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
const specs = html => (html.match(/data-tip="[^"]*"/g) || [])
  .map(a => JSON.parse(unesc(a.slice('data-tip="'.length, -1))));
const rowText = s => (s.rows || []).filter(Boolean).map(r => `${r.v} ${r.k || ''}`.trim()).join(' | ');

test('a spend day is a stacked bar whose card is the split by model', () => {
  // No legend, just "200 to Fable, 300 to Fable 5.1" on hover. The series is
  // `history`, all-time: the chart runs from its first day to today whatever
  // `days` says, so a 7-day summary still draws every day there is.
  const yday = easternFields(new Date(Date.now() - 86400000)).on;
  const s = { days: 7, palette: ['claude-opus-5', 'claude-fable-5-1', '', '', '', '', '', ''], by_day: [], history: [
    { key: yday, usd: 4, messages: 6, models: [
      { key: 'claude-fable-5-1', usd: 3, messages: 2 }, { key: 'claude-opus-5', usd: 1, messages: 4 }] },
  ] };
  const html = spendDays(s);
  const all = specs(html);
  // The column's own card, then one per segment (two models), then today's.
  assert.equal(all.length, 4);
  const [col, seg1, seg2, today] = all;
  assert.equal(col.sub, '$4 spent · 6 messages');
  assert.equal(rowText(col), '$3 fable 5.1 | $1 opus 5');             // dearest first, every row level
  assert.equal(col.rows[0].t, '75%');
  assert.ok(!col.rows[0].on && !col.rows[1].dim);
  assert.equal(col.rows[0].c, 'var(--s2)');                           // the slot is the model's, not its rank
  assert.equal(col.rows[1].c, 'var(--s1)');
  // Segments sit slot-1-first from the baseline, so the DOM leads with fable
  // (slot 2) and the hovered segment is the row that is lit.
  assert.ok(seg1.rows[0].on && seg1.rows[1].dim, 'the hovered model leads');
  assert.ok(seg2.rows[1].on && seg2.rows[0].dim);
  assert.ok(html.indexOf('var(--s2)"') < html.indexOf('var(--s1)"'), 'top of the stack first in the DOM');
  assert.equal(today.foot, 'today, and the day is not over');
  assert.equal(today.rows.length, 0);
  assert.ok(html.includes('<i class="none"></i>'), 'a $0 day is a hairline, still hoverable');
  assert.equal(col.foot, 'the most expensive day so far');
  // Thirty-five days of history under a 7-day window: thirty-five columns,
  // the empty ones drawn at zero, the foot naming the first day.
  const first = easternFields(new Date(Date.now() - 34 * 86400000)).on;
  const long = { ...s, history: [{ key: first, usd: 1, messages: 1, models: [{ key: 'claude-opus-5', usd: 1, messages: 1 }] }, ...s.history] };
  const h2 = spendDays(long);
  assert.equal((h2.match(/class="daycol"/g) || []).length, 35);
  assert.ok(h2.includes(dayLabel(first)), 'the left label is the first day there is');
});
