// life hub — laptop console: shared render helpers.
//
// Loaded first (index.html: ui.js, then views/*.js, then app.js — plain
// <script> tags, no modules, no build step). Everything here is a pure
// function of its arguments except toast/conn, which touch two fixed
// elements. Nothing in this file talks to the hub: that is app.js's api()
// and the views'. The Node tests in test/ load this file alone.
'use strict';

// The owner's id in the hub's data: author, role, by and the calendar kind.
// A private fork sets its own id here; the rest of the console names no one.
const OWNER = 'owner';

// Every page registers itself here as `views.<name> = { draw, redraw }`
// (views/<name>.js); app.js's render() dispatches `#/<name>/…` on it.
const views = {};

// A page's nav tab and its places row (places.js) are registered by its own
// view file, so the core files name no private page and a module whose script
// is absent has no tab and no row (distributable v4). A tab: { key, label,
// order, badge } — app.js builds the bar lowest `order` first, and `badge`
// names the board count (GET /board badges.<badge>) the tab wears.
const TABS = [];
function registerTab(t) { TABS.push(t); }
const PLACES = {};
function registerPlace(key, row) { PLACES[key] = row; }

// Headless Chrome (make web-smoke/web-shot/web-preview) waits for the network
// to go quiet before --dump-dom, so a parked 25 s /changes long-poll kept it
// waiting forever: the live loops stand down there (2026-09-14).
const HEADLESS = typeof navigator !== 'undefined' && /HeadlessChrome/.test(navigator.userAgent);

// ---------- the current route ----------
// `gen` moves on every route change (app.js render()), and each route gets a
// fresh #view element, so a slow draw from the page he just left paints into
// a detached node and can never cover the new one. `redraw` is the ONE
// in-place repaint hook: the page that is showing sets it and the router
// clears it. `live` is what the live-update loop (app.js) runs when the hub
// says something moved — the redraw unless the page names another (a refetch,
// or a narrower repaint); `every` spaces a heavy page's refetches; `typing`
// lets it run while a box on the page has the caret. A draw that lost the
// race cannot claim any of it (its view is detached).
const route = { gen: 0, redraw: null, live: null, every: 0, typing: false, wake: null };
function setRedraw(fn, view, opts) {
  if (view && view.isConnected === false) return;
  const o = opts || {};
  route.redraw = fn;
  route.live = o.live === false ? null : (o.live || fn);
  route.every = o.every || 0;
  route.typing = !!o.typing;
  route.wake = o.wake || null;
}
function pageRedraw() { return route.redraw ? route.redraw() : null; }
// What a page leaves behind that the next must not inherit — a plot's hover
// data, a list's "Show more" — each module sweeps once here, at load; the
// router runs them all on every route change (render, as it swaps #view).
const onLeave = [];
// A sort column, tab or range chip: set one key of the page's state, keep it
// in localStorage under `store` when the choice should outlive the page, and
// repaint in place. Returns the onclick handler.
const sortSetter = (state, key, store) => v => {
  state[key] = v;
  if (store) localStorage.setItem(store, v);
  pageRedraw();
};

// innerHTML, skipped when the markup is exactly what this element last got
// from here and nothing else has replaced it since — so a live refresh that
// changed nothing keeps open <details>, scroll and hover as they were.
function paint(el, html) {
  if (!el) return false;
  if (el._painted === html && el.firstChild === el._paintedFirst) return false;
  el.innerHTML = html;
  el._painted = html;
  el._paintedFirst = el.firstChild;
  return true;
}

// A fetch that failed, in the page's own words, with one way out.
// `retry` is an onclick expression; the default redraws the page.
function failHTML(what, retry) {
  return card(`Couldn't load ${esc(what)} · <button class="sm" onclick="${esc(retry || 'render()')}">Retry</button>`, 'err');
}
// Nothing to show, in the page's words (plain text; escaped here). `how`:
// none = the page's centred grey line, 'card' = the same on a white card,
// 'plot' = a small grey line where a chart would have been.
function emptyHTML(text, how) {
  if (how === 'plot') return `<div class="small muted empty-plot">${esc(text)}</div>`;
  const line = `<div class="empty">${esc(text)}</div>`;
  return how === 'card' ? card(line) : line;
}
// The one switch between views of the same thing (rec-80272f58,
// 2026-10-11): the P&L's Table | Graph control, a sunken track with the
// chosen half raised, is the default every page uses (Net worth | Credit,
// an account's lines and ranges, Engagement's ranges and tabs). `opts` are
// [key, label, {title, n}]; `onclick(key)` is the button's onclick expression.
function segHTML(opts, cur, onclick, cls) {
  return `<div class="seg${cls ? ' ' + cls : ''}" role="tablist">${opts.map(([k, label, o]) => {
    const on = String(k) === String(cur);
    return `<button role="tab" data-k="${esc(k)}" aria-selected="${on}" class="${on ? 'on' : ''}" onclick="${esc(onclick(k))}"${
      o && o.title ? ` title="${esc(o.title)}"` : ''}>${label}${o && o.n != null ? `<span class="n">${num(o.n)}</span>` : ''}</button>`;
  }).join('')}</div>`;
}
// A page's first paint while its read is in flight.
const loadingHTML = () => `<div class="pane wide">${emptyHTML('Loading…')}</div>`;

function conn(ok) { const el = document.getElementById('conn'); if (el) el.classList.toggle('bad', !ok); }
let toastTimer;
function toast(msg) {
  const el = document.getElementById('toast');
  el.textContent = msg; el.classList.add('show');
  clearTimeout(toastTimer); toastTimer = setTimeout(() => el.classList.remove('show'), 2600);
}
// One button's write: run it, toast `done` (or the hub's error), then repaint
// either way (`after`, default the page) so a switch that failed flips back to
// what the hub holds. True on success.
async function act(fn, done, after = () => pageRedraw()) {
  let ok = true;
  try { await fn(); if (done) toast(done); } catch (e) { toast(e.message); ok = false; }
  if (after) await after();
  return ok;
}

// ---------- helpers ----------
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// ---------- refs ----------
// A ref is the typed pointer the hub sets on every row: `kind:<id>`. Where
// one opens on the console is ONE table (docs/reviews/2026-08-28-followups.md
// §2): the calendar's rows, a rec's links and anything else that follows a
// reference look here, and nothing switches on an id's own prefix. An ask or
// a proposal is a card: it opens in its session when the row knows one, else
// on the sessions board, which is where every card that needs him is listed
// (the separate #/asks page went on 2026-09-02).
// A card whose row does not know its session, and a calendar item, open
// through `#/open/<id>` (app.js openRef): the hub is asked where the thing
// lives and the address is replaced with it — so an id written in a card's
// text lands ON the card or the item, not on a board he has to search
// (rec-caa3e181, 2026-09-20).
const REF_PAGE = {
  rec: id => `#/recs/${id}`,
  thread: id => `#/sessions/${id}`,
  cal: id => `#/open/${id}`,
  ask: (id, tid) => tid ? `#/sessions/${tid}/${id}` : `#/open/${id}`,
  action: (id, tid) => tid ? `#/sessions/${tid}/${id}` : `#/open/${id}`,
};
const splitRef = ref => { const r = String(ref || ''), c = r.indexOf(':'); return c > 0 ? [r.slice(0, c), r.slice(c + 1)] : ['', '']; };
function refHref(ref, threadID) {
  const [kind, id] = splitRef(ref);
  const page = REF_PAGE[kind];
  return page ? page(esc(id), esc(threadID || '')) : '';
}
// refOf: the ref a BARE id would carry — mirrors store.KindOf: a known
// prefix, the stamped `YYYYMMDD-HHMMSS-hex` of a proposal, else a thread.
const REF_KINDS = { ask: 'ask', cal: 'cal', rec: 'rec', p: 'prompt' };
function refOf(id) {
  const s = String(id || ''), m = /^([a-z]+)-[0-9a-f]{4,8}$/.exec(s);
  const kind = m && REF_KINDS[m[1]] ? REF_KINDS[m[1]] : /^\d{8}-\d{6}-[0-9a-f]{4,8}$/.test(s) ? 'action' : 'thread';
  return kind + ':' + s;
}
// A hub id standing in prose: rec-/ask-/cal- plus 8 hex (4 before 2026-08-26),
// or a proposal's stamp. Never a piece of a longer slug — a session id reads
// "…-rec-caa3e181-accepted-56a2fed3" — nor of a path or a URL fragment. The
// phone's Refs.swift carries the same rule; shared/ref-cases.json pins both.
const REF_ID = '(?:rec|ask|cal)-(?:[0-9a-f]{8}|[0-9a-f]{4})|\\d{8}-\\d{6}-[0-9a-f]{4,8}';
const REF_WHOLE = new RegExp(`^(?:${REF_ID})$`);
const REF_IN_TEXT = new RegExp(`(<a [^>]*>.*?</a>)|\\u0000(\\d+)\\u0000|(?<![\\w/#-])(${REF_ID})(?![\\w-])`, 'g');
/// The provider's tile, as the hub serves it (internal/brand): letters on the
/// provider's colour, so the card is recognised before its name is read.
/// Money's accounts, Configuration's plan and sources.
/// A source with a real logo (brand/logos.go, 2026-10-06 "get a nice
/// logo") draws it in the ink instead of the letters.
function brandMark(b) {
  b = b || {};
  const inner = b.logo ? `<svg viewBox="0 0 24 24" aria-label="${esc(b.mark || '')}"><path fill="currentColor" d="${esc(b.logo)}"/></svg>` : esc(b.mark || '·');
  return `<span class="bmark${b.logo ? ' logo' : ''}" style="background:${esc(b.color || 'var(--muted)')};color:${esc(b.ink || '#fff')}">${inner}</span>`;
}

// Money and counts: one wording with the hub (internal/format) and the phone
// (Format.swift), pinned by shared/format-cases.json (2026-09-14). Minus is
// U+2212. usd: whole from $100, cents below, no ".00"; usd0: always whole;
// usdPrice: a per-share price, always cents (4 places under a cent).
const moneySign = (v, digits) => (v < 0 && /[1-9]/.test(digits) ? '−$' : '$') + digits;
const usd = n => {
  const v = Number(n) || 0, a = Math.abs(v), cents = Math.round(a * 100);
  return moneySign(v, cents >= 10000 || cents % 100 === 0 ? Math.round(a).toLocaleString('en-US') : (cents / 100).toFixed(2));
};
const usd0 = n => { const v = Number(n) || 0; return moneySign(v, Math.round(Math.abs(v)).toLocaleString('en-US')); };
const usdSigned = n => { const s = usd(n); return s[0] === '−' ? s : '+' + s; };
const usd0Signed = n => { const s = usd0(n); return s[0] === '−' ? s : '+' + s; };
const usdShort = n => {
  const v = Number(n) || 0, a = Math.abs(v);
  const tenths = x => (Math.round(x * 10) / 10).toFixed(1).replace(/\.0$/, '');
  const s = Math.round(a / 1e5) >= 10 ? tenths(a / 1e6) + 'M'
    : Math.round(a / 1e3) >= 10 ? Math.round(a / 1e3).toLocaleString('en-US') + 'k'
    : Math.round(a) >= 1000 ? tenths(a / 1e3) + 'k'
    : String(Math.round(a));
  return moneySign(v, s);
};
const usdPrice = n => {
  const v = Number(n) || 0, a = Math.abs(v);
  const [whole, frac] = a.toFixed(a > 0 && a < 0.01 ? 4 : 2).split('.');
  return moneySign(v, Number(whole).toLocaleString('en-US') + '.' + frac);
};
// usdCents: a transaction's amount, a fact and not a balance — always cents
// ("$234.43", where usd says "$234"; 2026-10-14 23:36).
const usdCents = n => {
  const v = Number(n) || 0;
  const [whole, frac] = Math.abs(v).toFixed(2).split('.');
  return moneySign(v, Number(whole).toLocaleString('en-US') + '.' + frac);
};
// A plain count with its thousands separator: 20125 messages reads as a
// serial number, 20,125 reads as a quantity.
const num = n => { const v = Math.round(Number(n) || 0); return (v < 0 ? '−' : '') + Math.abs(v).toLocaleString('en-US'); };
// A share count, to four places: a fractional buy and a 62,500-share grant.
const qty = n => (Number(n) || 0).toLocaleString('en-US', { maximumFractionDigits: 4 });
// A signed percentage from a fraction: 0.123 → "+12.3%".
const pct = (v, digits) => (v >= 0 ? '+' : '−') + Math.abs(v * 100).toFixed(digits === undefined ? 1 : digits) + '%';
// A count that moved: "+12", "−3", "0".
const signed = n => (Math.round(Number(n) || 0) > 0 ? '+' : '') + num(n);
// A value already in percent points, always signed: 4.25 → "+4.3%".
const pctPoints = (v, digits) => (v < 0 ? '−' : '+') + Math.abs(v).toFixed(digits === undefined ? 1 : digits) + '%';
// A change as a share of where it started: (12, 40) → "+30%". One place
// under 10%, none above, unless `digits` says; '' with no base to divide by.
const pctDelta = (d, base, digits) => {
  if (!base) return '';
  const p = d / Math.abs(base) * 100;
  return pctPoints(p, digits === undefined ? (Math.abs(p) < 10 ? 1 : 0) : digits);
};
// A part's share of a whole, rounded: "48%"; '' with no whole to share.
const ratioPct = (n, d) => (d > 0 ? Math.round(n / d * 100) + '%' : '');
// An average holding period: "1.4y", or days under a year ("76d").
const holdLabel = years => (years >= 1 ? years.toFixed(1) + 'y' : Math.round(years * 365) + 'd');
// What a chat has cost in tokens. Millions on any long session, so two
// significant figures and a unit beat a nine-digit number nobody reads.
const tokens = n => {
  n = Number(n) || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + 'M tok';
  if (n >= 1e3) return Math.round(n / 1e3) + 'k tok';
  return n + ' tok';
};
// Hover text for a chat's dollar figure: which model earned what. A session
// that ran all night usually changed model when a plan bucket filled, and the
// rates differ by 5x (2026-08-25). Only GET one thread returns the split.
const costByModel = t => {
  const by = t.cost_by_model || [];
  if (!by.length) return 'total cost so far';
  return by.map(c => `${c.model || 'default model'} ${usd(c.cost_usd)}`).join(' · ');
};

// The phone's shortAgo (Chips.swift) rounds the same way and must keep doing
// so — it floored until 2026-09-01, so 45 hours read "2d ago" here and "1d"
// there. Each bucket stops 30s/30m/12h short of its ceiling so a rounded value
// is promoted rather than printed as "60m ago".
function ago(iso) {
  if (!iso) return '';
  const s = (Date.now() - new Date(iso)) / 1000;
  if (s < 45) return 'just now';
  if (s < 3600 - 30) return Math.round(s / 60) + 'm ago';
  if (s < 86400 - 1800) return Math.round(s / 3600) + 'h ago';
  if (s < 86400 * 30) return Math.round(s / 86400) + 'd ago';
  return new Date(iso).toLocaleDateString(EN);
}
function when(iso) {
  if (!iso) return '';
  const d = new Date(iso), now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  return sameDay ? clock(d) : dayLabel(d) + ' ' + clock(d);
}
// A chat row's clock, the day said in words (2026-10-07: "this is
// the time, but not the day… make it more descriptive, like with the date or
// like last Tuesday"): "today 3:15 PM", "yesterday 12:53 AM", "Mon 9:15 AM"
// inside the week, "Oct 1 4:02 PM" past it, the year once it differs. `when`
// drops the day for today, which is exactly the row that read as "12:52 AM"
// twenty hours later. The phone's `dayClock` (Format.swift) says the same.
function dayWhen(iso, now) {
  if (!iso) return '';
  const d = new Date(iso); now = now || new Date();
  const hm = clock(d);
  const midnight = x => new Date(x.getFullYear(), x.getMonth(), x.getDate());
  const days = Math.round((midnight(now) - midnight(d)) / 86400000);
  if (days === 0) return 'today ' + hm;
  if (days === 1) return 'yesterday ' + hm;
  if (days > 1 && days < 7) return d.toLocaleDateString(EN, { weekday: 'short' }) + ' ' + hm;
  return dayWords(d, now) + ' ' + hm;
}

// The turn in flight in a finished turn's words: "23 tool calls · today
// 10:09 PM · $1.12" — its tool calls, when the newest of them was made (the
// clock moves with the count, 2026-10-07 23:52), its dollars so far (the
// hub's turn_tools / turn_at / turn_cost_usd). The chat's working
// line and the session card both print it, where a raw step label used to sit
// (2026-10-07 22:12: "I really preferred it when it says tool calls, the
// time, the date, and then the number of dollars… I don't understand what
// run verify the proof again means"). `tools` false leaves the count to a
// run block already showing it; `cost` false leaves the dollars to a session
// card whose state line already prints them. The phone's `turnFacts`
// (Format.swift).
function turnFacts(t, tools, cost) {
  if (!t) return '';
  const n = t.turn_tools || 0;
  return [tools !== false && n ? n + ' tool call' + (n === 1 ? '' : 's') : '',
    t.turn_at ? dayWhen(t.turn_at) : '', cost !== false && t.turn_cost_usd ? usd(t.turn_cost_usd) : ''].filter(Boolean).join(' · ');
}

// THE TOOL CHAIN of the turn in flight (2026-10-27 21:57): the count on the working line opens it — what the turn is doing
// and what it is waiting on, and the Agent-tool helpers it has running ("3
// subagents running" and each one's description), since helpers run inside
// the turn and never as sessions of their own. `evs` are the turn's events
// (GET /threads/{id}/events, one run_id), oldest first; a tool_result answers
// the oldest tool_use still open, so the calls left without one are the ones
// it is waiting on. `tasks` is the thread's turn_tasks (the CLI's own list of
// background tasks still pending): when the hub sends it, an async helper
// runs until it leaves that list; without it, a launched helper counts as
// running for the rest of the turn. The phone's `toolChain` (Format.swift).
const AGENT_TITLE = /^(Agent|Task)(?: · |$)/;
function toolChain(evs, tasks) {
  // A result with no call before it (the window opened mid-turn) answers
  // nothing on screen.
  const steps = [], open = [];
  for (const e of evs || []) {
    if (e.kind === 'tool_use') {
      const agent = AGENT_TITLE.test(e.title || '');
      const s = { id: e.id, agent, open: true, error: false, launched: false,
        label: agent ? (e.title || '').replace(AGENT_TITLE, '') || 'helper' : (e.summary || e.title || e.kind) };
      steps.push(s); open.push(s);
    } else if (e.kind === 'tool_result' && open.length) {
      const s = open.shift();
      s.open = false; s.error = e.title === 'error';
      s.launched = s.agent && /^Async agent launched/.test(e.body || '');
    }
  }
  const helpers = [];
  const add = d => { if (d && !helpers.includes(d)) helpers.push(d); };
  if (Array.isArray(tasks)) for (const k of tasks) { if (k.type === 'local_agent') add(k.description || 'helper'); }
  for (const s of steps) if (s.agent && (s.open || (!Array.isArray(tasks) && s.launched))) add(s.label);
  return { steps, helpers, waiting: steps.filter(s => s.open && !s.agent).map(s => s.label) };
}
// The helpers line's words: "1 subagent running", "3 subagents running".
const helpersWords = n => n ? `${n} subagent${n === 1 ? '' : 's'} running` : '';
// The helpers the hub lists for a running thread on its own (turn_tasks),
// before any chain is loaded — the working line counts them unopened.
const taskHelpers = t => ((t && t.turn_tasks) || []).filter(k => k.type === 'local_agent').length;

// THE HELPERS A SESSION HAS GOING, drawn where he looks (2026-11-03: a
// role had two going and he found neither): every background task of
// the turn, an Agent helper or a background shell, as one row — its kind, what
// it is, how long it has run, the newest line it printed and, when that line
// counts ("3/10", "45%"), a bar. The chat's rows open its log; the Sessions
// card's are the same rows, closed. The phone's `TaskRow` (Format.swift).
// The rows a session shows: its background tasks, then the approved actions
// the hub is running for it, turn or not (2026-11-05 00:15, of
// a role's mail cleanup: "show me the work she's doing"). The hub lists the
// tasks until they end, past the reply that left them running (01:49).
const liveTasks = t => [...((t && t.turn_tasks) || []), ...((t && t.running_actions) || [])];
const taskKind = k => k.type === 'local_agent' ? 'subagent' : k.type === 'local_bash' ? 'shell' : String(k.type || 'task').replace(/^local_/, '');
// "4m", "1h 05m": how long it has run, from when the hub first listed it.
function taskAge(k, now) {
  if (!k.started_at) return '';
  const m = Math.max(0, Math.floor(((now || Date.now()) - Date.parse(k.started_at)) / 60000));
  return m < 60 ? m + 'm' : Math.floor(m / 60) + 'h ' + String(m % 60).padStart(2, '0') + 'm';
}
// "3/10" or "45%", '' with no count.
const taskCount = k => !k.total ? '' : k.total === 100 ? k.done + '%' : k.done + '/' + k.total;
function taskBarHTML(k) {
  if (!k.total) return '';
  return `<div class="tbar"><span class="tbar-t"><i style="width:${Math.round(100 * Math.min(k.done, k.total) / k.total)}%"></i></span><span class="tbar-n">${esc(taskCount(k))}</span></div>`;
}
// One helper's row. `o.open`: its log is showing; `o.click`: the head's onclick.
function taskRowHTML(k, o = {}) {
  const head = `<span class="verb">${esc(taskKind(k))}</span><span class="task-d">${esc(k.description || 'helper')}</span>${k.started_at ? `<span class="muted task-age" title="started ${esc(dayWhen(k.started_at))}">${esc(taskAge(k))}</span>` : ''}`;
  return `<div class="task-row${o.open ? ' on' : ''}">
    ${o.click ? `<a href="#" class="task-h" onclick="event.preventDefault();${o.click}"><span class="caret">›</span>${head}</a>` : `<div class="task-h">${head}</div>`}
    ${taskBarHTML(k)}
    ${k.doing && !o.open ? `<div class="small muted task-doing">${esc(k.doing)}</div>` : ''}
    ${o.log || ''}</div>`;
}

// The Markdown subset the agents are told to write (docs: preamble spec):
// bullets, numbered lists, # headings, **bold**, `code`, links, pipe tables,
// ``` fences — anything else renders as plain text rather than as syntax.
// Inline half of the subset, on its own: **bold**, *italic*, `code`, links.
// The one-liners on this page — card titles, list previews, calendar rows —
// printed the asterisks because md() was only ever called on message and note
// BODIES (2026-08-24: "the markdown rendering isnt working on web app").
// They cannot call md() either: it emits <p>/<ul> blocks, which do not belong
// inside a <strong> title and break the one/two-line clamps.
// links=false renders link text and bare URLs as plain text. Mandatory for
// anything drawn INSIDE a clickable card: a card is an <a>, HTML cannot nest
// anchors, and the browser repairs it by closing the card early — which split
// every failed session in the sessions list into three loose boxes, because
// the CLI's error paragraph ends in status.claude.com (2026-08-24).
// GFM's trailing-punctuation rule for bare URLs: ", . ; : ! ?" and quotes
// hanging off the end belong to the sentence, not the link — an ask detail
// ending "…pull/82, merge when ready" linked the comma, which 404s on GitHub
// (2026-09-06). A trailing ")" stays only while a matching "(" sits
// inside the URL (Wikipedia's "/wiki/A_(b)"). Runs on escaped text, so a
// quote after the URL arrives as &quot;/&#39; — a trailing entity is trimmed
// as one unit, as GFM does with entity references. Returns [link, spillover].
function trimBareURL(u) {
  let rest = '';
  for (;;) {
    const ent = u.match(/&#?[a-zA-Z0-9]+;$/);
    if (ent) { u = u.slice(0, -ent[0].length); rest = ent[0] + rest; continue; }
    const c = u.slice(-1);
    if (',.;:!?\'"'.includes(c) ||
        (c === ')' && (u.match(/\(/g) || []).length < (u.match(/\)/g) || []).length)) {
      u = u.slice(0, -1); rest = c + rest; continue;
    }
    return [u, rest];
  }
}

function mdInline(src, links = true) {
  // A picture inside a sentence is its caption as a link: only a line of its
  // own draws the picture (md, BLOB_LINE).
  let s = esc(String(src ?? '').replace(/!\[([^\]]*)\]\(/g, (_, a) => `[${a || 'image'}](`));
  s = links
    ? s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
       .replace(/(^|[\s(])(https?:\/\/[^\s<\]]+)/g, (_, pre, u) => {
         const [link, rest] = trimBareURL(u);
         return `${pre}<a href="${link}" target="_blank" rel="noopener">${link}</a>${rest}`;
       })
    : s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '$1');
  // An asterisk is only syntax when it opens at a word boundary (start,
  // whitespace, an opening bracket) and hugs its content on both sides.
  // Without that, "you have Bash(npm install:*) and Bash(npm run:*)" matched
  // from the first ":*" to the second and rendered as "Bash(npm install:)"
  // plus an italic run, both literal asterisks deleted — the phone had the
  // same bug via CommonMark's left-flanking rule. Also protects "ops/*.py and
  // src/*.go" and "2**32 and 2**64". Dropping characters out of a message is
  // worse than dropping the italics.
  // Code spans are lifted out first and put back last: they were substituted
  // in place, so the emphasis passes then ran over their contents and
  // "`--host *`" came out italic. Inside backticks the characters are the
  // point.
  const code = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => '\u0000' + (code.push(c) - 1) + '\u0000');
  // A bare hub id is a link to the thing (2026-09-20, on "Decide
  // rec-120a5f85 / Close ask-955bfb8e": "What are the Rex and Ass? You need to
  // give me links to these things"). Not inside a link that is already there,
  // and not inside code he copies — except a code span that is ONLY an id,
  // which is how a session usually writes one.
  if (links) s = s.replace(REF_IN_TEXT, (m, a, ci, id) => {
    if (a) return a;
    if (ci !== undefined) return REF_WHOLE.test(code[ci]) ? `<a href="${refHref(refOf(code[ci]))}">${m}</a>` : m;
    return `<a href="${refHref(refOf(id))}">${id}</a>`;
  });
  return s
    .replace(/(^|[\s([{])\*\*([^\s*][^*]*[^\s*]|[^\s*])\*\*/g, '$1<strong>$2</strong>')
    .replace(/(^|[\s([{])\*([^\s*][^*\n]*[^\s*]|[^\s*])\*/g, '$1<em>$2</em>')
    .replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${code[i]}</code>`);
}

// A clamped preview of a whole reply: same inline rules, and the block
// markers become "• " so two lines of a bulleted reply read as prose instead
// of "- **Did:** …".
// A preview lives inside a clickable card and is clamped to two lines, so it
// is always link-free: the whole card is the link.
function mdPreview(src) {
  return String(src ?? '').split('\n').map(l => l.trim())
    .filter(l => l && !mdTableSep(l) && !/^`{3,}[\w.+#-]*$/.test(l))
    .map(l => l.startsWith('|') && mdTableRow(l) ? mdTableRow(l).join(' · ') : l)
    .map(l => mdInline(l.replace(/^[-*•]\s+/, '• ').replace(/^(#{1,4})\s+/, ''), false))
    .join(' ');
}

// A dead turn's last "message" is the CLI's own error paragraph — three lines
// of boilerplate and a status-page URL. On a card, one line is the whole
// signal: the session failed, and why. The full text stays in the chat.
function failedPreview(text) {
  const t = String(text ?? '').trim().replace(/\s+/g, ' ');
  const m = t.match(/^(.{0,120}?[.!?])(\s|$)/);
  const first = ((m ? m[1] : t.slice(0, 120)) || '').replace(/\.$/, '');
  return '⚠ Session failed · ' + esc(first || 'no result');
}

// A pipe table row: "| a | b |" (outer pipes optional) → its cells, or null.
// Split on "|" outside code spans, so "`a | b`" stays one cell.
function mdTableRow(l) {
  const t = l.trim();
  if (!t.includes('|')) return null;
  const cells = []; let cur = '', fence = false;
  for (const c of t) {
    if (c === '`') fence = !fence;
    if (c === '|' && !fence) { cells.push(cur); cur = ''; } else cur += c;
  }
  cells.push(cur);
  if (t.startsWith('|')) cells.shift();
  if (t.endsWith('|') && cells.length) cells.pop();
  return cells.map(c => c.trim());
}
// "|---|:--:|---:|" → per-column alignment, or null when the row is not one.
function mdTableSep(l) {
  const cells = mdTableRow(l);
  if (!cells || !cells.length || !cells.every(c => /^:?-+:?$/.test(c))) return null;
  return cells.map(c => c.endsWith(':') ? (c.startsWith(':') ? 'center' : 'right') : 'left');
}

// A fenced block: the language (when given) and Copy over the text, which
// scrolls sideways rather than wrapping — a wrapped JSON line reads as two.
function mdCode(text, lang) {
  return `<div class="md-code"><div class="md-code-bar"><span>${esc(lang || '')}</span>` +
    `<button type="button" class="md-copy" onclick="mdCopy(this)">Copy</button></div>` +
    `<pre><code>${esc(text)}</code></pre></div>`;
}
// Copies exactly what the session wrote: textContent of the <code>, so the
// indentation and newlines are the file's.
function mdCopy(btn) {
  const text = btn.closest('.md-code').querySelector('code').textContent;
  const done = () => { btn.textContent = 'Copied'; setTimeout(() => { btn.textContent = 'Copy'; }, 1500); };
  // The async clipboard refuses an unfocused document; the selection copy
  // does not care.
  const old = () => {
    const ta = document.createElement('textarea');
    ta.value = text; document.body.appendChild(ta); ta.select();
    document.execCommand('copy'); ta.remove(); done();
  };
  if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(done, old);
  else old();
}

// Blobs are stored under their original extension, so an attachment that is a
// picture can be shown as one instead of as the word "attachment"
// (2026-08-23) — the thumbnail links to the full file. HEIC only decodes in
// Safari, hence the onerror fallback to a plain link. An SVG (a session's
// drawn grid) is a picture too (2026-10-20: "Svgs don't open").
const isImageRef = r => /\.(png|jpe?g|gif|webp|heic|heif|avif|svg)$/i.test(String(r || ''));
const isAudioRef = r => /\.(m4a|mp3|wav|aac|caf)$/i.test(String(r || ''));
const isHTMLRef = r => /\.html?$/i.test(String(r || ''));
// The file an .html blob downloads as: the link's text with the extension
// ("life, day by day.html"), else the blob's own name (its hash).
const htmlFileName = (ref, caption) =>
  (String(caption || '').trim() ? String(caption).trim().replace(/[\/\\:]/g, '-') + '.html' : String(ref).split('/').pop());
function attachHTML(ref, caption) {
  const url = '/api/v1/blobs/' + esc(ref);
  const name = esc(String(ref).split('/').pop());
  // A voice sample plays in place (2026-10-17: "give me players for the voices").
  if (isAudioRef(ref)) return `<audio controls preload="none" src="${url}"></audio>`;
  // An .html page is a FILE, never a viewer inside the app
  // (2026-10-26 02:08, on a role's HTML report): a thumbnail of its top
  // (a scaled, script-less, untouchable frame), then Download — saved under
  // the link's text — and Open in browser, a tab of its own. The Swift twin
  // is BlobHTML (Attachments.swift).
  if (isHTMLRef(ref)) {
    const file = esc(htmlFileName(ref, caption));
    return `<div class="att-html"><div class="att-thumb"><iframe src="${url}" sandbox="" scrolling="no" tabindex="-1" loading="lazy" title="${file}"></iframe></div>`
      + `<div class="att-html-row"><a href="${url}" download="${file}" class="small att-file">Download</a>`
      + `<a href="${url}" target="_blank" class="small att-file">Open in browser</a></div></div>`;
  }
  if (!isImageRef(ref)) return `<a href="${url}" target="_blank" class="small att-file">📎 ${name}</a>`;
  // Click opens it in the lightbox instead of a new tab — the href stays so
  // ⌘-click and "open in new tab" still work.
  // The fallback is built from the element, never from the ref spliced into
  // the handler: the browser decodes &#39; back to a quote before it runs one
  // (GHSA-98ww-qvvc-42wq, 11-02).
  return `<a href="${url}" target="_blank" class="att" onclick="return !openImage(this.href)"><img src="${url}" alt="${name}"
    onerror="attFallback(this)"></a>`;
}
function attFallback(img) {
  const a = document.createElement('a');
  a.href = img.parentElement.href;
  a.target = '_blank';
  a.className = 'small att-file';
  a.textContent = '📎 ' + img.alt;
  img.parentElement.replaceWith(a);
}

// A line that is only a hub file — `![caption](…/api/v1/blobs/<ref>)`,
// `[name](…)` or the bare URL — is that file in the text: the picture
// itself, or a 📎 chip for a PDF. Sessions pasted screenshots as links he
// could not see (2026-10-10: "the agent isn't showing these
// screenshots… where are those screenshots?"). Only the ref is kept, so the
// file loads from whatever host serves the page. The StyledText twin is
// `blobLine` (StyledText.swift).
const BLOB_LINE = /^\s*(?:!?\[([^\]]*)\]\()?(?:https?:\/\/[^\s/)]+)?\/api\/v1\/blobs\/([\w.\/-]+)\)?\s*$/;
function blobLine(l) {
  const m = String(l || '').match(BLOB_LINE);
  return m && !m[2].includes('..') ? { ref: m[2], caption: (m[1] || '').trim() } : null;
}
const blobLineRef = l => blobLine(l)?.ref ?? null;
// Files on lines in a row (blank lines between are fine) draw as ONE strip of
// small tiles, each captioned with its [name], a click opening it full size
// (2026-10-10 15:15: "they're just so damn big… put screens side
// by side… small attachment cards, like one, two, three, four").
function filesHTML(files) {
  return `<div class="md-files">${files.map(f =>
    `<figure>${attachHTML(f.ref, f.caption)}${f.caption ? `<figcaption>${esc(f.caption)}</figcaption>` : ''}</figure>`).join('')}</div>`;
}

function md(src) {
  const lines = String(src ?? '').split('\n');
  let out = '', list = null;
  const inline = mdInline;
  const close = () => { if (list) { out += `</${list}>`; list = null; } };
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const l = raw.trimEnd();
    let m;
    // A fenced block (2026-09-18: a settings.json to paste came out as a
    // column of paragraphs, indentation gone). ```lang … ``` is the one way a
    // session hands him text to copy VERBATIM — JSON, a command, a config —
    // so nothing inside is markdown, whitespace is kept, and the block has a
    // Copy button. A fence indented under a list item loses that indent; an
    // unclosed fence runs to the end (a reply still streaming in).
    if ((m = l.match(/^(\s*)(`{3,})\s*([\w.+#-]*)$/))) {
      close();
      const body = [];
      let j = i + 1;
      for (; j < lines.length; j++) {
        const t = lines[j].trim();
        if (t.startsWith(m[2]) && /^`+$/.test(t)) break;
        body.push(lines[j].startsWith(m[1]) ? lines[j].slice(m[1].length) : lines[j].trimStart());
      }
      out += mdCode(body.join('\n'), m[3]);
      i = j;
      continue;
    }
    // A pipe table (2026-09-17: a buy plan came out as "| Date | Buy |"
    // paragraphs). Header row + separator + rows; a run of pipe rows with no
    // separator is a headerless table rather than a column of pipes.
    const row0 = l.trim().startsWith('|') ? mdTableRow(l) : null;
    if (row0 && !mdTableSep(l)) {
      close();
      let j = i + 1;
      const align = j < lines.length ? mdTableSep(lines[j]) : null;
      if (align) j++;
      const rows = [];
      for (; j < lines.length && lines[j].trim().startsWith('|'); j++) {
        if (!mdTableSep(lines[j])) rows.push(mdTableRow(lines[j]));
      }
      const td = (cells, tag) => '<tr>' + cells.map((c, k) => {
        const a = align && align[k] && align[k] !== 'left' ? ` style="text-align:${align[k]}"` : '';
        const html = inline(c);
        // A cell that is one link is all link: a click anywhere in it opens
        // it, like a hyperlink in an email (2026-10-29).
        const hrefs = new Set([...html.matchAll(/<a [^>]*href="([^"]*)"/g)].map(m => m[1]));
        return `<${tag}${a}${hrefs.size === 1 ? ' class="md-link"' : ''}>${html}</${tag}>`;
      }).join('') + '</tr>';
      out += '<table class="md-table">';
      if (align) out += `<thead>${td(row0, 'th')}</thead>`; else rows.unshift(row0);
      if (rows.length) out += `<tbody>${rows.map(r => td(r, 'td')).join('')}</tbody>`;
      out += '</table>';
      i = j - 1;
      continue;
    }
    const blob = blobLine(l);
    if (blob) {
      close();
      const files = [blob];
      let j = i + 1;
      for (; j < lines.length; j++) {
        if (!lines[j].trim()) continue;
        const f = blobLine(lines[j]);
        if (!f) break;
        files.push(f);
      }
      out += filesHTML(files);
      i = j - 1;
      continue;
    }
    if ((m = l.match(/^\s*[-*•]\s+(.*)$/))) {
      if (list !== 'ul') { close(); out += '<ul>'; list = 'ul'; }
      out += `<li>${inline(m[1])}</li>`;
    } else if ((m = l.match(/^\s*(\d+)[.)]\s+(.*)$/))) {
      // A list a block interrupted picks up at the number it was written
      // with: steps "1." ```…``` "2." "3." read 1, 1, 2 without `start`.
      if (list !== 'ol') { close(); out += m[1] === '1' ? '<ol>' : `<ol start="${+m[1]}">`; list = 'ol'; }
      out += `<li>${inline(m[2])}</li>`;
    } else if ((m = l.match(/^(#{1,4})\s+(.*)$/))) {
      close(); out += `<h3>${inline(m[2])}</h3>`;
    } else if (l.trim() === '') {
      close();
    } else {
      close(); out += `<p>${inline(l)}</p>`;
    }
  }
  close();
  return out;
}

// The fold line (2026-09-22): past this a card's detail MAY fold — the
// cheap gate, counted on the text so both surfaces and a test agree; the
// phone's isLongText (StyledText.swift) is the same rule. Whether it does
// fold is fitFolds' call, on the rendered height.
const LONG_LINES = 18, LONG_CHARS = 1600;
const longLineCount = t => String(t || '').split('\n').filter(l => l.trim() !== '').length;
const isLongText = t => longLineCount(t) > LONG_LINES || String(t || '').length > LONG_CHARS;

function statusPill(s) {
  const cls = { needs_you: 'needs', running: 'running', done: 'done' }[s] || '';
  const label = s === 'needs_you' ? 'your turn' : s;
  const dot = s === 'running' ? '<span class="dot pulse"></span>' : '';
  return `<span class="pill ${cls}">${dot}${esc(label)}</span>`;
}
// The pulsing "running" pill with words of the caller's choosing — a card's
// "session running", a Respond dialog's "running now — it reads this next".
const runningPill = label => `<span class="pill running"><span class="dot pulse"></span>${esc(label)}</span>`;
// The green pulsing "speaking" pill: this session's card is in his ears
// right now (board pills tone `speaking`, 2026-09-25).
const speakingPill = label => `<span class="pill speaking"><span class="dot pulse"></span>${esc(label)}</span>`;
// The pills that MOVE on their own: a turn in flight (blue, pulsing), a card
// being SAID into his AirPods right now (green, pulsing; 2026-09-25:
// "if you have a thread that is speaking actively, I want there to be a
// 'speaking' on it"), and a line queued for his ears behind his microphone,
// a replay or a pitch round (amber, still: "waiting to speak"). One table
// for the Sessions board's rows, the open chat's head and — since 2026-09-26
// — a calendar row's session (`live` on GET /calendar).
const LIVE_PILL = { running: runningPill, speaking: speakingPill,
  waiting: w => `<span class="pill waiting"><span class="dot"></span>${esc(w)}</span>` };
const livePillsHTML = pills => (pills || []).map(p => LIVE_PILL[p.tone] ? LIVE_PILL[p.tone](p.word) : '').join('');

// A day + clock time is read by the hub in its own zone (hub.json time_zone,
// the owner's), so the instant he picked is formatted there rather than
// in the browser's zone. app.js sets it from GET /status (life-distributable#55).
let hubZone = 'America/New_York';
function easternFields(date) {
  const p = {};
  for (const part of new Intl.DateTimeFormat('en-US', {
    timeZone: hubZone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hour12: false,
  }).formatToParts(date)) p[part.type] = part.value;
  return { on: `${p.year}-${p.month}-${p.day}`, at_time: `${p.hour === '24' ? '00' : p.hour}:${p.minute}` };
}

// A bare YYYY-MM-DD parses as UTC midnight, i.e. the evening before in the
// US; noon keeps the day. Money and Recs both date things this way.
// Every day, month and clock the console prints, pinned to en-US as money is
// (rec-1cc1e939, 2026-10-11): shared/format-cases.json holds the cases, and
// this file, Format.swift (FormatTests) and internal/format read the one
// table, so the same day reads the same on Money, Sort, the P&L, a goal and a
// session. A view never calls toLocaleDateString itself. `day` takes a hub
// day string (local noon, so a DST change cannot shift it) or a Date.
const EN = 'en-US';
const day = s => s instanceof Date ? s : new Date(String(s).length <= 10 ? s + 'T12:00:00' : s);
const dayFmt = (s, o) => { const d = day(s); return isNaN(d) ? String(s) : d.toLocaleDateString(EN, o); };
// Local calendar days as YYYY-MM-DD strings — never UTC: a bare day is the
// day on his wall calendar, and UTC after 8 pm Eastern reads as tomorrow.
const localDate = s => { const [y, m, d] = String(s).split('-').map(Number); return new Date(y, m - 1, d); };
const ymd = d => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
const addDays = (s, n) => { const d = localDate(s); d.setDate(d.getDate() + n); return ymd(d); };
const todayYMD = () => ymd(new Date());
// Whole days from a to b (both YYYY-MM-DD); noon on each side, so a DST
// change in between cannot round a day away.
const daysBetween = (a, b) => Math.round((day(b) - day(a)) / 86400000);
// "Sep 8, 2025": the day with its year (a table cell, a certification range).
const shortDay = s => dayFmt(s, { month: 'short', day: 'numeric', year: 'numeric' });
// The same day without the year, for a label inside a chart or a chip.
const dayLabel = d => dayFmt(d, { month: 'short', day: 'numeric' });
// With the weekday, for the title line of a plot's tooltip: on a daily series
// "Sat" is half of why a day looks the way it does (nothing settles on a
// weekend), and it is the one thing the date alone will not tell him.
const fullDay = s => dayFmt(s, { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' });
// "Sunday, October 11, 2026": the calendar's day heading and an event's tip.
const longDay = s => dayFmt(s, { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
// "Sun, Oct 11": the calendar's drag ghost.
const weekDay = s => dayFmt(s, { weekday: 'short', month: 'short', day: 'numeric' });
// "Oct 3" this year, "Oct 3, 2025" another (a goal note, an expense row); the
// year is noise until it changes. `now` is for tests.
const dayWords = (s, now) => dayFmt(s, { month: 'short', day: 'numeric', ...(day(s).getFullYear() === (now || new Date()).getFullYear() ? {} : { year: 'numeric' }) });
// Months from a hub "YYYY-MM" (a day string works too: its month). Mid-month
// noon, so no timezone can move it into the month before.
const monthDate = m => m instanceof Date ? m : new Date(String(m).slice(0, 7) + '-15T12:00:00');
const monthFmt = (m, o) => { const d = monthDate(m); return isNaN(d) ? String(m) : d.toLocaleDateString(EN, o); };
const monthName = m => monthFmt(m, { month: 'short', year: 'numeric' });   // "Aug 2026"
const monthLong = m => monthFmt(m, { month: 'long', year: 'numeric' });    // "August 2026"
const monthWord = m => monthFmt(m, { month: 'long' });                      // "August"
const monthShort = m => monthFmt(m, { month: 'short' });                    // "Aug"
const monthYY = m => monthFmt(m, { month: 'short', year: '2-digit' });      // "Sep 25"
// "Oct" this year, "Oct 2025" another: a month picker's label.
// "3:15 PM" from a Date, an ISO stamp or a hub "HH:MM".
const clock = x => {
  const d = x instanceof Date ? x : /^\d{1,2}:\d{2}$/.test(String(x)) ? new Date(2000, 0, 1, ...String(x).split(':').map(Number)) : new Date(x);
  return isNaN(d) ? String(x) : d.toLocaleTimeString(EN, { hour: 'numeric', minute: '2-digit' });
};

// The model that filed something, short: "claude-fable-5[1m]" → "fable 5",
// "claude-haiku-4-5-20251001" → "haiku 4.5". The full id stays on the page.
// Recs and Spend both print it, so it lives here.
function modelShort(id) {
  if (!id) return '';
  if (id === OWNER || id === 'unknown') return id;
  const m = String(id).replace(/^claude-/, '').replace(/\[1m\]$/, '').replace(/-\d{8}$/, '').split('-');
  return m.length > 1 ? m[0] + ' ' + m.slice(1).join('.') : m[0];
}

// A cost is money out, so it reads like one: "−$389" in red, beside the
// "+$118" green benefit lines (2026-09-23: "make the spend here either
// negative or red or something that looks more like spend"). A free tier's
// $0 stays plain — nothing left. The P&L's Spent table has four columns of
// its own, every cell through `usd` (rec-da931576).
/// "−$389" for $389 of spend, "$0" for none — the phone's `spent`.
const spent = n => usd0(-(Number(n) || 0));

// A window over a long list (2026-08-29: "I wanted to show all of them"):
// a page at a time, "Show all", and where you are — with hundreds of rows
// "Show more" alone says nothing. `fn` names the page's global that grows the
// window: fn() by a page, fn(true) to everything.
function pagerHTML(total, shown, page, fn) {
  if (total <= shown) return total > page ? `<div class="arow small muted">All ${total} shown.</div>` : '';
  const more = Math.min(page, total - shown);
  return `<div class="arow">
    <button class="sm" onclick="${fn}()">Show ${more} more</button>
    <button class="sm" onclick="${fn}(true)">Show all ${total}</button>
    <span class="small muted">${shown} of ${total}</span></div>`;
}

// One pager: how many rows of a list are drawn now. rows() cuts the list,
// html() is the pagerHTML line whose buttons call more() by name, paint is
// what more() repaints (settable after, for a list that draws its own table).
// reset() is a new search/tab; every pager starts over on a route change
// (pagersReset, the router's onLeave).
const pagers = {};
function makePager(name, page, paint) {
  const p = pagers[name] = {
    shown: page, paint,
    rows: rows => rows.slice(0, p.shown),
    html: (total, shown) => pagerHTML(total, shown, page, `pagers.${name}.more`),
    more: all => { p.shown = all ? Infinity : p.shown + page; if (p.paint) p.paint(); },
    reset: () => { p.shown = page; },
  };
  return p;
}
// Pages, not a growing window. 2026-10-27 (01:32, on a long table):
// "there isn't any way of collapsing it once it's back… this is blowing up
// out of control. What I'd much prefer is that it goes up to 20 rows, goes up
// at the top… pagination where I can either hit next or previous 20… I'd want
// both showing at any given time, but if you're at the end, then previous
// could be grayed out or next be grayed out. And then this, like, one to 20
// of 198." So: both buttons always drawn, the one with nowhere to go disabled
// (app.css button:disabled is the grey), the window's place beside them, and
// nothing at all when the list fits on one page. The row goes ABOVE the table.
function pagesHTML(total, at, page, fn) {
  if (total <= page) return '';
  const end = Math.min(total, at + page);
  return `<div class="pages">
    <button class="sm" onclick="${fn}(-1)"${at <= 0 ? ' disabled' : ''}>Previous</button>
    <button class="sm" onclick="${fn}(1)"${end >= total ? ' disabled' : ''}>Next</button>
    <span class="small muted">${at + 1}–${end} of ${total}</span></div>`;
}
// One paged list: rows() cuts the current page, html() is the pagesHTML row
// whose buttons call go(±1) by name, paint repaints (settable after). Shares
// the pagers registry so a route change resets it to page one.
function makePages(name, page, paint) {
  const p = pagers[name] = {
    at: 0, page, total: 0, paint,
    rows: rows => { p.total = rows.length; return rows.slice(p.at, p.at + page); },
    html: total => pagesHTML(total, p.at, page, `pagers.${name}.go`),
    go: dir => {
      p.at = Math.max(0, p.at + dir * page);
      if (p.total && p.at >= p.total) p.at = Math.floor((p.total - 1) / page) * page;
      if (p.paint) p.paint();
    },
    reset: () => { p.at = 0; },
  };
  return p;
}
function pagersReset() { for (const p of Object.values(pagers)) p.reset(); }
onLeave.push(pagersReset);

// A labelled <select> for the forms (calendar's new item, a goal's cadence).
const select = (id, label, opts, cur) =>
  `<label class="field" style="flex:1;min-width:150px"><span>${label}</span><select id="${id}">
    ${opts.map(o => `<option value="${o}"${o === cur ? ' selected' : ''}>${o}</option>`).join('')}</select></label>`;

// ---------- the three shapes every page is built from ----------
// `card` is the white box a list is made of, `pill` the small coloured
// label. `inner` is HTML the caller already escaped; a pill's text is escaped
// here.
const card = (inner, cls = '', attrs = '') => `<div class="card${cls ? ' ' + cls : ''}"${attrs ? ' ' + attrs : ''}>${inner}</div>`;
const pill = (text, cls = '') => `<span class="pill${cls ? ' ' + cls : ''}">${esc(text)}</span>`;

// ---------- one ask ----------
// An ask is drawn INSIDE a session's chat, as the bubble under the reply that
// raised it (class `ask`, id ask-card-<id> so #/sessions/<thread>/<ask-id> can
// scroll to and flash it). The Your-turn stack that drew it as a card went on
// 2026-09-02, and its frame went with it.
//
// `answered` counts as still needing action: the owner has replied but the agent
// has not closed it, so the card keeps its buttons (and a ⏳, not a ✓ — the
// tick read as "this is finished", 2026-08-24). An error card is not work
// the owner does — it is a run that wants starting again (2026-08-24): one line,
// and the only button that means anything is Restart (retry.go).
//
// Answering is the composer's (prompts engine phase 3, 2026-08-27). Reply /
// Done / Dismiss were three buttons for one act — "Reply, done, and
// dismiss: basically, none of those need to be options." A card's answer
// buttons arm the session's own composer with this card and that pick
// (views/threads.js armReply; from elsewhere it opens the session there
// first): the words and figures typed the normal way, WHICH session hears
// them and WHEN from the composer's own selects — one prompt that references
// this card.
//
// Nothing free-form goes in an onclick: a JSON-quoted title inside a
// double-quoted attribute ended the attribute at its first quote and silently
// broke the button for every ask (2026-08-24). A button carries only the
// id and the hub's outcome value, and re-reads the card from the hub.
// The install cell's two halves, shared with the calendar's row (2026-09-12:
// "Install app build should be its own kind of cell on the calendar"): the
// OTA link the Install button carries — the https form, since an
// itms-services link only means something to iOS — and the detail without
// its link line, because the button IS the link.
const askInstallLink = detail =>
  ((detail || '').match(/https:\/\/[^\s)\]>"',;]+/) || (detail || '').match(/itms-services:\/\/[^\s)\]>"',;]+/) || [])[0];
const askDetailShown = detail =>
  (detail || '').split('\n').filter(l => !/https:\/\/|itms-services:\/\//.test(l)).join('\n').trim();
// Which device an install card is for (2026-09-30): the hub's `target`
// ("phone" | "mac"), or — a calendar row, an older hub — the title's word.
const askInstallTarget = a => a.target || (/^install (desktop|mac) build \d+/i.test(a.title || '') ? 'mac' : 'phone');

// askBody: a long detail folds the way a long chat message does (isLongText)
// — a tall screen's worth, faded, and a "Show N more lines" button that pops
// it open to full height, "Show less" pops it shut (2026-09-14: a
// 2,000-line read card swallowed the session; 2026-09-24: the scroll box that
// replaced it is worse — "I hate the internal scroll bar"). The 10-26 in-card
// pagination (Show more / Back up sliding a window) is gone
// (2026-10-26 21:08: "This pagination system is terrible and I hate it… put it
// back to the way that it was, where it would pop open and then you pop it
// closed"), and the shut box is twice its old height, 30 lines ("These need
// to be made to be substantially longer"). `key` ("ask:<id>") keeps the fold
// across redraws. Shut, the button is drawn hidden and fitFolds shows it with
// its number — or never does.
//
// The fold's second rule (2026-10-07 02:26, on a two-paragraph
// message that wrapped to fourteen lines and said "Show all · 2 lines": "it
// doesn't create any expansion… it would never say show only 2 lines, this
// wouldn't have a button instead"): past isLongText the body clamps with its
// fade, but the button exists only when opening it would show at least
// FOLD_MIN_LINES more lines, and says how many. Measured on the rendered box
// — scrollHeight against the clamp, in line-heights — because the text's
// newlines say nothing about how many lines it wraps to. Under the line the
// body is shown whole (`.fits`). The phone's FoldedText is the same rule.
const FOLD_MIN_LINES = 5;
const foldLabel = (open, n) => open ? 'Show less' : `Show ${n} more lines`;
const foldButtonHTML = (open, onclick) =>
  `<button type="button" class="fold small"${open ? '' : ' hidden'} onclick="${onclick}">${foldLabel(open, 0)}</button>`;
function fitFolds(root) {
  for (const el of (root || document).querySelectorAll('.ask-long:not(.open):not([data-fit])')) {
    if (!el.clientHeight) continue;            // not laid out (a hidden pane): a later pass
    const cs = getComputedStyle(el);
    const lh = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.45;
    const n = Math.round((el.scrollHeight - el.clientHeight) / lh);
    el.dataset.fit = n;
    const btn = el.nextElementSibling;
    if (!btn || !btn.classList.contains('fold')) continue;
    if (n < FOLD_MIN_LINES) { el.classList.add('fits'); btn.hidden = true; continue; }
    btn.dataset.n = n;
    btn.textContent = foldLabel(false, n);
    btn.hidden = false;
  }
}
// Every paint is fitted once a frame: the chat redraws every 5 s and cards
// come and go on every page, so one observer watches the whole document
// rather than each painter remembering to call it. Fitting marks the nodes
// (`data-fit`), so the pass its own edits trigger finds nothing to do.
if (typeof document !== 'undefined' && typeof MutationObserver !== 'undefined') {
  let fitQueued = false;
  const queueFit = () => {
    if (fitQueued) return;
    fitQueued = true;
    requestAnimationFrame(() => { fitQueued = false; fitFolds(); });
  };
  document.addEventListener('DOMContentLoaded', () => {
    new MutationObserver(queueFit).observe(document.body, { childList: true, subtree: true });
    queueFit();
  });
  addEventListener('resize', queueFit);
}
const cardLongOpen = new Set();
function askBody(detail, key) {
  if (!isLongText(detail)) return md(detail);
  const open = cardLongOpen.has(key);
  return `<div class="ask-long${open ? ' open' : ''}">${md(detail)}</div>
    ${foldButtonHTML(open, `toggleCardLong(this,'${esc(key)}')`)}`;
}
function toggleCardLong(btn, key) {
  const open = !cardLongOpen.delete(key);
  if (open) cardLongOpen.add(key);
  btn.previousElementSibling.classList.toggle('open', open);
  btn.textContent = foldLabel(open, btn.dataset.n);
}

// ---------- the one card (2026-09-18) ----------
// Every card in the chat — an ask, an approval, a rec — is ONE shape
// (2026-09-18: "they should have the same components, and there shouldn't be
// any excess styling code… reuse as much code as possible, standardize the
// UI"): a tinted box, a mark and the title, the body, a meta row, and ONE row
// of word buttons at the bottom — the pick on the composer FILLED in the
// card's tint, the suggested first pick outlined in it (2026-10-11). The words
// are the hub's — `outcomes` on an ask, an action and a rec are one shape
// (store.Outcome) — and each arms the composer with that pick; the client
// adds only the silent closes (Dismiss; a read's Read) and the two buttons
// that are not answers (Install, Restart). A closed card, whatever closed it,
// FOLDS to one row with Reopen inside (2026-09-15: "there's still some
// UI footprint that can be used to reopen it or just even reread it";
// 2026-10-25: every closed card, not only a dismissed one); `opened` is the
// chat's remembered fold (data-k survives the 5 s redraw).
//
// A card is { ref, id, tint, mark, title, body, meta, acts, folded, at, cost }:
//   ref     ask | action | rec — the id's namespace (data-k)
//   tint    '' red (a stopped session, a proposal) | read | install | rec | err-card
//   acts    [{ label, js | href, primary, suggested }] — the row of an open
//           card: `primary` filled (on the bar, or a one-shot act such as
//           Install), `suggested` outlined (the first pick, nothing armed)
//   folded  the row's word (a closed card; acts = Reopen)
const btnCls = b => 'sm' + (b.primary ? ' primary' : b.suggested ? ' suggested' : '');
const cardBtn = b => b.href
  ? `<a class="btn ${btnCls(b)}" href="${esc(b.href)}" target="_blank">${b.label}</a>`
  : `<button class="${btnCls(b)}" onclick="${b.js}">${b.label}</button>`;
function cardHTML(c, opened) {
  const id = esc(c.id), acts = (c.acts || []).map(cardBtn).join('');
  const body = c.body ? `<div class="small">${c.body}</div>` : '';
  // THE MESSAGE IS THE CARD (2026-09-24: "I love audio so much that I
  // kind of want to make it the main response format… I want to see the text
  // for what is played"). What his AirPods spoke when this card arrived
  // (asks.said / actions.said) LEADS the card, in the card's own words, and
  // the detail — his steps, when there are any — follows it. A title the
  // message already says (a read minted from a `Say:` reply is titled from
  // its first sentence) is not drawn twice: the mark moves onto the message.
  // THE HEAD LINE (2026-09-24: "I don't like the 2 emojis on the same
  // row. But I do like the blue dot… it symbols this is a read cell or a your
  // action cell"): the mark and the card's one word (READ · DO · DECIDE ·
  // APPROVE, the Sessions cell's corner word) on a line of their own, and
  // Play — the spoken message again through the browser (sayAgain), a second
  // click stops it — at the far end. Under it the title, when the message
  // does not already say it, then the message in plain body type.
  const titled = !saidCovers(c.said, c.title) && !saidCovers(c.body, c.title, true);
  // A line still queued for his AirPods wears "Waiting to speak" instead, and
  // a double click drops the line, never the card (sayHushCard;
  // 2026-09-28: "dismissing the audio is not the same as dismissing the card").
  // …and a line in his ears right now wears "Speaking", the same double click
  // stopping it (2026-09-29: "it's speaking through the headphones now,
  // but on the card, it still says play").
  const play = !c.said ? ''
    : c.speaking ? `<button class="say-btn speaking" data-ref="${esc(c.id)}" data-say="${esc(c.said)}" data-thread="${esc(c.thread || '')}" ondblclick="sayHushCard(this)" title="Double-click: stop speaking"></button>`
    : c.waiting ? `<button class="say-btn waiting" data-ref="${esc(c.id)}" data-say="${esc(c.said)}" data-thread="${esc(c.thread || '')}" ondblclick="sayHushCard(this)" title="Double-click: don't speak this one"></button>`
    : `<button class="say-btn${c.said.trim() === sayNow ? ' saying' : ''}" data-ref="${esc(c.id)}" data-say="${esc(c.said)}" data-thread="${esc(c.thread || '')}" onclick="sayAgain(this)" title="Play again"></button>`;
  // ONE time per card, on the head line (2026-10-22: "it's not
  // obvious whether or not this is the presented time or the accepted time"):
  // when it was raised while it is open, when it closed once it is — the
  // chat's own "today 3:15 PM", never "34m ago" in one place and a clock in
  // another. The phone's Card `at`.
  // `cost`: the turn that raised it, which rides on its first card (the chat's
  // end line went with the tool calls, 2026-10-25).
  const at = [c.at ? esc(dayWhen(c.at)) : '', c.cost ? usd(c.cost) : ''].filter(Boolean).join(' · ');
  const head = `<div class="card-h">${c.mark}<span class="verb">${esc(c.verb || '')}</span>${at ? `<span class="at">${at}</span>` : ''}${play}</div>`;
  const said = c.said ? `<div class="said">${esc(c.said)}</div>` : '';
  // A closed card is one row (rowHTML): its state, title and time; open it to
  // reread or Reopen.
  if (c.folded) {
    return rowHTML({ id: 'ask-card-' + id, key: c.ref + ':' + id, opened, k: c.folded, title: mdInline(c.title), when: at,
      body: said + body + (c.meta ? `<div class="small muted">${c.meta}</div>` : '') + (acts ? `<div class="acts">${acts}</div>` : '') });
  }
  return `<div class="${['ask', c.tint].filter(Boolean).join(' ')}" id="ask-card-${id}">
      ${head}${titled ? `<div class="t">${mdInline(c.title)}</div>` : ''}
      ${said}${body}
      ${c.meta ? `<div class="row small muted">${c.meta}</div>` : ''}
      <div class="acts">${acts}</div></div>`;
}
// THE ROW (2026-10-25, the decluttered chat, mock B3: "this is exactly
// what i wanted"). Everything in a chat that is not an open card is one line
// of the same shape: a closed card, his message, a note, a check-in. `k` is
// the word that leads it (✓ DONE, YOU, ⏰ CHECK-IN), then the title cut to
// the line, the time at the end; a click opens it in place. `cls` "me" is his
// (the grey fill; every other row is white). `key` keeps it open across the
// redraw (data-k, wireChat). The phone's ChatRow.
function rowHTML({ id, key, opened, cls, k, title, when, body }) {
  return `<details class="chat-row${cls ? ' ' + cls : ''}"${id ? ` id="${esc(id)}"` : ''} data-k="${esc(key)}" data-def="0"${opened ? ' open' : ''}>
    <summary><span class="k">${k}</span><span class="t">${title}</span><span class="when">${when || ''}</span></summary>
    <div class="row-body">${body}</div></details>`;
}
// Markdown compared as words: links to their text, marks and a cut title's
// "…" dropped. Used to spot a body line that only repeats the title.
const plainText = s => (s || '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/[*_`#…]/g, '').trim();
// saidCovers: does the spoken message (or the body) already say the title?
// Compared plain, case folded, the way the phone's SessionCell.body drops a
// title-repeating line. A folded card open hides the title it covers: a read's
// title is its message's first words cut with "…" (2026-10-23: "the first
// paragraph kinda truncated… the second bit being the text that was actually
// on the card"). The body covers it only by STARTING with it (`lead`): an
// approval titled "git commit" keeps its title over a will-run block that
// merely holds "git commit -m x" (2026-10-24).
function saidCovers(said, title, lead) {
  const t = plainText(title).toLowerCase();
  if (!t || !said) return false;
  const s = plainText(said).toLowerCase();
  return lead ? s.startsWith(t) : s.includes(t);
}
// Read a card's spoken line again, on command (2026-09-23: "sometimes I
// miss the audio or find the reads later so I want a way to have the
// messages / asks read out on command"). The push spoke it once, into the
// Mac's Bluetooth device (notify/speak.go); this plays the same recorded
// sentence as audio the hub renders in its session's voice (GET /voice/audio),
// to whatever the Mac's output is now. Not the browser's speechSynthesis: it
// went silent while still firing its start (2026-09-27: "nothing
// happens, no sound comes out"). One line at a time: the same line again
// stops it, another line replaces it. `sayNow` is the line playing, so a
// re-render of the page keeps its button's `saying` mark (app.css) — the
// board redraws while a session runs, and the mark used to vanish with it.
let sayGen = 0, sayThread = null, sayNow = null, sayAudio = null;
function sayHush() {
  document.querySelectorAll('.say-btn.saying').forEach(e => e.classList.remove('saying'));
  if (sayAudio) { sayAudio.pause(); sayAudio.removeAttribute('src'); sayAudio = null; }
  sayNow = null;
}
// sayHushCard: the queued line goes (POST /voice/hush), the card stays; the
// button turns back into Play at once, and the board's next poll agrees.
function sayHushCard(el) {
  post('/voice/hush', { card: el.dataset.ref }).catch(() => {});
  el.classList.remove('waiting', 'speaking');
  el.ondblclick = null;
  el.onclick = () => sayAgain(el);
}
function sayAgain(el) {
  if (typeof Audio === 'undefined') return;
  const text = (el.dataset.say || el.textContent).trim();
  const was = sayNow === text;
  const gen = ++sayGen;
  sayHush();
  // One voice across every console window (windows.js): a replay in another
  // window stops here.
  if (typeof winSend === 'function') winSend('say', {});
  // The hub holds the floor while a line is replayed: its session's row says
  // "speaking" and no push talks over it (POST /voice, 2026-09-25). A
  // replay that was cut off by the next one does not end the next one's mark.
  const thread = el.dataset.thread || '';
  const voice = secs => {
    if (secs || gen === sayGen || thread !== sayThread) post('/voice', { thread_id: thread, secs }).catch(() => {});
  };
  sayThread = was ? null : thread;
  if (was) { voice(0); return; }
  const a = new Audio('/api/v1/voice/audio?text=' + encodeURIComponent(text) + '&thread=' + encodeURIComponent(thread));
  const done = () => {
    if (sayAudio !== a) return;
    sayAudio = null; sayNow = null;
    document.querySelectorAll('.say-btn.saying').forEach(e => e.classList.remove('saying'));
    voice(0);
  };
  a.onended = a.onerror = done;
  // "speaking" starts when the sound does, not on the click
  // (2026-09-27: "it'll say it's speaking, but no audio comes out").
  a.onplaying = () => voice(Math.ceil(a.duration || text.split(/\s+/).length / 2.5) + 2);
  sayAudio = a; sayNow = text;
  el.classList.add('saying');
  a.play().catch(done);
}

// The hub's outcomes as the row, in the hub's order — the same order the
// composer strip draws its chips (2026-09-18: "Buttons are in a
// different order on the bar than on the card"). The hub lists the decisive
// ones first and "Reply" (value "") last. `armed` is the pick this card has
// on the composer right now ('' = Reply; undefined/null = none): that one is
// primary (filled) and nothing else is marked; with none, the first decisive
// one is suggested (outlined) — never filled, so that arming it is a visible
// change (2026-10-11: "if you hit accept… the 'accept' doesn't
// highlight at all"). `arm(value)` is each button's onclick.
function outcomeBtns(outcomes, arm, armed) {
  const none = armed === undefined || armed === null;
  return (outcomes || []).map((o, i) => ({ label: esc(o.label), js: arm(o.value), primary: !none && armed === o.value, suggested: none && i === 0 && !!o.value }));
}

// Where a card stands is the hub's (store.AskStanding): `open` = his move,
// `closed` = finished, neither = answered and waiting on its session;
// `folded` = the hub's word for how it closed ("dismissed", "read", "done"),
// the word its row leads with. What a
// card wants from him, in one word, is the ask's own `verb` (store.AskVerb).
// The paused card (2026-09-30: "a good, well-styled card for… hitting a
// session limit… serious… fit in our general design"). A session limit is a
// wait with an end, so it is not the grey error: amber like "waiting", ⏸ and
// PAUSED on the head line, and the wait itself as a bar from the moment it
// stopped to the reset (hub `resumes_at`), the two clocks and what is left
// under it. The hub resumes it at the reset; Resume now is the Restart.
const PAUSE_MARK = '⏸︎';
const pauseRe = /hit your (?:[a-z0-9 ]{0,24} )?limit[^\n]*?resets\s/i;
const isPauseText = s => pauseRe.test(s || '');
function pauseBarHTML(a) {
  const from = new Date(a.created_at).getTime(), to = new Date(a.resumes_at).getTime(), now = Date.now();
  const pct = to > from ? Math.max(0, Math.min(100, (now - from) / (to - from) * 100)) : 100;
  const mins = Math.ceil((to - now) / 60000);
  const left = mins <= 0 ? 'resuming…' : mins < 60 ? `${mins} min` : `${Math.floor(mins / 60)} h ${mins % 60} min`;
  return `<div class="pause-bar"><i style="width:${pct.toFixed(1)}%"></i></div>
    <div class="pause-foot"><span>${clock(new Date(from))}</span><span>${clock(new Date(to))} · ${left}</span></div>`;
}

// `armed`: the pick this card has on the chat's composer (threads.js
// chatArmed), undefined off a chat or when nothing of it is armed. `cost`:
// the turn's dollars, on the first card it raised (threads.js msgHTML).
function askHTML(a, opened, armed, cost) {
  const id = esc(a.id), err = a.kind === 'error', paused = err && !!a.resumes_at;
  const onBar = armed !== undefined && armed !== null;
  const open = !a.closed, answered = open && !a.open;
  // A read card is blue, never red (2026-08-28): it asks him to read an
  // answer, not to unblock anything — red is for a stopped session. Replying
  // to one does not turn it red either: it is still a read, now waiting on
  // the agent to close it. The install cell (2026-09-08) is teal: the
  // Install button IS the OTA link (the https form; the phone rewrites it),
  // and the link line leaves the detail.
  const read = a.kind === 'read' && open, install = a.kind === 'install' && open;
  // The desktop app's build (2026-09-30, `target` mac): no link — Install
  // runs the hub's `mac` lane, which builds if it must and restarts the app.
  const mac = install && askInstallTarget(a) === 'mac';
  const link = install && !mac && askInstallLink(a.detail);
  const dismiss = { label: 'Dismiss', js: `cardFold('ask','${id}')` };
  const respond = { label: 'Respond', js: `openRespond('${id}')`, primary: onBar, suggested: !onBar };
  // A closed card carries Reopen when the hub says so (`reopen`: every kind
  // but an install, 2026-09-29) — the done ones too, not just a fold.
  const acts = !open ? (a.reopen ? [{ label: 'Reopen', js: `cardFold('ask','${id}',true)` }] : [])
    // An error is a run that wants starting again (2026-08-24).
    : err ? [{ label: paused ? 'Resume now' : 'Restart', js: `retryAsk('${id}')`, primary: true }, dismiss]
    : install ? [mac ? { label: 'Install', js: `macInstall('${id}')`, primary: true } : link ? { label: 'Install', href: link, primary: true } : respond, dismiss]
    // A read: Respond arms the reply (sending closes it, words or not); Read
    // is its silent close — done, "Read it", wakes nothing (2026-09-17:
    // "when I hit the white read button… I have read this message and I
    // don't care about it").
    : read ? [respond, { label: 'Read', js: `resolveAsk('${id}','done','${esc(((a.outcomes || [])[0] || {}).label || '')}')` }]
    // Every other kind: the hub's words (Decided · Just
    // reply), each arming the composer with that pick, then Dismiss — the
    // silent close (2026-08-28: "once I read it, I can just dismiss it").
    : (a.outcomes && a.outcomes.length ? outcomeBtns(a.outcomes, v => `openRespond('${id}','${v}')`, armed) : [respond]).concat(dismiss);
  return cardHTML({
    ref: 'ask', id: a.id, title: a.title, verb: a.verb || 'for you',
    tint: paused && open ? 'paused' : err ? 'err-card' : read ? 'read' : install ? 'install' : '',
    mark: answered ? '⏳ ' : paused ? PAUSE_MARK + ' ' : err ? '⚠︎ ' : read ? '🔵 ' : install ? '📲 ' : '🔴 ',
    // An item's steps (2026-10-18): each its own Did it · Skip · Reply, on the
    // one card, staged on the chat's bar until Send (2026-10-23).
    body: paused && open ? pauseBarHTML(a) : err ? '' : askBody(install && !mac ? askDetailShown(a.detail) : (a.detail || ''), 'ask:' + a.id)
      + (a.steps && typeof calStepsHTML === 'function' ? calStepsHTML(a, typeof armStepReply === 'function'
        ? { card: a.id, armed: n => chatArmed('step', (a.cal_id || a.id) + '.' + n) } : undefined) : ''),
    said: a.said, thread: a.thread_id, waiting: open && a.waiting_to_speak, speaking: open && a.speaking, acts, cost,
    at: open ? a.created_at : a.resolved_at || a.created_at,
    folded: open ? '' : a.folded === 'dismissed' || a.state === 'dismissed' ? '✕ dismissed' : '✓ ' + esc(a.folded || a.state),
  }, opened);
}

// forYouPill: the "N for you" on a session's row — blue "N to read" when
// every item only asks him to read (board `reads`), teal "N to install" when
// every item is an app build (board `installs`, 2026-09-12), red otherwise.
function forYouPill(n, reads, installs) {
  if (!n) return '';
  if (reads >= n) return `<span class="pill read">${n} to read</span>`;
  if ((installs || 0) >= n) return `<span class="pill install">${n} to install</span>`;
  return `<span class="pill needs">${n} for you</span>`;
}

const kindPill = { money: 'amber', delete: 'needs', contact: 'purple', share: 'purple', commit: 'purple' };
// The approval card (2026-09-17: "the approve cells [should] have the
// same styling as the other cells… their buttons are on the bottom. The
// buttons really just allow you to send a message in the box"): 🔴 title,
// the detail, what it will run, the kind row, and the hub's Approve · Deny ·
// Reply — each ARMS the session's composer (armActionReply,
// views/threads.js); Send posts one prompt naming the action with his pick
// and the hub decides the row from it (threads DecideAction). A scheduled
// job's proposal has no session: its card names the job and Approve/Deny
// decide it from the card (decideAction, views/asks.js). Dismiss is the
// silent close either way — nothing runs, nobody is told (2026-09-18:
// "once they become out of date… sometimes I forget to respond to things").
// Drawn INSIDE the chat under the reply that proposed it (no message id of
// its own: after the last message that precedes it in time), with id
// `ask-card-<id>` so a deep link scrolls to it like an ask. A decided one is
// a row: ✓ approved, ✕ denied. The audit trail under it
// ("proposed by claude:thread:…") went 2026-10-22 ("This
// proposed by who business is probably not necessary").
// The job behind a thread-less action's `source` (claude:job:<name>), or ''.
const actionJob = a => (!a.thread_id && /^claude:job:/.test(a.source || '')) ? a.source.slice('claude:job:'.length) : '';
function actionHTML(a, opened, armed, cost) {
  const id = esc(a.id), job = actionJob(a);
  const open = a.open, no = a.state === 'denied' || a.state === 'failed';
  const payload = a.exec_payload && a.exec_type && a.exec_type !== 'none'
    ? `<details style="margin:6px 0"><summary class="muted">will run: ${esc(a.exec_type)}</summary>
       <pre class="io mono">${esc(typeof a.exec_payload === 'string' ? a.exec_payload : JSON.stringify(a.exec_payload, null, 2))}</pre></details>`
    : '';
  const arm = v => a.thread_id ? `armActionReply('${id}','${v}')` : `decideAction('${id}',${v === 'approved'})`;
  const acts = !open ? (a.reopen ? [{ label: 'Reopen', js: `cardFold('action','${id}',true)` }] : []) : outcomeBtns(a.outcomes, arm, armed).concat({ label: 'Dismiss', js: `cardFold('action','${id}')` });
  return cardHTML({
    ref: 'action', id: a.id, title: a.title, tint: '', verb: 'approve',
    mark: '🔴 ',
    body: askBody(a.detail || '', 'action:' + a.id) + payload,
    said: a.said, thread: a.thread_id, waiting: open && a.waiting_to_speak, speaking: open && a.speaking,
    meta: `${pill(a.kind, kindPill[a.kind])} ${job ? pill('⚙ ' + job + ' job') : ''} ${a.project ? esc(a.project) : ''}`,
    at: open ? a.created_at : a.decided_at || a.created_at,
    acts, cost, folded: open ? '' : a.folded === 'dismissed' || a.state === 'dismissed' ? '✕ dismissed'
      // Cut off mid-run (the hub restarted, or a kill): neither ✓ nor ✕.
      : a.state === 'uncertain' ? '? Unknown - check before retrying' : (no ? '✕ ' : '✓ ') + esc(a.state),
  }, opened);
}

const DOM_PILL = { money: 'amber', health: 'done', audience: 'purple', tools: 'running', home: '', other: '' };
// What saying yes costs — the hub's `cost_label` ("$11/mo", "$33,837 one-off",
// "free", "price not checked"), the phone's exact words. Unpriced is muted.
function costLabel(r) {
  const c = r.cost_label || '';
  return r.cost_cents > 0 ? `<strong>${esc(c)}</strong>` : `<span class="muted">${esc(c)}</span>`;
}
// A rec's status in words and pills, ONE set for the Recs card and All
// recommendations (rec-0adcadce): an open rec wears
// no status pill (its buttons say it is open), a parked one reads "later",
// never the hub's raw "deferred"; a scored one adds its outcome.
const REC_STATE_WORD = { proposed: 'open', deferred: 'later', accepted: 'accepted', declined: 'declined', done: 'done', superseded: 'superseded', expired: 'expired' };
const STATUS_PILL = { proposed: 'amber', deferred: 'purple', accepted: 'running', done: 'done', declined: '', expired: '', superseded: '' };
const OUTCOME_PILL = { worked: 'done', mixed: 'amber', failed: 'needs', unclear: '' };
const recStateWord = r => REC_STATE_WORD[r.status] || r.status;
function recPills(r) {
  return (r.status === 'proposed' ? '' : pill(recStateWord(r), STATUS_PILL[r.status]))
    + (r.outcome ? pill(r.outcome, OUTCOME_PILL[r.outcome]) : '');
}
