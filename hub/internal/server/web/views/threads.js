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
  const [threads, b] = await Promise.all([
    get('/threads'), reuse && board ? board : loadBoard().catch(() => board || EMPTY_BOARD),
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
  const group = (label, list, count) => !list.length ? '' :
    `<h3>${label}${count ? ` <span class="muted">· ${count}</span>` : ''}</h3>` + list.map(t => {
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
        // Land on the card (approval or ask), not at the foot of the chat:
        // the board's `first` — the approval card, else the first ask.
        first: b.first[t.id] || '',
        cells: cards.slice(0, ROW_CARDS).map(c => miniCardHTML(t.id, c)).join('')
          + (cards.length && more > 0 ? `<a class="small muted more-cards" href="#/sessions/${encodeURIComponent(t.id)}/${encodeURIComponent(cards[Math.min(ROW_CARDS, cards.length - 1)].id)}">and ${more} more</a>` : '')
          + (todo ? `<div class="small wrap2" style="margin-top:6px;color:var(--${tone})">→ ${mdInline(todo, false)}</div>` : '')
          + (detail ? `<div class="small muted wrap3" style="margin-top:2px">${detail}</div>` : ''),
        // Every card drawn as a cell = the count pills would say it twice. A
        // running session keeps its RUNNING pill, in the
        // card's foot beside the model and the dollars.
        pills: n && cards.length - ((bundles[t.id] || {}).steps || []).length >= n ? runningOnly(b, t) : sessionPills(b, t),
      });
    }).join('');
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
      const rows = threads.filter(t => sectionOf(b, t) === h.key);
      return group(h.label, h.show ? rows.slice(0, h.show) : rows, rows.length ? h.count : 0);
    }).join('')}
    ${threads.length ? `<a class="all-link${activeID === ALL ? ' on' : ''}" href="#/sessions/all"><span>All sessions</span><span class="muted">${threads.length}</span></a>`
      : emptyHTML('No sessions yet.')}`);
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
function sessionCardHTML(t, o) {
  const failed = t.last_message_kind === 'error' && !isPauseText(t.last_message), running = t.status === 'running';
  const href = `#/sessions/${encodeURIComponent(t.id)}${o.first ? '/' + encodeURIComponent(o.first) : ''}`;
  return `
    <div class="card click sess${o.active ? ' on' : ''}" onclick="if(!event.target.closest('a'))location.hash='${href}'">
      <a class="sess-t${t.unread > 0 ? ' unread' : ''}" href="${href}">${mdInline(t.title || t.id, false)}</a>
      ${o.cells || ''}
      ${failed ? `<div class="small wrap2 err mt6">${failedPreview(t.last_message)}</div>` : ''}
      ${running && turnFacts(t, true, false) ? `<div class="sess-run small trunc">${esc(turnFacts(t, true, false))}</div>` : ''}
      <div class="small muted sess-foot">${o.pills || ''}${t.model ? `<span class="pill" title="${esc(t.model)}">${esc(t.model_label || modelShort(t.model))}</span>` : ''}<span>${ago(t.last_message_at || t.updated_at)}${t.schedule ? ' · ' + esc(t.schedule_label || cadenceWords(t.schedule)) : ''}${t.cost_usd ? ' · ' + usd(t.cost_usd) : ''}</span></div>
    </div>`;
}

// A session's open cards in the board's own order: approvals (the session is
// stopped on them), then its asks as the hub sorted them. Each is the chat
// card's tint and mark (askHTML / actionHTML in ui.js), so the cell here and
// the card in the chat read as one object.
function sessionCards(bundle) {
  if (!bundle) return [];
  const acts = (bundle.actions || []).map(a => ({ id: a.id, title: a.title, detail: a.detail, tint: '', mark: '🔴', verb: 'approve' }));
  // …then the owner's dated steps that sit in this chat (`steps`, the
  // calendar's). An answered ask is never here: the board drops it.
  const asks = (bundle.asks || []).concat(bundle.steps || []).map(a => {
    const k = a.kind, paused = k === 'error' && !!a.resumes_at;
    return {
      id: a.id, title: a.title, verb: a.verb || 'for you',
      tint: k === 'read' ? 'read' : k === 'install' ? 'install' : paused ? 'paused' : k === 'error' ? 'err-card' : '',
      mark: k === 'read' ? '🔵' : k === 'install' ? '📲' : paused ? PAUSE_MARK : k === 'error' ? '⚠︎' : '🔴',
      // An install's title is the whole instruction; an error's body is a log.
      detail: k === 'install' || k === 'error' ? '' : a.detail,
    };
  });
  return acts.concat(asks);
}
// How many of a session's open cards its row draws; the rest is "and N more".
const ROW_CARDS = 2;

// One open card as a cell on its session's card: the chat's `.ask` box, small.
// The title is whole; the body is the first three lines of what the card says.
// A card the hub minted from a reply has that reply's first line for a title
// AND as the first line of its body; here, three lines tall, it is said once.
function miniCardHTML(threadID, c) {
  const lines = (c.detail || '').trim().split('\n');
  const title = plainText(c.title);
  if (lines.length > 1 && title && plainText(lines[0]).startsWith(title)) lines.shift();
  const body = mdPreview(lines.join('\n').trim());
  return `<a class="ask mini ${c.tint}" href="#/sessions/${encodeURIComponent(threadID)}/${encodeURIComponent(c.id)}">
      <div class="t"><span class="verb">${esc(c.verb)}</span>${c.mark} ${mdInline(c.title, false)}</div>
      ${body ? `<div class="small muted wrap3">${body}</div>` : ''}</a>`;
}
// …and its SPEAKING pill, the one live fact a cell cannot carry (2026-09-25).
const runningOnly = (b, t) => livePillsHTML((b.pills || {})[t.id]);

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
// events = the steps loaded so far, by event id — a STORE, only ever added to
// (see loadEvents). steps = the hub's COUNT per message, which is what a run
// block's headline prints; a block's rows are fetched when it is opened
// (fetching = one is in flight, so a redraw does not ask twice).
// cards = every ask and approval of this session by ref ("ask:<id>"), for the
// ones drawn INSIDE a run block (steps.segments): drawActivity needs the
// objects and runs on the fast path too, where the message-level maps are not
// rebuilt.
const chatState = { id: null, lastMsgID: 0, lastEvKey: '', lastAsks: '', lastActs: '', lastPrompts: '', lastRecs: '', recs: [], cards: {}, actIDs: '', open: new Map(), focus: '', focusAct: undefined, send: { target: 'this', when: 'now', at: '', re: null }, events: new Map(), steps: new Map(), fetching: new Set() };
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
  chatState.id = id; chatState.lastMsgID = 0; chatState.lastEvKey = ''; chatState.lastActs = ''; chatState.open = new Map();
  chatState.events = new Map(); chatState.steps = new Map(); chatState.fetching = new Set();
  chatState.focus = focus || ''; chatState.focusAct = undefined; chatState.actIDs = ''; chatState.lastPrompts = ''; chatState.lastRecs = ''; chatState.recs = [];
  chatState.lastAsks = ''; chatState.cards = {};
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
// piece — starter → activity → reply — and turns follow in the order they
// STARTED (the ts of the run's earliest message), a key that never moves.
// Keying a run by its latest message would put a turn with no
// reply yet above the previous turn whose reply landed a second later.
function orderedMsgs(msgs) {
  const runStart = {};
  for (const m of msgs) if (m.run_id) runStart[m.run_id] = Math.min(runStart[m.run_id] || Infinity, +new Date(m.ts));
  const key = m => (m.run_id && runStart[m.run_id]) || +new Date(m.ts);
  return msgs.slice().sort((a, b) => key(a) - key(b) || +new Date(a.ts) - +new Date(b.ts) || a.id - b.id);
}

// Steps are a store the console only ever ADDS to, never a window it
// re-reads. The old version fetched "the newest 400" every refresh, so each
// new tool call pushed the oldest event out of the window and the block above
// a steering message counted DOWN — 193, 192, 191 — while the block under the
// message counted up.
//
// Counting what you happen to have fetched is the deeper bug, and it is gone:
// every headline is the HUB's count (GET /threads/{id}/steps, straight out of
// SQL), so a 5,000-step turn reads the same as a 5-step one. This store holds
// only what is DRAWN — the newest page, whatever streams in after it, and any
// block the owner opens (ensureBlock) — so opening a session is one small read
// however long its history is.
const evPage = 400;
async function loadEvents(id) {
  const store = chatState.events;
  const take = evs => { for (const e of (evs || [])) store.set(e.id, e); return evs || []; };
  if (!store.size) {
    take(await get(`/threads/${id}/events?limit=${evPage}`).catch(() => []));
    return held();
  }
  const newest = Math.max(...store.keys());
  take(await get(`/threads/${id}/events?since=${newest}&limit=1000`).catch(() => []));
  // A shell call's English summary is filled in by Haiku a few seconds after
  // the call itself, so recent tool_use rows get a second look.
  const stillComing = Date.now() - 120000;
  const blank = [...store.values()].filter(e => e.kind === 'tool_use' && !e.summary && +new Date(e.ts) > stillComing).map(e => e.id);
  if (blank.length) take((await get(`/threads/${id}/events?ids=${blank.slice(-40).join(',')}`).catch(() => [])).filter(e => e.summary));
  return held();
}
const held = () => [...chatState.events.values()].sort((a, b) => a.id - b.id);

// One folded block, opened: read exactly its steps (the hub gave the block's
// first and last event id) and redraw. Anything already held is skipped, so
// the live block — which streams in through `since` — never re-reads.
async function ensureBlock(id, msgID) {
  const c = chatState.steps.get(msgID);
  const key = msgID + ':' + (c ? c.last_id : 0);
  if (!c || !c.steps || chatState.fetching.has(key)) return false;
  let have = 0;
  for (const e of chatState.events.values()) if (e.id >= c.first_id && e.id <= c.last_id) have++;
  if (have >= c.steps) return false;
  chatState.fetching.add(key);
  try {
    let since = c.first_id - 1;
    while (since < c.last_id && chatState.id === id) {
      const page = await get(`/threads/${id}/events?since=${since}&before=${c.last_id + 1}&limit=2000`).catch(() => []);
      if (!page.length) break;
      for (const e of page) chatState.events.set(e.id, e);
      since = page[page.length - 1].id;
    }
  } finally { chatState.fetching.delete(key); }
  return true;
}

async function refreshThread(id, first) {
  if (chatState.id !== id) return;
  const gen = route.gen;
  const [t, msgs, steps, asks, allActs, queued, recPage] = await Promise.all([
    get('/threads/' + id), get(`/threads/${id}/messages?limit=200`),
    // The counts every run block prints — the hub's, not "what we fetched".
    get(`/threads/${id}/steps`).catch(() => []),
    get(`/asks?state=all&thread=${id}`).catch(() => []),
    // Every state: a dismissed proposal folds where it was (2026-09-18), a
    // decided one stays as its grey card (below).
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
  chatState.steps = new Map((steps || []).map(s => [s.message_id, s]));
  const events = await loadEvents(id);
  const el = document.getElementById('chat');
  if (!el || chatState.id !== id || gen !== route.gen) return;
  // This session's approvals, drawn as cards in the conversation like its
  // asks (see actionHTML): pending ones with their buttons, dismissed ones
  // folded, decided ones grey with who decided — a decided card used to leave
  // the chat, so an approved proposal was only the owner's "↩ Approved · <id>"
  // reply. Deciding one
  // changes its state without a new message, so it is part of "did the
  // structure change".
  const acts = (allActs || []).filter(a => a.thread_id === id);
  // The deep-linked card (#/sessions/<thread>/<id>) is read on its own, once
  // per change of the list: GET /actions/{id} is the one read that carries
  // events[] — Phase 2's audit trail, drawn under the card — and it finds an
  // action the proposed list no longer has (decided already, reached from a
  // calendar row), so the chat still shows it, closed, with who decided it
  // and when. A focus that is an ask id 404s here, and that is the end of it.
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
  // Cards placed IN the tool chain, at the step the agent raised them. The hub cuts each run block at its cards
  // (GET /threads/{id}/steps → segments), so the split and every segment's
  // count are its, not "what we happen to hold". drawActivity draws them;
  // the message-level maps below leave them alone.
  chatState.cards = {};
  for (const a of (asks || [])) chatState.cards['ask:' + a.id] = a;
  for (const a of acts) chatState.cards['action:' + a.id] = a;
  const inChain = new Set();
  for (const s of (steps || [])) for (const g of (s.segments || [])) if (g.ref && chatState.cards[g.ref]) inChain.add(g.ref);

  const msgsEl = document.getElementById('msgs');
  const anchor = chatAnchor(msgsEl);
  const ordered = orderedMsgs(msgs);
  const newest = msgs.length ? Math.max(...msgs.map(m => m.id)) : 0;
  // "Did the activity change" = the hub's counts moved, or a step we hold
  // gained its English summary (the in-place redraw below picks both up).
  const evKey = (steps || []).map(s => s.message_id + '/' + s.steps + '/' + (s.segments || []).map(g => g.ref + '@' + g.steps).join('+')).join(',')
    + ':' + events.length + ':' + events.filter(e => e.summary).length;
  const running = t.status === 'running';
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
  chatState.lastRecs = recKey;
  el.dataset.status = t.status;

  // Steps stream in far faster than messages; when only they moved, swap the
  // activity blocks in place so the composer, its draft and the scroll
  // position survive (before, a live run's steps never refreshed at all).
  if (!structural) {
    paintChatPills(t);
    if (evKey !== chatState.lastEvKey) {
      chatState.lastEvKey = evKey;
      drawActivity(ordered, events, running, t);
      restoreChatAnchor(msgsEl, anchor);
    } else {
      paintWorking(running, chatState.liveBlock, t); // the clock and dollars move between steps
    }
    return;
  }
  chatState.lastEvKey = evKey;

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
    if (inChain.has('ask:' + a.id)) continue;
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
  // end if it is newer than everything (the reply has not landed yet).
  const actByMsg = {}, tail = [];
  for (const a of acts) {
    if (inChain.has('action:' + a.id)) continue; // drawn where it was proposed
    const at = +new Date(a.created_at);
    let host = null;
    for (const m of ordered) if (+new Date(m.ts) <= at) host = m;
    if (host) (actByMsg[host.id] ||= []).push(a); else tail.push(a);
  }
  // A rec sits under the reply that filed it — the hub names it
  // (message_id: the first reply after the rec was filed) — or at the end
  // while that reply is still being written. Every status is drawn: a
  // decided one is the grey line that says what the owner chose.
  const recByMsg = {}, recById = {}, tailRecs = [];
  const msgIDs = new Set(ordered.map(m => m.id));
  const lastTurn = lastTurnID(ordered);
  for (const r of recs) {
    recById[r.id] = r;
    if (r.message_id && msgIDs.has(r.message_id)) (recByMsg[r.message_id] ||= []).push(r); else tailRecs.push(r);
  }
  // A reply can land while the owner is mid-sentence; the composer keeps what
  // they have typed in its own state (composer.js), so the redraw restores it — an
  // approval's note too, since its card arms the composer (2026-09-17).
  el.innerHTML = `
    <div class="chat-head">
      <a href="#/sessions" class="sm btn" style="text-decoration:none">←</a>
      <div class="grow head-id" style="min-width:0">
        <div class="trunc"><strong>${mdInline(t.title || t.id)}</strong></div>
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
      ${ordered.map(m => msgHTML(m, askByMsg[m.id], actByMsg[m.id], askById, recByMsg[m.id], recById, m.id === lastTurn) + runSlot(m)).join('')}
      ${tailAsks.map(chatAskHTML).join('')}
      ${tail.map(chatActionHTML).join('')}
      ${tailRecs.map(chatRecHTML).join('')}
      <div id="run-orphan"></div>
      <div class="working small muted" id="run-working" hidden></div>
    </div>
    ${queuedHTML(queued)}
    <div class="composer-wrap">${composerHTML('chat', chatComposerSpec(running))}</div>`;

  drawActivity(ordered, events, running, t);
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

// Where the reader is, so a redraw puts it back: a button press (Dismiss) or
// a streaming tool call must never scroll the chat for them. Sticking to the
// bottom whenever the reader is within 80 px of it is wrong — that is where
// the card just answered sits. The
// anchor is the first element with an id still on screen (msg-N, ask-card-X,
// run-…; ids survive a redraw) and how far below the top edge it sits: a
// plain scrollTop would drift, because a card that folds or a run block that
// grows above moves everything under it. No id on screen = the chat is not
// drawn yet (openThread's empty #msgs): that is the one placement on the
// newest message. The run slots' own ids (run-…, run-orphan, run-working)
// are skipped — they are empty until drawActivity fills them, so on the
// first draw one of them would count as "already drawn".
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
  if (type === 'cal') {
    // The step's name, not its id, so a session started from a row says
    // what it is about. One GET per step, cached; the line fills in when it lands.
    const known = calTitleCache[id];
    if (known === undefined) {
      calTitleCache[id] = null;
      get('/calendar/' + encodeURIComponent(id)).then(i => {
        calTitleCache[id] = (i && i.title) || '';
        for (const el of document.querySelectorAll(`[data-caltitle="${id}"]`)) el.innerHTML = mdInline(calTitleCache[id] || id, false);
      }).catch(() => { calTitleCache[id] = ''; });
    }
    return `<div class="re small muted">↩ <b>${esc(label)}</b> · 📅 <a href="${refHref(ref) || '#/calendar'}" data-caltitle="${esc(id)}">${known ? mdInline(known, false) : esc(id)}</a></div>`;
  }
  return `<div class="re small muted">↩ <b>${esc(label)}</b> · ${esc(type)} ${esc(id)}</div>`;
}
const calTitleCache = {};

// The newest session row: the one end line that says "turn ended".
// Repeating it on every reply says nothing, so every earlier reply row is its
// day, time and cost alone. The phone's `lastTurnID` (ThreadDetail) picks
// the same row.
function lastTurnID(ordered) {
  for (let i = ordered.length - 1; i >= 0; i--) {
    const m = ordered[i];
    if (m.role !== 'owner' && m.role !== 'system' && m.kind !== 'schedule') return m.id;
  }
  return null;
}

function msgHTML(m, asks, acts, askById, recs, recById, last) {
  const role = m.role === 'owner' ? 'owner' : m.role === 'system' ? 'system' : 'claude';
  const cls = m.kind === 'error' ? ' error' : m.kind === 'read' ? ' read' : '';
  const atts = (m.attachments || []).map(attachHTML).join('');
  const askCards = (asks || []).map(chatAskHTML).join('') + (acts || []).map(chatActionHTML).join('')
    + (recs || []).map(chatRecHTML).join('');
  // Day, time, dollars; no tokens. The session's token total stays in the head.
  const meta = `${dayWhen(m.ts)}${m.cost_usd ? ' · ' + usd(m.cost_usd) : ''}`;
  // THERE IS NO WHITE CELL: anything the owner must read or do is in one of
  // the card cells.
  // A session's reply is the turn ENDING, never a text bubble: the cards the
  // turn raised are the reply (the hub mints a read card from any reply text
  // since 09-13, so a new row is always empty), and the row is one muted line
  // — that it ended, when, what it cost — in the card's colour. A row that
  // still carries text (before 09-13, or text folded beside a card) keeps it
  // behind that line, closed, for the record only. Errors stay red bubbles.
  // The phone draws the same line (ThreadDetail.chatBubble).
  // A session limit is a pause, not a crash: its row is the
  // same one line in the paused card's amber, and the card under it says when.
  if (m.kind === 'error' && isPauseText(m.text)) {
    return `<div class="msg ${role} end paused" id="msg-${esc(String(m.id))}"><div><span class="end-line">turn paused · session limit · ${meta}</span></div></div>${askCards}`;
  }
  if (role === 'claude' && m.kind !== 'error') {
    const line = `<span class="end-line">${last ? 'turn ended · ' : ''}${meta}</span>`;
    const old = (m.text || '').trim() || atts;
    const body = old
      ? `<details class="old-reply"><summary>${line}</summary><div class="bubble">${md(m.text)}${atts ? `<div class="atts">${atts}</div>` : ''}</div></details>`
      : `<div>${line}</div>`;
    return `<div class="msg ${role} end${cls}" id="msg-${esc(String(m.id))}">${body}</div>${askCards}`;
  }
  // A long message folds, so a big paste cannot fill the pane. Past LONG_LINES lines or LONG_CHARS
  // characters the body clamps to a screen's worth with a fade and a "Show
  // N more lines" button if N is five or more once it is drawn, else it is
  // shown whole (ui.js fitFolds); open, it is a scroll box no taller than the pane,
  // wide tables scroll sideways and their header row stays pinned while the
  // rows go by. The fold survives the 5 s redraw (chatState.open, key
  // "long:<id>"). The phone folds the same way (ThreadDetail.chatBubbleBox).
  const long = isLongText(m.text);
  const openLong = long && chatState.open.get('long:' + m.id) === true;
  const bodyCls = long ? ` class="body long${openLong ? ' open' : ''}"` : ' class="body"';
  const fold = long ? foldButtonHTML(openLong, `toggleLong(${+m.id})`) : '';
  return `<div class="msg ${role}${cls}${long ? ' has-long' : ''}">
    <div class="bubble">
      ${m.kind === 'checkin' ? '<div class="small muted">⏰ scheduled check-in</div>' : ''}
      ${role === 'owner' ? replyLineHTML(m, askById, recById) : ''}
      <div${bodyCls}>${md(m.text)}${atts ? `<div class="atts">${atts}</div>` : ''}</div>${fold}
      <div class="meta"><span>${meta}</span>${sentByHTML(m)}</div>
    </div></div>${askCards}`;
}

function toggleLong(id) {
  const k = 'long:' + id;
  if (chatState.open.get(k) === true) chatState.open.delete(k); else chatState.open.set(k, true);
  if (chatState.id) refreshThread(chatState.id, true);
}

// Who wrote a blue or dashed row, in its lower-right corner — you, the hub or
// a session — so a blue message says where it came from. A session is a link
// to it. The phone draws the same words (ThreadDetail.sentBy).
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

// Blobs are stored under their original extension, so an attachment that is a
// picture can be shown as one instead of as the word "attachment" — the thumbnail links to the full file. HEIC only decodes in
// Safari, hence the onerror fallback to a plain link.
const isImageRef = r => /\.(png|jpe?g|gif|webp|heic|heif|avif)$/i.test(String(r || ''));
function attachHTML(ref) {
  const url = '/api/v1/blobs/' + esc(ref);
  const name = esc(String(ref).split('/').pop());
  if (!isImageRef(ref)) return `<a href="${url}" target="_blank" class="small att-file">📎 ${name}</a>`;
  // Click opens it in the lightbox instead of a new tab — the href stays so
  // ⌘-click and "open in new tab" still work.
  return `<a href="${url}" target="_blank" class="att" onclick="return !openImage(this.href)"><img src="${url}" alt="${name}"
    onerror="this.parentElement.outerHTML='<a href=\\'${url}\\' target=_blank class=\\'small att-file\\'>📎 ${name}</a>'"></a>`;
}

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

// ---- activity (what a run actually did) ----
// The old version printed every event's raw title in one flat list, so half
// the lines were the word "result" and a tool call read as a shell command.
// Now it matches the app:
// one collapsible block per run, each result folded into the call above it,
// each call written in plain English with the raw command and output one
// click away.
// One slot per starter MESSAGE, not per run: a steering message shares its
// run_id with the message that started the turn, so a per-run id made two
// divs with the same id and only the first was ever filled.
const runSlotID = m => 'run-' + String(m.run_id).replace(/[^A-Za-z0-9_-]/g, '_') + '-' + m.id;
const startsRun = m => !!m.run_id && m.role !== 'claude' && m.kind !== 'error';
function runSlot(m) {
  return startsRun(m) ? `<div class="activity" id="${runSlotID(m)}"></div>` : '';
}

function drawActivity(ordered, events, running, t) {
  const byRun = {};
  for (const e of (events || [])) (byRun[e.run_id || ''] ||= []).push(e);
  // The run in flight = the newest starter message whose reply has not landed.
  const replied = new Set(ordered.filter(m => m.role === 'claude' || m.kind === 'error').map(m => m.run_id).filter(Boolean));
  let live = null;
  if (running) {
    for (const m of ordered) if (m.run_id && m.role !== 'claude' && !replied.has(m.run_id)) live = m.run_id;
  }
  const known = new Set(ordered.map(m => m.run_id).filter(Boolean));
  // Split each run's steps at its starters, so a message steered in mid-turn
  // breaks the tool chain where it landed instead of the whole chain being
  // printed both above and below it. Live dot: last starter.
  const starters = {};
  for (const m of ordered) if (startsRun(m)) (starters[m.run_id] ||= []).push(m);
  for (const [run, ms] of Object.entries(starters)) {
    const evs = byRun[run] || [];
    ms.forEach((m, i) => {
      const el = document.getElementById(runSlotID(m));
      if (!el) return;
      const from = i === 0 ? -Infinity : +new Date(m.ts);          // first takes anything earlier
      const to = i + 1 < ms.length ? +new Date(ms[i + 1].ts) : Infinity;
      const slice = evs.filter(e => { const t = +new Date(e.ts); return t >= from && t < to; });
      const isLive = run === live && i === ms.length - 1;
      // The headline is the hub's count for THIS message; the rows are
      // whatever steps are held (opening the block fetches the rest).
      const count = chatState.steps.get(m.id);
      // A block with no steps still has to draw the cards the hub cut it at
      // (a card in a segment is drawn by blockHTML alone; emptying the slot
      // would lose it: a "1 to read" with no cell).
      const show = (count && (count.steps || (count.segments || []).length)) || slice.length || isLive;
      el.innerHTML = show ? blockHTML(slice, isLive, 'run:' + run + ':' + m.id, count, m.id) : '';
    });
  }
  const orphanEl = document.getElementById('run-orphan');
  let orphan = [];
  if (orphanEl) {
    orphan = running && !live ? (events || []).filter(e => !known.has(e.run_id)) : [];
    orphanEl.innerHTML = orphan.length ? activityHTML(orphan, true, 'run:live') : '';
  }
  chatState.liveBlock = !!live || orphan.length > 0;
  paintWorking(running, chatState.liveBlock, t);
}

// THE working line, same on both surfaces (the phone spells it out in
// ThreadDetail.swift; keep them in step). A running turn ends the chat the
// way a finished one does: its fold, then one line of facts. "turn ended ·
// today 10:06 PM · $7.27" becomes "working · today 10:09 PM · $1.12", the
// clock being the turn's last output, so a hung turn shows an old time. With
// no live block yet the count rides on this line too, behind the pulse. A
// line left on a stopped session reads as one that never finished.
const showWorking = running => !!running;
function paintWorking(running, liveBlock, t) {
  const el = document.getElementById('run-working');
  if (!el) return;
  el.hidden = !showWorking(running);
  if (el.hidden) return;
  const line = ['working', turnFacts(t, !liveBlock)].filter(Boolean).join(' · ');
  el.innerHTML = `${liveBlock ? '' : '<span class="dot pulse"></span> '}${esc(line)}`;
}

// One run block, cut at the cards the agent raised while it ran. The hub gives the cut and each piece's own count
// (steps.segments); with none, this is the single fold it always was.
//
// A segment whose card this chat is not drawing — a proposal the pending list
// no longer carries — is FOLDED INTO the next one rather than left as a cut
// with nothing between: the counts add up, so the chain still reads as one
// run of N steps.
function blockHTML(evs, live, key, count, msgID) {
  const segs = (count && count.segments) || [];
  if (!segs.length) return activityHTML(evs, live, key, count, msgID);
  const merge = (a, b) => !a ? b : {
    ref: b.ref, tools: a.tools + b.tools, thoughts: a.thoughts + b.thoughts, steps: a.steps + b.steps,
    first_id: Math.min(a.first_id || b.first_id, b.first_id || a.first_id), last_id: Math.max(a.last_id, b.last_id),
  };
  const drawn = [];
  let carry = null;
  for (const s of segs) {
    const acc = merge(carry, s);
    if (s.ref && !chatState.cards[s.ref]) { carry = acc; continue; }
    drawn.push(acc);
    carry = null;
  }
  if (carry) drawn.push({ ...carry, ref: '' });
  return drawn.map((s, i) => {
    const rows = s.steps ? evs.filter(e => e.id >= s.first_id && e.id <= s.last_id) : [];
    const fold = s.steps ? activityHTML(rows, live && i === drawn.length - 1, key + ':' + i, s, msgID) : '';
    return fold + (s.ref ? cardInChainHTML(s.ref) : '');
  }).join('');
}

// The card itself, drawn where it was raised. It is the ordinary card — the
// same Respond/Done/Dismiss, the same Approve/Deny — because answering one
// mid-chain is the point: the reply is delivered into the turn still running
// as a steering message and the agent carries on.
function cardInChainHTML(ref) {
  const c = chatState.cards[ref];
  if (!c) return '';
  return ref.startsWith('action:') ? chatActionHTML(c) : chatAskHTML(c);
}

// A dismissed card's fold remembers the owner's click across the redraw (cardHTML).
const chatOpened = (ref, id) => chatState.open.get(ref + ':' + id) === true;
const chatAskHTML = a => askHTML(a, chatOpened('ask', a.id));
const chatActionHTML = a => actionHTML(a, chatOpened('action', a.id));
const chatRecHTML = r => recHTML(r, chatOpened('rec', r.id));

// count = the hub's row for this message ({tools, thoughts, steps}); it wins
// over anything counted here, which is only ever the steps we happen to hold.
function activityHTML(evs, live, key, count, msgID) {
  const tools = count ? count.tools : evs.filter(e => e.kind === 'tool_use').length;
  const thoughts = count ? count.thoughts : evs.filter(e => e.kind === 'thinking').length;
  const all = count ? count.steps : evs.length;
  const parts = [];
  if (tools) parts.push(tools + ' tool call' + (tools === 1 ? '' : 's'));
  if (thoughts) parts.push(thoughts + ' thought' + (thoughts === 1 ? '' : 's'));
  if (!parts.length) parts.push(all ? all + ' step' + (all === 1 ? '' : 's') : 'starting…');
  // No "now: <step>" behind the count: a raw step label reads as noise. The
  // working line under the chat carries the turn's clock and dollars
  // (paintWorking), and the steps are one click away.
  // ALWAYS folded until the owner clicks it, live or not. A running turn used
  // to open itself, so entering a session landed the reader in the middle of
  // a wall of steps instead of on the conversation. `data-def` lets the toggle handler tell the owner's click apart
  // from the open state we ourselves rendered.
  const open = chatState.open.get(key) ?? false;
  // Opened but not all of it is here yet: say so rather than showing a short
  // list that looks like the whole run.
  const missing = all > evs.length ? `<div class="ev flat"><span class="hl muted">loading ${all - evs.length} earlier step${all - evs.length === 1 ? '' : 's'}…</span></div>` : '';
  // One mark leads the row: the pulse while the turn runs and the block is
  // shut, ▾ open, ▸ ⚙ finished. `.run.live` (app.css) hides the other.
  return `<details class="run${live ? ' live' : ''}" data-k="${esc(key)}" data-msg="${msgID || 0}" data-def="${open ? 1 : 0}"${open ? ' open' : ''}>
    <summary>${live ? '<span class="dot pulse"></span>' : '<span class="gear">⚙</span>'} <span class="hl">${esc(parts.join(' · '))}</span></summary>
    <div class="evs">${missing}${pairRows(evs).map(evRowHTML).join('')}</div></details>`;
}

/// Each tool_result belongs to the last tool_use still waiting for one.
function pairRows(evs) {
  const out = [];
  for (const e of evs) {
    if (e.kind === 'tool_result') {
      const i = out.findLastIndex(p => p[0].kind === 'tool_use' && !p[1]);
      if (i >= 0) { out[i][1] = e; continue; }
    }
    out.push([e, null]);
  }
  return out;
}

const firstLine = s => String(s || '').split('\n').find(l => l.trim()) || '';
// An opened step is one plain terminal block, not a stack of grey boxes: a
// light-mode terminal with lightly coloured text.
// A thought: the purple line IS the thought — shut, its first line; open, all
// of it, and nothing printed twice under it. A tool call: a shell command in
// blue behind `$`, whole (the headline is its English), what went in when
// the headline cannot say it (an edit's lines), then the output in grey
// — its first OUT_FOLD lines, the rest behind "… N more lines" — or the error
// in red, whole ("the error is good"). A body that is still the input's JSON
// (stored before 10-05) is not shown: the title already says it.
// An interim text step (what the agent said mid-turn, 💬) folds like the
// thought: shut, its first two lines and an ellipsis (`.ev.said .hl`); open,
// all of it. A long one on a single nowrap line could not be read at all.
const OUT_FOLD = 12;
function evRowHTML([e, r]) {
  const icon = { thinking: '✻', tool_use: '⌘', text: '💬' }[e.kind] || '↳';
  const bad = r && r.title === 'error';
  const key = 'ev:' + e.id;
  const open = chatState.open.get(key) === true;
  const tick = r ? `<span class="tick${bad ? ' err' : ''}">${bad ? '✕' : '✓'}</span>` : '';
  const fold = (cls, extra) => {
    const o = chatState.open.get(key + extra) === true;
    return `<details class="${cls}" data-k="${esc(key + extra)}" data-def="${o ? 1 : 0}"${o ? ' open' : ''}>`;
  };
  if (e.kind === 'thinking') {
    return `${fold('ev think', '')}<summary><span class="ic">${icon}</span><span class="hl one">Thinking · ${esc(firstLine(e.body))}</span><span class="hl all">${esc(e.body)}</span></summary></details>`;
  }
  if (e.kind === 'text' && e.body) {
    return `${fold('ev said', '')}<summary><span class="ic">${icon}</span><span class="hl">${esc(e.body)}</span></summary></details>`;
  }
  const head = `<span class="ic">${icon}</span><span class="hl">${esc(e.kind === 'tool_use' ? (e.summary || e.title) : (firstLine(e.body) || e.title || e.kind))}</span>${tick}`;
  if (e.kind !== 'tool_use') return `<div class="ev flat">${head}</div>`;
  // The call line is a shell command whole behind `$`; any other tool's
  // headline already names the call ("Edit app/…/x.swift"), so its block
  // holds only what went in and what came out.
  const tool = e.title.split(' · ')[0];
  const call = tool === 'Bash' ? `<pre class="io call">${esc('$ ' + e.body)}</pre>` : '';
  const input = tool === 'Bash' || e.body.startsWith('{') ? '' : e.body;
  const lines = r ? (r.body || '(no output)').split('\n') : [];
  const out = !r ? '' : bad || lines.length <= OUT_FOLD
    ? `<pre class="io out${bad ? ' err' : ''}">${esc(lines.join('\n'))}</pre>`
    : `<pre class="io out">${esc(lines.slice(0, OUT_FOLD).join('\n'))}</pre>${fold('more', ':more')}<summary>… ${lines.length - OUT_FOLD} more lines</summary><pre class="io out">${esc(lines.slice(OUT_FOLD).join('\n'))}</pre></details>`;
  return `${fold('ev' + (bad ? ' bad' : ''), '')}<summary>${head}</summary>${call}${input ? `<pre class="io in">${esc(input)}</pre>` : ''}${out}</details>`;
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
function queuedHTML(prompts) {
  const ps = (prompts || []).filter(p => p.state === 'queued')
    .sort((a, b) => (a.not_before || '').localeCompare(b.not_before || ''));
  if (!ps.length) return '';
  const mine = p => p.author === 'owner' ? 'you' : p.author === 'hub' ? 'the hub' : 'this session';
  const next = ps[0];
  return `<details class="queued" id="queued"${queuedIsOpen() ? ' open' : ''}>
    <summary>
      <span class="hl small muted">Coming up · ${ps.length}</span>
      <span class="pill">next ${esc(next.not_before ? when(next.not_before) : 'now')}</span>
      <span class="grow trunc small muted">${esc(promptWords(next))}</span>
    </summary>
    <div class="qlist">
    ${ps.map(p => `<div class="row qrow">
      ${p.repeat ? `<span class="pill done" title="standing check-in">${esc(cadenceWords(p.repeat))}</span>` : ''}
      <span class="pill">${esc(p.repeat ? 'next ' : '')}${esc(p.not_before ? when(p.not_before) : 'now')}</span>
      ${p.target === 'new' ? '<span class="pill">new session</span>' : ''}
      ${p.outcome ? pill(p.outcome) : ''}
      <span class="grow trunc small" title="${esc(p.text || '')}">${esc(promptWords(p))} <span class="muted">· from ${esc(mine(p))}</span></span>
      <button class="sm" onclick="cancelPrompt('${esc(p.id)}')">${p.repeat ? 'Turn off' : 'Cancel'}</button>
    </div>`).join('')}
    </div>
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

// What each pick does when sent: the hub's own hint on each outcome
// (store/close.go), the line under the chips.
const outcomeMeans = outcomes => Object.fromEntries((outcomes || []).map(o => [o.value, o.hint || '']));

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
    await post(`/threads/${chatState.id}/messages`, { text: p.text, attachments: p.refs, via: 'console' });
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
  // The queued strip's fold outlives the redraw and the page (queuedHTML).
  const q = document.getElementById('queued');
  if (q) q.addEventListener('toggle', () => localStorage.setItem('queuedOpen', q.open ? '1' : '0'));
  const msgs = document.getElementById('msgs');
  if (!msgs) return;
  wireDrop(msgs, 'chat');
  // <details> toggles do not bubble; capture them so an expanded step stays
  // expanded when the run re-renders. Only a state that differs from the one
  // we rendered is remembered — a browser also fires `toggle` for the blocks
  // that came out of innerHTML already open, and recording those would pin a
  // finished run open forever.
  msgs.addEventListener('toggle', e => {
    const el = e.target, k = el.dataset && el.dataset.k;
    if (!k) return;
    if (String(+el.open) === el.dataset.def) chatState.open.delete(k);
    else chatState.open.set(k, el.open);
    // A block's steps are read when it is opened, not before: a session with
    // thousands of them costs one small read to open.
    const msgID = +(el.dataset.msg || 0);
    if (el.open && msgID) {
      const id = chatState.id;
      ensureBlock(id, msgID).then(got => { if (got && chatState.id === id) refreshThread(id, true); });
    }
  }, true);
  const draft = document.getElementById(cdom('chat') + '-draft');
  if (draft) draft.focus();
}

