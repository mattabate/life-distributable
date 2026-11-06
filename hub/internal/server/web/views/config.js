// life hub — laptop console: Configuration (2026-10-06: "here's how the
// sessions work, they're powered by Claude Code … here are my goals … here
// are my data sources"). views.config. It replaced the Sources tab: dense
// blocks, no wide white space —
//   Powered by: the plan (its price is the card charge the hub found), this
//     month's list-price spend and each limit's headroom, the model order new
//     sessions start on (/spend/model) and the cap above it (/settings
//     model_cap). One block per provider, so a second one is a second block,
//     not a redesign.
//   Devices: the hub's Mac and the phone, from /status.
//   Team: the owner's two switches (/settings team_mode, plain_sessions),
//     then the roles (/personas) under their `team` label, a card each with
//     the session's name and the role's title. Roles are made by asking the
//     chief of staff, never a form, so there is none here.
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

// The model ladder's toggle (2026-10-06): the same buttons as the Spend bar
// (spend.js spendModelBar, setDefaultModel): the rung new sessions start on
// filled, a shut rung struck through, auto. The ladder is the hub's, top
// first (fable, opus, sonnet); /settings `models` stands in when /spend/model
// did not answer.
function cfgModelHTML(q, m, st) {
  const runs = m?.starts_on || m?.default_model || q.next_model || '';
  const rungs = m?.rungs?.length ? m.rungs
    : (m?.options?.length ? m.options : st?.models || []).map(o => ({ model: o, open: true }));
  if (!runs && !rungs.length) return '';
  const steps = rungs.map(r => r.open
    ? `<button class="sm ${r.model === runs ? 'on' : ''}" onclick="setDefaultModel(${esc(JSON.stringify(r.model))})">${esc(modelShort(r.model))}</button>`
    : `<button class="sm" disabled title="${esc(r.why || 'closed')}" style="text-decoration:line-through;opacity:.5">${esc(modelShort(r.model))}</button>`
  ).join('<span class="muted">›</span>');
  const auto = rungs.length ? `<span class="muted">·</span><button class="sm ${m?.explicit ? '' : 'on'}" onclick="setDefaultModel('')">auto</button>` : '';
  return `<div class="cfg-model"><span class="small muted">New sessions run on</span><span class="chips-row">${steps || `<strong class="small">${esc(modelShort(runs))}</strong>`}${auto}</span></div>`;
}

// The cap: the best model the ladder may start a session on (/settings
// `model_cap`, "" = none), one button per rung of `models` and "none".
function cfgCapHTML(st) {
  if (!st || !(st.models || []).length) return '';
  const cap = st.model_cap || '';
  const btn = (v, label) => `<button class="sm ${cap === v ? 'on' : ''}" onclick="setModelCap('${esc(v)}')">${esc(label)}</button>`;
  return `<div class="cfg-model"><span class="small muted">Never above</span><span class="chips-row">${
    st.models.map(x => btn(x, modelShort(x))).join('<span class="muted">›</span>')}<span class="muted">·</span>${btn('', 'no cap')}</span></div>`;
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
    ${lims ? `<div class="cfg-lims">${lims}</div>` : q.error ? `<div class="small err">${esc(q.error)}</div>` : ''}
    ${cfgModelHTML(q, m, st)}
    ${cfgCapHTML(st)}
  </div>`;
}

function cfgGoalsHTML(goals) {
  const on = (goals || []).filter(g => g.status === 'active');
  if (!on.length) return emptyHTML('No active goals.');
  return goalGroups(on).map(([name, gs]) => `${name ? `<div class="goal-group small muted">${esc(name)}</div>` : ''}
    <div class="cfg-goals">${gs.map(g => `<a class="cfg-goal" href="#/goals/${encodeURIComponent(g.id)}" style="--h:${goalHue(g)}">
      ${goalEmblem(g)}<span>${mdInline(g.title, false)}</span></a>`).join('')}</div>`).join('');
}

// One switch tile: the title, the switch right after it.
function cfgSwitchHTML(key, title, on) {
  return `<label class="cfg-setting">
      <span class="top"><strong>${esc(title)}</strong><span class="switch"><input type="checkbox"${on ? ' checked' : ''} onchange="setSetting('${key}', this.checked)"><span></span></span></span>
    </label>`;
}

// The team: the two switches, then the roles under their `team` label (in the
// hub's role order; a role with no label sits under "Other" when anything is
// labelled). A card per role: the name of the session playing it (its title,
// from /threads?persona=1), then the role's title. A role with no session yet
// is its title alone.
function cfgTeamHTML(st, personas, sessions) {
  const switches = st ? `<div class="cfg-settings">
      ${cfgSwitchHTML('team_mode', 'Personas', !!st.team_mode)}
      ${st.team_mode ? cfgSwitchHTML('plain_sessions', 'Plain sessions', !!st.plain_sessions) : ''}
    </div>` : failHTML('settings', 'pageRedraw()');
  if (!st || !st.team_mode) return switches;
  const byKey = new Map((sessions || []).map(t => [t.persona, t]));
  const byId = new Map((sessions || []).map(t => [t.id, t]));
  const groups = new Map();
  for (const p of personas || []) {
    const team = String(p.team || '').trim();
    if (!groups.has(team)) groups.set(team, []);
    groups.get(team).push(p);
  }
  const named = [...groups].filter(([t]) => t);
  if (groups.has('')) named.push([named.length ? 'Other' : '', groups.get('')]);
  const card = p => {
    const t = byId.get(p.session) || byKey.get(p.key);
    const id = p.session || (t && t.id) || '';
    const name = (t && t.title) || p.role || p.key;
    const nm = id ? `<a class="nm" href="#/sessions/${encodeURIComponent(id)}">${esc(name)}</a>` : `<span class="nm">${esc(name)}</span>`;
    return `<div class="cfg-ocard"><div class="hd">${nm}</div>
      ${name !== p.role && p.role ? `<span class="role muted">${esc(p.role)}</span>` : ''}</div>`;
  };
  const list = personas ? named.map(([team, ps]) => `${team ? `<div class="goal-group small muted">${esc(team)}</div>` : ''}
      <div class="cfg-ocards">${ps.map(card).join('')}</div>`).join('') || emptyHTML('No roles yet.')
    : failHTML('the team', 'pageRedraw()');
  return `${switches}<div class="cfg-team">${list}</div>`;
}

// Devices: the hub's Mac (how long it has been up, its clock's zone) and the
// phone, when the app is installed (its profile's days left).
function cfgDevicesHTML(s) {
  const up = Number(s.uptime_s) || 0;
  const upWord = up >= 86400 ? `${Math.floor(up / 86400)}d` : up >= 3600 ? `${Math.floor(up / 3600)}h` : `${Math.floor(up / 60)}m`;
  const mac = [`up ${upWord}`, s.time_zone].filter(Boolean).join(' · ');
  const phone = s.app_installed
    ? [s.app_profile_days_left != null ? `profile ${s.app_profile_days_left} ${s.app_profile_days_left === 1 ? 'day' : 'days'} left` : 'app installed',
      s.app_profile_expires ? `until ${dayLabel(String(s.app_profile_expires).slice(0, 10))}` : ''].filter(Boolean).join(' · ')
    : 'app not installed';
  const tile = (name, word) => `<div class="cfg-setting">
      <span class="top"><strong>${esc(name)}</strong></span>
      <span class="small muted">${esc(word)}</span>
    </div>`;
  return `<div class="cfg-settings">${tile('Mac · hub', mac)}${tile('Phone', phone)}</div>`;
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

// The owner's switches: PATCH /settings takes team_mode, plain_sessions and
// model_cap, and answers with the whole GET shape.
async function setSetting(key, on) {
  return act(() => patch('/settings', { [key]: on }));
}

async function setModelCap(model) {
  return act(() => patch('/settings', { model_cap: model }));
}

async function drawConfig(view) {
  const [q, m, goals, src, st, status] = await Promise.all([
    get('/spend/quota').catch(() => null), get('/spend/model').catch(() => null),
    get('/goals').catch(() => null), get('/sources').catch(() => null),
    get('/settings').catch(() => null), get('/status').catch(() => null)]);
  // The roles exist only with team mode on (/personas is a 404 without it).
  const [personas, sessions] = st && st.team_mode ? await Promise.all([
    get('/personas').catch(() => null), get('/threads?persona=1').catch(() => [])]) : [[], []];
  setRedraw(() => drawConfig(view), view, { every: 60000 });
  const groups = (src?.groups || []).filter(g => (g.sources || []).length);
  paint(view, `<div class="pane wide cfg"><h2>Configuration</h2>
    <div class="cfg-top">
      <section class="card"><h3>Powered by</h3>${q ? cfgPlanHTML(q, m, st) : failHTML('the plan', 'pageRedraw()')}</section>
      <section class="card cfg-dev"><h3>Devices</h3>${status ? cfgDevicesHTML(status) : failHTML('devices', 'pageRedraw()')}</section>
    </div>
    <div class="cfg-top">
      <section class="card cfg-dev"><h3>Team</h3>${cfgTeamHTML(st, personas, sessions)}</section>
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
