// life hub — laptop console: Recommendations (the list, one rec, deciding and
// scoring). views.recs.
'use strict';
// ================= recommendations =================
// The pull half of the system. An ask pushes — it is the owner's turn, now.
// A rec waits: it notifies nobody (no push, no
// document.title count — the nav badge is a count of what is sitting here, not
// a summons) and never appears on "Your turn", because the only moment a "want this from
// your Amazon cart?" is welcome is the moment they came looking
// (2026-08-23: "there's a concept of needs action, and then there's a concept
// of recommendations"). So this page is a browse surface, and the one thing
// it must make effortless is the DECLINE NOTE: his reason is taste, taste is
// what the next session has to read before it recommends the same thing
// again, and a reason hidden behind a second click never gets typed.
//
// There is no track-record header any more (2026-09-26: "there's this
// whole track record part of the recommendations page. I don't think that's
// necessary. I think it can be removed from the web app and the phone"). The
// numbers still exist — `lifectl recs stats` reads /recs/stats — and this page
// takes only the due-to-score line from it.
//
// TWO VIEWS, NOT SEVEN TABS (2026-09-26: "the later accepted decline
// done, those buttons aren't particularly helpful… think of how the sessions
// page looks, where it's got the ones that you need to make action on, and
// then at the bottom… all sessions… a table view"). The page is what is open;
// the one line at its foot opens every rec as a table (#/recs/all), each row
// saying its domain and where it ended up. The hub still filters by any
// status (`/recs?status=…`, `lifectl recs <status>`).
views.recs = {
  draw: (view, rest) => {
    const [id] = String(rest[0] || '').split('?');
    return id === 'all' ? recsAll(view) : id ? recDetail(view, id) : recsPage(view);
  },
  redraw: () => (route.redraw ? pageRedraw() : render()),
};
registerTab({ key: 'recs', label: 'Recs', order: 20, badge: 'recs' });
registerPlace('recs', { label: 'Recs', view: 'views/recs.js', phone: 'RecsView.swift' });

// modelShort and dayLabel moved to ui.js (2026-08-28), DOM_PILL and costLabel
// too (2026-09-18: the chat's rec card draws them) — a helper two pages read
// is a shared shape, not a Recs detail. A rec's status pills are ui.js
// recPills (rec-0adcadce).

/// The hub's `dates_label` ("act by Jan 15 (12d) · review Oct 1"). act_by is a
/// deadline the hub enforces, so the line reads amber within a week of it and
/// red once it has gone (`days_left`).
function recDates(r) {
  if (!r.dates_label) return '';
  const left = r.days_left;
  const style = left == null ? '' : left < 0 ? 'color:var(--red)' : left <= 7 ? 'color:var(--amber)' : '';
  return `<span style="${style}">${esc(r.dates_label)}</span>`;
}

async function recsPage(view) {
  // `again` = a REDRAW, not the first draw. The guard below asks whether the
  // caret is in any composer on the page, and the chat leaves its own box
  // focused — so an unqualified guard made arriving from a session draw
  // nothing at all and leave the chat on screen under the Recs route.
  const draw = async (again) => {
    // Never redraw over a reason being typed (there is no poll on this page,
    // but a decision elsewhere on the list triggers one). The composer keeps
    // the words either way; this keeps his cursor where he left it.
    const ae = document.activeElement;
    if (again && ae && ae.tagName === 'TEXTAREA' && ae.closest('[data-c]')) return;
    const [d, st] = await Promise.all([get(recsQuery()), get('/recs/stats').catch(() => null)]);
    const every = d.recs || [];
    const list = every.filter(r => r.open);
    if (paint(view, `<div class="pane wide rec-col">
      <h2 style="margin:0 0 12px">Recommendations</h2>
      ${list.map(recCard).join('') || emptyHTML('Nothing open.')}
      ${st && st.unscored_due?.length ? `<div class="small muted" style="margin-top:10px">${st.unscored_due.length} accepted rec${st.unscored_due.length === 1 ? '' : 's'} due to be scored</div>` : ''}
      ${every.length ? `<a class="all-link" href="#/recs/all"><span>All recommendations</span><span class="muted">${every.length}</span></a>` : ''}
    </div>`)) wireRecNotes(view);
  };
  setRedraw(() => draw(true), view);
  await draw();
}

// No domain picker on either page (the desktop dropped it 2026-10-08: "the
// dead domain picker gone"); the All table keeps its Domain column.
const recsQuery = () => '/recs?status=all';

// ---- All recommendations (#/recs/all): the All sessions table's shape —
// twenty rows at a time and a box that narrows it by title; one line per
// rec: title, domain, where it ended up (and how it turned out), when, cost.
let recsAllQuery = '';
const recsPager = makePager('recs', 20);
async function recsAll(view) {
  const draw = async () => {
    const recs = (await get(recsQuery())).recs || [];
    if (!paint(view, `<div class="pane wide">
      <div class="small"><a href="#/recs">‹ Recommendations</a></div>
      <div class="spread" style="margin:6px 0 12px;gap:8px">
        <h2 style="margin:0">All recommendations <span class="muted">· ${recs.length}</span></h2>
        <input type="text" id="recs-q" placeholder="Find a rec…" value="${esc(recsAllQuery)}" style="max-width:240px" autocomplete="off">
      </div>
      <div class="card all-table" id="recs-table"></div>
    </div>`)) return;
    const paintAll = recsPager.paint = () => {
      const el = document.getElementById('recs-table');
      if (!el) return;
      const words = recsAllQuery.toLowerCase().split(/\s+/).filter(Boolean);
      const rows = recs.filter(r => words.every(w => (r.title || '').toLowerCase().includes(w)));
      const shown = recsPager.rows(rows);
      el.innerHTML = !rows.length ? emptyHTML('No rec by that name.') : `
        <table>
          <thead><tr><th>Recommendation</th><th>Domain</th><th></th><th>When</th><th class="n">Cost</th></tr></thead>
          <tbody>${shown.map(r => {
            const href = `#/recs/${esc(r.id)}`;
            return `<tr onclick="location.hash='${href}'">
              <td class="all-t"><a href="${href}"${r.status === 'proposed' ? ' class="unread"' : ''}>${mdInline(r.title, false)}</a></td>
              <td>${pill(r.domain, DOM_PILL[r.domain])}</td>
              <td class="all-p">${recPills(r)}</td>
              <td class="small muted">${ago(r.decided_at || r.created_at)}</td>
              <td class="n small">${costLabel(r)}</td></tr>`;
          }).join('')}</tbody>
        </table>
        ${recsPager.html(rows.length, shown.length)}`;
    };
    const q = document.getElementById('recs-q');
    // A new search starts from the first twenty again.
    q.oninput = () => { recsAllQuery = q.value; recsPager.reset(); paintAll(); };
    recsPager.reset();
    paintAll();
  };
  setRedraw(draw, view);
  await draw();
}


// THREE BANDS, NOT FIVE (2026-09-09: "these cards are too hard to read
// because they're too busy… they almost are made out of five horizontal bars:
// gray, white, blue, white, gray… we could set the description into the top
// bar to remove that white area. Instead of the blue bar, we could replace the
// send button with [three] buttons"). What is left is:
//
//   ┌──────────────────────────────────┐
//   │ title · cost · pills             │  .rec-head — the record, tinted,
//   │ why, in two lines                │  description INSIDE it
//   ├──────────────────────────────────┤
//   │ your note…                       │  the composer's words band
//   ├──────────────────────────────────┤
//   │ Attach  where   Accept Decline … │  its footer band — the answers
//   └──────────────────────────────────┘
//
// The two white bands that went were the description (now the head's last
// line) and the chip strip the composer drew above the words; the answers it
// held are now the footer's buttons, so pressing one sends (see
// composerActionsHTML). A decided rec has no box, so it is head + his note.
function recCard(r) {
  // A deferred rec keeps its box: he can decide it early — the hub's
  // standing calls it neither open nor closed. (Nothing can park one any
  // more — Later is gone from both surfaces, 2026-09-09.)
  const proposed = !r.closed;
  const why = mdPreview(r.because || r.detail || '');
  return `<div class="card rec" id="rec-${esc(r.id)}">
    <div class="rec-head">
      <div class="spread">
        <strong><a href="#/recs/${esc(r.id)}" style="color:inherit;text-decoration:none">${mdInline(r.title, false)}</a></strong>
        <span class="small" style="white-space:nowrap">${costLabel(r)}</span>
      </div>
      <div class="row small muted" style="flex-wrap:wrap;gap:8px;margin-top:8px">
        ${pill(r.kind)}
        ${r.model ? `<span class="pill" title="${esc(r.model)}">${esc(modelShort(r.model))}</span>` : ''}
        ${recPills(r)}
        ${recRunning(r)}
        ${recDates(r)}
        <div class="grow"></div>
        ${recSourceThread(r) ? `<a href="#/sessions/${esc(recSourceThread(r))}" title="The session that filed it">its chat ›</a>` : ''}
        <a href="#/recs/${esc(r.id)}">details ›</a>
      </div>
      ${why ? `<div class="small muted wrap2 why">${why}</div>` : ''}
    </div>
    ${r.status !== 'proposed' && r.decision_note ? `<div class="rec-body small">
        <span class="muted">your note:</span> ${mdInline(r.decision_note)}</div>` : ''}
    ${proposed ? recDecideBox(r) : ''}
  </div>`;
}

// The whole decision, in one box (2026-08-25: "you only need one box…
// accept always means that the response that you put into that box is going
// to an agent"). What he types is his note on the record AND the message the
// agent gets — accepted or declined, both go the same way — so there is no
// second screen and no second box between the card and the session.
//
// THREE answers, and each of them is a button that sends (2026-09-09:
// "there would only need to be three options here: Accept, Decline, Reply. As
// is the case, we no longer need the 'Later' option, neither here nor on the
// phone"): Accept · Decline · Reply, in the footer band where Send used to
// sit. The note and the destination (its session / a new one) are the same for
// all three — every answer reaches an agent the same way, a reply just carries
// no verdict — so there is nothing left for a chip strip to say.
//
// LATER IS GONE. It was the third answer from 2026-08-26 (park the rec until a
// day); it never earned its two extra controls — a chip and a date field — on
// a card he called too busy, and the hub still takes `status: deferred` for a
// caller that wants it (`lifectl rec <id> defer <date>`), so the primitive and
// the one parked rec survive with no UI to make more.
//
// A press over an EMPTY box does not decide: it renames itself "Send with no
// note" and waits for a second press (`confirmEmpty`, composer.js). Reply is
// different — a reply with nothing in it goes nowhere, so it asks for words
// rather than confirming.
//
// The box is THE composer (composer.js), the same one the chat and the
// calendar draw, so the pictures and the destination come for free
// (2026-08-29: "model all of the prompt boxes in the same way with a maximal
// set of features… you should not have as much duplicate code"). recReplyOf
// (threads.js) still makes the card it answers — the chat's copy of this box
// keeps its chips, since there the answers cannot live in a shared footer.
//
// No time select here (2026-09-09: "there doesn't need to be a concept
// of sending a message later on the recommendations in the UI… I, as a user,
// am never going to schedule a message to be sent in the future to a
// recommendation"). The hub's `/decide` and `/reply` still take the prompt
// timing fields — that primitive stays — but no rec box on either surface
// sends them: an answer to a rec goes now.
//
// The three buttons and their tooltips are the rec's own `outcomes`
// (store.RecOutcomes) — the hub's words, drawn by composer.js outcomeActions.
const recKey = id => 'rec:' + id;
function recDecideBox(r) {
  const src = recSourceThread(r);
  // An upload from this box is filed against the session that filed the rec.
  draftThread[recKey(r.id)] = src || null;
  return replyBox(recKey(r.id), recReplyOf(r, '', true), {
    outcomes: r.outcomes, chips: true,
    placeholder: 'Your reply',
    redraw: () => pageRedraw(),
    after: async () => { refreshBadges(); if (route.redraw) await pageRedraw(); else render(); },
    send: p => recSend(r, p),
  });
}

// The session that filed it is working right now, so there is nothing to
// follow up on (2026-09-09: "if the session for that recommendation is
// running, it should show that somewhere on the recommendations card. Tells me
// not to follow up with this recommendation… it basically just needs to say
// 'running'"). The hub stamps `thread_running` on every rec it serves; this is
// the same blue pill a session card wears.
function recRunning(r) {
  return r.thread_running
    ? `<span title="its session is working right now — nothing to follow up on">${runningPill('running')}</span>` : '';
}

// The session that filed it, which is where its decision goes by default.
function recSourceThread(r) {
  return r.thread_id || (String(r.source || '').startsWith('claude:thread:') ? r.source.slice(14) : '');
}

// Every open rec on the page gets the composer's wiring: the draft and the
// pictures it keeps across a redraw, ⌘↵, the file input and the drop zone.
function wireRecNotes(root) {
  for (const el of root.querySelectorAll('[data-c]')) wireComposer(el.dataset.c);
}

// Deciding is not the end of it: his answer goes to an agent, which is what
// mints the calendar items, proposals and asks that carry an accepted rec out
// (their ids land in `links`) and what records a decline where a future
// session will read it. One post does both — decide and deliver — and opens
// the session that got it in a NEW TAB, so this tab stays on the list, one
// rec shorter, and he closes the tab to be back on it (2026-08-27: "i'd
// prefer when we start new sessions like this we open a new tab. that way
// the recommendations (which have since changed) are still open and i can
// get to them by closing the tab instead of having to hit back").
//
// The button pressed is accepted|declined, or nothing at all: that is Reply,
// which posts to /reply instead — same note, same destination, no verdict.
//
// NO PICKER, NO JUMP (the desktop since 2026-10-08, ported 10-11): the answer
// goes to the session that filed the rec — a new one when none did — and he
// stays on the page; the rec's "its chat ›" is the way in when he wants it.
async function recSend(r, p) {
  const deliver = recSourceThread(r) ? 'source' : 'new';
  // The picture goes IN the decision or reply — the same message as the
  // note. No timing fields: a rec answer goes now (2026-09-09).
  const body = { by: OWNER, note: p.text, deliver, attachments: p.refs };
  let out;
  if (!p.outcome) {
    out = await post(`/recs/${r.id}/reply`, body);
  } else {
    body.status = p.outcome;
    out = await post(`/recs/${r.id}/decide`, body);
  }
  const label = p.outcome || 'sent';
  toast(out && out.delivery_error ? `${label}, but: ${out.delivery_error}` : label);
}

// ---- one rec: the whole record, including how it turned out. The chat that
// filed it is a button under the pills, not a muted link among them
// (2026-08-27: "a 'click into chat that created' link / button… on both mobile
// + web") — reading the reasoning should not cost a decision. ----
const OUTCOMES = [['worked', 'It worked'], ['mixed', 'Mixed'], ['failed', 'It failed'], ['unclear', 'Too early / unclear']];
async function recDetail(view, id) {
  const back = `<a href="#/recs">‹ Recommendations</a>`;
  const draw = async () => {
    const r = await get('/recs/' + encodeURIComponent(id));
    const proposed = !r.closed;
    const decided = r.status === 'accepted' || r.status === 'done';
    const links = String(r.links || '').split(',').map(s => s.trim()).filter(Boolean);
    // `links` are bare ids (the column predates refs): refOf names their kind.
    const linkHref = id => refHref(refOf(id));
    // Read top to bottom, as the desktop's page (2026-10-08:
    // "the details page is just really dense… the chat bar needs to be much
    // more prominent… this chat bar should go up here"): back, the title and
    // its cost, one muted line of facts, the chat bar, then the record under
    // plain headings as text in an 860 column, no boxes. A section with
    // nothing to say is not drawn.
    const section = (label, body) => body ? `<div class="rec-sec"><h3>${label}</h3><div>${body}</div></div>` : '';
    const field = (label, text) => text ? section(label, md(text)) : '';
    const src = recSourceThread(r);
    const scored = !!r.outcome || decided;
    if (paint(view, `<div class="pane wide rec-col rec-page">
      <div class="small">${back}</div>
      <div class="spread" style="margin:10px 0 8px">
        <h2 style="margin:0">${mdInline(r.title)}</h2>
        <span class="small">${costLabel(r)}</span></div>
      <div class="row small muted" style="flex-wrap:wrap;gap:8px">
        ${pill(r.kind)}
        ${r.model ? `<span class="pill" title="${esc(r.model)}">${esc(modelShort(r.model))}</span>` : ''}
        ${recPills(r)}
        ${recRunning(r)}
        ${recDates(r)}
        ${r.goal_id ? `<a href="#/goals/${esc(r.goal_id)}">${esc(r.goal_id)}</a>` : ''}
        ${src ? `<a href="#/sessions/${esc(src)}" title="The session that filed it">its chat ›</a>` : ''}
      </div>

      ${proposed ? `<div class="card rec decide" style="margin-top:18px">${recDecideBox(r)}</div>` : ''}

      ${field('The recommendation', r.detail)}
      ${field('Why', r.because)}
      ${field('If it works', r.expect)}

      ${r.status === 'proposed' ? '' : section('Decision', `<strong>${esc(r.status === 'deferred' ? 'later — back on ' + dayLabel(r.review_on) : r.status)}</strong>
          <span class="muted">${r.decided_at ? ' · ' + when(r.decided_at) : ''}${r.decided_by ? ' · by ' + esc(r.decided_by) : ''}</span>
          ${r.decision_note ? `<div style="margin-top:6px">${md(r.decision_note)}</div>` : ''}`)}

      ${!scored ? '' : section('Outcome', `${r.outcome ? `<strong>${esc(r.outcome)}</strong><span class="muted">${r.outcome_at ? ' · ' + when(r.outcome_at) : ''}</span>
            ${r.outcome_note ? `<div style="margin-top:6px">${md(r.outcome_note)}</div>` : ''}`
        : `<span class="muted">Not scored yet${r.review_on ? ' — due ' + esc(dayLabel(r.review_on)) : ''}</span>`}
        ${decided ? `<div class="row" style="flex-wrap:wrap;margin-top:8px">
          ${OUTCOMES.map(([o, label]) => `<button class="sm" onclick="scoreRec('${esc(r.id)}','${o}')">${label}</button>`).join('')}
        </div>
        <textarea class="rscore" rows="1" placeholder="What actually happened (optional)" style="margin-top:8px"></textarea>` : ''}`)}

      ${links.length ? section(`What ${r.status === 'deferred' ? 'deferring' : 'accepting'} it minted`,
        links.map(ref => linkHref(ref) ? `<a href="${linkHref(ref)}" class="mono">${esc(ref)}</a>` : `<span class="mono">${esc(ref)}</span>`).join(' · ')) : ''}
      ${r.prev_id ? `<div class="small muted" style="margin-top:8px">Supersedes <a href="#/recs/${esc(r.prev_id)}">${esc(r.prev_id)}</a>.</div>` : ''}
      <div class="small muted" style="margin-top:28px">${esc(r.id)} · filed ${when(r.created_at)} by ${esc(r.source || 'unknown')}${r.model ? ' · ' + esc(r.model) : ''}</div>
    </div>`)) wireRecNotes(view);
  };
  setRedraw(draw, view);
  await draw();
}

async function scoreRec(id, outcome) {
  const box = document.querySelector('.rscore');
  // A failed score keeps his note in the box (no repaint).
  if (await act(() => post(`/recs/${id}/score`, { outcome, by: OWNER, note: (box ? box.value : '').trim() }), 'scored ' + outcome, null)) await pageRedraw();
}
