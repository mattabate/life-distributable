// life hub — laptop console: Recommendations (the list, one rec, deciding and
// scoring). views.recs.
'use strict';
// ================= recommendations =================
// The pull half of the system (docs/design/recommendations.md). An ask pushes
// — it is the owner's turn, now. A rec waits: it notifies nobody (no push, no
// document.title count — the nav badge is a count of what is sitting here, not
// a summons) and never appears on "Your turn", because the only moment a "want this from
// your Amazon cart?" is welcome is the moment the owner came looking. Needs
// action and recommendations are separate concepts. So this page is a browse
// surface, and the one thing it must make effortless is the DECLINE NOTE: the
// owner's reason is taste, taste is what the next session has to read before
// it recommends the same thing again, and a reason hidden behind a second
// click never gets typed.
//
// There is no track-record header. The numbers still exist — `lifectl recs
// stats` reads /recs/stats — and this page takes only the due-to-score line
// from it.
//
// TWO VIEWS, NOT SEVEN TABS, shaped like the sessions page: what needs action,
// then a table of everything at the bottom. The page is what is open;
// the one line at its foot opens every rec as a table (#/recs/all), each row
// saying its domain and where it ended up. The hub still filters by any
// status (`/recs?status=…`, `lifectl recs <status>`).
const RECS_DOMAINS = ['', 'money', 'health', 'audience', 'tools', 'home', 'other'];
const recsState = { domain: '' };

views.recs = {
  draw: (view, rest) => (rest[0] === 'all' ? recsAll(view) : rest[0] ? recDetail(view, rest[0]) : recsPage(view)),
  redraw: () => (route.redraw ? pageRedraw() : render()),
};

// modelShort and dayLabel moved to ui.js (2026-08-28), DOM_PILL and costLabel
// too (2026-09-18: the chat's rec card draws them) — a helper two pages read
// is a shared shape, not a Recs detail.
const STATUS_PILL = { proposed: 'amber', deferred: 'purple', accepted: 'running', done: 'done', declined: '', expired: '', superseded: '' };
const OUTCOME_PILL = { worked: 'done', mixed: 'amber', failed: 'needs', unclear: '' };

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
    // the words either way; this keeps the cursor where it was.
    const ae = document.activeElement;
    if (again && ae && ae.tagName === 'TEXTAREA' && ae.closest('[data-c]')) return;
    const [d, st] = await Promise.all([get(recsQuery()), get('/recs/stats').catch(() => null)]);
    const every = d.recs || [];
    const list = every.filter(r => r.open);
    if (paint(view, `<div class="pane wide">
      <div class="spread" style="margin-bottom:12px"><h2 style="margin:0">Recommendations</h2>${recsDomainSelect()}</div>
      ${list.map(recCard).join('') || emptyHTML(`Nothing open${recsState.domain ? ' in ' + recsState.domain : ''}.`)}
      ${st && st.unscored_due?.length ? `<div class="small muted" style="margin-top:10px">${st.unscored_due.length} accepted rec${st.unscored_due.length === 1 ? '' : 's'} due to be scored</div>` : ''}
      ${every.length ? `<a class="all-link" href="#/recs/all"><span>All recommendations</span><span class="muted">${every.length}</span></a>` : ''}
    </div>`)) wireRecNotes(view);
  };
  setRedraw(() => draw(true), view);
  await draw();
}

const recsDomainSelect = () => `<select style="max-width:170px" onchange="recsFilter('domain',this.value)">
  ${RECS_DOMAINS.map(dm => `<option value="${dm}"${dm === recsState.domain ? ' selected' : ''}>${dm || 'all domains'}</option>`).join('')}</select>`;
const recsQuery = () => '/recs?status=all' + (recsState.domain ? `&domain=${encodeURIComponent(recsState.domain)}` : '');

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
        <div class="row" style="gap:8px"><input type="text" id="recs-q" placeholder="Find a rec…" value="${esc(recsAllQuery)}" style="max-width:240px" autocomplete="off">${recsDomainSelect()}</div>
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
            const word = r.status === 'deferred' ? 'later' : r.status === 'proposed' ? 'open' : r.status;
            return `<tr onclick="location.hash='${href}'">
              <td class="all-t"><a href="${href}"${r.status === 'proposed' ? ' class="unread"' : ''}>${mdInline(r.title, false)}</a></td>
              <td>${pill(r.domain, DOM_PILL[r.domain])}</td>
              <td class="all-p">${pill(word, STATUS_PILL[r.status])}${r.outcome ? pill(r.outcome, OUTCOME_PILL[r.outcome]) : ''}</td>
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

function recsFilter(k, v) { recsState[k] = v; pageRedraw(); }

// THREE BANDS, NOT FIVE: a card of five horizontal bars was too busy to read.
// What is left is:
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
// composerActionsHTML). A decided rec has no box, so it is head + the owner's note.
function recCard(r) {
  // A deferred rec keeps its box: the owner can decide it early — the hub's
  // standing calls it neither open nor closed. (Nothing can park one any
  // more — Later is gone from both surfaces.)
  const proposed = !r.closed;
  const why = mdPreview(r.because || r.detail || '');
  return `<div class="card rec" id="rec-${esc(r.id)}">
    <div class="rec-head">
      <div class="spread">
        <strong><a href="#/recs/${esc(r.id)}" style="color:inherit;text-decoration:none">${mdInline(r.title, false)}</a></strong>
        <span class="small" style="white-space:nowrap">${costLabel(r)}</span>
      </div>
      <div class="row small muted" style="flex-wrap:wrap;gap:8px;margin-top:8px">
        ${pill(r.domain, DOM_PILL[r.domain])}
        ${pill(r.kind)}
        ${r.model ? `<span class="pill" title="${esc(r.model)}">${esc(modelShort(r.model))}</span>` : ''}
        ${r.status === 'proposed' ? '' : pill(r.status === 'deferred' ? 'later' : r.status, STATUS_PILL[r.status])}
        ${r.outcome ? pill(r.outcome, OUTCOME_PILL[r.outcome]) : ''}
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

// The whole decision, in one box. What the owner types is their note on the
// record AND the message the
// agent gets — accepted or declined, both go the same way — so there is no
// second screen and no second box between the card and the session.
//
// THREE answers, and each of them is a button that sends: Accept · Decline · Reply, in the footer band where Send used to
// sit. The note and the destination (its session / a new one) are the same for
// all three — every answer reaches an agent the same way, a reply just carries
// no verdict — so there is nothing left for a chip strip to say.
//
// LATER IS GONE. It was a fourth answer (park the rec until a day); it never
// earned its two extra controls — a chip and a date field — on a busy card, and the hub still takes `status: deferred` for a
// caller that wants it (`lifectl rec <id> defer <date>`), so the primitive and
// the one parked rec survive with no UI to make more.
//
// A press over an EMPTY box does not decide: it renames itself "Send with no
// note" and waits for a second press (`confirmEmpty`, composer.js). Reply is
// different — a reply with nothing in it goes nowhere, so it asks for words
// rather than confirming.
//
// The box is THE composer (composer.js), the same one the chat and the
// calendar draw, so the pictures and the destination come for free, with
// no duplicate code. recReplyOf
// (threads.js) still makes the card it answers — the chat's copy of this box
// keeps its chips, since there the answers cannot live in a shared footer.
//
// No time select here: nobody schedules a message to a recommendation for
// later. The hub's `/decide` and `/reply` still take the prompt
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
    outcomes: r.outcomes,
    targets: src ? [['this', 'Reply in its session'], ['new', 'Reply in a new session']]
      : [['new', 'Reply in a new session']],
    placeholder: 'Your note — it is kept on the record and sent to the agent, whichever button you press.',
    redraw: () => pageRedraw(),
    after: async () => { refreshBadges(); if (route.redraw) await pageRedraw(); else render(); },
    send: p => recSend(r, p),
  });
}

// The session that filed it is working right now, so there is nothing to
// follow up on, and the card says "running". The hub stamps `thread_running` on every rec it serves; this is
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

// Deciding is not the end of it: the answer goes to an agent, which is what
// mints the calendar items, proposals and asks that carry an accepted rec out
// (their ids land in `links`) and what records a decline where a future
// session will read it. One post does both — decide and deliver — and opens
// the session that got it in a NEW TAB, so this tab stays on the list, one
// rec shorter, and closing the tab is the way back to it — no Back needed.
//
// The button pressed is accepted|declined, or nothing at all: that is Reply,
// which posts to /reply instead — same note, same destination, no verdict.
async function recSend(r, p) {
  const src = recSourceThread(r);
  const deliver = (p.target === 'new' || !src) ? 'new' : 'source';
  // The tab is opened HERE, inside the click (composerSend runs this far
  // synchronously): a window.open after the await is a popup to Safari and
  // gets blocked. A blocked or closed tab falls back to the in-place jump.
  const tab = deliver ? openTab() : null;
  try {
    // The picture goes IN the decision or reply — the same message as the
    // note. No timing fields: a rec answer goes now.
    const body = { by: 'owner', note: p.text, deliver, attachments: p.refs };
    let out;
    if (!p.outcome) {
      out = await post(`/recs/${r.id}/reply`, body);
    } else {
      body.status = p.outcome;
      out = await post(`/recs/${r.id}/decide`, body);
    }
    const label = p.outcome || 'sent';
    toast(out && out.delivery_error ? `${label}, but: ${out.delivery_error}` : label);
    if (out && out.session_id) {
      const to = sessionURL(out.session_id);
      if (tab && !tab.closed) tab.location.href = to; else location.hash = to.slice(to.indexOf('#'));
    } else if (tab) {
      tab.close();
    }
  } catch (e) {
    tab && tab.close();
    throw e;
  }
}

// A blank tab of our own origin, or null when the browser refused it.
function openTab() {
  try { return window.open('', '_blank'); } catch (e) { return null; }
}

// The console's own address for a session, absolute so a fresh tab lands on
// the same console with the same cookie.
function sessionURL(id) {
  return location.origin + location.pathname + '#/sessions/' + encodeURIComponent(id);
}

// ---- one rec: the whole record, including how it turned out. The chat that
// filed it is a button under the pills, not a muted link among them, on
// both surfaces — reading the reasoning should not cost a decision. ----
const OUTCOMES = [['worked', 'It worked'], ['mixed', 'Mixed'], ['failed', 'It failed'], ['unclear', 'Too early / unclear']];
async function recDetail(view, id) {
  const draw = async () => {
    const r = await get('/recs/' + encodeURIComponent(id));
    const proposed = !r.closed;
    const decided = r.status === 'accepted' || r.status === 'done';
    const links = String(r.links || '').split(',').map(s => s.trim()).filter(Boolean);
    // `links` are bare ids (the column predates refs): refOf names their kind.
    const linkHref = id => refHref(refOf(id));
    const field = (label, body) => body ? `<h3>${label}</h3><div class="card"><div class="small">${md(body)}</div></div>` : '';
    if (paint(view, `<div class="pane wide">
      <div class="small"><a href="#/recs">‹ Recommendations</a></div>
      <div class="spread" style="margin:6px 0 10px">
        <h2 style="margin:0">${mdInline(r.title)}</h2>
        <span class="small">${costLabel(r)}</span></div>
      <div class="row small muted" style="flex-wrap:wrap;gap:8px;margin-bottom:12px">
        ${pill(r.domain, DOM_PILL[r.domain])}
        ${pill(r.kind)}
        ${r.model ? `<span class="pill" title="${esc(r.model)}">${esc(modelShort(r.model))}</span>` : ''}
        ${pill(r.status, STATUS_PILL[r.status])}
        ${r.outcome ? pill(r.outcome, OUTCOME_PILL[r.outcome]) : ''}
        ${recRunning(r)}
        <span>${esc(r.effort)} effort · ${r.confidence}% confident when filed</span>
        ${recDates(r)}
        ${r.goal_id ? `<a href="#/goals/${esc(r.goal_id)}">${esc(r.goal_id)}</a>` : ''}
      </div>
      ${recSourceThread(r) ? `<div class="row" style="margin:-4px 0 12px"><a class="btn sm" href="#/sessions/${esc(recSourceThread(r))}">Open the chat that filed this ›</a></div>` : ''}

      ${field('What it is', r.detail)}
      ${field('Why — the evidence behind it', r.because)}
      ${field('What should change if it works', r.expect)}

      ${proposed ? `<div class="card decide">${recDecideBox(r)}</div>` : ''}

      <h3>Decision</h3>
      <div class="card small">
        ${r.status === 'proposed' ? '<span class="muted">Not decided yet.</span>' : `<strong>${esc(r.status === 'deferred' ? 'later — back on ' + dayLabel(r.review_on) : r.status)}</strong>
          <span class="muted">${r.decided_at ? ' · ' + when(r.decided_at) : ''}${r.decided_by ? ' · by ' + esc(r.decided_by) : ''}</span>
          ${r.decision_note ? `<div style="margin-top:6px">${md(r.decision_note)}</div>` : ''}`}
      </div>

      <h3>Outcome</h3>
      <div class="card small">
        ${r.outcome ? `<strong>${esc(r.outcome)}</strong><span class="muted">${r.outcome_at ? ' · ' + when(r.outcome_at) : ''}</span>
            ${r.outcome_note ? `<div style="margin-top:6px">${md(r.outcome_note)}</div>` : ''}`
        : `<span class="muted">Not scored yet${r.review_on ? ' — due ' + esc(dayLabel(r.review_on)) : ''}</span>`}
        ${decided ? `<div class="row" style="flex-wrap:wrap;margin-top:8px">
          ${OUTCOMES.map(([o, label]) => `<button class="sm" onclick="scoreRec('${esc(r.id)}','${o}')">${label}</button>`).join('')}
        </div>
        <textarea class="rscore" rows="1" placeholder="What actually happened (optional)" style="margin-top:8px"></textarea>` : ''}
      </div>

      ${links.length ? `<h3>What ${r.status === 'deferred' ? 'deferring' : 'accepting'} it minted</h3><div class="card small">
        ${links.map(ref => linkHref(ref) ? `<a href="${linkHref(ref)}" class="mono">${esc(ref)}</a>` : `<span class="mono">${esc(ref)}</span>`).join(' · ')}</div>` : ''}
      ${r.prev_id ? `<div class="small muted">Supersedes <a href="#/recs/${esc(r.prev_id)}">${esc(r.prev_id)}</a>.</div>` : ''}
      <div class="small muted" style="margin-top:10px">${esc(r.id)} · filed ${when(r.created_at)} by ${esc(r.source || 'unknown')}${r.model ? ' · ' + esc(r.model) : ''}</div>
    </div>`)) wireRecNotes(view);
  };
  setRedraw(draw, view);
  await draw();
}

async function scoreRec(id, outcome) {
  const box = document.querySelector('.rscore');
  // A failed score keeps the note in the box (no repaint).
  if (await act(() => post(`/recs/${id}/score`, { outcome, by: 'owner', note: (box ? box.value : '').trim() }), 'scored ' + outcome, null)) await pageRedraw();
}
