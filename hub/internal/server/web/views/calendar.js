// life hub — laptop console: Calendar. views.calendar.
'use strict';
// ================= calendar =================
// The page lists the agenda and closes items; it does not CREATE them. Every
// dated thing is put here by a session (`lifectl cal add`) — items are added
// by agents, never by hand — so the New-item form that used to sit at the top is gone (the
// POST /calendar route stays for lifectl). Two things it lists are not items
// and cannot be resolved as one: `run:` / `job:` rows are a thread's or job's
// next check-in projected forward, and `ask:` rows are open asks with no
// calendar item behind them — those close through /asks/{id}/resolve instead.
//
// The page is ONE week (2026-09-26 — Day, Month and the Schedule list went
// that day; see `views/calgrid.js`). `calEntryHTML` below is the row its
// detail panel opens, with its composer and Done · Won't do · Reopen.
let calLast = null;   // the last /calendar response, for the drag handlers
async function drawCalendar(view, rest) {
  calStateLoad();
  // `#/calendar/2026-08-31` deep-links the week holding a day; an old
  // `#/calendar/<mode>/<day>` link still lands on that day's week.
  const anchor = (rest || []).find(x => /^\d{4}-\d{2}-\d{2}$/.test(x || ''));
  if (anchor) calState.anchor = anchor;
  const draw = async () => {
    let [from, to] = calFetchRange();
    let c = await get(`/calendar?from=${from}&to=${to}`);
    // The first fetch guessed today from this machine's clock; the hub's
    // `today` decides, and a week the guess missed is fetched again.
    calToday = c.today || '';
    if (!calState.anchor && calToday && (calToday < from || calToday > to)) {
      [from, to] = calFetchRange();
      c = await get(`/calendar?from=${from}&to=${to}`);
    }
    calLast = c;
    await calDrawGrid(view, c);
  };
  await draw();
  setRedraw(draw, view);
}
views.calendar = { draw: drawCalendar, redraw: () => (route.redraw ? pageRedraw() : render()) };
// (The page fetched /goals until 2026-08-31, to colour the grid by goal. That
// colouring is gone — three colours, and only three — so the request is too.)

// One agenda row. Closed rows look closed, the same way the phone's
// `CalEntryRow` draws them, in the one glyph language of calgrid.js
// `calGlyph` (✓ + strikethrough read as its own opposite): **done** = ✓,
// muted, NOT struck — the record of what the owner got through stays on its
// day; **dismissed** = ✕ and the title struck through (cleared, not
// happening); **the owner's open rows** = ○ and full weight, however long ago their minute
// was. Open `owner` steps alone are bold, as on the phone. Top-level so the
// test file can render it without a browser. `opts.dated`: the row is drawn
// away from its day (the Overdue tray), so the meta line says the day too —
// "Tue 16 20:00" (`calWhenShort`, the phone's `calWhenShort`).
function calEntryHTML(i, opts) {
  const dated = !!(opts && opts.dated);
  const isItem = !!i.item;
  // A record (`did`) is closed by definition — it already happened — and
  // reads as its verb: "Read: …", "Approved: …", "Accepted: …" (2026-09-09).
  // `closed` is calgrid's one rule (done, did, an accepted rec's filed bar, a
  // decided action, won't-do); everything closed that is not won't-do is ✓.
  const wont = !!calWont(i), closed = calClosed(i), done = !wont && closed;
  // A proposed action's row carries its Approve/Deny (Phase 4: the calendar
  // is an interface, not a listing). A session's proposal is answered from
  // the session's composer like every card (2026-09-17), so the row's button
  // opens the session at the card with that pick armed (openApprove); a job's
  // proposal has no session and is decided here (decideAction).
  const proposed = i.kind === 'action' && i.state === 'proposed';
  const actID = proposed ? esc(String(i.ref || '').replace(/^action:/, '') || i.id) : '';
  const actArg = proposed && i.thread_id ? `{id:'${actID}',thread_id:'${esc(i.thread_id)}',title:'${esc(String(i.title || '').replace(/'/g, '’'))}'}` : '';
  // The owner's own task closes like a rec — with words, from a box under the
  // row (calStepBox) — so the buttons are not here for it. A closed item keeps
  // Reopen: the undo a stray tap needs. Agent items and asks keep the plain
  // pair. Homework is a COMPLETION, not a conversation: one tick, no words —
  // and the hub ticks it itself when the evidence lands (a full pitch round).
  // The hub's `tick` says which: no session waits on it (homework, a chore
  // that needs no agent behind it).
  const homework = isItem && (i.tick || i.kind === 'homework');
  const ownerStep = isItem && i.kind === 'owner' && !homework;
  // An install ask is the same teal cell here as in the chat — its own kind
  // of cell with an Install button, not Done:
  // title, description, one Install button carrying the OTA link, and
  // Won't install as the small out. It sits in Anytime while open; the tap
  // closes it and the record row ("Installed: build N") takes its place at
  // that minute. No link in the detail → Respond, as the chat card does.
  const install = calIsInstall(i) && !closed;
  const installLink = install ? askInstallLink(i.detail) : null;
  const acts = closed ? `<span class="small muted">${esc(i.state)}</span>${isItem
        ? ` <button class="sm" onclick="resolveCal('${esc(i.id)}','scheduled')" title="Put it back on the calendar (and the board, if it was due)">Reopen</button>` : ''}`
    // The row's decisive `outcomes` (Approve · Deny); Reply is the session's.
    : proposed ? (i.outcomes || []).filter(o => o.value).map((o, n) => `<button class="sm${n ? '' : ' primary'}" onclick="${actArg
        ? `openApprove(${actArg},'${esc(o.value)}')` : `decideAction('${actID}',${o.value === 'approved'})`}">${esc(o.label)}</button>`).join('\n                  ')
    : ownerStep || homework ? ''
    : isItem ? `<button class="sm" onclick="resolveCal('${esc(i.id)}','done')">Done</button>
                <button class="sm" onclick="resolveCal('${esc(i.id)}','dismissed')">Dismiss</button>`
    : install ? (installLink ? `<a class="btn sm install" href="${esc(installLink)}" target="_blank" title="Open the install page — the phone reports the new build and the card closes itself">Install</a>`
                  : `<button class="sm primary" onclick="openRespond('${esc(i.ask_id)}')">Respond</button>`)
                + `<button class="sm" onclick="resolveAskGlobal('${esc(i.ask_id)}','dismissed')" title="Skip this build">Won't install</button>`
    : i.ask_id ? `<button class="sm" onclick="resolveAskGlobal('${esc(i.ask_id)}','done')">Done</button>
                  <button class="sm" onclick="resolveAskGlobal('${esc(i.ask_id)}','dismissed')">Dismiss</button>`
    : '';
  // A deferred rec on its review day reads as the question it is (Phase
  // 2b): "Check back: <title>", and opens the rec's own page.
  const text = i.verb ? esc(i.verb) + ': ' + mdInline(i.title)
    : i.kind === 'rec' ? 'Check back: ' + mdInline(i.title) : mdInline(i.title);
  const title = done ? `<span class="tick">✓</span>${text}`
    : closed ? `<span class="tick">✕</span><s>${text}</s>`
    : (calLaneOf(i).owner ? '<span class="tick">○</span>' : '')
      + (calIsOwner(i) ? `<strong>${text}</strong>` : text);
  const href = refHref(i.ref, i.thread_id);
  // What the row's session is doing RIGHT NOW rides beside its link — the
  // board's own running / speaking / waiting-to-speak pills, stamped by the
  // hub as `live` — so the step just answered from says its session is
  // on it instead of falling silent. The page redraws every
  // 30 s and right after a send, so the pill comes and goes with the turn.
  const live = livePillsHTML(i.live);
  const link = (href && i.kind === 'rec' ? `<a href="${href}">open rec</a>`
    : href && i.kind === 'action' ? `<a href="${href}">${i.thread_id ? 'open in session' : 'open'}</a>`
    : href && i.did ? `<a href="${href}">open</a>`
    : i.thread_id ? `<a href="#/sessions/${esc(i.thread_id)}">open session</a>` : '') + (live ? ' ' + live : '');
  // A record says whose hands and which session: "by <actor> · <session
  // title>" — the portfolio line under the deed.
  const record = i.did ? `${i.actor ? 'by ' + esc(i.actor) : ''}${i.thread_title ? (i.actor ? ' · ' : '') + `<a href="#/sessions/${esc(i.thread_id)}">${esc(i.thread_title)}</a>` : ''} ` : '';
  const detail = install ? askDetailShown(i.detail) : i.detail;
  // An open to-do never says a time as if it were due then: "added Sep 20".
  const when = i.soon && !closed ? calAdded(i) : dated ? calWhenShort(i) : i.at;
  return `<div class="card cal${closed ? ' closed' : ''}${install ? ' install' : ''}${i.overdue && !closed ? ' overdue' : ''}"${proposed ? ` data-act="${actID}"` : ''}>
    <div class="spread"><span class="t">${title}</span>
      <span class="small muted">${i.overdue && !closed ? '<span style="color:var(--red);font-weight:600">overdue</span> · ' : ''}${when ? esc(when) + ' · ' : ''}<span class="pill" style="${calPillStyle(i)}">${esc(i.kind_label || i.kind || '')}</span></span></div>
    ${detail ? `<div class="small muted wrap2">${mdPreview(detail)}</div>` : ''}
    <div class="row small muted" style="margin-top:6px">
      ${record}${i.repeat ? esc(i.repeat) + ' · ' : ''}${i.goal_id ? `<a href="#/goals/${esc(i.goal_id)}">${esc(i.goal_id)}</a>` : ''}
      ${link}
      <div class="grow"></div>${acts}
    </div>${!closed && ownerStep ? calStepBox(i) : !closed && homework ? calHomeworkBox(i) : ''}</div>`;
}

// Homework is the reverse of the owner's own step: usually a tick, sometimes a
// talk — tick it with no session, or start one about it, both effortless. So
// it gets the Recs
// box: Did it / Skip pressed over an empty box turn into "Send with no note"
// and the second press closes it on the hub — no session wakes. Words go to
// a session with the item's name as its title, the hub framing the step
// (calendar.go header: title, day, goal, detail) above them; Send with words
// and no pick leaves the item open. The buttons
// are the row's own `outcomes` (store.TickOutcomes: Did it · Skip · Send).
function calHomeworkBox(i) {
  return replyBox(calKey(i.id), { ref: 'cal', id: i.id, title: i.title || '' }, {
    outcomes: i.outcomes,
    emptyMsg: 'Type something first — or press Did it twice to just tick it.',
    placeholder: 'How did it go? Words start a session about it — or just press Did it.',
    redraw: () => pageRedraw(),
    after: async () => { await pageRedraw(); refreshBadges(); },
    send: p => postReply(p, {
      close: async (p, words) => {
        if (words) return undefined;
        await post(`/calendar/${i.id}/resolve`, { outcome: p.outcome || 'done', by: 'owner' });
        return p.outcome === 'wont' ? 'skipped' : 'done';
      },
      prompt: p => ({
        target: i.thread_id ? 'new-or:' + i.thread_id : 'new', title: i.title || '',
        in_reply_to: calKey(i.id), ...(p.outcome ? { outcome: p.outcome } : {}),
      }),
      sent: p => (p.outcome === 'done' ? 'done — ' : p.outcome === 'wont' ? 'skipped — ' : '')
        + (i.thread_id ? 'your words went to its session' : 'a session is starting about it'),
    }),
  });
}

// Closing one of the owner's own steps is a message, not a button: a stray
// tap must not mark it done, so it needs words. The box is THE composer
// (composer.js) — the same one the chat and the Recs page draw, with no
// duplicate code. Which means a calendar answer carries everything a chat
// message does: a pasted screenshot, Attach, drop, which session hears it,
// and when.
//
// Send posts ONE prompt at `cal:<id>` — the road the phone's Respond sheet
// already takes. With done|wont the hub claims the item as it queues the
// prompt (threads.Queue → Manager.ClaimCal), so the step closes and the words
// arrive as its answer; with no chip lit the step stays open and the words
// just go. Nothing here writes the row itself.
// The chips are the row's own `outcomes` (store.StepOutcomes: Done · Won't
// do · Reply), decisive first, "Reply" last — the hub's order on every card.
const CAL_STEP_MEANS = {
  done: 'the step closes as done; your words go to the session that put it here',
  wont: "the step closes as won't do; your words go to that session",
  '': 'the step stays open — your words go to the session that put it here',
};
const calKey = id => 'cal:' + id;
// A to-do no session is behind (soon, no thread) closes on the hub with the
// words on the row; nothing is woken to hear them.
const CAL_TODO_MEANS = {
  done: 'the to-do closes as done, with your words on it',
  wont: "the to-do closes as won't do, with your words on it",
  '': 'the to-do stays open — your words start a session about it',
};
const calLoneTodo = i => !!i.soon && !i.thread_id;
// The outcomes ride on `re` as chips here, not as footer buttons: a step
// never closes silently, so the pick is lit first and the words sent after.
// No route: answering a step always means now, to the session that put it
// here — same as the phone's Respond sheet.
function calStepBox(i) {
  const lone = calLoneTodo(i);
  return replyBox(calKey(i.id), {
    ref: 'cal', id: i.id, title: i.title || '',
    outcomes: i.outcomes || [], means: lone ? CAL_TODO_MEANS : CAL_STEP_MEANS,
  }, {
    // Done and Won't do both close it, and the owner's step never closes silently.
    requireWords: true,
    placeholder: st => st.re && st.re.outcome
      ? 'What happened? Done and Won\'t do close it with your words.'
      : lone ? 'Your words start a session about it.'
      : 'Your words go to the session that put this here.',
    redraw: () => pageRedraw(),
    after: async () => { await pageRedraw(); refreshBadges(); },
    send: p => postReply(p, {
      close: async p => {
        if (!lone || !p.outcome) return undefined;
        await post(`/calendar/${i.id}/resolve`, { outcome: p.outcome, by: 'owner', note: p.text });
        return p.outcome === 'done' ? 'done' : "won't do";
      },
      // `new-or:` so a session that has since ended does not swallow the answer.
      prompt: p => ({
        target: 'new-or:' + (i.thread_id || 'calendar'),
        in_reply_to: calKey(i.id), ...(p.outcome ? { outcome: p.outcome } : {}),
      }),
      sent: p => p.outcome === 'done' ? 'done — your words went to its session'
        : p.outcome === 'wont' ? "won't do — your words went to its session"
        : 'sent',
    }),
  });
}

// Every open step of the owner's on the page gets the composer's wiring: the draft it
// keeps across the 30 s redraw, ⌘↵, the file input and the drop zone.
function wireCalNotes(root) {
  for (const el of root.querySelectorAll('[data-c]')) wireComposer(el.dataset.c);
}
// The kind pill takes its lane's own colour, so a row says the same thing in
// the agenda that its chip says on the grid: red = the owner's, grey = the agents',
// purple = a rec (calgrid.js, CAL_LANES). Inline rather than a pill class
// because the classes are named for the board's palette, not this one —
// `.pill.purple` is the accent BLUE, which would have quietly made a rec the
// one row on the page whose colour disagreed with its chip.
const calPillStyle = i => {
  const c = calShade(i);
  return `background:color-mix(in srgb, ${c} 15%, transparent);color:${c}`;
};
// Where a row opens: refHref (ui.js) — one table for every ref on the console.

async function resolveCal(id, state) {
  return act(() => post(`/calendar/${id}/resolve`, { state, by: 'owner' }), 'item ' + state,
    async () => { await pageRedraw(); refreshBadges(); });
}
