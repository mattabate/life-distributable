// life hub — laptop console: Goals (list, one goal, its notes). views.goals.
'use strict';
// ================= goals =================
// The goals are the system's long-term memory: every session writes what it
// learned to one, and the sessions themselves create and edit them. The owner
// reads here. The one thing they still edit by hand is the name and the
// statement the sessions steer by, and whether the goal is active —
// everything else (the digest, the notes, new goals, horizon, sources) is the
// agent's.
//
// The list is a grid of small tiles, the name only, each wearing the goal's
// emblem — a symbol in the goal's own hue, both from the hub (`emblem`) so
// the phone draws the same — with the name sitting on the tile's floor, so a
// two-line name and a one-line name end on the same line.
// The statement, the note count and the status pill stay off the list; the
// goal's own page has them.
views.goals = {
  draw: (view, rest) => (rest[0] ? goalDetail(view, rest[0]) : goalList(view)),
  redraw: () => (route.redraw ? pageRedraw() : render()),
};

async function goalList(view) {
  const draw = async () => {
    const goals = await get('/goals');
    paint(view, `<div class="pane wide"><h2>Goals</h2>
      <div class="goal-grid">${goals.map(g => {
        const on = g.status === 'active';
        return `<a class="goal-tile${on ? '' : ' off'}" href="#/goals/${encodeURIComponent(g.id)}" style="--h:${goalHue(g)}">
          ${goalEmblem(g)}
          <span class="name">${mdInline(g.title, false)}</span>
          ${on ? '' : `<span class="state">${esc(g.status)}</span>`}
        </a>`;
      }).join('') || emptyHTML('No goals.')}</div>
    </div>`);
  };
  await draw();
  setRedraw(draw, view);
}

// The symbols a goal's emblem can name (shared/api.md `emblem.symbol`), as
// 24-unit stroke icons. A word the hub sends that is not here draws the
// target, the hub's own fallback.
const GOAL_ICONS = {
  health: '<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1-1.1a5.5 5.5 0 0 0-7.8 7.8l1 1L12 21l7.8-7.6 1-1a5.5 5.5 0 0 0 0-7.8z"/>',
  money: '<path d="M12 2v20"/><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/>',
  agent: '<path d="M12 3l1.9 5.6 5.6 1.9-5.6 1.9L12 18l-1.9-5.6L4.5 10.5l5.6-1.9z"/><path d="M19 15.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>',
  market: '<path d="M12.6 20.6l7.8-7.8a1 1 0 0 0 0-1.4L12.6 3.6A2 2 0 0 0 11.2 3H4a1 1 0 0 0-1 1v7.2c0 .5.2 1 .6 1.4l7.8 7.8a1.6 1.6 0 0 0 2.2.2z"/><circle cx="7.5" cy="7.5" r="1.2"/>',
  audience: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.9"/><path d="M16 3.1a4 4 0 0 1 0 7.8"/>',
  learn: '<path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>',
  art: '<path d="M12 22a10 10 0 1 1 9.2-6 3 3 0 0 1-2.8 2h-2.4a2 2 0 0 0-1.6 3.2l.4.6A1.4 1.4 0 0 1 13.7 22z"/><circle cx="7.5" cy="10.5" r="1.1"/><circle cx="12" cy="7" r="1.1"/><circle cx="16.5" cy="10.5" r="1.1"/>',
  goal: '<circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/>',
};

function goalHue(g) { return Math.round(Number(g.emblem?.hue) || 0) % 360; }

// The emblem: a tinted disc with the symbol, in the goal's hue (--h on the
// element that carries it). Off goals draw it grey via the tile's .off rule.
function goalEmblem(g) {
  const icon = GOAL_ICONS[g.emblem?.symbol] || GOAL_ICONS.goal;
  return `<span class="emblem" style="--h:${goalHue(g)}"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"
    stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icon}</svg></span>`;
}

// The goal's page: the head (name, statement, the switch), the digest, then
// the notes as one dated list — day and who wrote it down the left, the note
// on the right, a hairline between. No kind pill: the kind is a session's
// filing word, meaningless to the reader.
// The by-line is the session's title, linked, from the hub's `by`/`thread_id`.
async function goalDetail(view, id) {
  const draw = async () => {
    const [g, notes] = await Promise.all([
      get('/goals/' + encodeURIComponent(id)),
      get(`/goals/${encodeURIComponent(id)}/notes?limit=30`).catch(() => []),
    ]);
    // A live redraw keeps a half-typed edit.
    const typed = document.getElementById('g-title') ? goalEditValue() : null;
    paint(view, `<div class="pane wide goal-page">
      <a href="#/goals" class="crumb">← Goals</a>
      ${/* The head panel: name, statement, the active switch — the three
            things the owner still edits by hand. Edit swaps the panel for its
            form in place; the digest and the notes below are read-only. */''}
      <div class="card goal-head" id="ghead">${typed ? goalEditHTML(g, typed) : goalHeadHTML(g)}</div>

      <h3>Where this stands${g.digest_at ? ` <span class="when">updated ${ago(g.digest_at)}</span>` : ''}</h3>
      <div class="card goal-digest prose">${g.digest ? md(g.digest) : '<span class="muted">No digest yet.</span>'}</div>

      <h3>Notes</h3>
      <div class="card goal-notes">${(notes || []).map(n => `<div class="gnote">
        <div class="gnote-by"><span class="when">${noteDay(n.created_at)}</span>${noteBy(n)}</div>
        <div class="prose">${md(n.text)}</div>
      </div>`).join('') || emptyHTML('No notes yet.')}</div>
    </div>`);
  };
  await draw();
  setRedraw(draw, view);
}

// "Sep 24", with the year once it is not this one.
function noteDay(iso) {
  const d = new Date(iso);
  const opts = { month: 'short', day: 'numeric' };
  if (d.getFullYear() !== new Date().getFullYear()) opts.year = 'numeric';
  return d.toLocaleDateString(undefined, opts);
}

function noteBy(n) {
  const by = n.by || n.author || '';
  return n.thread_id ? `<a href="#/sessions/${encodeURIComponent(n.thread_id)}">${esc(by)}</a>` : esc(by);
}

function goalHeadHTML(g) {
  const active = g.status === 'active';
  return `<div class="row top">
      ${goalEmblem(g)}
      <h2 class="grow m0">${mdInline(g.title)}</h2>
      <label class="row small muted" style="gap:6px;flex:none">${esc(g.status)}
        <span class="switch"><input type="checkbox"${active ? ' checked' : ''}
          onchange="setGoalActive('${esc(g.id)}', this.checked)"><span></span></span></label>
      <button class="sm" onclick="editGoal()">Edit</button>
    </div>
    <div class="prose statement">${g.statement ? md(g.statement) : '<span class="muted">No statement yet.</span>'}</div>
    <div class="small muted meta">${esc(g.horizon)} · ${esc(g.cadence)} review</div>`;
}

// The form is the head panel's own contents, not a second panel: the title
// box sits where the title was, the statement box where the statement was.
function goalEditHTML(g, v) {
  return `<label class="field"><span>Name</span>
      <input type="text" id="g-title" value="${esc(v.title)}"></label>
    <label class="field" style="margin-bottom:0"><span>Statement — what the sessions steer by</span>
      <textarea id="g-statement" rows="6">${esc(v.statement)}</textarea></label>
    <div class="row mt10">
      <span class="grow"></span>
      <button class="sm" onclick="pageRedraw()">Cancel</button>
      <button class="primary sm" onclick="saveGoal('${esc(g.id)}')">Save</button>
    </div>`;
}

function goalEditValue() {
  const val = x => (document.getElementById('g-' + x) || {}).value ?? '';
  return { title: val('title').trim(), statement: val('statement').trim() };
}

// Swap the head panel for its form without a round trip: the goal it shows is
// the one on screen, so the values come from the DOM the panel was drawn from.
function editGoal() {
  const head = document.getElementById('ghead');
  const title = head.querySelector('h2')?.textContent ?? '';
  const id = decodeURIComponent(location.hash.split('/')[2] || '');
  get('/goals/' + encodeURIComponent(id)).then(g => {
    head.innerHTML = goalEditHTML(g, { title: g.title || title, statement: g.statement || '' });
    document.getElementById('g-title').focus();
  }).catch(e => toast(e.message));
}

async function saveGoal(id) {
  const body = goalEditValue();
  if (!body.title) { toast('a goal needs a name'); return; }
  // The id is a slug of the title made when the goal was created, so a
  // rename keeps the id and every note stays attached — nothing to redirect.
  // A failed save keeps the form open with their words (no repaint).
  if (await act(() => patch(`/goals/${encodeURIComponent(id)}`, body), 'goal saved', null)) await pageRedraw();
}

// The switch is the whole status control: on = active, off = paused. A goal a
// session closed as done reads "done" with the switch off; flipping it on
// reopens it.
async function setGoalActive(id, on) {
  return act(() => patch(`/goals/${encodeURIComponent(id)}`, { status: on ? 'active' : 'paused' }));
}
