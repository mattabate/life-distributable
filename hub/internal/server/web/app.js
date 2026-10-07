// life hub — laptop console.
//
// Same hub, same REST API, same objects as the iOS app (shared/api.md). This
// exists because the work the sessions ask the owner for — make an API key, run
// an OAuth flow, hand over a CSV — is laptop work, and bouncing to the phone to
// read the ask and reply is friction.
//
// No build step and no framework on purpose: the hub is one static binary
// with no node toolchain, and `make check` should not grow a JS lane. Auth is
// the `life_token` cookie the hub already sets from /?token=…
//
// Files (all plain scripts, index.html loads them in this order):
//   ui.js          shared render helpers, the one ask card, the action card
//   views/<x>.js   one page each, attaching views.<x> = { draw, redraw }
//   app.js         api(), the change feed, the router, the board + badges, boot
'use strict';

// ---------- api ----------
// The decider code: the second credential Approve/Deny needs. It lives in this
// browser only — never on the hub's disk, where every session can read — so
// the hub can tell the owner's own click apart from a session holding the same token. `ops/decider-set.sh`
// prints it; withDecider() (views/asks.js) asks for it the first time the hub
// refuses — on an approval sent from the composer or a job proposal's card.
const decider = {
  get: () => localStorage.getItem('life.decider') || '',
  set: v => localStorage.setItem('life.decider', (v || '').trim().toUpperCase()),
  clear: () => localStorage.removeItem('life.decider'),
};

async function api(path, opts = {}) {
  const headers = opts.body && !(opts.body instanceof FormData) ? { 'Content-Type': 'application/json' } : {};
  if (decider.get()) headers['X-Life-Decider'] = decider.get();
  // fetch() rejects (TypeError "Failed to fetch") only when no answer came
  // back — in practice a hub restart, a second or two of refused connections.
  // Retry through it quietly instead of toasting it at a Send (2026-09-18).
  // A graceful drain finishes in-flight requests, so a POST that was handled
  // got its answer and is never sent twice.
  let r;
  for (let i = 0; ; i++) {
    try {
      r = await fetch('/api/v1' + path, { credentials: 'same-origin', headers, ...opts });
      break;
    } catch (e) {
      if (i >= 5 || !(e instanceof TypeError)) throw e;
      await new Promise(ok => setTimeout(ok, 250 * 2 ** i)); // 0.25 … 4 s, ~8 s in all
    }
  }
  conn(r.ok);
  seenBuild(r.headers.get('X-Hub-Build'));
  if (r.status === 401) { document.body.innerHTML = '<div class="empty">Unauthorized. Open <code>/?token=&lt;hub token&gt;</code> once.</div>'; throw new Error('unauthorized'); }
  if (r.status === 204) return null;
  const txt = await r.text();
  let j = null; try { j = txt ? JSON.parse(txt) : null; } catch { /* non-JSON */ }
  if (!r.ok) {
    const err = new Error((j && j.error) || txt || ('HTTP ' + r.status));
    err.status = r.status;   // withDecider() needs to tell 403 from a 409
    throw err;
  }
  return j;
}
// ---------- the console notices it has been rebuilt ----------
// This page is a hash router: #/goals → #/goals/<id> is a
// fragment change, never a document load. A tab left open across a `make check
// && ops/hub.sh restart` therefore keeps running yesterday's JS forever, and
// the symptom is a feature that is provably live on the hub and provably
// absent on the owner's screen.
//
// Every hub response stamps `X-Hub-Build` (a hash of the console files in the
// running binary, server.go). The first one is this page's own build; a later
// one that differs means the files under it changed, so the page reloads. The
// route is in the URL fragment, so they land back on exactly what they were
// reading. A restart breaks the parked /changes request, and the retry after
// it reads the new build, so the wait is a few seconds.
let hubBuild = null;
function seenBuild(b) {
  if (!b) return;
  if (hubBuild === null) { hubBuild = b; return; }
  if (b === hubBuild) return;
  // `hubBuild` is deliberately left at the OLD value, so a reload held back
  // below is retried by the next poll rather than lost.
  // Never eat something the owner is in the middle of writing: a composer with text
  // in it holds the reload back, and the next poll tries again.
  const el = document.activeElement;
  if (el && /^(TEXTAREA|INPUT)$/.test(el.tagName) && el.value) return;
  for (const t of document.querySelectorAll('textarea')) if (t.value.trim()) return;
  location.reload();
}

const get = p => api(p);
const post = (p, b) => api(p, { method: 'POST', body: b === undefined ? undefined : JSON.stringify(b) });
const patch = (p, b) => api(p, { method: 'PATCH', body: JSON.stringify(b) });
const put = (p, b) => api(p, { method: 'PUT', body: JSON.stringify(b) });

// ---------- live: one change feed instead of a timer per page ----------
// The phone's loop, on the console: refresh → GET /changes?since=… (parked up
// to 25 s) → refresh. Nothing is re-read while nothing moved, and a change
// lands the moment the hub has it instead of on the next 5/20/30 s tick. The
// open page says what "refresh" means for it through setRedraw (ui.js). While
// the tab is hidden the loop stops; showing it again refreshes at once.
// On an error it retries after 5 s, then every 60 s, refreshing each time.
const live = { token: 0, busy: false, again: false, timer: null, last: 0 };
const nap = ms => new Promise(ok => setTimeout(ok, ms));

async function liveRefresh() {
  if (live.busy) { live.again = true; return; }
  live.busy = true;
  try {
    do {
      live.again = false;
      await refreshBadges();
      await pageLive();
    } while (live.again);
  } finally { live.busy = false; }
}

// Repaint the open page, unless that would eat something the owner is doing: a
// drag on the calendar grid, or a form field they are typing in (a page that keeps
// its own drafts says `typing`). A heavy page asks for `every` ms between
// repaints; the change that arrives early is picked up when that runs out.
async function pageLive() {
  const fn = route.live;
  if (!fn || document.hidden) return;
  if (document.body.classList.contains('cal-dragging')) return;
  const view = document.getElementById('view');
  if (!route.typing && view && busyForm(view)) return;
  if (route.every) {
    const wait = live.last + route.every - Date.now();
    if (wait > 0) {
      const gen = route.gen;
      clearTimeout(live.timer);
      live.timer = setTimeout(() => { if (gen === route.gen) pageLive(); }, wait);
      return;
    }
  }
  live.last = Date.now();
  try { await fn(); } catch { /* the next change tries again */ }
}
function busyForm(view) {
  const a = document.activeElement;
  if (a && view.contains(a) && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName)) return true;
  for (const f of view.querySelectorAll('input:not([type=checkbox]):not([type=radio]):not([type=file]),textarea')) {
    if (f.value !== f.defaultValue) return true;
  }
  return false;
}

// One parked GET /changes loop (the board's and the open chat's): ask with the
// last version, call onChange when something moved. On an error, nap
// backoff(fails) ms and call onChange anyway — the error may have hidden a
// change. Ends the moment alive() says so. `baseline`: the first answer counts
// as a change (the chat drew before it; a change in between is inside it).
async function longPoll({ query = '', alive, onChange, backoff, baseline = false }) {
  if (HEADLESS) return;
  let since = '', fails = 0;
  while (alive()) {
    try {
      const r = await get(`/changes?wait=25${query}${since ? '&since=' + encodeURIComponent(since) : ''}`);
      if (!alive()) return;
      if (!r || !r.version) throw new Error('no version'); // never spin on a bad answer
      const moved = since ? r.changed : baseline;
      since = r.version;
      fails = 0;
      if (moved) await onChange();
    } catch {
      await nap(backoff(fails++));
      if (!alive()) return;
      await onChange();
    }
  }
}

function liveLoop() {
  const token = ++live.token;
  return longPoll({
    alive: () => token === live.token && !document.hidden,
    onChange: liveRefresh,
    backoff: fails => fails ? 60000 : 5000,
  });
}
document.addEventListener('visibilitychange', () => {
  if (document.hidden) { live.token++; return; }
  liveLoop();
  liveRefresh();
  if (route.wake) route.wake();
});

// ---------- router ----------
// Every page is a views/<name>.js that attached `views.<name> = { draw,
// redraw }` at load (ui.js declares `views`). draw(view, rest) fills #view for
// `#/<name>/<rest…>`; redraw repaints the page in place after a write.
async function render() {
  // The console lands on Sessions, which IS the Your turn page: its first
  // group is what needs the owner, with the card's own description under each
  // session. There is no separate #/asks page, so an old bookmark or an unknown hash lands here too.
  const hash = location.hash.replace(/^#\/?/, '') || 'sessions';
  const [name, ...rest] = hash.split('/');
  // `#/open/<id>` is an address, not a page: it is swapped for where the
  // thing lives, so Back never lands on it.
  if (name === 'open') { location.replace(await openRef(rest[0] || '')); return; }
  // The sources are cards on Configuration; a bare #/sources is that page.
  if (name === 'sources' && !rest.filter(Boolean).length) { location.replace('#/config'); return; }
  const page = views[name] || views.sessions;
  // Goals, Spend and a source's page live behind the Configuration tab, so
  // it stays lit while the owner is on any of them.
  const tab = ['sources', 'goals', 'spend'].includes(name) ? 'config' : views[name] ? name : 'sessions';
  document.querySelectorAll('#nav a').forEach(a => a.classList.toggle('on', a.dataset.tab === tab));
  // The page the owner is on may be behind the fold — More takes its name; and the
  // menu closes, this being the click that opened it landing.
  const navmenu = document.getElementById('navmenu');
  if (navmenu) navmenu.hidden = true;
  navFit();
  // Nothing a page left behind survives the route change. Its hooks go, and
  // #view itself is swapped for a fresh node: a draw from the page just left
  // that is still awaiting the hub then paints into a detached element,
  // never over the page on screen now (a slow Spend over a quick Sessions).
  route.gen++;
  setRedraw(null);
  for (const f of onLeave) f();
  clearTimeout(live.timer);
  live.last = Date.now();
  const old = document.getElementById('view');
  const view = document.createElement(old.tagName);
  view.id = old.id;
  if (old.className) view.className = old.className;
  old.replaceWith(view);
  const gen = route.gen;
  try { await page.draw(view, rest); } catch (e) {
    // The card says only "Couldn't load"; the console (and ops/web-preview.html) gets the why.
    console.error('render', name, e);
    if (gen === route.gen) view.innerHTML = `<div class="pane wide">${failHTML(name || 'this page')}</div>`;
  }
}
window.addEventListener('hashchange', render);

// Where a bare id opens (ui.js REF_PAGE sends every id that does not carry
// its place here): an ask or a proposal on its card in its session, a
// calendar item on its day with its panel open. One read of the hub; an id
// the hub does not know lands on the page of its kind.
async function openRef(id) {
  const ref = refOf(id), [kind] = splitRef(ref);
  try {
    if (kind === 'ask' || kind === 'action') {
      const o = await get(`/${kind}s/${encodeURIComponent(id)}`);
      if (o.thread_id) return refHref(ref, o.thread_id);
    } else if (kind === 'cal') {
      const i = await get('/calendar/' + encodeURIComponent(id));
      calState.anchor = i.day; calState.open = i.id;
    }
  } catch (e) { console.error('openRef', id, e); }
  return kind === 'rec' ? refHref(ref) : kind === 'cal' ? '#/calendar' : '#/sessions';
}

// ---------- what "your turn" means ----------
// ONE definition, and it is not in this file any more: the hub's board
// (internal/attention, GET /board?surface=web) decides what is the owner's
// turn, how it bundles by session, what each bundle opens on and what the
// three ovals say. The nav badge and the sessions list both read
// the same response, and the phone reads the same package with
// surface=mobile — so the console and the app can never print different
// numbers for the same thing. The console rule the hub applies: EVERY active
// ask + EVERY proposed action, nothing held back, nothing filtered by run
// state — this is where a running session gets steered. Since 2026-08-28 the
// phone holds nothing back either, and since 2026-09-23 the two boards are
// one board (the phone's folded `laptop` list is gone).
//
// `board` is the last response, kept so the draw functions that run in the
// same tick as a refresh can share it instead of asking twice.
let board = null;
const EMPTY_BOARD = { surface: 'web', count: 0, working: 0, calendar: [], sessions: [], for_you: {}, reads: {}, installs: {}, first: {}, badges: { your_turn: 0, calendar: 0, recs: 0 } };
async function loadBoard() {
  const b = await get('/board?surface=web');
  board = { ...EMPTY_BOARD, ...(b || {}) };
  board.for_you = board.for_you || {};
  board.first = board.first || {};
  board.badges = { ...EMPTY_BOARD.badges, ...(board.badges || {}) };
  return board;
}

// ---------- badges ----------
async function refreshBadges() {
  try {
    const b = await loadBoard();
    // Sessions wears the your-turn oval: it is the one tab that shows it.
    setBadge('sessions', b.badges.your_turn);
    // Recs get the same red oval as the other two, on the console only. It is
    // a COUNT, not a notification: open recs are undecided items the owner can
    // see sitting there when they look at the window, and nothing here pushes.
    // Deliberately kept out of document.title, which is the push channel.
    setBadge('recs', b.badges.recs);
    // Calendar: fired items whose day has come and nothing has closed since.
    setBadge('calendar', b.badges.calendar);
    document.title = b.badges.your_turn ? `(${b.badges.your_turn}) life` : 'life';
    return b;
  } catch { return null; }
}
function setBadge(tab, n) {
  const el = document.getElementById('badge-' + tab);
  if (!el) return;
  el.textContent = n || '';
  el.classList.toggle('show', !!n);
}

// The nav order is the order in index.html and NOTHING reorders it: Sessions,
// Recs, Calendar, Configuration. A badge
// colours a tab; it never moves one, so every tab is always where it was. The
// phone's tab bar and its More list are the same list in the same order with
// the same labels (RootView.swift `MoreDest`), so a change here is a change
// there.

// ---------- the bar fits its window ----------
// One row, always. Wide: every tab. Narrow: the
// buttons lose their long words first (.tight), then the LAST tabs fold into
// a More menu — the phone's bar, which keeps Sessions · Recs · Calendar and
// puts the rest behind More. Which tabs fold is
// measured, not a breakpoint: whatever no longer fits, from the right, so
// the order on both sides of the fold stays index.html's. Sessions never
// folds. When the open page is behind the fold, More wears its name and
// the accent, so the bar always says where you are; the folded tabs' red ovals
// add up on More. Re-measured on resize and whenever anything on the bar
// changes width (a badge appearing, the button reading "Snapping…") — a ResizeObserver, coalesced to one pass a
// frame so its own moves never feed back into it.
const navTabs = [...document.querySelectorAll('#navtabs a')];
function navFit() {
  const nav = document.getElementById('nav'), tabs = document.getElementById('navtabs');
  const menu = document.getElementById('navmenu'), more = document.getElementById('navmore');
  const conn = document.getElementById('conn');
  if (!nav || !tabs || !menu || !more || !conn) return;
  // An open menu stays open across a re-measure: opening it is itself a
  // resize (its rows go from nothing to their size), and a pass that closed
  // it would close it on the very click that opened it.
  const open = !menu.hidden;
  navTabs.forEach(a => tabs.appendChild(a));
  nav.classList.remove('tight');
  more.hidden = true; menu.hidden = true;
  const fits = () => conn.getBoundingClientRect().right <= nav.getBoundingClientRect().right - 14 + 0.5;
  if (fits()) return;
  nav.classList.add('tight');
  if (fits()) return;
  more.hidden = false; menu.hidden = !open;
  navMoreDraw();
  while (!fits() && tabs.children.length > 1) {
    menu.prepend(tabs.lastElementChild);
    navMoreDraw();
  }
}
function navMoreDraw() {
  const menu = document.getElementById('navmenu'), more = document.getElementById('navmore');
  const on = menu.querySelector('a.on');
  more.classList.toggle('on', !!on);
  more.querySelector('.label').textContent = on ? on.firstChild.textContent.trim() : 'More';
  let n = 0;
  menu.querySelectorAll('.badge.show').forEach(b => { n += Number(b.textContent) || 0; });
  setBadge('more', n);
}
function navMoreToggle(e) {
  e.stopPropagation();
  const menu = document.getElementById('navmenu');
  menu.hidden = !menu.hidden;
}
document.addEventListener('click', () => { const m = document.getElementById('navmenu'); if (m) m.hidden = true; });
document.addEventListener('keydown', e => { if (e.key === 'Escape') { const m = document.getElementById('navmenu'); if (m) m.hidden = true; } });
let navFitPending = false;
function navRefit() {
  if (navFitPending) return;
  navFitPending = true;
  requestAnimationFrame(() => { navFitPending = false; navFit(); });
}
window.addEventListener('resize', navRefit);
if (typeof ResizeObserver === 'function') {
  const ro = new ResizeObserver(navRefit);
  ['nav', 'navnew', 'navmore'].forEach(id => { const el = document.getElementById(id); if (el) ro.observe(el); });
  navTabs.forEach(a => ro.observe(a));
}

// ---------- boot ----------
navFit();
render();
refreshBadges();
liveLoop();
