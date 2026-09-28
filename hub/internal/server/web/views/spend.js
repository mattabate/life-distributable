// life hub — laptop console: Spend (plan quota, the model ladder, spend by
// day). views.spend.
'use strict';
// ================= spend =================
// The same page as the phone's SpendView, in the same order: the model bar,
// plan limits, by day, unpriced models. The console used to show only the
// limits (parity). There is no "By model" table: the split by model is
// INSIDE each day's bar and its tooltip. There is no range picker either:
// the chart is all-time, so a picker sliced nothing on screen, and each tap
// re-routed the page to the top.
const SPEND_QUERY = 'days=30';

/// A model's colour is its slot in the hub's `palette` — a property of the
/// model, so the 7d and 1y ranges paint Opus the same blue; nothing past the
/// eighth slot gets a generated hue, it draws grey.
function spendColor(s, model) {
  const i = (s.palette || []).indexOf(model);
  return i < 0 || i > 7 ? 'var(--s-other)' : `var(--s${i + 1})`;
}

/// The day's card: the split by model, dearest first, each with its dollars
/// and its share of the day. Hovering ONE segment lights that model's row and the rest go
/// quiet; the column itself shows every row level. No legend on the chart:
/// this card is the legend, one day at a time.
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
  // whatever window the summary was asked for (a 30-day window can hide the
  // dearest days) — a segment per model with slot
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
    <div class="spread small muted"><span>${esc(dayLabel(days[0].key))}</span><span>today</span></div>`;
}

// The toggle inside the "New sessions run on …" bar. What it sets is pinned
// into the NEXT new session and never changes a running one, so the prompt
// cache survives a chat. The line under it is the picker's own answer FOR
// THAT RUNG (hub ExplainFrom), so it only says the ladder stepped in when a
// bucket really did override the owner's pick; `next_reason` describes an
// unpinned session and must not be shown under a set toggle.
//
// It is the whole ladder, top first: the rung new
// sessions start on is filled, a rung our limits shut is struck through and
// cannot be picked, and "auto" hands the choice back to the ladder.
function spendModelBar(q, m) {
  if (!q.next_model && !m) return '';
  const runs = m?.starts_on || m?.default_model || q.next_model || '';
  const rungs = m?.rungs?.length ? m.rungs : (m?.options || []).map(o => ({ model: o, open: true }));
  const steps = rungs.map(r => r.open
    ? `<button class="sm ${r.model === runs ? 'on' : ''}" onclick="setDefaultModel('${r.model}')">${esc(modelShort(r.model))}</button>`
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
    return `<div class="card">
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
  const byDay = s ? `<h3>By day</h3><div class="card">${spendDays(s)}</div>` : '<div class="card err">Spend summary unavailable.</div>';
  paint(view, `<div class="pane wide"><h2>Spend</h2>
    ${spendModelBar(q, m)}
    <h3>Limits</h3>
    ${q.available ? wins : card(`Plan usage unavailable: ${esc(q.error || '')}`, 'err')}
    ${byDay}
    ${s?.unknown_models?.length ? `<div class="small err mt8">Unpriced (assumed Opus rate): ${esc(s.unknown_models.join(', '))}</div>` : ''}
  </div>`);
}
// A redraw (the model toggle, Retry) repaints in place: `render()` would
// swap #view for a fresh node and land the reader at the top of the page.
views.spend = { draw: drawSpend, redraw: () => (route.redraw ? pageRedraw() : render()) };
