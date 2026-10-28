// life hub — laptop console: Sessions (the list, one chat, the composer).
// views.sessions — see ui.js for the shared pieces and app.js for the router.
'use strict';
// ================= sessions =================
// #/sessions/<thread>/<ask-id> opens the thread AT that ask. Clicking a card
// used to drop the reader at the bottom of a hundred-message chat with
// nothing saying which card was their turn.
async function drawSessions(view, rest) {
  // NO SESSION OPEN = THE EMPTY CHAT, never a blank "nothing" page.
  // `#/sessions` draws the list and, beside it, a chat with nothing in it yet
  // and the composer ready: clicking Sessions from Spend means you can start
  // typing.
  // The `#/sessions/new` route went with the "Pick a session" pane; an old
  // link to it lands here.
  // `#/sessions/all` is not a session: it is every session, ended ones
  // included, drawn in the chat's pane (drawAllSessions) — where Recent went.
  const all = rest[0] === 'all';
  const id = rest[0] === 'new' || all ? '' : (rest[0] || '');
  const focus = rest[1] || '';
  view.innerHTML = `
    <div class="pane list ${id || all ? 'hide-narrow' : ''}" id="list"></div>
    <div class="pane chat ${id || all ? '' : 'hide-narrow'}" id="chat"></div>`;
  await Promise.all([drawList(all ? ALL : id), id ? openThread(id, focus) : all ? drawAllSessions() : newSession()]);
  // The root change feed (app.js) repaints the list with the board it just
  // loaded; the open chat has its own thread-scoped feed (watchThread). The
  // composer holds a draft, so the list refreshes even while the owner types.
  const wake = id ? () => { watchThread(id); refreshThread(id).catch(() => {}); } : null;
  setRedraw(() => drawList(all ? ALL : id, true), view, { typing: true, wake });
  if (id) watchThread(id);
}
views.sessions = { draw: drawSessions, redraw: () => render() };

// Scroll the focused ask into view and flash it once. Runs after the chat is
// in the DOM; only on the first draw, so a poll five seconds later does not
// yank the reader back to it while they read something else.
function focusAsk(askID) {
  if (!askID) return false;
  const el = document.getElementById('ask-card-' + askID);
  if (!el) return false;
  if (el.tagName === 'DETAILS') el.open = true; // a folded card's row
  el.scrollIntoView({ block: 'center' });
  el.classList.add('flash');
  setTimeout(() => el.classList.remove('flash'), 1600);
  pinCard('ask-card-' + askID);
  return true;
}

// HOLD THE CARD WHERE IT LANDED while the chat settles. The card is centred
// the moment the chat is drawn, but the screenshots and run blocks above it
// fill in over the next few hundred ms and push it down — in a long chat,
// off the bottom. So for a few seconds every frame puts the card back at the
// height it was centred at, re-finding it by id (a redraw replaces the node).
// The owner's own wheel, touch, key or click on the chat lets go at once: it
// never fights them.
function pinCard(elID) {
  const box = document.getElementById('msgs'), el = document.getElementById(elID);
  if (!box || !el) return;
  const at = el.getBoundingClientRect().top - box.getBoundingClientRect().top;
  const until = performance.now() + 4000;
  let done = false;
  const stop = () => {
    done = true;
    for (const ev of ['wheel', 'touchstart', 'keydown', 'mousedown']) box.removeEventListener(ev, stop);
  };
  for (const ev of ['wheel', 'touchstart', 'keydown', 'mousedown']) box.addEventListener(ev, stop, { passive: true });
  const hold = () => {
    const b = document.getElementById('msgs'), c = document.getElementById(elID);
    if (done || !b || !c || b !== box || performance.now() > until) return stop();
    const drift = c.getBoundingClientRect().top - b.getBoundingClientRect().top - at;
    if (Math.abs(drift) > 1) b.scrollTop += drift;
    requestAnimationFrame(hold);
  };
  requestAnimationFrame(hold);
}

async function drawList(activeID, reuse) {
  // Asks + approvals per session come from the board's `for_you`, for the
  // sessions list. Its "Your turn" group is the sessions these belong to, and
  // every card prints its own share, so N cards visibly adding up to the
  // badge is the whole point. `reuse`: the live loop loaded the board a
  // moment ago, so it is not asked for twice.
  const gen = route.gen;
  listSide = activeID;
  // `team`: the sessions with a role (team mode; `[]` when it is off, so the
  // grid under All sessions is not drawn).
  const [threads, b, team] = await Promise.all([
    get('/threads'), reuse && board ? board : loadBoard().catch(() => board || EMPTY_BOARD),
    get('/threads?persona=1').catch(() => []),
  ]);
  const list = document.getElementById('list');
  if (!list || gen !== route.gen) return;
  const by = id => b.for_you[id] || 0;
  // The nav oval hangs off Sessions now — this page IS Your turn (2026-09-02).
  setBadge('sessions', b.count);
  // The open chat's head moves with the same board its row does.
  if (chatState.id) paintChatPills((threads || []).find(t => t.id === chatState.id));
  // A card is the session's NAME, what it needs from the owner, what it is
  // doing and which model it runs on — never the last reply. A dead turn's
  // error still shows: that is a fact about the session, not a description.
  // Tokens stay off the card (they are inside the session; the card shows
  // dollars).
  const bundles = {};
  for (const x of b.sessions || []) bundles[x.id] = x;
  // A SESSION ANOTHER ONE STARTED IS A ROW INSIDE THAT ONE'S CARD: a thread
  // whose `parent_id` is filed on this list is a cell on its parent's card,
  // like an open card: its title, its state, its own card's tint and verb
  // when it has one, three rows and then "and N more".
  // The hub's `home` names the card: the top of the chain, so a run started
  // by a run started by a session is a cell on that session's card.
  const home = b.home || {};
  const nested = t => !!home[t.id];
  const kids = {};
  for (const t of threads) if (nested(t) && sectionOf(b, t)) (kids[home[t.id]] ||= []).push(t);
  const subCell = k => {
    const c = sessionCards(bundles[k.id])[0];
    const href = `#/sessions/${encodeURIComponent(k.id)}${b.first[k.id] ? '/' + encodeURIComponent(b.first[k.id]) : ''}`;
    const live = k.status === 'running';
    return `<a class="ask mini sub ${c ? c.tint : 'quiet'}" href="${href}">
      <div class="t"><span class="verb">${esc(c ? c.verb : live ? 'running' : 'idle')}</span>${live ? '<span class="sess-dot"></span>' : ''}${mdInline(k.title || k.id, false)}</div></a>`;
  };
  const subCells = t => {
    const list = kids[t.id] || [];
    if (!list.length) return '';
    const key = 'subs ' + t.id, shown = moreOpen.has(key) ? list.length : SUB_CELLS;
    return list.slice(0, shown).map(subCell).join('')
      + (list.length > SUB_CELLS ? `<a class="small muted more-cards" href="#" onclick="event.preventDefault();toggleMoreCards('${esc(key)}')">${shown > SUB_CELLS ? 'Show less' : `and ${list.length - SUB_CELLS} more`}</a>` : '');
  };
  const group = (label, list, count) => !list.length ? '' :
    `<h3>${label}${count ? ` <span class="muted">· ${count}</span>` : ''}</h3>` + list.map(t => card(t)).join('');
  const card = t => {
      const n = by(t.id);
      // EVERY OPEN CARD IS ITS OWN CELL: the board's bundle for this
      // session, drawn one cell per card. The one-card todo/detail below is
      // only the fallback for a board that counts cards it did not bundle.
      // A ROW DRAWS TWO, THEN SAYS HOW MANY MORE. The to-dos are the
      // owner's dated steps — the
      // calendar's, uncounted — so the bundle carries them now (`steps`) and
      // the row stands for what the chat draws (board `open`).
      const cards = sessionCards(bundles[t.id]);
      const open = Math.max((b.open || {})[t.id] || 0, cards.length);
      const more = open - Math.min(cards.length, ROW_CARDS);
      const shown = moreOpen.has(t.id) ? cards.length : ROW_CARDS;
      // The todo line's colour is the first cards pill's: blue to read, teal
      // to install, red when something is blocked on the owner. A session
      // wears one pill per class (red, blue, teal in that order — an install
      // is called out separately) and the hub names the row's card in
      // the same order, so the first coloured pill is the todo's own class.
      const cardsPill = ((b.pills || {})[t.id] || []).find(p => TODO_COLOR[p.tone]);
      const tone = TODO_COLOR[cardsPill ? cardsPill.tone : 'needs'];
      const todo = n && !cards.length ? (b.todo || {})[t.id] || '' : '';
      // …and what the card actually SAYS, under it. The Your turn page is
      // gone and this is the one thing it had that a session row did not.
      // The hub caps it at 400 chars; .wrap3 cuts
      // it at three lines, so a long ask never pushes the next card off
      // screen. mdPreview is the same flattening a reply preview gets: the
      // ask's "- " bullets become "• " clauses on one running line.
      const detail = todo ? mdPreview((b.detail || {})[t.id] || '') : '';
      return sessionCardHTML(t, {
        active: t.id === activeID,
        live: ((b.pills || {})[t.id] || []).some(p => p.tone === 'running'),
        // Land on the card (approval or ask), not at the foot of the chat:
        // the board's `first` — the approval card, else the first ask.
        first: b.first[t.id] || '',
        cells: cards.slice(0, shown).map(c => miniCardHTML(t.id, c)).join('')
          + (cards.length > ROW_CARDS ? `<a class="small muted more-cards" href="#" onclick="event.preventDefault();toggleMoreCards('${esc(t.id)}')">${shown > ROW_CARDS ? 'Show less' : `and ${more} more`}</a>`
            : cards.length && more > 0 ? `<a class="small muted more-cards" href="#/sessions/${encodeURIComponent(t.id)}">and ${more} more</a>` : '')
          + (todo ? `<div class="small wrap2" style="margin-top:6px;color:var(--${tone})">→ ${mdInline(todo, false)}</div>` : '')
          + (detail ? `<div class="small muted wrap3" style="margin-top:2px">${detail}</div>` : '')
          + subCells(t),
        // The cells and the blue dot say what the count and RUNNING pills
        // did; only the live audio pills stay (speaking, waiting).
        pills: audioPills(b, t),
      });
  };
  // A SESSION ON THIS LIST IS WORKING OR WAITING ON THE OWNER — Your turn or Working
  // — and the hub files it (board `section`) and words each heading (board
  // `headings`: "3 in 2 sessions · 1 working"). The phone draws the same
  // list, so the ladder (running → Working; any card open → Your turn, reads
  // and installs mixed in — no separate To read heading)
  // lives once, in internal/attention, not here and in ThreadsView.swift.
  // Everything that ended is behind the one line at the foot — the phone's
  // "All sessions" row, in its words; no Recent group.
  // No "+ New session" button in this column — it is on the toolbar, on
  // every page.
  paint(list, `
    ${(b.headings || NO_HEADINGS).map(h => {
      const rows = threads.filter(t => sectionOf(b, t) === h.key && !nested(t));
      return group(h.label, h.show ? rows.slice(0, h.show) : rows, rows.length ? h.count : 0);
    }).join('')}
    ${threads.length ? `<a class="all-link${activeID === ALL ? ' on' : ''}" href="#/sessions/all"><span>All sessions</span><span class="muted">${threads.length}</span></a>`
      : emptyHTML('No sessions yet.')}
    ${(team || []).length ? `<div class="foot-links team">${team.map(t =>
      `<a class="all-link person${t.id === activeID ? ' on' : ''}" href="#/sessions/${encodeURIComponent(t.id)}"><span><strong>${esc(t.title)}</strong><span class="muted">${esc(t.role || '')}</span></span><span class="count">${by(t.id)}</span></a>`).join('')}</div>` : ''}`);
}

// The heading a session is filed under: the board's word for it, else (a
// session newer than the last board) running → Working; anything else has no
// heading and is only under All sessions.
const NO_HEADINGS = [{ key: 'working', label: 'Working', count: '' }];
const sectionOf = (b, t) => (b.section || {})[t.id] || (t.status === 'running' ? 'working' : '');
const ALL = ' all'; // drawList's activeID while #/sessions/all is open (no thread id has a space)

// ONE SESSION'S CARD on the list: whole titles, and the cards and what they
// mean readable without clicking into the chat. The NAME has the card's full width and
// wraps — the pills that shared its line and cut it to "Generated puzzle c…"
// are in the foot now, beside the model and the dollars. Between the two sit
// the session's open cards, EACH ITS OWN CELL (miniCardHTML) in the chat
// card's own tint — red blocked on the owner, blue to read, teal to install — so
// two things waiting read as two things, each opening the chat AT that card.
// The box is a <div> because a link cannot hold links; a click anywhere on it
// that is not one of them opens the session at its first card.
//
// THE NAME AND THE ONE THING IT NEEDS FROM THE OWNER: no model, dollars or
// tool-call row, since the chat's head carries those. A Your turn card is the
// name over its first cell ("and N more" under it); a Working card is one
// line, a blue dot, the name and when it last moved. Only the live audio pills
// stay (speaking, waiting): the fact a cell cannot carry.
function sessionCardHTML(t, o) {
  // The blue dot is the board's "running" pill, which a chain top wears for
  // the runs under it (attention.Live), so a session whose helpers or child
  // runs are at work reads as active before a message is sent to it.
  const live = t.status === 'running' || !!o.live;
  const failed = !live && t.last_message_kind === 'error' && !isPauseText(t.last_message);
  const href = `#/sessions/${encodeURIComponent(t.id)}${o.first ? '/' + encodeURIComponent(o.first) : ''}`;
  const bare = !o.cells;
  return `
    <div class="card click sess${bare ? ' sess-line' : ''}${o.active ? ' on' : ''}" onclick="if(!event.target.closest('a'))location.hash='${href}'">
      <div class="sess-h">${live ? '<span class="sess-dot"></span>' : ''}<a class="sess-t${t.unread > 0 ? ' unread' : ''}" href="${href}">${mdInline(t.title || t.id, false)}</a>${bare ? `<span class="small muted sess-ago">${ago(t.last_message_at || t.updated_at)}</span>` : ''}</div>
      ${o.pills ? `<div class="small sess-foot">${o.pills}</div>` : ''}
      ${failed ? `<div class="small wrap2 err mt6">${failedPreview(t.last_message)}</div>` : ''}
      ${o.cells || ''}
    </div>`;
}

// A session's open cards in the board's own order: approvals (the session is
// stopped on them), then its asks as the hub sorted them. Each is the chat
// card's tint and mark (askHTML / actionHTML in ui.js), so the cell here and
// the card in the chat read as one object.
function sessionCards(bundle) {
  if (!bundle) return [];
  const acts = (bundle.actions || []).map(a => ({ id: a.id, title: a.title, tint: '', mark: '🔴', verb: 'approve' }));
  // …then the owner's dated steps that sit in this chat (`steps`, the
  // calendar's). An answered ask is never here: the board drops it.
  const asks = (bundle.asks || []).concat(bundle.steps || []).map(a => {
    const k = a.kind, paused = k === 'error' && !!a.resumes_at;
    return {
      id: a.id, title: a.title, verb: a.verb || 'for you',
      tint: k === 'read' ? 'read' : k === 'install' ? 'install' : paused ? 'paused' : k === 'error' ? 'err-card' : '',
      mark: k === 'read' ? '🔵' : k === 'install' ? '📲' : paused ? PAUSE_MARK : k === 'error' ? '⚠︎' : '🔴',
    };
  });
  return acts.concat(asks);
}
// How many of a session's open cards its row draws; the rest is "and N more"
// (one, like the desktop's `deskCards`).
const ROW_CARDS = 1;
// …and how many of its sub-sessions, the rest "and N more".
const SUB_CELLS = 3;
// "AND N MORE" OPENS THE REST IN PLACE: the row's older cards drop in under
// the newest one, each its own cell; "Show less" folds them. Kept per session
// across the list's repaints.
const moreOpen = new Set();
let listSide = '';
function toggleMoreCards(id) {
  if (!moreOpen.delete(id)) moreOpen.add(id);
  drawList(listSide, true);
}

// One open card as a cell on its session's card: the chat's `.ask` box, small.
// The title is whole and alone - no words under it; the chat holds the rest.
function miniCardHTML(threadID, c) {
  return `<a class="ask mini ${c.tint}" href="#/sessions/${encodeURIComponent(threadID)}/${encodeURIComponent(c.id)}">
      <div class="t"><span class="verb">${esc(c.verb)}</span>${c.mark} ${mdInline(c.title, false)}</div></a>`;
}
// …and its SPEAKING pill, the one live fact a cell cannot carry.
const audioPills = (b, t) => livePillsHTML(((b.pills || {})[t.id] || []).filter(p => p.tone === 'speaking' || p.tone === 'waiting'));

// ---- All sessions (#/sessions/all) ----
// Where Recent went, for finding something done previously: every session,
// newest first, and a box that narrows it by name as you type. It draws in
// the chat's pane, so the list of what is waiting stays beside it. A TABLE,
// fifty rows at a time (a grid of hundreds of cards looked bad, and twenty
// was too few to find anything by eye): one line per session — name, state,
// model, when, dollars — and the shared pager.
let allQuery = '';
const allPager = makePager('sessions', 50);
async function drawAllSessions() {
  const gen = route.gen;
  const [threads, b] = await Promise.all([get('/threads'), board || loadBoard().catch(() => EMPTY_BOARD)]);
  const view = document.getElementById('chat');
  if (!view || gen !== route.gen) return;
  chatState.id = null;
  view.innerHTML = `
    <div class="chat-head">
      <button class="sm narrow-only" onclick="showList()">←</button>
      <div class="grow" style="min-width:0"><strong>All sessions</strong> <span class="muted">· ${threads.length}</span></div>
      <input type="text" id="all-q" placeholder="Find a session…" value="${esc(allQuery)}" style="max-width:280px" autocomplete="off">
    </div>
    <div class="msgs"><div class="card all-table" id="all-table"></div></div>`;
  const paintAll = allPager.paint = () => {
    const el = document.getElementById('all-table');
    if (!el) return;
    const words = allQuery.toLowerCase().split(/\s+/).filter(Boolean);
    const rows = threads.filter(t => words.every(w => (t.title || t.id).toLowerCase().includes(w)));
    const shown = allPager.rows(rows);
    el.innerHTML = !rows.length ? emptyHTML('No session by that name.') : `
      <table>
        <thead><tr><th>Session</th><th></th><th>Model</th><th>Last</th><th class="n">Cost</th></tr></thead>
        <tbody>${shown.map(t => {
          const first = (b.first || {})[t.id] || '';
          const href = `#/sessions/${encodeURIComponent(t.id)}${first ? '/' + encodeURIComponent(first) : ''}`;
          return `<tr onclick="location.hash='${href}'">
            <td class="all-t"><a href="${href}"${t.unread > 0 ? ' class="unread"' : ''}>${mdInline(t.title || t.id, false)}</a>${t.schedule ? `<span class="small muted"> · ${esc(t.schedule_label || cadenceWords(t.schedule))}</span>` : ''}</td>
            <td class="all-p">${sessionPills(b, t)}</td>
            <td class="small muted">${esc(t.model_label || modelShort(t.model || ''))}</td>
            <td class="small muted">${ago(t.last_message_at || t.updated_at)}</td>
            <td class="n small">${t.cost_usd ? usd(t.cost_usd) : ''}</td></tr>`;
        }).join('')}</tbody>
      </table>
      ${allPager.html(rows.length, shown.length)}`;
  };
  const q = document.getElementById('all-q');
  // A new search starts from the first fifty again.
  q.oninput = () => { allQuery = q.value; allPager.reset(); paintAll(); };
  allPager.reset();
  paintAll();
  q.focus();
}

// The capsules a session card wears, in the hub's order and words (board
// `pills`, else the thread's own `pill`): "running" + "2 for you", "1 to read"
// + "1 to install", "done". The tone is the pill's class; `idle` has none.
const TODO_COLOR = { needs: 'red', read: 'accent', install: 'teal' };
const PILL_CLASS = { needs: 'needs', read: 'read', install: 'install', running: 'running', done: 'done', waiting: 'waiting' };
// The pills that move on their own — running, speaking, waiting to speak —
// are LIVE_PILL in ui.js (shared with the calendar's rows since 2026-09-26).
// The hub marks the session for as long as the line takes to say; the list
// repaints as it starts and ends.
function sessionPills(b, t) {
  const pills = (b && b.pills || {})[t.id] || (t.pill ? [t.pill] : null);
  if (!pills) return statusPill(t.status);
  return pills.map(p => LIVE_PILL[p.tone] ? LIVE_PILL[p.tone](p.word)
    : pill(p.word, PILL_CLASS[p.tone])).join('');
}

// The open chat's head: the same pills, but the live ones are the controls.
// Hovered, "running" reads Stop and stops the turn; "speaking" / "waiting to
// speak" read Stop speaking and hush the line — the card stays (headHush).
const HEAD_ACT = {
  running: ['Stop', "threadAct('stop')", 'Stop'],
  speaking: ['Stop', 'headHush()', 'Stop speaking'],
  waiting: ['Stop', 'headHush()', 'Stop speaking'],
};
function headPills(b, t) {
  const pills = (b && b.pills || {})[t.id] || (t.pill ? [t.pill] : null)
    || (t.status === 'running' ? [{ tone: 'running', word: 'running' }] : null);
  if (!pills) return statusPill(t.status);
  return pills.map(p => {
    const act = HEAD_ACT[p.tone];
    if (!act) return pill(p.word, PILL_CLASS[p.tone]);
    const dot = p.tone === 'waiting' ? '<span class="dot"></span>' : '<span class="dot pulse"></span>';
    return `<button class="pill ${p.tone} pill-act" onclick="${act[1]}" title="${esc(act[2])}"><span class="w">${dot}${esc(p.word)}</span><span class="h"><span class="dot"></span>${esc(act[0])}</span></button>`;
  }).join('');
}
// Stop what this session is saying: the hub's "speaking" mark ends, and every
// open card of it whose line is still queued is hushed (POST /voice/hush).
// Nothing is dismissed.
function headHush() {
  const id = chatState.id;
  post('/voice', { thread_id: id, secs: 0 }).catch(() => {});
  for (const card of chatState.waitingCards || []) post('/voice/hush', { card }).catch(() => {});
  toast('stopped speaking');
  setTimeout(() => refreshThread(id, true).catch(() => {}), 1200);
}

// The open chat's head wears the same pills as its row, and they move on
// their own — "waiting to speak" → "speaking" → nothing — without a message,
// a card or a status change, so the head is repainted IN PLACE by every
// refresh: the chat's own (refreshThread, whose full redraw is gated on the
// structure changing) and the list's (drawList, which holds the board the
// live loop just loaded, while the chat's `board` may be a beat older).
// Otherwise the head keeps the pills of its last full redraw — "waiting to
// speak" stuck in the head for as long as the chat stays open, while its row
// on the left has long gone quiet.
// A change of those pills also rereads the chat, so a card's own "Waiting to
// speak" moves with its row (refreshThread's askKey carries the mark).
function paintChatPills(t) {
  const el = document.getElementById('chat-pills');
  if (!el || !t || chatState.id !== t.id) return;
  const html = headPills(board, t);
  const was = chatState.pillsHTML;
  if (html === was && chatState.pillsFor === t.id) return;
  const moved = chatState.pillsFor === t.id;
  chatState.pillsHTML = html; chatState.pillsFor = t.id;
  el.innerHTML = html;
  if (moved) refreshThread(t.id).catch(() => {});
}

// The open chat's own change feed: one parked GET /changes?thread= at a time,
// so a streaming turn's steps land as they happen and a quiet chat costs one
// request every 25 s. It ends when the chat, the route or the tab goes away;
// coming back to the tab starts it again (the page's `wake`).
function watchThread(id) {
  const gen = route.gen;
  const token = chatState.watch = (chatState.watch || 0) + 1;
  return longPoll({
    query: '&thread=' + encodeURIComponent(id),
    alive: () => chatState.id === id && route.gen === gen && chatState.watch === token && !document.hidden,
    onChange: () => refreshThread(id).catch(() => {}),
    backoff: () => { const el = document.getElementById('chat'); return el && el.dataset.status === 'running' ? 3000 : 15000; },
    // The first answer is the baseline, and a change that landed between
    // the opening draw and it is already inside it — so it refreshes too.
    // Without that, a turn that ended ~1 s after the chat opened kept
    // "starting…" and a Stop button forever, and Stop said "thread is not
    // running".
    baseline: true,
  });
}

// A cadence in words: "weekly@Mon 09:00" → "weekly Mon 09:00".
function cadenceWords(s) { return (s || '').replace('@', ' '); }

// A new session takes files too: the phone can start a thread from a photo,
// so the laptop — where the screenshot usually is — has to as well. Same lane
// as the chat composer: upload as observations first, pass the blob refs.
// The page the open composer belongs to (places.js), or null when the owner
// did not come here from a page. Read again by createSession(), which puts it in words
// at the top of the first message.
let newPlace = null;
// What the empty transcript says: a plain greeting, not the route, hash and
// files that draw the page (placeBlock() in words). The place still goes with
// the message (createSession); the transcript just does not print it. Same
// two lines on the phone (NewThreadSheet.swift).
const HELLO = 'Ready when you are.';
const PLACE_RIDES_ALONG = 'The page you came from goes with your message.';
// A NEW SESSION IS AN EMPTY CHAT. Same head, same transcript pane, same
// composer box as an open session — the transcript just has nothing in it
// yet. The goal and the cadence are the session's to infer from the owner's
// words (DESIGN.md: "Goals are inferred by the agent… follow-through is the
// agent's call"), so there are no selects asking for them.
// It is also what `#/sessions` shows with no session open (drawSessions) —
// the one chat surface, empty until the owner types.
async function newSession() {
  const gen = route.gen;
  const view = document.getElementById('chat');
  if (!view || gen !== route.gen) return;
  chatState.id = null;
  composerReset('new');
  clearDraft('new');
  // Only a press of "+ New session" carries a place; landing on #/sessions any
  // other way (the nav tab, a bookmark, a back button) carries none, and must
  // not inherit the last one. That press is also the one time a narrow
  // window swaps the list for the chat — the same swap opening a session does.
  newPlace = pendingPlace; pendingPlace = null;
  if (newPlace) {
    const list = document.getElementById('list');
    if (list) list.classList.add('hide-narrow');
    view.classList.remove('hide-narrow');
  }
  view.innerHTML = `
    <div class="chat-head">
      <button class="sm narrow-only" onclick="showList()">←</button>
      <div class="grow" style="min-width:0">
        <div class="trunc"><strong>New session</strong></div>
        <div class="small muted trunc" id="new-start" hidden></div>
      </div>
    </div>
    <div class="msgs" id="msgs">
      <div class="hello">
        <div class="hello-line">${HELLO}</div>
        ${newPlace ? `<div class="small muted">${PLACE_RIDES_ALONG}</div>` : ''}
      </div>
    </div>
    <div class="composer-wrap">${composerHTML('new', newComposerSpec())}</div>`;
  wireComposer('new');
  wireDrop(document.getElementById('msgs'), 'new');
  const box = document.getElementById('c-new-draft');
  if (box) box.focus();
  // A picture of the page the button was pressed on, taken before the router
  // replaced it. Uploaded in the background: typing can go on while it lands.
  if (pendingShot) {
    const p = pendingShot; pendingShot = null;
    p.then(f => { if (f && document.getElementById('c-new-draft')) uploadFiles([f], 'new'); });
  }
  // "Starts on <model>" comes from /spend/quota, which reads every transcript
  // and, right after a hub restart, can take 5–25 s — the pane looked hung.
  // This pane used to wait on it before drawing anything — the box, the
  // hello, the snap's upload. Nothing waits on it now: the line lands when
  // the answer does, or never.
  get('/spend/quota').then(quota => {
    const line = document.getElementById('new-start');
    if (!line || gen !== route.gen || !quota || !quota.next_model) return;
    line.textContent = `Starts on ${modelShort(quota.next_model)}${quota.next_reason ? ' — ' + quota.next_reason : ''}`;
    line.hidden = false;
  }).catch(() => {});
}

// A narrow window shows one pane at a time; the empty chat's ← is the way
// back to the list there (a wide window shows both, and hides the button).
function showList() {
  const list = document.getElementById('list'), chat = document.getElementById('chat');
  if (list) list.classList.remove('hide-narrow');
  if (chat) chat.classList.add('hide-narrow');
}

// The empty chat's box: no card to answer, no route (there is no "this
// session" yet, and "later" is a thing said in the message), one Send.
function newComposerSpec() {
  return {
    placeholder: () => 'What do you want done?',
    targets: [], when: false,
    sendLabel: 'Start session',
    send: createSession,
  };
}

// ---- new session from anywhere ----
// The toolbar button, on every route. It does what the phone's in-app snap
// does: whatever the owner is looking at goes with the message,
// so "this number is wrong" needs no explaining of which number.
let pendingShot = null, pendingPlace = null;
async function startSessionHere() {
  const btn = document.getElementById('navnew');
  // innerHTML, not textContent: the label is "+ New" plus a span the narrow
  // bar hides (app.css .tight), and it has to come back whole.
  const label = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.textContent = 'Snapping…'; }
  // Where the owner is, in words, read at the same instant as the picture and
  // sent as TEXT above their message (places.js): a screenshot alone made the
  // session guess which file drew the thing in question.
  // Independent of the snap on purpose — the picture is best-effort, this is
  // four lines that always land.
  pendingPlace = placeNow();
  // Capture BEFORE navigating — one frame later this page is gone.
  pendingShot = snapPage().catch(e => { console.warn('snapshot failed', e); return null; });
  pendingShot.finally(() => { if (btn) { btn.disabled = false; btn.innerHTML = label; } });
  // The empty chat is `#/sessions` itself; already there (no session open)
  // means a fresh box and a fresh shot, since the hash would not change.
  const here = location.hash.replace(/^#\/?/, '').replace(/\/+$/, '');
  if (here === 'sessions' || here === '') await newSession();
  else location.hash = '#/sessions';
}

// A picture of the console, taken by the console. There is no camera on a
// laptop tab and `getDisplayMedia` would put an OS picker in front of every
// click, so the page draws ITSELF: the live DOM is cloned into an
// `<svg><foreignObject>`, the stylesheet is inlined (a <link> never loads
// inside an SVG-as-image, and neither does an <img src>, so same-origin
// pictures are refetched as data URLs), and the result is rasterised on a
// canvas at 2×. Best effort by design — a session that starts without the
// picture is still a session, so every failure path returns null.
async function snapPage() {
  // Named for the page being photographed, read now — by the time the canvas
  // has the pixels the router has already moved on to the composer.
  const route = (location.hash.replace(/^#\/?/, '') || 'sessions').replace(/[^a-z0-9]+/gi, '-');
  const w = Math.max(320, Math.min(document.documentElement.clientWidth || 1400, 2000));
  const h = Math.max(240, Math.min(document.documentElement.clientHeight || 900, 2000));
  const clone = document.documentElement.cloneNode(true);
  clone.setAttribute('xmlns', 'http://www.w3.org/1999/xhtml');

  // Scroll offsets do not survive a clone: a list scrolled halfway down would
  // photograph from its top, which is not what is on screen. Pair the two
  // trees (same order, both static) and slide each scrolled box's contents.
  const orig = document.documentElement.querySelectorAll('*');
  const copies = clone.querySelectorAll('*');
  const pairs = [];
  for (let i = 0; i < orig.length && i < copies.length; i++) pairs.push([orig[i], copies[i]]);

  // A picture keeps the box it has on screen whether or not it loads in the
  // copy — a thumbnail that collapses to nothing moves everything under it.
  for (const [o, c] of pairs) {
    if (o.tagName !== 'IMG') continue;
    const r = o.getBoundingClientRect();
    if (r.width && r.height) { c.style.width = r.width + 'px'; c.style.height = r.height + 'px'; }
  }

  for (const [o, c] of pairs) {
    if (!o.scrollTop && !o.scrollLeft) continue;
    const shift = document.createElement('div');
    let top = -o.scrollTop;
    // The copy never lays out exactly like the page, and `-scrollTop` adds up
    // every difference above the fold: a long chat sat 60,000px down came out
    // as an EMPTY pane, its last bubbles a screen above the frame. So a block box drops the rows that are off screen and is
    // placed by its first visible row, measured on the live page — nothing
    // above the frame is left to drift, and the SVG is a fraction of the size.
    const kids = [...o.children], ckids = [...c.children];
    if (o.scrollTop && getComputedStyle(o).display === 'block' && kids.length === ckids.length) {
      const box = o.getBoundingClientRect();
      let first = -1, firstTop = 0;
      kids.forEach((k, j) => {
        const r = k.getBoundingClientRect();
        if (!r.height && !r.width) return;
        if (r.bottom < box.top || r.top > box.bottom) ckids[j].remove();
        else if (first < 0) { first = j; firstTop = r.top; }
      });
      if (first >= 0) {
        const cs = getComputedStyle(o);
        const natural = o.clientTop + (parseFloat(cs.paddingTop) || 0) + (parseFloat(getComputedStyle(kids[first]).marginTop) || 0);
        top = firstTop - box.top - natural;
        // flow-root: the row's own margin stays inside, so `natural` is exact.
        shift.style.display = 'flow-root';
      }
    }
    shift.style.marginTop = top + 'px';
    shift.style.marginLeft = -o.scrollLeft + 'px';
    while (c.firstChild) shift.appendChild(c.firstChild);
    c.appendChild(shift);
    c.style.overflow = 'hidden';
  }
  clone.querySelectorAll('script, link, .lightbox, #toast').forEach(n => n.remove());

  // Same-origin pictures (chips, chat attachments) as data URLs; anything
  // that will not come back is drawn as an empty box of the same size rather
  // than a broken icon — removing it would move the rows under it.
  const BLANK = 'data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==';
  await Promise.all([...clone.querySelectorAll('img')].map(async (im, n) => {
    const src = im.getAttribute('src') || '';
    if (src.startsWith('data:')) return;
    try {
      if (n >= 24) throw new Error('past the cap');
      const r = await fetch(src, { credentials: 'same-origin' });
      if (!r.ok) throw new Error('HTTP ' + r.status);
      const b = await r.blob();
      im.setAttribute('src', await new Promise((res, rej) => {
        const fr = new FileReader(); fr.onload = () => res(fr.result); fr.onerror = rej; fr.readAsDataURL(b);
      }));
    } catch { im.setAttribute('src', BLANK); im.removeAttribute('srcset'); }
  }));

  const css = [...document.styleSheets].map(s => {
    try { return [...s.cssRules].map(r => r.cssText).join('\n'); } catch { return ''; }
  }).join('\n');
  const head = clone.querySelector('head') || clone;
  const style = document.createElement('style');
  style.textContent = css;
  head.appendChild(style);

  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}">` +
    `<foreignObject x="0" y="0" width="${w}" height="${h}">` +
    new XMLSerializer().serializeToString(clone) + '</foreignObject></svg>';
  const img = new Image();
  await new Promise((res, rej) => {
    img.onload = res;
    img.onerror = () => rej(new Error('the page would not render into an image'));
    img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
  });
  const scale = 2;
  const canvas = document.createElement('canvas');
  canvas.width = w * scale; canvas.height = h * scale;
  const ctx = canvas.getContext('2d');
  ctx.scale(scale, scale);
  // The body's own background is painted by the CSS above, but a transparent
  // PNG on a dark viewer would swallow the text.
  ctx.fillStyle = getComputedStyle(document.body).backgroundColor || '#fff';
  ctx.fillRect(0, 0, w, h);
  ctx.drawImage(img, 0, 0, w, h);
  const blob = await new Promise(res => canvas.toBlob(res, 'image/png'));
  if (!blob) return null;
  return new File([blob], `console-${route}.png`, { type: 'image/png' });
}

// The composer's `send` for the empty chat (composer.js checked the words,
// the pictures and that every upload landed). The route, the URL and the
// files that draw it go in ABOVE what the owner typed, so the session can grep
// for the thing in the picture instead of guessing. Then the chat IS that
// session: the router opens it, and the message is its first bubble.
async function createSession(p) {
  const prompt = newPlace ? placeBlock(newPlace) + '\n\n' + p.text : p.text;
  // via: the hub stamps the message with the minute and the trail query, so
  // the session pulls what the owner was doing from the log (never pasted here).
  const t = await post('/threads', { prompt, attachments: p.refs, via: 'console' });
  location.hash = '#/sessions/' + encodeURIComponent(t.id);
}

// ---- one session ----
// `open` remembers which activity blocks and which steps are expanded across
// re-renders: the chat is re-drawn from innerHTML on every change, so any
// state living in the DOM (a <details> the user opened) would snap shut every
// few seconds while a run streams.
// focus = an ask id from the URL to scroll to and flash on the first draw.
// focusAct = that card read on its own when it is an action (see refreshThread).
// send = where the composer's next message goes (prompts engine phase 3):
// this session or a new one, now or at a time — and `re`, the card it
// answers, when Respond armed one (see armReply). It lives here rather than
// in the DOM for the same reason as `open` — the chat redraws every 5s.
// recs = this session's recommendations as last drawn (their cards arm the
// composer by id, and re-reading a rec from the hub for that is a round trip).
// cards = every ask and approval of this session by ref ("ask:<id>"): a
// reply line names an approval by it, and an armed card is redrawn from it.
// sendingSince/sendingAfter = a message on its way INTO this chat (Send,
// now): when it left, and the newest message id held then; the owner's
// message landing after it ends the count.
// thread/running = the session as last read, so a Send can repaint the
// working line at once; liveSince = when the turn in flight was started, for
// its "starting · N s" until the first tool call.
// costs = a turn's dollars by the ref of the first card it raised.
// full = the whole conversation is drawn (the toggle, chatFullHTML); false,
// the default every time a chat opens, draws only the cards still open.
// liveRun = the run id of the turn in flight; chain = its tool chain, loaded
// only while open (a click on the working line's count, chainHTML).
const newChain = () => ({ open: false, run: '', evs: [], first: 0, last: 0, loaded: false, all: false, busy: false, me: false, subs: {} });
const chatState = { id: null, lastMsgID: 0, lastAsks: '', lastActs: '', lastPrompts: '', lastRecs: '', recs: [], cards: {}, actIDs: '', open: new Map(), focus: '', focusAct: undefined, send: { target: 'this', when: 'now', at: '', re: null }, sendingSince: 0, sendingAfter: 0, thread: null, running: false, liveSince: 0, liveRun: '', chain: newChain(), costs: {}, full: false };
// Attachment drafts by box: 'chat', 'new', and one 'rec:<id>' per note box
// on the Recs page (the rec box takes a pasted or dropped screenshot like the
// composer).
// draftThread names the session an upload is filed against, for boxes that
// are not the chat. Both live in composer.js now — every box on the console
// keeps its pictures the same way.
// A card answered from OUTSIDE its session (the Your-turn stack): Respond
// routes to the session and leaves the ask here for openThread to pick up.
let armedReply = null;

async function openThread(id, focus) {
  chatState.id = id; chatState.lastMsgID = 0; chatState.lastActs = ''; chatState.open = new Map();
  chatState.sendingSince = 0; chatState.liveSince = 0;
  chatState.focus = focus || ''; chatState.focusAct = undefined; chatState.actIDs = ''; chatState.lastPrompts = ''; chatState.lastRecs = ''; chatState.recs = [];
  chatState.lastAsks = ''; chatState.cards = {}; chatState.full = false; chatState.chain = newChain(); chatState.liveRun = '';
  // The chat's composer state IS chatState.send (composer.js keeps it across
  // the 5s redraws); a new session starts it empty.
  chatState.send = composerReset('chat');
  // An ask carried over from another page (openRespond), or a ready-made
  // strip entry (openApprove: a calendar row's Approve/Deny).
  if (armedReply && armedReply.thread_id === id) chatState.send.re = armedReply.re;
  armedReply = null;
  clearDraft('chat');
  const el = document.getElementById('chat');
  el.innerHTML = `<div class="chat-head"><span class="muted">Loading…</span></div><div class="msgs" id="msgs"></div>`;
  await refreshThread(id, true);
  // Opening a chat reads it, as on the phone: its title stops being bold.
  post(`/threads/${id}/read`).catch(() => {});
}

// Conversational order, not timestamp order — same rule as the app
// (ThreadDetail.ordered): a message the owner sends mid-run is stored before that
// run's reply but is really the next turn. Each turn (one run id) draws as one
// piece - starter → cards → reply - and turns follow in the order they
// STARTED (the ts of the run's earliest message), a key that never moves.
// Keying a run by its latest message would put a turn with no
// reply yet above the previous turn whose reply landed a second later.
function orderedMsgs(msgs) {
  const runStart = {};
  for (const m of msgs) if (m.run_id) runStart[m.run_id] = Math.min(runStart[m.run_id] || Infinity, +new Date(m.ts));
  const key = m => (m.run_id && runStart[m.run_id]) || +new Date(m.ts);
  return msgs.slice().sort((a, b) => key(a) - key(b) || +new Date(a.ts) - +new Date(b.ts) || a.id - b.id);
}

async function refreshThread(id, first) {
  if (chatState.id !== id) return;
  const gen = route.gen;
  const [t, msgs, asks, allActs, queued, recPage] = await Promise.all([
    get('/threads/' + id), get(`/threads/${id}/messages?limit=200`),
    get(`/asks?state=all&thread=${id}`).catch(() => []),
    // Every state: a closed proposal folds to its row where it was.
    get(`/actions?state=&thread=${encodeURIComponent(id)}&limit=500`).catch(() => []),
    // This session's own future: what it will be told, and when — by the owner or
    // by itself (a self-prompt checking back on a step). Phase 3 replaced the
    // single "next run" line with the whole queue.
    get(`/prompts?state=queued&thread=${id}`).catch(() => []),
    // The recs this session filed, each with the reply it was filed in
    // (message_id): they are cells of the chat too.
    get(`/recs?status=all&thread=${id}`).catch(() => ({ recs: [] })),
  ]);
  const recs = (recPage && recPage.recs) || [];
  chatState.recs = recs;
  const el = document.getElementById('chat');
  if (!el || chatState.id !== id || gen !== route.gen) return;
  // This session's approvals, drawn as cards in the conversation like its
  // asks (see actionHTML): pending ones with their buttons, decided and
  // dismissed ones folded to their row. Deciding one changes its state
  // without a new message, so it is part of "did the structure change".
  const acts = (allActs || []).filter(a => a.thread_id === id);
  // The deep-linked card (#/sessions/<thread>/<id>) is read on its own, once
  // per change of the list: GET /actions/{id} is the one read that carries
  // events[] (the audit trail, inside the card) and it finds an action the
  // list no longer has (reached from a calendar row), so the chat still shows
  // it. A focus that is an ask id 404s here, and that is the end of it.
  const ids = acts.map(a => a.id).join(',');
  if (ids !== chatState.actIDs) { chatState.actIDs = ids; chatState.focusAct = undefined; }
  if (chatState.focus && chatState.focusAct === undefined) {
    chatState.focusAct = await get('/actions/' + encodeURIComponent(chatState.focus)).catch(() => null);
  }
  const fa = chatState.focusAct;
  if (fa && fa.thread_id === id) {
    const i = acts.findIndex(a => a.id === fa.id);
    if (i >= 0) acts[i] = fa; else acts.push(fa);
  }
  const actKey = acts.map(a => a.id + ':' + (a.state || '')).join(',');
  chatState.cards = {};
  for (const a of (asks || [])) chatState.cards['ask:' + a.id] = a;
  for (const a of acts) chatState.cards['action:' + a.id] = a;

  const msgsEl = document.getElementById('msgs');
  const anchor = chatAnchor(msgsEl);
  const ordered = orderedMsgs(msgs);
  const newest = msgs.length ? Math.max(...msgs.map(m => m.id)) : 0;
  const running = t.status === 'running';
  chatState.thread = t; chatState.running = running;
  // The turn in flight = the newest starter whose reply has not landed: its
  // ts is where "starting · N s" counts from.
  const replied = new Set(ordered.filter(m => m.role === 'claude' || m.kind === 'error').map(m => m.run_id).filter(Boolean));
  const live = running ? ordered.filter(m => m.run_id && m.role !== 'claude' && !replied.has(m.run_id)).pop() : null;
  chatState.liveSince = live ? Date.parse(live.ts) : 0;
  chatState.liveRun = (live && live.run_id) || '';
  // The owner's message landed: the "sending · N s" count is over (a Send
  // that never lands stops counting after five minutes rather than forever).
  if (chatState.sendingSince && (msgs.some(m => m.role === 'owner' && m.id > chatState.sendingAfter) || Date.now() - chatState.sendingSince > 300000)) {
    chatState.sendingSince = 0;
  }
  const promptKey = (queued || []).map(p => p.id).join(',');
  // A card raised mid-turn changes the chat without a new message: it must
  // redraw the moment the hub has it, not when the turn ends.
  // …and so does its "Waiting to speak" mark, which flips with no change of
  // state.
  const askKey = (asks || []).map(a => a.id + ':' + a.state + ':' + (a.message_id || 0) + (a.waiting_to_speak ? ':w' : '')).join(',');
  chatState.waitingCards = (asks || []).filter(a => a.state === 'open' && a.waiting_to_speak).map(a => a.id);
  // A rec changes without a message when the owner decides it (from the Recs page
  // or this card), and moves under its reply when that lands.
  const recKey = recs.map(r => r.id + ':' + r.status + ':' + (r.message_id || 0) + ':' + (r.updated_at || '')).join(',');
  const structural = first || newest !== chatState.lastMsgID || el.dataset.status !== t.status
    || actKey !== chatState.lastActs || promptKey !== chatState.lastPrompts || recKey !== chatState.lastRecs
    || askKey !== chatState.lastAsks;
  chatState.lastMsgID = newest;
  chatState.lastAsks = askKey;
  chatState.lastActs = actKey;
  chatState.lastPrompts = promptKey;
  chatState.queued = queued || [];
  chatState.lastRecs = recKey;
  el.dataset.status = t.status;

  // Nothing new but the turn's count, clock and dollars: repaint those in
  // place, so the composer, its draft and the scroll position survive.
  if (!structural) {
    paintChatPills(t);
    paintWorking(running, t);
    loadChain();
    return;
  }

  const askByMsg = {}, askById = {}, tailAsks = [];
  // A dismissed card folds to one grey line where it was, with Reopen inside
  // (dismiss means "no reply now", but a reply may still come later).
  //
  // Everything else must land SOMEWHERE. A card with no message_id — raised
  // outside a run (the hub, a calendar item, `lifectl ask add` at a terminal),
  // or raised in a turn whose reply has not landed yet — used to key
  // askByMsg[undefined] and be drawn on no screen at all, so the session read
  // "1 TO READ" with no cell anywhere in the chat. Same order the
  // phone uses (ThreadDetail.placeAsks): its own message, else the last
  // message written before it, else the end of the chat.
  // Live = not closed (the hub's `closed`, store.AskStanding).
  const activeAsk = a => !a.closed;
  for (const a of (asks || [])) {
    askById[a.id] = a;
    let host = null;
    if (a.message_id && ordered.some(m => m.id === a.message_id)) host = a.message_id;
    else if (activeAsk(a) || a.folded === 'dismissed') {
      const at = +new Date(a.created_at);
      for (const m of ordered) if (+new Date(m.ts) <= at) host = m.id;
    }
    if (host) (askByMsg[host] ||= []).push(a);
    else if (activeAsk(a)) tailAsks.push(a); // newer than every message: the turn is still in flight
    // A resolved card with no home stays hidden: it is history, and it was
    // never drawn in this chat anyway.
  }
  // An approval has no message of its own: it goes under the last message
  // written before it was proposed (the turn that proposed it), or at the
  // end if it is newer than everything (the reply has not landed yet). One
  // OLDER than every loaded message is history and is not drawn unless it is
  // still waiting on a decision (or is the focused card): the chat loads a
  // newer window, and a decided row from days ago would hang under today's.
  const actByMsg = {}, tail = [];
  for (const a of acts) {
    const at = +new Date(a.created_at);
    let host = null;
    for (const m of ordered) if (+new Date(m.ts) <= at) host = m;
    if (host) (actByMsg[host.id] ||= []).push(a);
    else if (a.state === 'proposed' || !ordered.length || a.id === chatState.focus) tail.push(a);
  }
  // A rec sits under the reply that filed it — the hub names it
  // (message_id: the first reply after the rec was filed) — or at the end
  // while that reply is still being written. Every status is drawn: a
  // decided one is the row that says what the owner chose. A rec whose reply
  // is older than the newest loaded message (past the 200-message window)
  // is not drawn at the tail: only one with no reply yet, or a reply newer
  // than every loaded message, goes there.
  const recByMsg = {}, recById = {}, tailRecs = [];
  const msgIDs = new Set(ordered.map(m => m.id));
  const newestID = Math.max(0, ...msgIDs);
  for (const r of recs) {
    recById[r.id] = r;
    if (r.message_id && msgIDs.has(r.message_id)) (recByMsg[r.message_id] ||= []).push(r);
    else if (!r.message_id || r.message_id > newestID) tailRecs.push(r);
  }
  // A reply can land while the owner is mid-sentence; the composer keeps what
  // they have typed in its own state (composer.js), so the redraw restores it — an
  // approval's note too, since its card arms the composer (2026-09-17).
  chatState.costs = {};
  // Open cards only: a chat opens on what is still the owner's to act on (the
  // asks, approvals and recs still open) and the working line; every message
  // row and closed card is behind "Show full chat" at the top. A message in
  // flight stays while the session works on it. A deep link to a closed card
  // turns the full chat on, so the card is there to land on.
  if (focusNeedsFull(chatState.focus, askById, recById, acts)) chatState.full = true;
  const full = chatState.full;
  const liveIDs = new Set(running && !full ? ordered.filter(m => m.run_id && m.role !== 'claude' && !replied.has(m.run_id)).map(m => m.id) : []);
  let rows;
  if (full) {
    rows = ordered.map(m => msgHTML(m, askByMsg[m.id], actByMsg[m.id], recByMsg[m.id], askById, recById)).join('')
      + tailAsks.map(a => chatAskHTML(a)).join('') + tail.map(a => chatActionHTML(a)).join('') + tailRecs.map(r => chatRecHTML(r)).join('');
  } else {
    rows = ordered.map(m => {
      const [ka, kc, kr] = openCards(askByMsg[m.id], actByMsg[m.id], recByMsg[m.id]);
      if (liveIDs.has(m.id)) return msgHTML(m, ka, kc, kr, askById, recById);
      noteLeadCost(m, ka, kc, kr);
      return ka.map(a => chatAskHTML(a)).join('') + kc.map(a => chatActionHTML(a)).join('') + kr.map(r => chatRecHTML(r)).join('');
    }).join('');
    const [ta, tc, tr] = openCards(tailAsks, tail, tailRecs);
    rows += ta.map(a => chatAskHTML(a)).join('') + tc.map(a => chatActionHTML(a)).join('') + tr.map(r => chatRecHTML(r)).join('');
    if (!rows && !running && !chatState.sendingSince) rows = '<div class="small muted">Nothing open</div>';
  }
  el.innerHTML = `
    <div class="chat-head">
      <button class="sm narrow-only" onclick="showList()">←</button>
      <div class="grow" style="min-width:0">
        <div class="trunc"><strong>${mdInline(t.title || t.id)}</strong></div>
      </div>
      <div class="head-id">
        <span id="chat-pills">${headPills(board, t)}</span>${t.model ? `<span class="pill" title="${esc(t.model)}">${esc(t.model_label || modelShort(t.model))}</span>` : ''}
      </div>
      <div class="small muted trunc head-meta">${[
        t.schedule ? esc(t.schedule_label || cadenceWords(t.schedule)) : '',
        t.next_run_at ? 'next ' + esc(when(t.next_run_at)) : '',
        t.cost_usd ? `<span title="${costByModel(t)}">${usd(t.cost_usd)}</span>` : '',
        t.tokens ? `<span title="${t.tokens.toLocaleString('en-US')} tokens: ${(t.tokens_in || 0).toLocaleString('en-US')} in, ${(t.tokens_out || 0).toLocaleString('en-US')} out, ${(t.tokens_cache_read || 0).toLocaleString('en-US')} cache read, ${(t.tokens_cache_write || 0).toLocaleString('en-US')} cache write">${tokens(t.tokens)}</span>` : '',
      ].filter(Boolean).join(' · ')}</div>
    </div>
    <div class="msgs" id="msgs">
      ${chatFullHTML(full)}
      ${rows}
      <div class="working small muted" id="run-working" hidden></div>
      <div class="chain small" id="run-chain" hidden></div>
    </div>
    ${queuedHTML(queued)}
    <div class="composer-wrap">${composerHTML('chat', chatComposerSpec(running))}</div>`;

  paintWorking(running, t);
  paintChain();
  loadChain();
  const box = document.getElementById('msgs');
  // Opening a chat places the reader on the newest message; after that the
  // chat never scrolls for them — a redraw puts back what they were reading. `first`
  // is not "opening": every card click, Stop and cancel pass it to force a
  // full redraw, and they must all stay put. Opening = nothing drawn yet.
  if (!anchor || !anchor.id) box.scrollTop = box.scrollHeight; else restoreChatAnchor(box, anchor);
  // The deep-linked ask wins over "open on the newest", once.
  if (chatState.focus && focusAsk(chatState.focus)) chatState.focus = '';
  wireChat();
}

// The one button at the top of the chat: "Show full chat" over the open cards,
// "Open cards only" over the whole conversation. The whole chat is drawn on
// demand, never by default.
const chatFullHTML = full => `<button type="button" class="fold small chat-mode" onclick="toggleFullChat()">${full ? 'Open cards only' : 'Show full chat'}</button>`;
function toggleFullChat() {
  chatState.full = !chatState.full;
  refreshThread(chatState.id, true);
}
// The cards still to act on, of each kind: an ask not closed, an approval
// still open, a rec not closed.
const openCards = (asks, acts, recs) => [(asks || []).filter(a => !a.closed), (acts || []).filter(a => a.open), (recs || []).filter(r => !r.closed)];
// A deep link (#/sessions/<thread>/<id>) to a card the open-cards view does
// not draw: a closed ask, a decided approval, a decided rec.
function focusNeedsFull(focus, askById, recById, acts) {
  if (!focus) return false;
  const ask = (askById || {})[focus], rec = (recById || {})[focus], act = (acts || []).find(a => a.id === focus);
  return !!((ask && ask.closed) || (rec && rec.closed) || (act && !act.open));
}

// Where the reader is, so a redraw puts it back: a button press (Dismiss) or
// a streaming tool call must never scroll the chat for them. Sticking to the
// bottom whenever the reader is within 80 px of it is wrong — that is where
// the card just answered sits. The
// anchor is the first element with an id still on screen (msg-N, ask-card-X;
// ids survive a redraw) and how far below the top edge it sits: a plain
// scrollTop would drift, because a row that opens above moves everything
// under it. No id on screen = the chat is not drawn yet (openThread's empty
// #msgs): that is the one placement on the newest message. The working line
// (run-working) is skipped: it is not part of the conversation.
function chatAnchor(msgsEl) {
  if (!msgsEl) return null;
  const top = msgsEl.getBoundingClientRect().top;
  for (const el of msgsEl.querySelectorAll('[id]')) {
    if (/^run-/.test(el.id)) continue;
    const r = el.getBoundingClientRect();
    if (r.bottom > top) return { id: el.id, offset: r.top - top };
  }
  return { id: '', offset: msgsEl.scrollTop };
}
function restoreChatAnchor(msgsEl, a) {
  if (!msgsEl || !a || !a.id) return;
  const el = document.getElementById(a.id);
  if (!el) return;
  msgsEl.scrollTop += el.getBoundingClientRect().top - msgsEl.getBoundingClientRect().top - a.offset;
}

// What an owner message answered, and the pick that rode with it: "↩ Got it ·
// <card>". Without this line the chat showed their words alone, and the chip
// looked like it did nothing.
// The label is the hub's (m.replies[].label, store.ReplyLabel): an ask's is
// the card's own button word, a rec's the Recs page's — a decision made THERE
// lands here as "↩ Accepted · <rec>" over their note, and one made on the card
// reads the same; the chat is the one record of both. One ↩ line per card the
// message answered (m.replies, 2026-09-17).
function replyLineHTML(m, askById, recById) {
  const list = Array.isArray(m.replies) && m.replies.length ? m.replies : [{ ref: m.in_reply_to || '', outcome: m.outcome || '' }];
  return list.map(r => replyOneHTML(m.thread_id, r.ref || '', r.label || r.outcome || '', askById, recById)).join('');
}
function replyOneHTML(threadID, ref, label, askById, recById) {
  if (!ref) return '';
  const [type, id] = ref.split(':');
  if (type === 'ask') {
    const a = (askById || {})[id];
    const title = a ? mdInline(a.title, false) : esc(id);
    return `<div class="re small muted">↩ <b>${esc(label)}</b> · <a href="#/sessions/${esc(threadID)}/${esc(id)}" onclick="focusAsk('${esc(id)}');return false">${title}</a></div>`;
  }
  if (type === 'rec') {
    const r = (recById || {})[id];
    const title = r ? mdInline(r.title, false) : esc(id);
    return `<div class="re small muted">↩ <b>${esc(label)}</b> · 💡 <a href="#/sessions/${esc(threadID)}/${esc(id)}" onclick="focusAsk('${esc(id)}');return false">${title}</a></div>`;
  }
  if (type === 'action') {
    // The proposal's title when the chat holds it (still open, or the
    // deep-linked one); the id otherwise — the link reads the card either way.
    const a = chatState.cards['action:' + id];
    const title = a ? mdInline(a.title, false) : esc(id);
    return `<div class="re small muted">↩ <b>${esc(label)}</b> · <a href="#/sessions/${esc(threadID)}/${esc(id)}" onclick="focusAsk('${esc(id)}');return false">${title}</a></div>`;
  }
  if (type === 'cal' || type === 'step') {
    // The step's name, not its id, so a session started from a row says
    // what it is about. One GET per item, cached; the line fills in when it
    // lands. A `step:<item>.<n>` is one row of that item: its own name.
    const [item, n] = calRefItem(ref);
    const known = calTitle(ref);
    if (calItemCache[item] === undefined) {
      calItemCache[item] = null;
      get('/calendar/' + encodeURIComponent(item)).then(i => {
        calItemCache[item] = i || {};
        for (const el of document.querySelectorAll('[data-calref]')) if (calRefItem(el.dataset.calref)[0] === item) el.innerHTML = mdInline(calTitle(el.dataset.calref) || item, false);
      }).catch(() => { calItemCache[item] = {}; });
    }
    const href = refHref('cal:' + item) || '#/calendar';
    return `<div class="re small muted">↩ <b>${esc(label)}</b> · 📅 <a href="${href}" data-calref="${esc(ref)}">${known ? mdInline(known, false) : esc(n ? `step ${n}` : id)}</a></div>`;
  }
  return `<div class="re small muted">↩ <b>${esc(label)}</b> · ${esc(type)} ${esc(id)}</div>`;
}
// Item by id, from /calendar/{id}: null while the GET is out, {} if it failed.
const calItemCache = {};
// cal:<item> → [item, 0]; step:<item>.<n> → [item, n].
function calRefItem(ref) {
  const [type, id = ''] = ref.split(':'), dot = id.lastIndexOf('.');
  return type === 'step' && dot > 0 ? [id.slice(0, dot), +id.slice(dot + 1)] : [id, 0];
}
// A cal:/step: ref's name: the item's title, or the step's own.
function calTitle(ref) {
  const [item, n] = calRefItem(ref), i = calItemCache[item];
  if (!i) return '';
  if (!n) return i.title || '';
  const s = (i.steps || []).find(x => x.n === n);
  return s ? s.title : '';
}

// Every message is a row (ui.js rowHTML): the owner's in grey, the rest
// white, one line each that opens in place. A session's reply draws nothing
// of its own: the cards the turn raised ARE the reply, and its dollars ride
// on the first of them. A reply that still carries text is a "Reply" row. An
// error is a red row; a session limit a paused one, its card under it saying
// when. The phone draws the same rows (ThreadDetail).
// A reply's dollars ride on the first card it raised (chatState.costs).
// Returns the cards' refs.
function noteLeadCost(m, asks, acts, recs) {
  const cards = [...(asks || []).map(a => 'ask:' + a.id), ...(acts || []).map(a => 'action:' + a.id), ...(recs || []).map(r => 'rec:' + r.id)];
  if (m.role === 'claude' && m.cost_usd && cards.length) chatState.costs[cards[0]] = m.cost_usd;
  return cards;
}
function msgHTML(m, asks, acts, recs, askById, recById) {
  const cards = noteLeadCost(m, asks, acts, recs);
  const html = (asks || []).map(a => chatAskHTML(a)).join('') + (acts || []).map(a => chatActionHTML(a)).join('') + (recs || []).map(r => chatRecHTML(r)).join('');
  const atts = (m.attachments || []).map(r => attachHTML(r)).join('');
  const text = m.shown ?? m.text ?? '';
  const row = (k, cls, title, body) => rowHTML({ id: 'msg-' + m.id, key: 'msg:' + m.id, opened: chatState.open.get('msg:' + m.id) === true,
    cls, k, title, when: dayWhen(m.ts) + (m.role === 'claude' && m.cost_usd && !cards.length ? ' · ' + usd(m.cost_usd) : ''),
    body: body + (atts ? `<div class="atts">${atts}</div>` : '') });
  const line = mdInline(firstLine(text), false);
  if (m.kind === 'error') return (isPauseText(text) ? row('⏸ Paused', 'paused', 'session limit', md(text)) : row('⚠ Error', 'error', line, md(text))) + html;
  if (m.kind === 'schedule') {
    // The agent set (or cleared) its check-in: when it comes back, then the
    // instructions it left itself.
    const cut = text.indexOf('\n\n');
    return row('⏰ Check-in', 'sched', esc(cut < 0 ? text : text.slice(0, cut)), cut < 0 ? '' : md(text.slice(cut + 2))) + html;
  }
  if (m.role === 'claude') return (text.trim() || atts ? row('Reply', '', line, md(text)) : '') + html;
  if (m.kind === 'checkin') return row('⏰ Check-in', 'sched', line, md(text)) + html;
  if (m.role === 'system') return row('Note', '', line, md(text)) + html;
  // The owner's message, or one another session or the hub sent in their
  // place: the card it answered leads the title ("↩ Got it · …"), whole in
  // the body. With no words of its own, the title is the card it answered.
  const a = m.author || 'owner';
  const k = a === 'owner' ? 'You' : a === 'hub' ? 'Hub' : esc(m.author_title || 'Session');
  const re = (m.replies || []).find(r => r.label || r.outcome) || (m.outcome ? { outcome: m.outcome, ref: m.in_reply_to } : null);
  const what = line || (re && esc(refTitle(re.ref || '', askById, recById))) || (atts ? '📎' : '');
  const title = re ? `↩ ${esc(re.label || re.outcome)}${what ? ' · ' + what : ''}` : what;
  const foot = a === 'owner' ? '' : sentByHTML(m);
  return row(k, 'me', title, replyLineHTML(m, askById, recById) + md(text) + (foot ? `<div class="small muted">${foot}</div>` : '')) + html;
}
const firstLine = s => String(s || '').split('\n').find(l => l.trim()) || '';
// The title of the card a ref names, when the chat holds it ('' otherwise).
function refTitle(ref, askById, recById) {
  const [type, id = ''] = ref.split(':');
  const c = type === 'ask' ? (askById || {})[id] : type === 'rec' ? (recById || {})[id] : type === 'action' ? chatState.cards[ref] : null;
  return c ? plainText(c.title) : type === 'cal' || type === 'step' ? calTitle(ref) : '';
}

// Who wrote an owner-side row the owner did not: the hub or a session. A
// session is a link to it. The phone draws the same words (ThreadDetail.sentBy).
function sentByHTML(m) {
  const a = m.author || '';
  if (!a) return '';
  let who;
  if (a === 'owner') who = 'you';
  else if (a === 'hub') who = 'the hub';
  else if (a.startsWith('claude:thread:')) {
    const id = a.slice('claude:thread:'.length);
    who = id === m.thread_id ? 'this session'
      : `<a href="#/sessions/${esc(id)}">${esc(m.author_title || id)}</a>`;
  } else who = esc(a);
  return `<span class="by">sent by ${who}</span>`;
}

// (attachHTML, the picture-or-file of a blob, lives in ui.js: md() draws one
// for a line that links a hub file.)

// ---------- lightbox ----------
// One overlay, as big as the window allows, click anywhere or Esc to close.
// Returns true so an <a> can cancel its own navigation with it.
function openImage(url, name) {
  closeImage();
  name = name || decodeURIComponent(String(url).split('/').pop() || '');
  const box = document.createElement('div');
  box.className = 'lightbox';
  box.id = 'lightbox';
  box.innerHTML = `<img src="${esc(url)}" alt="${esc(name || '')}">
    <div class="cap"><span>${esc(name || '')}</span>
      <a href="${esc(url)}" target="_blank" rel="noopener">open the file</a></div>`;
  box.addEventListener('click', e => { if (e.target.tagName !== 'A') closeImage(); });
  document.body.appendChild(box);
  return true;
}
function closeImage() { document.getElementById('lightbox')?.remove(); }
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeImage(); });

// THE working line, same on both surfaces (the phone spells it out in
// ThreadDetail.swift; keep them in step): while a turn runs the chat ends in
// "working · 23 tool calls · today 10:09 PM · $1.12", the clock being the
// turn's last output, so a hung turn shows an old time. The count opens the
// turn's tool chain under the line (chainHTML). A line left on a stopped session reads as one
// that never finished, so it shows only while the session runs.
//
// Before any of that, "sending · N s" from the moment a message left this
// box until it is in the chat (with "· working" or "· starting" behind it
// when the session is already at work), then "starting · N s" until the
// first tool call. The seconds tick on the console's own clock (tickSince),
// not the poll.
// startingSince: where "starting · N s" counts from. A message queued while
// the previous turn was still working is filed minutes before its turn opens;
// counting from its ts read "starting · 548 s" the moment the previous reply
// landed. The hub's turn_at is the turn's start until its first tool call, so
// the later of the two.
const startingSince = t => Math.max(chatState.liveSince, (t && t.turn_at && Date.parse(t.turn_at)) || 0);
function paintWorking(running, t) {
  const el = document.getElementById('run-working');
  if (!el) return;
  const since = chatState.sendingSince;
  el.hidden = !running && !since;
  if (el.hidden) return;
  const dot = '<span class="dot pulse"></span> ';
  if (since) el.innerHTML = `${dot}sending · ${sinceHTML(since)}${running ? ' · ' + esc(t && t.turn_tools ? 'working' : 'starting') : ''}`;
  else if (!(t && t.turn_tools) && chatState.liveSince) el.innerHTML = `${dot}starting · ${sinceHTML(startingSince(t))}`;
  else {
    // The count is the way into the tool chain (paintChain), and the
    // helpers the turn has running ride on the line, opened or not.
    const n = (t && t.turn_tools) || 0, c = chatState.chain;
    const helpers = chainLive(c) ? toolChain(c.evs, t && t.turn_tasks).helpers.length : taskHelpers(t);
    const words = [n ? callsWord(n) : '', helpersWords(helpers)].filter(Boolean).join(' · ');
    const link = words ? `<a href="#" class="chain-link${c.open ? ' on' : ''}" onclick="event.preventDefault();toggleChain()">${esc(words)}</a>` : '';
    el.innerHTML = dot + '<span>' + ['working', link, esc(turnFacts(t, false))].filter(Boolean).join(' · ') + '</span>';
  }
}

// THE TOOL CHAIN under the working line (ui.js toolChain). With no helper it
// is the turn's calls, newest last, the ones still waiting drawn darker.
// With helpers it is one row per worker: the session's own row ("<title> ·
// N tool calls") and one per helper, each opening its own calls on a second
// click; a helper opens once the hub serves its transcript
// (/threads/{id}/tasks/{task}/events), else it stays a plain "<description>
// · subagent" row. No dot inside the chain: the working line's is the one
// live mark. Only the last CHAIN_ROWS calls until the reader asks for the
// earlier ones. It lives exactly as long as the turn: when the turn ends,
// the working line and the chain go together and the chat is its open cards
// again.
const CHAIN_ROWS = 12;
function callsHTML(steps, all, total, earlierFn) {
  const calls = steps.filter(s => !s.agent);
  const shown = all ? calls : calls.slice(-CHAIN_ROWS);
  const earlier = Math.max(calls.length, total) - shown.length;
  const more = earlier > 0 ? `<a href="#" class="chain-more muted" onclick="event.preventDefault();${earlierFn}">${earlier} earlier</a>` : '';
  return more + shown.map(s => `<div class="chain-row${s.open ? ' live' : s.error ? ' err' : ''}">${esc(s.label)}</div>`).join('');
}
const callsWord = n => `${n} tool call${n === 1 ? '' : 's'}`;
// o: {name, me (the session's own calls open), subs: {[task id]: {ok, open, evs}}, tasks}.
function chainHTML(ch, all, total, o = {}) {
  const agents = ch.steps.filter(s => s.agent).length;
  const own = callsHTML(ch.steps, all, Math.max(0, total - agents), 'chainEarlier()');
  if (!ch.helpers.length) return own;
  const subs = o.subs || {}, tasks = o.tasks || [];
  const mine = Math.max(ch.steps.length - agents, total - agents);
  const row = (open, words, click) => click
    ? `<a href="#" class="chain-w${open ? ' on' : ''}" onclick="event.preventDefault();${click}"><span class="caret">›</span>${words}</a>`
    : `<div class="chain-w"><span class="caret"></span>${words}</div>`;
  let html = row(o.me, `${esc(o.name || 'Working')} · ${callsWord(mine)}`, 'toggleChainMe()');
  if (o.me) html += `<div class="chain-calls">${own}</div>`;
  for (const d of ch.helpers) {
    const task = tasks.find(k => k.type === 'local_agent' && k.description === d);
    const s = task && subs[task.id];
    if (s && s.ok) {
      const sc = toolChain(s.evs);
      html += row(s.open, `${esc(d)} · ${callsWord(sc.steps.filter(x => !x.agent).length)}`, `toggleChainSub('${esc(task.id)}')`);
      if (s.open) html += `<div class="chain-calls">${callsHTML(sc.steps, true, 0, '')}</div>`;
    } else html += row(false, `${esc(d)} · subagent`, '');
  }
  return html;
}
const chainLive = c => c.loaded && !!c.run;
function paintChain() {
  const el = document.getElementById('run-chain');
  if (!el) return;
  const c = chatState.chain, t = chatState.thread;
  el.hidden = !(chatState.running && c.open && chainLive(c));
  if (!el.hidden) el.innerHTML = chainHTML(toolChain(c.evs, t && t.turn_tasks), c.all, (t && t.turn_tools) || 0,
    { name: t && t.title, me: c.me, subs: c.subs, tasks: (t && t.turn_tasks) || [] });
}
function toggleChainMe() { chatState.chain.me = !chatState.chain.me; paintChain(); }
function toggleChainSub(id) {
  const s = chatState.chain.subs[id];
  if (s) { s.open = !s.open; paintChain(); }
}
// A helper's own steps, read from its transcript while the turn runs; a
// helper the hub cannot read (a 404) stays a plain row.
async function loadSubs() {
  const c = chatState.chain, id = chatState.id, t = chatState.thread;
  for (const k of ((t && t.turn_tasks) || []).filter(k => k.type === 'local_agent' && k.id)) {
    const s = c.subs[k.id] || (c.subs[k.id] = { ok: false, open: false, evs: [], last: 0, tried: false });
    if (s.tried && !s.ok) continue;
    s.tried = true;
    const page = await get(`/threads/${id}/tasks/${encodeURIComponent(k.id)}/events?` + (s.last ? `since=${s.last}&limit=500` : 'limit=500')).catch(() => null);
    if (chatState.chain !== c) return;
    if (!Array.isArray(page)) continue;
    s.ok = true;
    for (const e of page) { if (e.id > s.last) s.last = e.id; s.evs.push(e); }
  }
}
// The turn's events, read once and then only added to (since=), the hub's
// rule for a moving window (threads.Events).
// The turn is the newest event's run: a turn a helper's end woke has no
// message of its own, and an unanswered message hours back is not this
// turn. Only a message newer than every event (a send, before its first
// call) names the run instead.
async function loadChain() {
  const c = chatState.chain, id = chatState.id;
  if (!c.open || !chatState.running || c.busy) { paintChain(); return; }
  c.busy = true;
  try {
    const page = (await get(`/threads/${id}/events?` + (c.last ? `since=${c.last}&limit=500` : 'limit=300'))) || [];
    if (chatState.id !== id || chatState.chain !== c) return;
    const newest = page[page.length - 1];
    const run = chatState.liveRun && chatState.liveSince > (newest ? Date.parse(newest.ts) : 0) ? chatState.liveRun
      : newest ? newest.run_id : c.run;
    if (run !== c.run) Object.assign(c, { run, evs: [], first: 0, all: false, subs: {} });
    for (const e of page) {
      if (e.id > c.last) c.last = e.id;
      if (!c.first || e.id < c.first) c.first = e.id;
      if (e.run_id === run) c.evs.push(e);
    }
    c.loaded = true;
    await loadSubs();
  } catch { /* the next change tries again */ } finally { c.busy = false; }
  paintChain();
  paintWorking(chatState.running, chatState.thread);
}
function toggleChain() {
  const c = chatState.chain;
  c.open = !c.open;
  for (const s of Object.values(c.subs)) if (!s.ok) s.tried = false;
  paintWorking(chatState.running, chatState.thread);
  if (c.open) loadChain(); else paintChain();
}
// "N earlier": walk back (before=) to the start of the turn.
async function chainEarlier() {
  const c = chatState.chain, id = chatState.id, run = c.run;
  c.all = true;
  while (c.first > 1) {
    const page = await get(`/threads/${id}/events?before=${c.first}&limit=1000`).catch(() => null);
    if (!page || !page.length || chatState.chain !== c) break;
    c.first = Math.min(c.first, ...page.map(e => e.id));
    c.evs = page.filter(e => e.run_id === run).concat(c.evs);
    if (page.some(e => e.run_id !== run)) break;
  }
  paintChain();
}
const sinceHTML = since => `<span class="since" data-since="${since}">${Math.max(0, Math.floor((Date.now() - since) / 1000))} s</span>`;
// Every "N s" on the page, once a second; a sending line that should have
// gone (or come) is repainted here too.
function tickSince() {
  const el = document.getElementById('run-working');
  if (el && chatState.thread && !!chatState.sendingSince !== el.textContent.includes('sending')) paintWorking(chatState.running, chatState.thread);
  for (const s of document.querySelectorAll('[data-since]')) {
    s.textContent = Math.max(0, Math.floor((Date.now() - +s.dataset.since) / 1000)) + ' s';
  }
}
if (typeof document !== 'undefined') setInterval(tickSince, 1000);

// A folded card's row remembers the owner's click across the redraw (cardHTML).
const chatOpened = (ref, id) => chatState.open.get(ref + ':' + id) === true;
// The pick a card has on this chat's composer ('' = Reply), undefined when
// none: its row draws that one filled (ui.js outcomeBtns).
const chatArmed = (ref, id) => { const c = composerCards('chat').find(x => x.ref === ref && x.id === id); return c ? (c.outcome || '') : undefined; };
// chatState.costs = a turn's dollars by the ref of the first card it raised
// (msgHTML), so a card redrawn on its own keeps them.
const chatAskHTML = a => askHTML(a, chatOpened('ask', a.id), chatArmed('ask', a.id), chatState.costs['ask:' + a.id]);
const chatActionHTML = a => actionHTML(a, chatOpened('action', a.id), chatArmed('action', a.id), chatState.costs['action:' + a.id]);
const chatRecHTML = r => recHTML(r, chatOpened('rec', r.id), chatArmed('rec', r.id), chatState.costs['rec:' + r.id]);

// The cards' rows follow the bar: whenever the strip changes - a button armed,
// a chip switched, a x - every card that was or is on it is redrawn in place,
// so its filled button is the bar's pick. Only those cards: the rest are
// untouched.
let chatArmedWas = new Set();
function chatPaintArmed() {
  // A step's strip repaints the card its row is on (`card`).
  const now = new Set(composerCards('chat').map(c => c.card || c.ref + ':' + c.id));
  for (const ref of new Set([...chatArmedWas, ...now])) {
    const i = ref.indexOf(':'), kind = ref.slice(0, i), id = ref.slice(i + 1);
    const el = document.getElementById('ask-card-' + id);
    const c = kind === 'rec' ? chatState.recs.find(r => r.id === id) : chatState.cards[ref];
    if (!el || !c) continue;
    el.outerHTML = kind === 'rec' ? chatRecHTML(c) : kind === 'action' ? chatActionHTML(c) : chatAskHTML(c);
  }
  chatArmedWas = now;
}

// The chat head's Stop is its "running" pill, hovered (headPills); Check in
// and Archive are not on the head.
async function threadAct(what) {
  const id = chatState.id;
  const said = { stop: 'Stopped' }[what] || what;
  try { await post(`/threads/${id}/${what}`); toast(said); await refreshThread(id, true); }
  // A refusal means the page is behind the hub (Stop on a turn that already
  // ended): redraw so the buttons match the session.
  catch (e) { toast(e.message); refreshThread(id, true).catch(() => {}); }
}

// ---- this session's future (prompts engine phase 3, 2026-08-27) ----
// A session used to show one line, "next run". It can now be woken by several
// things — the owner's answer aimed at tomorrow morning, its own check-back
// on a step it started — so the queue is listed, with the cost said out loud, because each
// one re-reads the whole session.
// A standing row (one clock, 2026-08-28) is the session's check-in itself:
// `repeat` is its cadence, its pill reads "daily 09:00" like the session card,
// and its button says Turn off, because cancelling it IS the schedule going
// off (the hub posts the same green "cleared" card that Settings → off does).
// The row's words are the prompt's TITLE, not its text: a calendar wake's text is its title,
// a blank line, and the whole instruction, so the row printed the title twice and
// then a paragraph. Full text is the hover title; the words live on the item.
//
// The strip FOLDS, and shut is where it rests. The bar carries the
// count and WHEN THE NEXT ONE FIRES, and its words after that when they fit —
// .trunc, so a long title fades into the row rather than wrapping the bar.
// Rows are sorted by that fire time: the API hands prompts back newest-CREATED
// first, which printed Sep 28 above Sep 19 and would have left the bar's "next"
// disagreeing with the first row under it.
// The fold is remembered across a live redraw AND across page loads
// (localStorage) — a strip that sprang open on every redraw would be worse
// than no fold at all.
const promptWords = p => p.title || p.text || (p.repeat ? 'the default check-in' : '(no words)');
const queuedIsOpen = () => localStorage.getItem('queuedOpen') === '1';
// Open, the strip draws the NEXT FIVE and an "N more" button: two dozen wakes
// filled the screen. The button draws the rest for this visit (queuedAll,
// back to five on leaving the chat). The phone's ComingUpStrip folds the same
// way. The list no longer scrolls inside itself.
const QUEUED_WINDOW = 5;
let queuedAll = false;
onLeave.push(() => { queuedAll = false; });
function queuedMore() {
  queuedAll = true;
  const q = document.getElementById('queued');
  if (!q) return;
  q.outerHTML = queuedHTML(chatState.queued);
  wireQueued();
}
// The strip's fold outlives the redraw and the page (localStorage queuedOpen).
function wireQueued() {
  const q = document.getElementById('queued');
  if (q) q.addEventListener('toggle', () => localStorage.setItem('queuedOpen', q.open ? '1' : '0'));
}
// A wake's clock: `when`, with the year once it differs (a wake next
// November read "Nov 5 10:30 AM" at the foot of a list that ended in March).
const wakeWhen = (iso, now) => {
  if (!iso) return 'now';
  const d = new Date(iso); now = now || new Date();
  const t = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  if (d.toDateString() === now.toDateString()) return t;
  const day = d.toLocaleDateString([], d.getFullYear() === now.getFullYear()
    ? { month: 'short', day: 'numeric' } : { month: 'short', day: 'numeric', year: 'numeric' });
  return day + ' ' + t;
};
function queuedHTML(prompts) {
  const ps = (prompts || []).filter(p => p.state === 'queued')
    .sort((a, b) => (a.not_before || '').localeCompare(b.not_before || ''));
  if (!ps.length) return '';
  const mine = p => p.author === 'owner' ? 'you' : p.author === 'hub' ? 'the hub' : 'this session';
  const row = p => `<div class="row qrow">
      ${p.repeat ? `<span class="pill done" title="standing check-in">${esc(cadenceWords(p.repeat))}</span>` : ''}
      <span class="pill">${esc(p.repeat ? 'next ' : '')}${esc(wakeWhen(p.not_before))}</span>
      ${p.target === 'new' ? '<span class="pill">new session</span>' : ''}
      ${p.outcome ? pill(p.outcome) : ''}
      <span class="grow trunc small" title="${esc(p.text || '')}">${esc(promptWords(p))} <span class="muted">· from ${esc(mine(p))}</span></span>
      <button class="sm" onclick="cancelPrompt('${esc(p.id)}')">${p.repeat ? 'Turn off' : 'Cancel'}</button>
    </div>`;
  // A run is drawn once. One wake = its row, no fold; several = a fold whose
  // bar names the count and, shut, when the next one fires; open, the bar is
  // the count alone and the rows carry the rest (app.css hides the bar's pill
  // and words under [open]).
  if (ps.length === 1) return `<div class="queued one" id="queued">
    <div class="row qrow"><span class="hl small muted">Coming up</span>${row(ps[0])}</div>
  </div>`;
  const next = ps[0];
  const shown = queuedAll ? ps : ps.slice(0, QUEUED_WINDOW);
  const hidden = ps.length - shown.length;
  return `<details class="queued" id="queued"${queuedIsOpen() ? ' open' : ''}>
    <summary>
      <span class="hl small muted">Coming up · ${ps.length}</span>
      <span class="pill">next ${esc(wakeWhen(next.not_before))}</span>
      <span class="grow trunc small muted">${esc(promptWords(next))}</span>
    </summary>
    <div class="qlist">${shown.map(row).join('')}${hidden ? `<div class="row qrow"><button class="sm" onclick="queuedMore()">${hidden} more</button></div>` : ''}</div>
  </details>`;
}

async function cancelPrompt(id) {
  return act(() => post(`/prompts/${id}/cancel`), 'cancelled', () => refreshThread(chatState.id, true));
}

// ---- composer: answering a card (Respond, 2026-08-28) ----
// The box itself is composer.js — the same one the Recs page and the calendar
// draw — every prompt box has the same full feature set. What is here is only what a CHAT's box answers
// and where its Send posts.
//
// Respond used to open a <dialog> with its own thin text box: too thin, it
// covered the new-session bar, and it could not send figures. So
// Respond now ARMS THE COMPOSER instead: one short strip above the box names
// the card being answered and carries its outcome chips; the words, the
// attachments (paste, drop, Attach — they sit ABOVE the strip, so a pasted
// screenshot never lands between the card's name and the answer), and
// send-to / when are the ordinary composer's. Send posts one prompt
// referencing the card, exactly what the dialog posted. The strip lives in
// chatState.send.re — which IS the composer's own state — so the 5 s redraw
// keeps it, and × puts the composer back to plain messages.
//
// The chips are the HUB's words in the HUB's order (ask.outcomes, per kind,
// decisive first and "Reply" last — store/close.go AskOutcomes), the
// order the card's own row draws: a read card says "Read it", a decision
// "Decided", an access ask "Granted" — never a generic "I did this" on a
// finding that asked for nothing. A card with none (a crash) is words only.
// What each pick does, in words, under the chips — the composer draws this for
// every box, so an ask says it the same way a rec and a calendar step do. A
// read card has one pick and no way to keep it open: it closes whether or not
// the owner types a reply.
const askMeans = (outcomes, kind) => Object.fromEntries(outcomes.map(o =>
  [o.value, kind === 'read' ? 'the card closes when you send — your words go to the session'
    : o.value ? 'the card leaves your board when you send' : 'the card stays open — the agent has the ball']));
// `outcome` is the card button's pick (2026-09-18: each of the hub's words
// is a button on the card); undefined = no pick yet: "Reply" (value "")
// where the card offers it, wherever the hub lists it, else the first.
const replyOf = (a, outcome) => {
  const outcomes = Array.isArray(a.outcomes) && a.outcomes.length ? a.outcomes : [{ value: '', label: 'Reply' }];
  const start = outcomes.find(o => !o.value) || outcomes[0];
  return {
    ref: 'ask', id: a.id, title: a.title || '', kind: a.kind || '', outcomes, outcome: outcome === undefined ? start.value : outcome,
    label: a.kind === 'read' ? 'Replying to' : 'Answering', means: askMeans(outcomes, a.kind),
    // A read closes either way, so the strip is only the banner (no chip to
    // pick), and an empty Send asks "Are you sure?" first.
    bare: a.kind === 'read', confirmEmpty: a.kind === 'read',
    href: `#/sessions/${a.thread_id || chatState.id}/${a.id}`, focus: 'focusAsk',
  };
};

// Arming ADDS to the strip (composerArm): a read and a rec pressed one after
// the other are both on the bar, and one Send answers both.
function armReply(ask, outcome) {
  composerArm('chat', replyOf(ask, outcome));
  focusAsk(ask.id);
}

// One step of an item's card, staged as its own strip on the bar: Send names
// it `step:<item>.<n>` with the pick, the hub closes it then; nothing moves
// before.
function armStepReply(askID, n, outcome) {
  const a = chatState.cards['ask:' + askID];
  const s = a && (a.steps || []).find(x => x.n === n);
  if (!s) return;
  composerArm('chat', stepReplyOf(a, s, outcome));
  focusAsk(a.id);
}
function stepReplyOf(a, s, outcome) {
  const outcomes = [{ value: 'done', label: 'Did it' }, { value: 'wont', label: 'Skip' }, { value: '', label: 'Reply' }];
  return {
    ref: 'step', id: (a.cal_id || a.id) + '.' + s.n, card: 'ask:' + a.id, title: s.title || '', kind: 'step', label: 'Step',
    outcomes, outcome: outcome || '',
    means: { done: 'ticked off when you send', wont: 'skipped when you send', '': 'the step stays open, your words go to the session' },
    href: `#/sessions/${a.thread_id || chatState.id}/${a.id}`,
  };
}

// A rec card's three buttons arm the same composer, so a rec is answered from
// the chat without a trip to the Recs page. The chips are the hub's
// (rec.outcomes — the Recs page's own row, Later went 2026-09-09). Send posts
// to /recs/{id}/decide (or /reply for words alone) with deliver=source, so the
// hub records the decision and relays it here exactly as the Recs page would
// — see chatSend.
function armRecReply(id, outcome) {
  const r = chatState.recs.find(x => x.id === id);
  if (!r) return;
  composerArm('chat', recReplyOf(r, outcome));
  focusAsk(r.id);
}

// The card a rec is answered through — here and on the Recs page, one shape,
// so both surfaces show the same chips, the same day picker and the same
// sentence under them. `fixed` is the Recs page's copy: the box sits ON the
// rec, so its title and × are not repeated inside it.
function recReplyOf(r, outcome, fixed) {
  return {
    ref: 'rec', id: r.id, title: r.title || '', kind: 'rec', label: '💡 Rec',
    outcomes: r.outcomes || [], outcome: outcome || '', means: outcomeMeans(r.outcomes),
    fixed: !!fixed, href: `#/recs/${r.id}`,
  };
}

// outcomeMeans (what each pick does when sent) lives in composer.js, which
// draws the line and must load without this file (the calendar's boxes).

// An approval card's three buttons arm the same composer, like every other
// cell. Send posts one
// prompt naming action:<id> with the pick; the hub decides the row from it
// (an approved one starts running) and the session wakes with the frame —
// the same row /actions/{id}/approve writes, the same decider code it needs
// (chatSend asks for it once). The chips and their lines are the hub's
// (action.outcomes, each with its hint).
function armActionReply(id, outcome) {
  const a = chatState.cards['action:' + id];
  if (!a) return;
  composerArm('chat', actionReplyOf(a, outcome));
  focusAsk(a.id);
}
function actionReplyOf(a, outcome) {
  return {
    ref: 'action', id: a.id, title: a.title || '', kind: 'action', label: '🔴 Approval',
    outcomes: a.outcomes || [], outcome: outcome || '', means: outcomeMeans(a.outcomes),
    href: `#/sessions/${a.thread_id || chatState.id}/${a.id}`, focus: 'focusAsk',
  };
}
// A calendar row's Approve/Deny (views/calendar.js) opens the session at the
// card with that pick armed — the composer is there, not on the calendar.
async function openApprove(a, outcome) {
  if (!a.thread_id) return;
  // Re-read from the hub, as openRespond does: the row carries only the id,
  // the card's words (outcomes) are the hub's.
  const full = await get('/actions/' + encodeURIComponent(a.id)).catch(() => a);
  armedReply = { thread_id: a.thread_id, re: actionReplyOf(full, outcome) };
  location.hash = `#/sessions/${a.thread_id}/${a.id}`;
}

// ---- the chat's spec: what its box says, and where Send posts ----
function chatComposerSpec(running) {
  return {
    placeholder: st => {
      const re = st.re;
      if (re && st.also.length) return 'One message for all of them (optional)';
      if (re && re.ref === 'rec') return re.outcome ? 'Your note to the session (optional)' : 'Your line — it goes to the session, the rec stays open';
      if (re && re.ref === 'action') return re.outcome ? 'Your note to the session (optional)' : 'Your line — it goes to the session, the proposal stays open';
      if (re) return re.outcome ? 'Anything to add (optional)' : 'Your answer…';
      return running ? 'Steer it while it works…' : 'Message…';
    },
    // Everything typed here goes now, to this session — no "which session",
    // no "when". A new session is + New session.
    targets: [], when: false,
    // No `check`: the one rule a rec answer had was Later's date, and Later is
    // gone (2026-09-09). Anything else here is a prompt, which the hub queues
    // to the minute whichever session it names.
    send: chatSend,
    after: () => (chatState.id ? refreshThread(chatState.id, true) : null),
    // The cards' rows follow the strip (chatPaintArmed).
    armed: chatPaintArmed,
  };
}

// Three roads out of one box, by what is armed above it.
async function chatSend(p) {
  const re = p.re;
  if (re && p.also && p.also.length) {
    // Several cards, one message: ONE prompt naming every
    // card with its pick — the hub closes each by its own rule (a read
    // closes, done/wont claims the ask, accepted/declined is written on the
    // rec's ledger row exactly as /recs/{id}/decide would) and the session
    // gets one frame per card over the owner's words. Always now, this session.
    const replies = [re, ...p.also].map(r => ({ ref: r.ref + ':' + r.id, outcome: r.outcome || '' }));
    await withDecider(() => post('/prompts?via=web', { target: 'new-or:' + chatState.id, text: p.text, attachments: p.refs, replies }));
    toast(`sent — ${replies.length} cards`);
    refreshBadges();
    return;
  }
  if (re && re.ref === 'rec') {
    // A rec answered from its cell goes through the Recs page's own calls: the
    // hub records the decision and relays it into this session (or a new one)
    // as a prompt answering rec:<id>, so the chat shows the same "↩ Accepted ·
    // <rec>" line whichever surface was used. The screenshot rides IN the
    // decision, never as a message after it.
    const body = composerTiming({ by: 'owner', note: p.text, deliver: p.target === 'new' ? 'new' : 'source', attachments: p.refs }, p);
    let out;
    if (p.outcome) {
      body.status = p.outcome;
      out = await post(`/recs/${re.id}/decide`, body);
    } else {
      out = await post(`/recs/${re.id}/reply`, body);
    }
    if (out && out.delivery_error) toast(out.delivery_error);
    // The pick is accepted|declined (Later has no button any more).
    toast(out && out.delivered === 'new' ? 'sent to a new session' : p.outcome || 'sent');
    refreshBadges();
    if (out && out.session_id && out.session_id !== chatState.id) location.hash = '#/sessions/' + out.session_id;
    return;
  }
  if (!re && p.target === 'this' && p.when === 'now') {
    // "sending · N s" until the message is in the chat (refreshThread
    // clears it when a newer owner message lands).
    chatState.sendingSince = Date.now(); chatState.sendingAfter = chatState.lastMsgID;
    paintWorking(chatState.running, chatState.thread);
    try {
      await post(`/threads/${chatState.id}/messages`, { text: p.text, attachments: p.refs, via: 'console' });
    } catch (e) {
      chatState.sendingSince = 0;
      paintWorking(chatState.running, chatState.thread);
      throw e;
    }
    return;
  }
  // `new-or:<id>` and not the bare id: an ended session must not swallow words
  // meant for tomorrow — the hub starts a fresh one instead. An answer to a
  // card always goes this way: the reference and the outcome travel as data
  // (`in_reply_to`), and the outcome closes the card at POST time even when
  // the words are aimed at tomorrow. An approval (`action:`) with a pick
  // decides the row at POST time too, and the hub wants the decider code for
  // that — withDecider asks once and remembers it.
  const body = composerTiming({ target: p.target === 'new' ? 'new' : 'new-or:' + chatState.id, text: p.text, attachments: p.refs }, p);
  if (re) { body.in_reply_to = re.ref + ':' + re.id; body.outcome = p.outcome; if (p.target === 'new') body.title = re.title.slice(0, 80); }
  await withDecider(() => post('/prompts?via=web', body));
  toast(p.when !== 'now' ? 'queued' : p.target === 'new' ? 'sent to a new session' : re && re.ref === 'action' && p.outcome ? p.outcome : 'sent');
  refreshBadges();
}

// The card buttons that do NOT go through the composer: the silent closes
// (Dismiss; a read's Read) and their undo (Reopen). One POST, nothing
// relayed, nobody told, then the chat redraws with the card folded or back.
// `note` is the resolution written on an ask: the read card's white Read
// button sends the hub's own word for its one outcome ("Read it"), so the
// closed card says what the owner did and the hub folds it like a dismissed one.
async function cardMove(path, body, word) {
  return act(() => post(path, body), word, async () => { await refreshThread(chatState.id, true); refreshBadges(); });
}
const resolveAsk = (id, state, note) => cardMove(`/asks/${id}/resolve`, note ? { state, note } : { state }, note || 'ask ' + state);
// cardFold: THE Dismiss (back=false) and Reopen (back=true) of every chat
// card — one call, the endpoint per card kind in this one table (three
// hand-written pairs until review-primitives step 7). A rec's dismiss is the
// hub's `expired` with the note "dismissed" (store.RecDismissed).
const CARD_FOLD = {
  ask: (id, back) => [`/asks/${id}/resolve`, { state: back ? 'open' : 'dismissed' }, back ? 'ask open' : 'ask dismissed'],
  action: (id, back) => [`/actions/${id}/${back ? 'reopen' : 'dismiss'}?via=web`, {}, back ? 'reopened' : 'dismissed'],
  rec: (id, back) => [`/recs/${id}/decide`, back ? { status: 'proposed' } : { status: 'expired', note: 'dismissed' }, back ? 'reopened' : 'dismissed'],
};
const cardFold = (ref, id, back) => cardMove(...CARD_FOLD[ref](id, back));

// ---- the chat's own wiring ----
// The box itself is wireComposer (composer.js). What is only the chat's: the
// whole transcript is a drop zone too, and a run's <details> remember what the
// owner opened across the 5 s redraw.
function wireChat() {
  wireComposer('chat');
  wireQueued();
  const msgs = document.getElementById('msgs');
  if (!msgs) return;
  wireDrop(msgs, 'chat');
  // <details> toggles do not bubble; capture them so an opened row stays
  // open when the chat redraws. A row is shut by default (data-def), so only
  // a state that differs from the one rendered is remembered.
  msgs.addEventListener('toggle', e => {
    const el = e.target, k = el.dataset && el.dataset.k;
    if (!k) return;
    if (String(+el.open) === el.dataset.def) chatState.open.delete(k);
    else chatState.open.set(k, el.open);
  }, true);
  const draft = document.getElementById(cdom('chat') + '-draft');
  if (draft) draft.focus();
}

