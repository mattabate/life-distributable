// life hub — laptop console: Spend (the model bar, plan limits, by day).
// views.spend — out of money.js since 2026-11-05 (distributable v4):
// Configuration's model order calls setDefaultModel, and Configuration goes
// public whole while Money stays a private module.
'use strict';

// The same page as the phone's SpendView, in the same order: the model bar,
// plan limits, by day, unpriced models. The console used to show only the
// limits (parity, 2026-08-28). The "By model" table under the day chart
// went on 2026-09-17 ("isn't particularly interesting"): the split by model
// is INSIDE each day's bar and its tooltip. The range picker and the four
// tiles under it (today / last N days / messages / avg per active day) went
// on 2026-09-25 — on the console: "I don't even really care about this
// spend by period… I just want the by-day bar chart… and the spend and
// limits." The chart was all-time already, so the picker sliced nothing he
// looked at, and each tap re-routed the page to the top.
const SPEND_QUERY = 'days=30';

/// A model's colour is its slot in the hub's `palette` — a property of the
/// model, so the 7d and 1y ranges paint Opus the same blue; nothing past the
/// eighth slot gets a generated hue, it draws grey. Same table as `mixColor`.
function spendColor(s, model) {
  const i = (s.palette || []).indexOf(model);
  return i < 0 || i > 7 ? 'var(--s-other)' : `var(--s${i + 1})`;
}

/// The day's card: the split by model, dearest first, each with its dollars
/// and its share of the day (2026-09-17: "when I scroll over any bar …
/// I see the breakdown in model spend for the day — '200 to Fable, 300 to
/// Fable 5.1'"). Hovering ONE segment lights that model's row and the rest go
/// quiet; the column itself shows every row level. The key under the chart
/// (spendKey) names the colours for a reader who is not hovering.
function spendDayTip(s, d, max, lit) {
  return {
    title: fullDay(d.key),
    sub: d.usd ? `${usd(d.usd)} spent · ${num(d.messages)} ${d.messages === 1 ? 'message' : 'messages'}` : '',
    rows: d.models.map(m => ({
      c: spendColor(s, m.key), v: usd(m.usd), k: modelShort(m.key),
      t: Math.round(100 * m.usd / d.usd) + '%', on: m.key === lit, dim: !!lit && m.key !== lit,
    })),
    foot: d.today ? 'today, and the day is not over'
      : d.usd ? (d.usd === max ? 'the most expensive day so far' : '')
        : 'nothing ran that day',
  };
}

function spendDays(s) {
  // One stacked bar per day from the first transcript to today — all-time,
  // whatever window the summary was asked for (2026-09-22: a 30-day
  // window hid the first four days, the dearest among them) — a segment per model with slot
  // 1 on the baseline in every bar; empty days are gaps in the data, so they
  // draw at zero rather than vanish.
  const by = Object.fromEntries((s.history || []).map(d => [d.key, d]));
  const slot = k => { const i = (s.palette || []).indexOf(k); return i < 0 ? 99 : i; };
  const today = easternFields(new Date()).on;
  const first = (s.history || [])[0]?.key || today;
  const days = [];
  // Whole UTC days from the first key: exact, no DST arithmetic.
  for (let t = Date.UTC(+first.slice(0, 4), +first.slice(5, 7) - 1, +first.slice(8, 10)); ; t += 86400000) {
    const key = new Date(t).toISOString().slice(0, 10);
    if (key > today) break;
    const b = by[key];
    days.push({ key, usd: b?.usd || 0, messages: b?.messages || 0, today: key === today, models: (b?.models || []).slice() });
  }
  if (!days.length) days.push({ key: today, usd: 0, messages: 0, today: true, models: [] });
  const max = Math.max(...days.map(d => d.usd)) || 1;
  return `<div class="row" style="align-items:flex-end;gap:2px;height:120px">${days.map(d => {
    // Segments are stacked bottom-up in slot order, so the DOM (top-down) is
    // that order reversed. Rows in the card stay dearest first — the reader
    // wants the biggest number first, the stack wants a fixed shape.
    const stack = d.models.slice().sort((a, b) => slot(b.key) - slot(a.key));
    const segs = stack.length
      ? stack.map(m => `<i style="height:${(m.usd / max * 100).toFixed(1)}%;background:${spendColor(s, m.key)}" ${tipAttr(spendDayTip(s, d, max, m.key))}></i>`).join('')
      : '<i class="none"></i>';
    // The hit target is the whole COLUMN, not the bar: a $0 day draws one
    // pixel tall and would otherwise be the one bar you cannot ask about.
    return `<div class="daycol" ${tipAttr(spendDayTip(s, d, max, null))}>${segs}</div>`;
  }).join('')}</div>
    <div class="spread small muted mt4"><span>${esc(dayLabel(days[0].key))}</span><span>today</span></div>
    ${spendKey(s, days)}`;
}

/// The models under the chart, in stack order (2026-09-30: "put the
/// models under the chart", no filters) — only ones that drew a bar; the
/// grey ones past the palette share one "other".
function spendKey(s, days) {
  const slot = k => { const i = (s.palette || []).indexOf(k); return i < 0 || i > 7 ? 99 : i; };
  const seen = new Set(days.flatMap(d => d.models.map(m => m.key)));
  const keys = [...seen].filter(k => slot(k) < 99).sort((a, b) => slot(a) - slot(b));
  const items = keys.map(k => [spendColor(s, k), modelShort(k)]);
  if ([...seen].some(k => slot(k) === 99)) items.push(['var(--s-other)', 'other']);
  return items.length ? `<div class="spend-key small">${items.map(([c, k]) =>
    `<span class="pkey"><i style="background:${c}"></i>${esc(k)}</span>`).join('')}</div>` : '';
}

// The toggle inside the "New sessions run on …" bar (rec-fce2fd74,
// 2026-08-29: "on the spend page, you have a toggle inside the bar that shows
// the current model"). Two rungs, because those are the two he uses; what it
// sets is pinned into the NEXT new session and never changes a running one,
// so the prompt cache survives a chat. The line under it is the picker's own
// answer FOR THAT RUNG (hub ExplainFrom), so it only says the ladder stepped
// in when a bucket really did override his pick; `next_reason` describes an
// unpinned session and must not be shown under a set toggle (2026-09-02).
//
// Since 2026-09-27 it is the whole ladder, top first ("show all
// available models in the rank, which one is used and which ones are
// blocked… click between the open ones to freeze things"): the rung new
// sessions start on is filled, a rung our limits shut is struck through and
// cannot be picked, and "auto" hands the choice back to the ladder.
function spendModelBar(q, m) {
  if (!q.next_model && !m) return '';
  const runs = m?.starts_on || m?.default_model || q.next_model || '';
  const rungs = m?.rungs?.length ? m.rungs : (m?.options || []).map(o => ({ model: o, open: true }));
  const steps = rungs.map(r => r.open
    ? `<button class="sm ${r.model === runs ? 'on' : ''}" onclick="setDefaultModel(${esc(JSON.stringify(r.model))})">${esc(modelShort(r.model))}</button>`
    : `<button class="sm" disabled style="text-decoration:line-through;opacity:.5">${esc(modelShort(r.model))}</button>`
  ).join('<span class="muted"> › </span>');
  const auto = `<button class="sm ${m?.explicit ? '' : 'on'}" onclick="setDefaultModel('')">auto</button>`;
  const shut = rungs.filter(r => !r.open).map(r => `${esc(modelShort(r.model))}: ${esc(r.why || 'closed')}`);
  const pin = m?.explicit ? `pinned to ${esc(modelShort(m.default_model))}` : 'auto';
  return `<div class="card">
    <div class="spread"><strong>New sessions run on ${esc(modelShort(runs))}</strong>
      <span class="chips-row">${steps}<span class="muted"> · </span>${auto}</span></div>
    <div class="small muted">${[pin, ...shut].join(' · ')}</div>
  </div>`;
}

async function setDefaultModel(id) {
  return act(() => put('/spend/model', { default_model: id }));
}

async function drawSpend(view) {
  const [q, s, m] = await Promise.all([get('/spend/quota').catch(() => null),
    get('/spend/summary?' + SPEND_QUERY).catch(() => null),
    get('/spend/model').catch(() => null)]);
  // Registered before anything can fail, so Retry and the live loop both work.
  setRedraw(() => drawSpend(view), view, { every: 30000 });
  if (!q) { paint(view, `<div class="pane wide"><h2>Spend</h2>${failHTML('plan limits', 'pageRedraw()')}</div>`); return; }
  // Tone, outlook and the foot line are the hub's (spend.fillWords), so this
  // card and the phone's say the same thing in the same colour.
  const wins = (q.windows || []).map(w => {
    const tone = w.tone === 'bad' ? 'var(--red)' : w.tone === 'warn' ? 'var(--amber)' : '';
    return `<div class="lim">
      <div class="spread"><strong>${esc(w.label)}</strong>
        <span style="text-align:right"><span style="font-weight:600;${tone ? 'color:' + tone : ''}">${Math.round(w.utilization)}%</span>
        ${w.outlook ? `<span class="small ${tone ? '' : 'muted'}" style="${tone ? 'color:' + tone : ''}"> · ${esc(w.outlook)}</span>` : ''}</span></div>
      <div class="meter ${esc(w.tone || '')}"><div style="width:${Math.min(100, w.utilization)}%"></div>${
        w.elapsed_pct > 0 ? `<i class="tick" style="left:${Math.min(100, w.elapsed_pct)}%"></i>` : ''}</div>
      ${w.foot ? `<div class="small muted">${esc(w.foot)}</div>` : ''}
    </div>`;
  }).join('');
  // The day chart is all-time (history[]), whatever window the summary was
  // asked for; the window only scopes the unpriced-models line.
  const byDay = s ? `<h3>Spend by day</h3><div class="card">${spendDays(s)}</div>` : '<div class="card err">Spend summary unavailable.</div>';
  // The limits share one card, a hairline between windows (2026-09-30:
  // three cards stacked was "too much white space in between the lines").
  paint(view, `<div class="pane wide"><h2>Spend</h2>
    ${spendModelBar(q, m)}
    <h3>Limits</h3>
    ${q.available ? `<div class="card limits">${wins}</div>` : card(`Plan usage unavailable: ${esc(q.error || '')}`, 'err')}
    ${byDay}
    ${s?.unknown_models?.length ? `<div class="small err mt8">Unpriced (assumed Opus rate): ${esc(s.unknown_models.join(', '))}</div>` : ''}
  </div>`);
}
// A redraw (the model toggle, Retry) repaints in place: `render()` would
// swap #view for a fresh node and land him at the top of the page.
views.spend = { draw: drawSpend, redraw: () => (route.redraw ? pageRedraw() : render()) };
registerPlace('spend', { label: 'Spend', view: 'views/spend.js', phone: 'SpendView.swift', tab: 'config' });
