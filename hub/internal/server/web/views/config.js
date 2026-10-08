// life hub — laptop console: Configuration. views.config. The one page for
// "how is this thing set up": three dense blocks, no wide white space —
//   Powered by: the plan the sessions run on, this month's list-price spend,
//     each limit's headroom and the rung new sessions start on. One block per
//     provider, so a second one is a second block, not a redesign.
//   Goals: the active goals as small tiles, each the door to its page.
//   Data sources: one card per source, the provider's tile, rows and the
//     newest row; a section per group (sources.go sourceTags), its name in
//     the group's colour. A card opens #/sources/<g>/<s>.
// #/goals and #/spend stay as pages behind this tab (app.js lights it for
// them). The phone and desktop draw the same page (ConfigView.swift).
'use strict';

// The newest row: "12m ago" inside a day, the date after that.
function cfgDay(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Date.now() - d < 864e5) return ago(iso);
  const o = { month: 'short', day: 'numeric' };
  if (d.getFullYear() !== new Date().getFullYear()) o.year = 'numeric';
  return d.toLocaleDateString('en-US', o);
}

// A limit with this little left is "nearly out". The hub's amber `warn` is a
// pace call that lights early; this is the plain "you are about to hit the
// wall" state, so it is red and named at the top of the block.
const CFG_LOW_LEFT = 10;
const cfgLeft = w => Math.max(0, 100 - Math.round(w.utilization));
const cfgLow = w => cfgLeft(w) <= CFG_LOW_LEFT;
// "resets 11:59 PM", the hub's own words from the window's foot line.
const cfgResets = w => (w.foot || '').split(' · ').find(s => s.startsWith('resets')) || '';

// The model ladder's toggle: the same buttons as the Spend page
// (spend.js spendModelBar, setDefaultModel): the rung new sessions start on
// filled, a shut rung struck through, auto.
function cfgModelHTML(q, m) {
  const runs = m?.starts_on || m?.default_model || q.next_model || '';
  if (!runs) return '';
  const rungs = m?.rungs?.length ? m.rungs : (m?.options || []).map(o => ({ model: o, open: true }));
  const steps = rungs.map(r => r.open
    ? `<button class="sm ${r.model === runs ? 'on' : ''}" onclick="setDefaultModel('${esc(r.model)}')">${esc(modelShort(r.model))}</button>`
    : `<button class="sm" disabled title="${esc(r.why || 'closed')}" style="text-decoration:line-through;opacity:.5">${esc(modelShort(r.model))}</button>`
  ).join('<span class="muted">›</span>');
  const auto = rungs.length ? `<span class="muted">·</span><button class="sm ${m?.explicit ? '' : 'on'}" onclick="setDefaultModel('')">auto</button>` : '';
  return `<div class="cfg-model"><span class="small muted">New sessions run on</span><span class="chips-row">${steps || `<strong class="small">${esc(modelShort(runs))}</strong>`}${auto}</span></div>`;
}

function cfgPlanHTML(q, m) {
  const p = q?.plan;
  if (!p) return failHTML('the plan', 'pageRedraw()');
  const day = p.charged_on ? new Date(p.charged_on + 'T12:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) : '';
  const price = p.usd ? [`${usd0(p.usd)}/${p.period === 'monthly' ? 'mo' : esc(p.period || '')}`, day && `charged ${day}`].filter(Boolean).join(' · ') : '';
  const wins = q.available ? q.windows || [] : [];
  const lims = wins.map(w => {
    const low = cfgLow(w);
    const tone = low || w.tone === 'bad' ? 'var(--red)' : w.tone === 'warn' ? 'var(--amber)' : '';
    return `<div class="cfg-lim${low ? ' low' : ''}">
      <span class="small">${esc(w.label)}</span>
      <span class="small n"${tone ? ` style="color:${tone}"` : ''}>${cfgLeft(w)}% left</span>
      <div class="meter ${low ? 'bad' : esc(w.tone || '')}"><div style="width:${Math.min(100, w.utilization)}%"></div>${
        w.elapsed_pct > 0 ? `<i class="tick" style="left:${Math.min(100, w.elapsed_pct)}%"></i>` : ''}</div>
    </div>`;
  }).join('');
  const alerts = wins.filter(cfgLow).map(w => `<div class="cfg-alert">
      <strong>${esc(w.label)} nearly out</strong><span>${[`${cfgLeft(w)}% left`, cfgResets(w)].filter(Boolean).map(esc).join(' · ')}</span>
    </div>`).join('');
  return `<div class="cfg-prov">
    ${alerts}
    <div class="cfg-plan">${brandMark(p.brand)}
      <span class="grow"><strong>${esc(p.name)}</strong><span class="small muted">${[esc(p.via), price].filter(Boolean).join(' · ')}</span></span>
      <a class="cfg-month" href="#/spend"><span class="n">${usd0(p.month_usd)}</span><span class="small muted">this month</span></a>
    </div>
    ${lims ? `<div class="cfg-lims">${lims}</div>` : q.error ? `<div class="small err">${esc(q.error)}</div>` : ''}
    ${cfgModelHTML(q, m)}
  </div>`;
}

function cfgGoalsHTML(goals) {
  const on = (goals || []).filter(g => g.status === 'active');
  if (!on.length) return emptyHTML('No active goals.');
  return `<div class="cfg-goals">${on.map(g => `<a class="cfg-goal" href="#/goals/${encodeURIComponent(g.id)}" style="--h:${goalHue(g)}">
      ${goalEmblem(g)}<span>${mdInline(g.title, false)}</span></a>`).join('')}</div>`;
}

function cfgSourceCard(g, s) {
  const bad = s.status === 'failing';
  const as = s.accounts || [];
  const sub = bad ? `<span class="small sfail clamp1">${esc(sourceFailBits(s))}</span>`
    : `<span class="small muted clamp1">${esc(s.summary || (as[0] && as[0].label) || '')}</span>`;
  const facts = [s.total != null ? num(s.total) + ' rows' : '', cfgDay(s.last)].filter(Boolean).join(' · ');
  return `<a class="scard${bad ? ' bad' : ''}" href="${sourceHash(g, s)}" style="--c:${esc(g.color || '#64748B')}">
    <span class="top">${brandMark(s.brand)}<span class="grow"><span class="nm">${esc(s.title || s.id)}</span>${sub}</span></span>
    <span class="bot"><span class="small muted">${esc(facts)}</span></span>
  </a>`;
}

// A section per group: the group's name in its colour (the goal's hue) over
// its own cards, each section on a row of its own (two goals side by side
// read as one list). Groups sharing a tag are one section. The hub's group
// order (most sources first) is the section order; inside a section, by name.
function cfgSourcesHTML(groups) {
  const secs = [];
  for (const g of groups) {
    const tag = g.tag || g.title;
    let sec = secs.find(s => s.tag === tag);
    if (!sec) secs.push(sec = { tag, color: g.color, cards: [] });
    for (const s of g.sources || []) sec.cards.push([g, s]);
  }
  if (!secs.length) return emptyHTML('Nothing connected.', 'card');
  const key = ([, s]) => (s.title || s.id).toLowerCase();
  return `<div class="sgroups">${secs.map(sec => `<section class="sgroup" style="--c:${esc(sec.color || '#64748B')}">
      <h4>${esc(sec.tag)}<span class="muted">${sec.cards.length}</span></h4>
      <div class="scards">${sec.cards.sort((a, b) => key(a).localeCompare(key(b))).map(([g, s]) => cfgSourceCard(g, s)).join('')}</div>
    </section>`).join('')}</div>`;
}

async function drawConfig(view) {
  const [q, m, goals, src] = await Promise.all([
    get('/spend/quota').catch(() => null), get('/spend/model').catch(() => null),
    get('/goals').catch(() => null), get('/sources').catch(() => null)]);
  setRedraw(() => drawConfig(view), view, { every: 60000 });
  const groups = (src?.groups || []).filter(g => (g.sources || []).length);
  paint(view, `<div class="pane wide cfg"><h2>Configuration</h2>
    <div class="cfg-top">
      <section class="card"><h3>Powered by</h3>${q ? cfgPlanHTML(q, m) : failHTML('the plan', 'pageRedraw()')}</section>
      <section class="card"><h3><a href="#/goals">Goals</a></h3>${goals ? cfgGoalsHTML(goals) : failHTML('goals', 'pageRedraw()')}</section>
    </div>
    <h3>Data sources</h3>
    ${src ? cfgSourcesHTML(groups) : failHTML('sources', 'pageRedraw()')}
  </div>`);
}

views.config = { draw: drawConfig, redraw: () => (route.redraw ? pageRedraw() : render()) };
