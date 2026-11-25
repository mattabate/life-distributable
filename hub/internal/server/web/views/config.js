// life hub — laptop console: Configuration (2026-10-06: "here's how the
// sessions work, they're powered by Claude Code … here are my goals … here
// are my data sources"). views.config. It replaced the Sources tab: dense
// blocks, no wide white space —
//   Powered by: the plan (its price is the card charge the hub found), this
//     month's list-price spend and each limit's headroom, the model ladder
//     sessions run on (/spend/model). One block per provider, so a second one is a second block,
//     not a redesign.
//   Devices: each Mac the voice reaches (/settings devices) with its Audio
//     switch, then the phone (/status).
//   Team: the org chart: the owner on top, a card per role that reports to
//     the owner (/personas reports_to), its goals' emblems (goals) beside its
//     name, its own reports listed inside. Roles and their lines are made by
//     asking the chief of staff, never a form, so there is none here.
//   Goals: the active goals as small tiles under the owner's sections, each
//     the door to its page.
//   Data sources: one card per source, the provider's tile, rows and the
//     newest row; a section per GOAL (sources.go sourceGoals), the goal's
//     title in the goal's own colour. A card opens #/sources/<g>/<s>.
// The phone and desktop draw the same page (ConfigView.swift).
'use strict';

// The newest row: "12m ago" inside a day, the date after that.
function cfgDay(iso) {
  if (!iso) return '';
  if (Date.now() - new Date(iso) < 864e5) return ago(iso);
  return dayWords(iso);
}

// A limit with this little left is "nearly out" (2026-10-06): the hub's amber
// `warn` is a pace call that lights at 64% too; this is the plain "you are
// about to hit the wall" state, so its row goes red: label, number and meter.
// No banner over the block (2026-10-13) — the row says it.
const CFG_LOW_LEFT = 10;
const cfgLeft = w => Math.max(0, 100 - Math.round(w.utilization));
const cfgLow = w => cfgLeft(w) <= CFG_LOW_LEFT;

// The model ladder, one row (#261): the same buttons as the Spend bar
// (spend.js spendModelBar, setDefaultModel), top first (fable, opus,
// sonnet). The rung sessions run on is filled; a rung its cutoff or the cap
// (/settings `model_cap`, `lifectl settings`) shut is struck through, with
// why on hover. When the ladder has stepped down from the rung chosen, the
// line under it says where and why. /settings `models` stands in when
// /spend/model did not answer.
function cfgModelHTML(q, m, st) {
  const runs = m?.starts_on || m?.default_model || q.next_model || '';
  const rungs = m?.rungs?.length ? m.rungs
    : (m?.options?.length ? m.options : st?.models || []).map(o => ({ model: o, open: true }));
  if (!runs && !rungs.length) return '';
  const steps = rungs.map(r => r.open
    ? `<button class="sm ${r.model === runs ? 'on' : ''}" onclick="setDefaultModel(${esc(JSON.stringify(r.model))})">${esc(modelShort(r.model))}</button>`
    : `<button class="sm" disabled title="${esc(r.why || 'closed')}" style="text-decoration:line-through;opacity:.5">${esc(modelShort(r.model))}</button>`
  ).join('<span class="muted">›</span>');
  const down = m?.explicit && m.starts_on && m.starts_on !== m.default_model
    ? [`${modelShort(m.default_model)} handed over to ${modelShort(m.starts_on)}`, m.reason].filter(Boolean).join(' · ') : '';
  return `<div class="cfg-model"><span class="small muted">Sessions run on</span><span class="chips-row">${steps || `<strong class="small">${esc(modelShort(runs))}</strong>`}</span>${
    down ? `<span class="small muted cfg-model-note">${esc(down)}</span>` : ''}</div>`;
}

function cfgPlanHTML(q, m, st) {
  const p = q?.plan;
  if (!p) return failHTML('the plan', 'pageRedraw()');
  const day = p.charged_on ? dayLabel(p.charged_on) : '';
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
  return `<div class="cfg-prov">
    <a class="cfg-plan" href="#/spend">${brandMark(p.brand)}
      <span class="grow"><strong>${esc(p.name)}</strong><span class="small muted">${[esc(p.via), price].filter(Boolean).join(' · ')}</span></span>
      <span class="cfg-month"><span class="n">${usd0(p.month_usd)}</span><span class="small muted">this month</span></span>
    </a>
    ${lims ? `<div class="cfg-lims">${lims}</div>` : q.billing === 'api' ? '<div class="small muted">Billed by the API, no plan windows</div>'
      : q.error ? `<div class="small err">${esc(q.error)}</div>` : ''}
    ${cfgModelHTML(q, m, st)}
  </div>`;
}

function cfgGoalsHTML(goals) {
  const on = (goals || []).filter(g => g.status === 'active');
  if (!on.length) return emptyHTML('No active goals.');
  return goalGroups(on).map(([name, gs]) => `${name ? `<div class="goal-group small muted">${esc(name)}</div>` : ''}
    <div class="cfg-goals">${gs.map(g => `<a class="cfg-goal" href="#/goals/${encodeURIComponent(g.id)}" style="--h:${goalHue(g)}">
      ${goalEmblem(g)}<span>${mdInline(g.title, false)}</span></a>`).join('')}</div>`).join('');
}

// The team: the org chart, a tree: the owner on top, a card per role that
// reports to the owner (`reports_to` "" or naming no role), the emblems of
// the goals it owns (`goals`) on the card's top line beside its name, and the
// roles under it listed inside the card, a step in per level. A role's name
// is the title of the session playing it (from /threads?persona=1), else its
// role words; a name opens the session. No switches: the team is the only
// mode (#259).
function cfgTeamHTML(st, personas, sessions, goals) {
  if (!personas) return `<div class="cfg-team">${failHTML('the team', 'pageRedraw()')}</div>`;
  if (!personas.length) return `<div class="cfg-team">${emptyHTML('No roles yet.')}</div>`;
  return `<div class="cfg-team">${cfgOrgHTML(personas, sessions, goals, st?.owner_name)}</div>`;
}

// The top is /settings owner_name (hub.json's), else You.
function cfgOrgHTML(personas, sessions, goals, owner) {
  const byKey = new Map((sessions || []).map(t => [t.persona, t]));
  const byId = new Map((sessions || []).map(t => [t.id, t]));
  const goalById = new Map((goals || []).map(g => [g.id, g]));
  const keys = new Set(personas.map(p => p.key));
  const sid = p => p.session || byKey.get(p.key)?.id || '';
  const name = p => byId.get(p.session)?.title || byKey.get(p.key)?.title || p.role || p.key;
  const boss = p => (p.reports_to && p.reports_to !== p.key && keys.has(p.reports_to) ? p.reports_to : '');
  const kids = b => personas.filter(p => boss(p) === b);
  // Each role once, however the lines run (the hub refuses a loop; a role
  // caught in one anyway is drawn under the owner).
  const placed = new Set();
  const under = (b, depth) => kids(b).filter(p => !placed.has(p.key) && placed.add(p.key))
    .flatMap(p => [[p, depth], ...under(p.key, depth + 1)]);
  const marks = p => (p.goals || []).map(id => goalById.get(id)).filter(Boolean).map(g =>
    `<a href="#/goals/${encodeURIComponent(g.id)}" title="${esc(g.title)}">${goalEmblem(g)}</a>`).join('');
  const link = (p, cls, inner) => sid(p) ? `<a class="${cls}" href="#/sessions/${encodeURIComponent(sid(p))}">${inner}</a>` : `<span class="${cls}">${inner}</span>`;
  const roleWords = p => (p.role && p.role !== name(p) ? p.role : '');
  const card = p => {
    const reps = under(p.key, 0);
    return `<div class="cfg-ocard">
      <div class="hd">${link(p, 'nm', esc(name(p)))}<span class="cfg-marks">${marks(p)}</span></div>
      ${roleWords(p) ? link(p, 'role muted', esc(roleWords(p))) : ''}
      ${reps.map(([r, d]) => `<div class="cfg-orep"${d ? ` style="padding-left:${d * 10}px"` : ''}>${
        link(r, 'cfg-who', `<strong>${esc(name(r))}</strong><span class="muted">${esc(roleWords(r))}</span>`)}<span class="cfg-marks sm">${marks(r)}</span></div>`).join('')}
    </div>`;
  };
  const top = kids('').filter(p => !placed.has(p.key) && placed.add(p.key));
  const cards = top.map(card);
  // Whatever a loop left out still gets a card.
  for (const p of personas) if (!placed.has(p.key)) { placed.add(p.key); cards.push(card(p)); }
  return `<div class="cfg-org"><div class="cfg-me">${esc((owner || '').trim() || 'You')}</div><div class="cfg-ocards">${cards.join('')}</div></div>`;
}

// Where a Mac's voice goes now, from its ear's last poll (/settings devices).
function cfgEarWord(d) {
  const out = d.ears === 'headphones' ? `${d.output || 'Headphones'} in now`
    : d.ears === 'speakers' ? 'on its speakers now'
    : d.ears === 'none' ? 'no output now' : 'voice not running';
  return [out, d.here ? "you're here" : ''].filter(Boolean).join(' · ');
}

// A Mac's name on its tile: a MacBook reads MacBook, the hub's says so.
function cfgMacName(d) {
  return `${/macbook/i.test(d.device) ? 'MacBook' : d.device === 'mac' ? 'Mac' : d.device}${d.hub ? ' · hub' : ''}`;
}

// Devices: one tile per Mac whose voice the hub knows (/settings devices: the
// hub's Mac first, how long it has been up and its clock's zone), each with
// the word Audio and its switch on the name line, as the desktop draws it
// (off = that Mac says no card's line); then the phone, when the app is
// installed (its profile's days left).
function cfgDevicesHTML(s, st) {
  const up = Number(s.uptime_s) || 0;
  const upWord = up >= 86400 ? `${Math.floor(up / 86400)}d` : up >= 3600 ? `${Math.floor(up / 3600)}h` : `${Math.floor(up / 60)}m`;
  const phone = s.app_installed
    ? [s.app_profile_days_left != null ? `profile ${s.app_profile_days_left} ${s.app_profile_days_left === 1 ? 'day' : 'days'} left` : 'app installed',
      s.app_profile_expires ? `until ${dayLabel(String(s.app_profile_expires).slice(0, 10))}` : ''].filter(Boolean).join(' · ')
    : 'app not installed';
  const macs = (st?.devices || []).filter(d => d.kind === 'mac');
  if (!macs.length) macs.push({ device: 'mac', hub: true, audio: true, ears: '' });
  const macTile = d => `<label class="cfg-setting cfg-mac">
      <span class="top"><strong>${esc(cfgMacName(d))}</strong><span class="cfg-vol"><span class="small muted">Audio</span><span class="switch"><input type="checkbox"${d.audio ? ' checked' : ''} onchange="setMacAudio(${esc(JSON.stringify(d.device))}, this.checked)"><span></span></span></span></span>
      <span class="small muted">${esc([d.saying ? 'saying a card' : cfgEarWord(d), d.hub ? `up ${upWord}` : '', d.hub ? s.time_zone : ''].filter(Boolean).join(' · '))}</span>
    </label>`;
  const tile = (name, word) => `<div class="cfg-setting">
      <span class="top"><strong>${esc(name)}</strong></span>
      <span class="small muted">${esc(word)}</span>
    </div>`;
  return `<div class="cfg-settings">${macs.map(macTile).join('')}${tile('Phone', phone)}</div>`;
}

// A Mac's Audio switch: PATCH /settings audio_by.
async function setMacAudio(device, on) {
  return act(() => patch('/settings', { audio_by: { [device]: on } }));
}

function cfgSourceCard(g, s) {
  const bad = s.status === 'failing';
  const as = s.accounts || [];
  const sub = bad ? `<span class="small sfail clamp1">${esc(sourceFailBits(s))}</span>`
    : `<span class="small muted clamp1">${esc(s.summary || (as[0] && as[0].label) || '')}</span>`;
  const facts = [s.count || (s.total != null ? num(s.total) + ' rows' : ''), cfgDay(s.last || s.last_ok)].filter(Boolean).join(' · ');
  return `<a class="scard${bad ? ' bad' : ''}" href="${sourceHash(g, s)}" style="--c:${esc(g.color || '#64748B')}">
    <span class="top">${brandMark(s.brand)}<span class="grow"><span class="nm">${esc(s.title || s.id)}</span>${sub}</span></span>
    <span class="bot"><span class="small muted">${esc(facts)}</span></span>
  </a>`;
}

// The cards grouped by goal (2026-10-07: "the data sources should be grouped
// under goals"), the goal's name in its colour (the hub's, the goal's hue);
// groups sharing a tag are one group. The hub's group order (the Goals card's)
// is the group order; inside a group, by name. One grid, filled left to right
// (2026-10-16): a goal's name sits over its first card and the next goal
// starts in the hole the last one left. Every cell keeps the name line
// (hidden after the first) so a row's cards line up.
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
  return `<div class="sflow">${secs.map(sec => sec.cards.sort((a, b) => key(a).localeCompare(key(b))).map(([g, s], i) =>
    `<div class="scell" style="--c:${esc(sec.color || '#64748B')}"><h4${i ? ' class="ghost" aria-hidden="true"' : ''}>${esc(sec.tag)}<span class="muted">${sec.cards.length}</span></h4>${cfgSourceCard(g, s)}</div>`).join('')).join('')}</div>`;
}

async function drawConfig(view) {
  const [q, m, goals, src, st, status] = await Promise.all([
    get('/spend/quota').catch(() => null), get('/spend/model').catch(() => null),
    get('/goals').catch(() => null), get('/sources').catch(() => null),
    get('/settings').catch(() => null), get('/status').catch(() => null)]);
  const [personas, sessions] = await Promise.all([
    get('/personas').catch(() => null), get('/threads?persona=1').catch(() => [])]);
  // A Mac saying a line redraws soon, so "saying a card" goes when the line does.
  setRedraw(() => drawConfig(view), view, { every: (st?.devices || []).some(d => d.saying) ? 3000 : 60000 });
  const groups = (src?.groups || []).filter(g => (g.sources || []).length);
  paint(view, `<div class="pane wide cfg"><h2>Configuration</h2>
    <div class="cfg-top">
      <section class="card"><h3>Powered by</h3>${q ? cfgPlanHTML(q, m, st) : failHTML('the plan', 'pageRedraw()')}</section>
      <section class="card cfg-dev"><h3>Devices</h3>${status ? cfgDevicesHTML(status, st) : failHTML('devices', 'pageRedraw()')}</section>
    </div>
    <div class="cfg-top">
      <section class="card cfg-dev"><h3>Team</h3>${cfgTeamHTML(st, personas, sessions, goals)}</section>
      <section class="card"><h3><a href="#/goals">Goals</a></h3>${goals ? cfgGoalsHTML(goals) : failHTML('goals', 'pageRedraw()')}</section>
    </div>
    <h3>Data sources</h3>
    ${src ? cfgSourcesHTML(groups) : failHTML('sources', 'pageRedraw()')}
  </div>`);
}

views.config = { draw: drawConfig, redraw: () => (route.redraw ? pageRedraw() : render()) };
// Last on the bar; Goals and Spend folded into it (2026-10-06): #/goals and
// #/spend are still pages, opened from its blocks.
registerTab({ key: 'config', label: 'Configuration', order: 90 });
registerPlace('config', { label: 'Configuration', view: 'views/config.js', phone: 'ConfigView.swift' });
