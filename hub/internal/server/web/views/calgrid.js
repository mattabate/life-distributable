// life hub — laptop console: the Calendar page drawn as a real calendar.
'use strict';
// ================= calendar: the grid =================
// A real calendar UI in the style of Google Calendar: drag things to change
// their time, with a place for the anytime tasks.
//
// So the page is a real week grid; a click opens `calEntryHTML`, the agenda's
// own row (full text, the owner's step composer, Done · Won't do · Reopen).
//
// What is actually draggable, and why the rest is not: a `cal:` row is an
// item, and moving it is `PATCH /calendar/{id} {day,at}` — one row, one date.
// A `run` row is not an item at all: it is a session's standing check-in
// PROJECTED onto a day, so moving one occurrence is meaningless and the drag
// rewrites the thread's whole cadence instead (with a confirm that says so).
// A `job` row lives in ops/schedule.json, which has no API, and an
// `action`/`rec`/`ask` row has no date of its own to move.

// ONE MODE: the week. Day, Month, Schedule and Just mine are gone; the lanes'
// ticks in the rail are the only filter.

// FEW COLOURS: red for the owner's tasks, grey for agent actions, purple for
// recommendations — so "what do I have to do today" reads at a glance.
//
// So every row belongs to one LANE, and the lane is both its colour
// and the one checkbox that hides it:
//   red    — THE OWNER'S: a dated step, an ask, a reminder, and an action still
//            waiting on their approval (a pending approval is their turn, not the
//            agent's, so it is red on this page the way it is red on the board).
//   blue-grey — SCHEDULED RUNS: a prompt an agent wrote to be sent to a
//            session at a minute (an `agent` item, a one-shot prompt wake).
//   grey   — the agents': session check-ins, jobs, and the trail of actions
//            already decided.
//   purple — a rec, its whole life: the minute it was filed ("Filed: …",
//            dark), the minute it was answered ("Accepted: …" / "Declined: …",
//            light), the day the owner asked to be asked again ("Check back: …").
// No colour-by-goal: a palette per goal was mostly noise. A decided action is
// grey. No tick hides the closed rows: this is a calendar, so the past always
// shows — the past is the record, and no tick deletes it.
// HOMEWORK is the fourth lane, light blue (e.g. a practice round from Learn).
// A `homework` cal item is a step of THE OWNER'S — the hub fires it as
// a physical ask, nags it, stacks it in Overdue, makes them close it with
// words — so `owner` marks both lanes. What differs is what the step is FOR: a
// red step unblocks an agent, a blue one makes the owner smarter.
// SCHEDULED RUNS is the fifth lane, its own shade of grey with its own
// checkbox: prompts sent at a set time by an agent, not by the owner. A
// scheduled run is a prompt an AGENT wrote to be delivered to a session at a
// minute — an `agent` cal item, or a one-shot `lifectl prompt` wake (a `run`
// row whose ref is `prompt:`). Blue-grey, so it reads apart from the plain
// grey of Agent runs, which keeps the standing check-ins (`run` with a
// `thread:` ref), the hub's jobs, decided actions and the agents' record.
// WHICH lane a row is in is the hub's call (`lane` on every entry, calendar.go
// `stampEntry`) — this table only says what
// each lane looks like and is called. `CalCals.all` in CalendarGrid.swift is
// the same six.
// CHORES is the sixth: a dated step of the owner's that no session is behind
// — an errand, watering the plants, a renewal. It is theirs but unblocks
// nothing, so it reads apart from
// the red of a step a session waits on. Orange, closer to red than to grey.
const CAL_LANES = [
  { key: 'mine', label: 'My tasks', color: '#d93025', owner: true },
  { key: 'chores', label: 'Chores', color: '#e8710a', owner: true },
  { key: 'homework', label: 'Homework', color: '#039be5', owner: true },
  { key: 'scheduled', label: 'One-off runs', color: '#78909c' },
  { key: 'agents', label: 'Recurring runs', color: '#5f6368' },
  { key: 'recs', label: 'Recs', color: '#8e24aa' },
];
const CAL_LANE_BY_KEY = Object.fromEntries(CAL_LANES.map(l => [l.key, l]));
const CAL_MINE = CAL_LANE_BY_KEY.mine, CAL_HOMEWORK = CAL_LANE_BY_KEY.homework, CAL_SCHEDULED = CAL_LANE_BY_KEY.scheduled,
  CAL_AGENTS = CAL_LANE_BY_KEY.agents, CAL_RECS = CAL_LANE_BY_KEY.recs;
// Two shades of the one purple, because a rec has a LIFE and both ends of it
// belong on the calendar: when it was filed and when it was answered. Dark =
// still asking. Light = answered,
// and a declined one wears ✕ and the line, like every other refusal here.
const CAL_REC_DONE = '#ce93d8';
// The past is a RECORD, not a plan: the future is prospective events, the
// past is a portfolio of steps done. A `did` row is one line of it —
// a card read, a decision, a grant, an approval, a rec accepted or
// scored, a step closed, an action the gate let through — placed at the
// minute it happened, with the verb for what happened. Its lane is its DOER's
// (the hub stamps it): the owner's hands red, the hub's and a session's grey.
// No row is a session run with its time.
// A did row's title reads as a deed: "Read: the bank feed costs $15/yr",
// "Approved: move $500", "Accepted: try a new search engine".
const calLabel = e => (e.verb ? e.verb + ': ' : '') + (e.title || '');

// THREE FORMS, THREE PLACES. A deed, a plan and a rec must be visually
// distinct, and colour alone could not carry it: when everything was a bar in
// the same grid, a deed and a plan read the same. So the SHAPE says which of
// three things a row is, and each has its own place on the page:
//
//   PLAN        — the grid, and only the grid. A time the owner or an agent
//                 chose: an item, a check-in, a job, an approval waiting on
//                 them. A closed item keeps its planned slot, ✓ and muted: its
//                 time is still the time planned. This is what a bar means.
//   RECORD      — a narrow strip down the right edge of each day, one mark per
//                 thing that happened at the minute it happened, the owner's
//                 marks and the agents' in their own column of the strip. No
//                 text, no bar: "what was I doing at any given time" is a shape
//                 you read at a glance and click to open, not slivers of prose.
//   RECS        — TWO BARS in the clock per recommendation, purple: a dark one
//                 at the minute it was filed and a light one at the minute it
//                 was answered (declined struck). A rec is a PLAN-shaped row —
//                 a bar at its real time, never an all-day event or a strip
//                 mark — and only a rec with no minute (a "Check back" on its
//                 review day) is an all-day chip.
//
// A closed cal item is NOT a record row: its day and time are the plan's, so
// it stays a bar. Everything else `did` (a card read, an action approved or
// run) has no time but the moment it happened.
const CAL_ITEM_KINDS = new Set(['owner', 'homework', 'agent', 'note']);
// A step of THE OWNER'S: the hub's `owner` and `homework` kinds — both fire a physical
// ask, both close with words, both are bold with a ○ in front.
const calIsOwner = e => e.kind === 'owner' || e.kind === 'homework';
const calIsRec = e => e.kind === 'rec';
const calIsRecord = e => !!e.did && !calIsRec(e) && !CAL_ITEM_KINDS.has(e.kind);
const calIsPlan = e => !calIsRecord(e);
// The shade a row is drawn in: its lane's colour, except a rec that has
// been answered, which is the pale purple with dark ink. The two rec classes let
// the CSS give the light bar its ink and keep it at full opacity — the shade
// IS its "closed", a faded pale purple would be a third colour.
const calRecDone = e => calIsRec(e) && !!e.did;
// An install ask is teal wherever it is drawn — its own kind of cell — open
// in Anytime or as the "Installed: build N" mark in the strip. It stays in
// the owner's lane — it is their tap — the teal is the cell's, as in the chat
// (app.css --teal, the same value).
const CAL_INSTALL = '#0d9488';
const calIsInstall = e => e.ask_kind === 'install' && (e.kind === 'ask' || !!e.did);
const calShade = e => (calRecDone(e) ? CAL_REC_DONE : calIsInstall(e) ? CAL_INSTALL : calColor(e));
const calRecCls = e => (calIsRec(e) ? (calRecDone(e) ? ' rec decided' : ' rec') : '');
// The id a pile opens under: one member opens its own card, many open the
// popout that lists them.
const calPileID = es => es.length > 1 ? 'grp:' + es.map(m => m.id).join(',') : es[0].id;

const CAL_HOUR_H = 44;   // px per hour in the time grid
const CAL_SLOT_MIN = 30; // an item is a moment, not a span; 30 min is the block we draw it as
const CAL_TOUCH_MIN = 5; // two blocks overlapping by less than this are side by side, not stacked (calLayout span)
const CAL_SNAP = 15;     // drag snaps to the quarter hour, like Google

// Local days are ui.js's localDate/ymd/addDays/todayYMD; these are the grid's minutes.
const calMin = at => { if (!at) return null; const [h, m] = String(at).split(':').map(Number); return h * 60 + m; };
const calHHMM = mins => `${String(Math.floor(mins / 60)).padStart(2, '0')}:${String(mins % 60).padStart(2, '0')}`;
const CAL_DOW = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

// ---------- page state ----------
// Persisted so the page comes back the way it was left; `open` (the detail
// panel) and `anchor` are of the moment and are not.
// A closed row always shows: the agenda keeps a done row on its day, ✓ and
// muted — it should LOOK done, not disappear — and no tick hides them; a
// saved `showClosed:false` from before is ignored.
// `anchor` '' = the hub's today: the owner has not moved off it.
const calState = {
  anchor: '', off: new Set(), open: '',
};
// TODAY IS THE HUB'S (cross-client audit 2026-09-27): the agenda's own
// `today`, computed once in the hub's zone, so a laptop on another clock never
// rings a different day than the phone. The device clock is only the first
// guess, before the first answer lands.
let calToday = '';
const calTodayYMD = () => calToday || todayYMD();
const calAnchor = () => calState.anchor || calTodayYMD();
function calStateLoad() {
  try {
    const s = JSON.parse(localStorage.getItem('calState') || '{}');
    // Absent keys keep the defaults above — an empty `{}` is a first visit,
    // not "they unticked everything". Keys from the six-calendar era ('log',
    // 'me', 'checkins'…) are not lanes and are dropped, so an old browser
    // opens with all four lanes on rather than with a lane stuck off.
    if (Array.isArray(s.off)) calState.off = new Set(s.off.filter(k => CAL_LANES.some(l => l.key === k)));
  } catch { /* first visit */ }
}
function calStateSave() {
  localStorage.setItem('calState', JSON.stringify({
    off: [...calState.off],
  }));
}

// ---------- what the page shows, and what we have to fetch ----------
function calVisible() {
  const a = calAnchor(), s = addDays(a, -localDate(a).getDay());
  return [s, addDays(s, 6)];
}
// The 6x7 block the rail's mini-month draws: Sunday on or before the 1st, 42 days.
function calMonthGrid(anchor) {
  const d = localDate(anchor), first = new Date(d.getFullYear(), d.getMonth(), 1);
  const s = ymd(new Date(d.getFullYear(), d.getMonth(), 1 - first.getDay()));
  return [s, addDays(s, 41)];
}
// One fetch covers the week AND the rail's mini-month.
function calFetchRange() {
  const [a, b] = calVisible(), [c, d] = calMonthGrid(calAnchor());
  return [a < c ? a : c, b > d ? b : d];
}

// ---------- filtering ----------
// The lane is the hub's `lane` (a rec purple its whole life, homework blue
// whoever closed it, a record its doer's, a pending approval the owner's, anything new
// grey — docs/design/calendar.md). A row without one is grey: a new row must
// never dilute the red.
const calLaneOf = e => CAL_LANE_BY_KEY[e.lane] || CAL_AGENTS;
const calColor = e => calLaneOf(e).color;
// "Closed" is the hub's `closed`: done, won't-do, accepted, a decided action,
// any record. It is a LOOK (✓ or ✕, muted), never a filter: the past always shows.
const calClosed = e => !!e.closed;
const calShown = e => !calState.off.has(calLaneOf(e).key);
// ---------- who can be dragged where ----------
// The hub's `move`: '' = nowhere. 'item' = a cal item still scheduled or
// fired (a drag on a fired one is a reschedule): PATCH day/at. 'run' = a
// session's daily@/weekly@ check-in: the drag rewrites the CADENCE. A fixed
// row carries `why`, the sentence it shows instead.
const calMovable = e => e.move || '';
const calWhyStuck = e => e.why || '';

// A repeating item's drag has two honest answers — one watering, or the
// whole chain — so it asks Google's question: only this one, or all future
// ones?
// Resolves 'one' | 'future' | null (cancelled).
function calScopeAsk(e, day, at) {
  return new Promise(res => {
    document.getElementById('cal-scope')?.remove();
    const w = document.createElement('div');
    w.id = 'cal-scope';
    const when = at ? `${day} at ${at}` : `${day}, all day`;
    w.innerHTML = `<div class="cal-scope-card">
      <h3>${esc(e.title)}</h3>
      <p>Repeats ${esc(e.repeat)}. Move to ${esc(when)} —</p>
      <button class="sm" data-v="one">Only this one</button>
      <button class="sm" data-v="future">This and all future</button>
      <button class="sm" data-v="">Cancel</button></div>`;
    w.addEventListener('click', ev => {
      const b = ev.target.closest('button');
      if (!b && ev.target !== w) return;
      w.remove();
      res(b?.dataset.v || null);
    });
    document.body.appendChild(w);
  });
}

// ================= the page =================
async function calDrawGrid(view, cal) {
  calScrollKeep(view);
  const [from, to] = calVisible();
  // The week's name between its own arrows, over the grid it moves — never a
  // second pair above the rail's.
  const chrome = `<div class="cal-bar">
      <button class="sm icon" onclick="calGo(-1)" title="Last week">‹</button>
      <h2 class="cal-title">${esc(calRangeLabel())}</h2>
      <button class="sm icon" onclick="calGo(1)" title="Next week">›</button>
      <button class="sm" onclick="calGo('today')">Today</button>
    </div>`;
  view.innerHTML = `<div class="pane wide cal-page">
    <div class="cal-body">${calRailHTML(cal)}<div class="cal-main">${chrome}${calTimeGridHTML(cal, from, to)}</div></div></div>
    ${calPanelHTML(cal)}`;
  calWire(view);
}

function calRangeLabel() {
  const [a, b] = calVisible();
  const fmt = (s, o) => localDate(s).toLocaleDateString([], o);
  const sameMonth = a.slice(0, 7) === b.slice(0, 7);
  return sameMonth
    ? `${fmt(a, { month: 'long' })} ${localDate(a).getDate()} – ${localDate(b).getDate()}, ${localDate(b).getFullYear()}`
    : `${fmt(a, { month: 'short', day: 'numeric' })} – ${fmt(b, { month: 'short', day: 'numeric', year: 'numeric' })}`;
}

// ---------- left rail: mini month, Show (3 lanes + closed), Overdue, Anytime ----------
// "Overdue" aggregates the open tasks in the past: every open step of the
// owner's whose moment has passed — yesterday's leftovers and earlier today —
// straight from the hub's `overdue` list, which is the phone tray's list too.
// The heading says what the hub calls it (a vaguer "For you" did not read as
// the backlog it is), and every row wears the day it slipped from.
function calRailHTML(cal) {
  const counts = calDayCounts(cal);
  // Anytime = the owner's undated to-dos (calendar items); Sessions waiting =
  // the open asks a live session is blocked on.
  const anytime = (cal.anytime || []).filter(e => calShown(e) && e.item);
  const waiting = (cal.anytime || []).filter(e => calShown(e) && !e.item);
  const overdue = (cal.overdue || []).filter(calShown);
  // Do soon: the owner's steps due on a LATER day (a weekly homework before
  // its day). Each row wears its day.
  const soon = (cal.soon || []).filter(calShown);
  // Due: what the owner owes today that has not slipped, so its count is
  // the tab's red number. Each row wears its day.
  const due = (cal.due || []).filter(calShown);
  return `<aside class="cal-rail">
    ${calMiniHTML(counts)}
    <div class="cal-railsec">
      <div class="cal-railhead">Show</div>
      ${CAL_LANES.map(c => `<label class="cal-check"><input type="checkbox" ${calState.off.has(c.key) ? '' : 'checked'}
        style="accent-color:${c.color}" onchange="calToggle('off','${c.key}')"><span style="color:${c.color};font-weight:600">${esc(c.label)}</span></label>`).join('')}
    </div>
    ${overdue.length ? `<div class="cal-railsec">
      <div class="cal-railhead" style="color:var(--red)">Overdue <span class="cal-n">${overdue.length}</span></div>
      ${overdue.map(e => calRailRow(e, true)).join('')}</div>` : ''}
    ${due.length ? `<div class="cal-railsec">
      <div class="cal-railhead">Due <span class="cal-n">${due.length}</span></div>
      ${due.map(e => calRailRow(e, true)).join('')}</div>` : ''}
    ${soon.length ? `<div class="cal-railsec">
      <div class="cal-railhead">Do soon <span class="cal-n">${soon.length}</span></div>
      ${soon.map(e => calRailRow(e, true)).join('')}</div>` : ''}
    ${waiting.length ? `<div class="cal-railsec">
      <div class="cal-railhead">Sessions waiting <span class="cal-n">${waiting.length}</span></div>
      ${waiting.map(e => calRailRow(e)).join('')}</div>` : ''}
    <div class="cal-railsec">
      <div class="cal-railhead">Anytime <span class="cal-n">${anytime.length}</span></div>
      ${anytime.length ? anytime.map(e => calRailRow(e)).join('')
        : ''}
    </div>
  </aside>`;
}

// An undated row opens the same panel a grid event does. It is never
// draggable: an "anytime" row is an open ASK, not a calendar item, and giving
// it a date is `lifectl cal add`, not a PATCH — so the affordance would lie.
// An overdue row carries its day (and minute) — "Tue 16 20:00" — because the
// rail is the one place a row sits away from its cell (`calWhenShort`, the
// phone's `calWhenShort` prints the same words).
function calRailRow(e, dated) {
  const when = dated ? `<span class="at">${esc(calWhenShort(e))}</span>` : '';
  return `<div class="cal-ev rail" data-id="${esc(e.id)}" style="--c:${calShade(e)}" title="${esc(e.title)}">
    <span class="dot"></span>${when}<span class="t">${esc(e.title)}</span></div>`;
}

// calWhenShort: "Tue 16" for an all-day row, "Tue 16 20:00" for a timed one —
// what a row away from its day needs to say about when it was due. Spelled
// by hand rather than by locale so the phone (`weekday(.abbreviated).day()`)
// and the console print the same words (a locale can put the number first).
function calWhenShort(e) {
  if (!e.day) return '';
  if (e.soon && !calClosed(e)) return calAdded(e);
  const d = localDate(e.day);
  const day = `${['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d.getDay()]} ${d.getDate()}`;
  return e.at ? `${day} ${e.at}` : day;
}

// calAdded: "added Sep 20" — what an open to-do says instead of a due day
// (the phone's `calAdded` prints the same words).
function calAdded(e) {
  const d = localDate(e.day);
  return `added ${['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][d.getMonth()]} ${d.getDate()}`;
}

function calDayCounts(cal) {
  const m = {};
  for (const d of cal.days || []) m[d.day] = (d.entries || []).filter(calShown).length;
  return m;
}

function calMiniHTML(counts) {
  const [gs] = calMonthGrid(calAnchor());
  const month = localDate(calAnchor()).getMonth(), today = calTodayYMD();
  const [va, vb] = calVisible();
  let cells = '';
  for (let i = 0; i < 42; i++) {
    const day = addDays(gs, i), d = localDate(day);
    const cls = [d.getMonth() === month ? '' : 'out', day === today ? 'today' : '',
      day >= va && day <= vb ? 'in' : ''].filter(Boolean).join(' ');
    cells += `<button class="${cls}" onclick="calJump('${day}')" title="${counts[day] || 0} on ${day}">${d.getDate()}</button>`;
  }
  return `<div class="cal-mini">
    <div class="cal-minihead"><span>${localDate(calAnchor()).toLocaleDateString([], { month: 'long', year: 'numeric' })}</span></div>
    <div class="cal-minidow">${CAL_DOW.map(d => `<span>${d[0]}</span>`).join('')}</div>
    <div class="cal-minigrid">${cells}</div></div>`;
}

// ---------- day / week: the plan grid and the record strip ----------
// Reading order down the page: what is all day (a dated row with no minute —
// a "Check back" rec among them), then the clock — bars for the plan and for
// both ends of every rec — with the day's record running down the right edge
// of its own column, beside the plan rather than inside it.
function calTimeGridHTML(cal, from, to) {
  const days = []; for (let s = from; s <= to; s = addDays(s, 1)) days.push(s);
  const byDay = {}; for (const d of cal.days || []) byDay[d.day] = (d.entries || []).filter(calShown);
  const today = calTodayYMD();
  const planOf = d => (byDay[d] || []).filter(calIsPlan);
  // A record with no minute has nowhere to sit on a strip; it falls back into
  // the all-day band rather than vanishing (nothing files one today).
  const bandOf = d => (byDay[d] || []).filter(e => !e.at);
  const recOf = d => (byDay[d] || []).filter(e => e.at && calIsRecord(e));
  const allDayRows = Math.max(1, ...days.map(d => bandOf(d).length));
  // The strip's width is reserved across the whole grid or none of it: columns
  // of two different widths in one week read as a rendering fault.
  const strip = days.some(d => recOf(d).length > 0);

  const head = days.map(d => {
    const dd = localDate(d);
    return `<div class="cal-dhead${d === today ? ' today' : ''}">
      <div class="dow">${CAL_DOW[dd.getDay()].toUpperCase()}</div>
      <div class="num">${dd.getDate()}</div></div>`;
  }).join('');

  const band = days.map(d => `<div class="cal-allday" data-day="${d}">${
    bandOf(d).map(calChipHTML).join('')}</div>`).join('');

  const hours = Array.from({ length: 24 }, (_, h) =>
    `<div class="cal-hour" style="height:${CAL_HOUR_H}px"><span>${h === 0 ? '' : calD3(h)}</span></div>`).join('');

  const cols = days.map(d => {
    const timed = planOf(d).filter(e => e.at);
    return `<div class="cal-col${d === today ? ' today' : ''}" data-day="${d}">
      ${Array.from({ length: 24 }, (_, h) => `<div class="cal-line" style="top:${h * CAL_HOUR_H}px"></div>`).join('')}
      <div class="cal-plan${strip ? ' inset' : ''}">
        ${calLayout(timed).map(p => calBlockHTML(p.e, p)).join('')}
        ${d === today ? calNowHTML() : ''}</div>
      ${strip ? calStripHTML(recOf(d)) : ''}</div>`;
  }).join('');

  // One scroller, sticky header rows inside it — never two scrollers side by
  // side, or the day headers drift off their columns by a scrollbar's width.
  const w = `grid-template-columns:repeat(${days.length}, minmax(0,1fr))`;
  const bandH = allDayRows * 21 + 8;
  return `<div class="cal-grid" id="cal-scroll"><div class="cal-inner">
    <div class="cal-corner head"></div>
    <div class="cal-cols head" style="${w}">${head}</div>
    <div class="cal-corner band small muted">All day</div>
    <div class="cal-cols band" style="${w};min-height:${bandH}px">${band}</div>
    <div class="cal-gut hours">${hours}</div>
    <div class="cal-cols body" style="${w};height:${24 * CAL_HOUR_H}px">${cols}</div>
  </div></div>`;
}

// (The Recs band — `calSuggHTML` / `calRecChipHTML`, a sticky row of purple
// chips above the clock — lived here 2026-09-09 → 09-11. A rec is a bar now,
// drawn by calBlockHTML / calChipHTML in its own shade via calShade.)

// The record strip: one mark per pile, at the minute it happened, the owner's marks in
// the left half and the agents' in the right, so the two answers to "what was
// happening then" never sit on top of each other. No words — the mark's
// tooltip has them, and the click opens the same popout a collapsed box does.
function calStripHTML(list) {
  if (!list.length) return '<div class="cal-strip"></div>';
  const marks = calCollapse(list).map(p => {
    const es = p.members, lane = calLaneOf(es[0]), mine = !!lane.owner;
    const top = (p.s / 60) * CAL_HOUR_H, h = Math.max(7, ((p.n - p.s) / 60) * CAL_HOUR_H - 2);
    return `<div class="cal-ev mark ${mine ? 'mine' : 'agents'}" data-id="${esc(calPileID(es))}"
      style="--c:${calShade(es[0])};top:${top}px;height:${h}px" title="${esc(calGroupTip(es))}"></div>`;
  }).join('');
  return `<div class="cal-strip" title="What happened — click a mark to read it">${marks}</div>`;
}

const calD3 = h => `${((h + 11) % 12) + 1} ${h < 12 ? 'AM' : 'PM'}`;

function calNowHTML() {
  const n = new Date(), mins = n.getHours() * 60 + n.getMinutes();
  return `<div class="cal-now" style="top:${(mins / 60) * CAL_HOUR_H}px"></div>`;
}

// A lane's pile collapses to ONE box instead of a row of unreadable slivers,
// while an owner's step at the same time as an agent's sits side by side. So
// the grouping never crosses lanes — the owner's step keeps its own box beside
// the agents' — and it chains through near-misses too: 10:00/10:15/10:30 in
// one lane, which used to overlap illegibly, become one box spanning the run.
// Two members is already a group. The box's label is the first member plus
// +N; the click opens a popout listing all of them (calGroupPanelHTML).
//
// The record collapses one level up, to the scale of a deliverable: did rows
// chain by lane AND session, and reach two hours, not thirty minutes — so the
// cards read and answered over an afternoon in one session become one box
// wearing that session's title, which is the deliverable. A chain never
// crosses lanes, so the owner's side of a session and the agent's sit side by
// side, and each hides with its own tick.
const CAL_DID_REACH = 120;
// A rec's two shades never chain into one box: a dark "filed" pile and a
// light "answered" pile at the same hour are two different things.
const calChainKey = e => calLaneOf(e).key + (calIsRec(e) ? (e.did ? ':answered' : ':filed') : e.did ? ':did:' + (e.thread_id || '') : '');
const calReach = e => (e.did && !calIsRec(e) ? CAL_DID_REACH : CAL_SLOT_MIN);
// A rec is a MOMENT, answered or not: its pile is one slot tall and holds only
// the recs filed (or answered) inside that slot — a later member never
// stretches the box or the chain (answers spread over hours must not chain
// into one tall box at the record's two-hour reach). The record proper still
// chains — an afternoon in one session is one deliverable.
const calChains = e => !calIsRec(e);
function calCollapse(list) {
  const evs = list.map(e => ({ e, s: calMin(e.at) })).sort((a, b) => a.s - b.s || a.e.id.localeCompare(b.e.id));
  const groups = [], open = {}; // chain key → the group still within reach
  for (const it of evs) {
    const key = calChainKey(it.e), g = open[key];
    // `n` is where the box ends (the last member's slot); `until` is how far
    // the chain still reaches — for a did row, further than the box is drawn.
    if (g && it.s < g.until) { g.members.push(it.e); if (calChains(it.e)) { g.n = it.s + CAL_SLOT_MIN; g.until = it.s + calReach(it.e); } }
    else open[key] = groups[groups.push({ e: it.e, members: [it.e], s: it.s, n: it.s + CAL_SLOT_MIN, until: it.s + calReach(it.e) }) - 1];
  }
  return groups.sort((a, b) => a.s - b.s);
}
// What a collapsed box is called: a session's run of steps wears the
// session's title; any other pile, its first member. `n` is the count the
// box shows beside it — "+2" more for a pile, "3 steps" for a run of record.
function calGroupLabel(es) {
  // A pile of answered recs is "Accepted: X +1", not "2 steps in <session>":
  // the session form is the RECORD's (an afternoon of cards), and a rec's
  // answer is about the rec, not the thread that filed it.
  const did = es.every(e => e.did) && !calIsRec(es[0]);
  const title = did && es[0].thread_title && es.every(e => e.thread_id === es[0].thread_id) ? es[0].thread_title : '';
  return title ? { title, n: `${es.length} steps` } : { title: calLabel(es[0]), n: `+${es.length - 1}` };
}

// Side-by-side layout for events at the same time — Google's rule, applied to
// a 30-minute block, because an item is a moment and has no length of its own.
// It lays out the COLLAPSED groups, so at one minute there is at most one box
// per lane, and only different lanes ever sit side by side.
function calLayout(list) {
  const evs = calCollapse(list);
  const out = [];
  let cluster = [], clusterEnd = -1;
  const flush = () => {
    if (!cluster.length) return;
    const colsEnd = [];
    for (const it of cluster) {
      let c = colsEnd.findIndex(end => end <= it.s);
      if (c < 0) { c = colsEnd.length; colsEnd.push(0); }
      colsEnd[c] = it.n; it.col = c;
    }
    // A box grows rightward over every column that is empty for its whole
    // height (a lone 09:30 under a four-wide 08:00 row fills its row):
    // `span` columns of `cols`, never fewer than one. A brush
    // under CAL_TOUCH_MIN (a 09:01 pile's last minute against a 09:30 step,
    // a hairline on the clock) does not count as taken.
    for (const it of cluster) {
      let span = 1;
      for (let c = it.col + 1; c < colsEnd.length; c++) {
        if (cluster.some(o => o.col === c && Math.min(o.n, it.n) - Math.max(o.s, it.s) > CAL_TOUCH_MIN)) break;
        span++;
      }
      out.push({ ...it, cols: colsEnd.length, span });
    }
    cluster = []; clusterEnd = -1;
  };
  for (const it of evs) {
    if (cluster.length && it.s >= clusterEnd) flush();
    cluster.push(it); clusterEnd = Math.max(clusterEnd, it.n);
  }
  flush();
  return out;
}

// One glyph language on every row the owner reads, both surfaces (✓ +
// strikethrough together read as both complete and not). Each mark says one
// thing:
//   ○              still to do — an open row of THE OWNER'S. Full colour, never
//                  struck: time passing never closes a task; only the owner or
//                  an agent does.
//   ✓              done. Muted but NOT struck — a check crossed out reads as
//                  its own opposite.
//   ✕ + strike     won't do / cleared. The only rows that get the line.
// `CalendarGrid.swift` (calGlyph) must keep saying the same three things.
// A record wears them too: what happened gets ✓ (read, decided, approved,
// accepted, ran, scored); what was refused or fell over — skipped, declined,
// denied, failed — gets ✕ and the line.
// Which of the three a row wears is the hub's: every entry carries `mark`
// (store.CalMark — "wont" | "done" | "todo" | none), so no list of refusal
// states lives here.
const CAL_MARK_GLYPH = { wont: '✕ ', done: '✓ ', todo: '○ ' };
const calGlyph = e => CAL_MARK_GLYPH[e.mark] || '';
// The strikethrough class: won't-do and its cousins only, per the vocabulary above.
const calWont = e => (e.mark === 'wont' ? ' wont' : '');
// A pile's glyph is the pile's fate, not its first member's: ✕ only when every
// step was refused, ✓ when every step is closed, else the first open step's. A
// session box whose first read was skipped is not a skipped session.
// `calGroupGlyph` in CalendarGrid.swift must say the same.
const calGroupGlyph = es => es.every(m => m.mark === 'wont') ? '✕ '
  : es.every(calClosed) ? '✓ '
  : calGlyph(es.find(m => !calClosed(m)));

function calBlockHTML(e, p) {
  if ((p.members || []).length > 1) return calGroupBlockHTML(p);
  const top = (p.s / 60) * CAL_HOUR_H, h = Math.max(20, (CAL_SLOT_MIN / 60) * CAL_HOUR_H);
  const w = (100 / p.cols) * (p.span || 1), left = p.col * (100 / p.cols);
  const mv = calMovable(e);
  const closed = calClosed(e);
  return `<div class="cal-ev block${closed ? ' closed' : ''}${calRecCls(e)}${calWont(e)}${e.overdue && !closed ? ' overdue' : ''}"
      data-id="${esc(e.id)}" ${mv ? `data-move="${mv}"` : ''} data-day="${esc(e.day)}" data-at="${esc(e.at || '')}"
      style="--c:${calShade(e)};top:${top}px;height:${h}px;left:calc(${left}% + 1px);width:calc(${w}% - 3px)"
      title="${esc((e.at ? e.at + ' · ' : '') + calLabel(e))}${mv ? '' : '\n' + esc(calWhyStuck(e))}">
    <span class="at">${esc(e.at || '')}</span> <span class="t">${calGlyph(e)}${esc(calLabel(e))}</span></div>`;
}

// The collapsed box: first member's time and title, then +N — or, for a
// session's run of record, the session's title and "N steps". It spans from
// the first member to the last, so the run it swallowed still reads as a
// stretch of time. Not draggable — moving five things at once is a guess —
// and closed/overdue only when every/any member is; the click opens the
// popout, whose rows each open their own full card.
function calGroupBlockHTML(p) {
  const es = p.members, e = es[0];
  const top = (p.s / 60) * CAL_HOUR_H, h = Math.max(20, ((p.n - p.s) / 60) * CAL_HOUR_H);
  const w = (100 / p.cols) * (p.span || 1), left = p.col * (100 / p.cols);
  const closed = es.every(calClosed);
  const { title, n } = calGroupLabel(es);
  return `<div class="cal-ev block group${closed ? ' closed' : ''}${calRecCls(e)}${es.some(m => m.overdue && !calClosed(m)) ? ' overdue' : ''}"
      data-id="grp:${esc(es.map(m => m.id).join(','))}" data-day="${esc(e.day)}" data-at="${esc(e.at || '')}"
      style="--c:${calShade(e)};top:${top}px;height:${h}px;left:calc(${left}% + 1px);width:calc(${w}% - 3px)"
      title="${esc(calGroupTip(es))}">
    <span class="at">${esc(e.at || '')}</span> <span class="t">${calGroupGlyph(es)}${esc(title)}</span><span class="n">${esc(n)}</span></div>`;
}
const calGroupTip = es => es.map(m => (m.at ? m.at + ' ' : '') + calGlyph(m) + calLabel(m)).join('\n');

// An all-day chip, and a month cell's row (`row`: always the lane's dot, no
// ○ — every row a month cell draws is an open step of the owner's).
function calChipHTML(e) {
  const mv = calMovable(e);
  const closed = calClosed(e);
  return `<div class="cal-ev chip${closed ? ' closed' : ''}${calRecCls(e)}${calWont(e)}${e.overdue && !closed ? ' overdue' : ''}"
      data-id="${esc(e.id)}" ${mv ? `data-move="${mv}"` : ''} data-day="${esc(e.day)}" data-at="${esc(e.at || '')}"
      style="--c:${calShade(e)}" title="${esc((e.at ? e.at + ' · ' : '') + calLabel(e))}${mv ? '' : '\n' + esc(calWhyStuck(e))}">
    ${e.at ? '<span class="dot"></span>' : ''}<span class="t">${calGlyph(e)}${esc(calLabel(e))}</span>${e.at ? `<span class="at">${esc(e.at)}</span>` : ''}</div>`;
}

// ---------- the detail panel ----------
// "If I'm going to do something on the calendar, I need to be able to see the
// full text. This thing where I can click in here is really very useful." So
// the panel is not a new card: it is `calEntryHTML`, the agenda's own row,
// with its composer, its Done · Won't do · Reopen and its links.
function calPanelHTML(cal) {
  if (!calState.open) return '';
  if (calState.open.startsWith('grp:')) return calGroupPanelHTML(cal);
  const e = calFind(cal, calState.open);
  if (!e) return '';
  const long = e.day ? localDate(e.day).toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' }) : '';
  const when = e.soon && !calClosed(e) ? `Anytime · added ${long}`
    : e.day ? long + (e.at ? ` · ${e.at}` : ' · all day')
    : 'No date — anytime';
  const c = calLaneOf(e);
  return `<div class="cal-overlay" onclick="if(event.target===this)calClose()">
    <div class="cal-panel">
      <div class="cal-panelhead">
        <span class="cal-swatch" style="background:${calShade(e)}"></span>
        <div><div class="cal-when">${esc(when)}</div><div class="small muted">${esc(c.label)}${e.repeat ? ' · ' + esc(e.repeat) : ''}</div></div>
        <div class="grow"></div><button class="sm icon" onclick="calClose()" title="Close">✕</button>
      </div>
      ${calEntryHTML(e)}
    </div></div>`;
}
// The popout behind a collapsed box: every
// member on its own line, in time order. Each row is a real .cal-ev, so the
// grid's own click wiring opens its full card in this same overlay.
function calGroupPanelHTML(cal) {
  const es = calState.open.slice(4).split(',').map(id => calFind(cal, id)).filter(Boolean);
  if (!es.length) return '';
  const lane = calLaneOf(es[0]);
  const when = localDate(es[0].day).toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
  // A run of record is named for its session: "7 steps in <session
  // title>" — the deliverable, then the steps that made it.
  const { title, n } = calGroupLabel(es);
  const what = n.startsWith('+') ? `${es.length} ${esc(lane.label.toLowerCase())} in one box` : `${esc(n)} in ${esc(title)}`;
  return `<div class="cal-overlay" onclick="if(event.target===this)calClose()">
    <div class="cal-panel">
      <div class="cal-panelhead">
        <span class="cal-swatch" style="background:${lane.color}"></span>
        <div><div class="cal-when">${esc(when)}</div><div class="small muted">${what}</div></div>
        <div class="grow"></div><button class="sm icon" onclick="calClose()" title="Close">✕</button>
      </div>
      ${es.map(e => `<div class="cal-ev rail${calClosed(e) ? ' closed' : ''}${calWont(e)}" data-id="${esc(e.id)}" style="--c:${calColor(e)}">
        <span class="dot"></span><span class="at">${esc(e.at || '')}</span><span class="t">${calGlyph(e)}${esc(calLabel(e))}</span></div>`).join('')}
    </div></div>`;
}

function calFind(cal, id) {
  for (const list of [cal.anytime || [], cal.overdue || [], cal.due || [], cal.soon || [], ...(cal.days || []).map(d => d.entries || [])]) {
    const e = list.find(x => x.id === id);
    if (e) return e;
  }
  return null;
}

// ================= chrome actions =================
function calToggle(set, key) {
  const s = calState[set];
  s.has(key) ? s.delete(key) : s.add(key);
  calStateSave(); pageRedraw();
}
function calGo(n) {
  calState.anchor = n === 'today' ? '' : addDays(calAnchor(), 7 * n);
  calSyncHash(); pageRedraw();
}
function calJump(day) { calState.anchor = day; calSyncHash(); pageRedraw(); }
function calOpen(id) { calState.open = id; pageRedraw(); }
function calClose() { calState.open = ''; pageRedraw(); }
// Deep-linkable without a re-render: `#/calendar/2026-08-31`.
function calSyncHash() {
  const h = calState.anchor ? `#/calendar/${calState.anchor}` : '#/calendar';
  if (location.hash !== h) history.replaceState(null, '', h);
}

// ================= wiring: clicks, drag, scroll position =================
function calWire(view) {
  // One pass: the panel is rendered inside `view`, so its composer is wired
  // by the same call. Wiring it twice would double every listener.
  wireCalNotes(view);
  calScrollRestore(view);
  calWireDrag(view);
  calWireKeys();
}

// Where the owner had scrolled to. Every redraw replaces the whole pane — a
// click on an event, the 30 s poll, a filter — and the grid would otherwise
// jump back to its opening position each time, losing the event just
// clicked. So the position is remembered against the range it belongs to,
// and only a range not looked at yet gets positioned automatically.
//
// One position per range, not one in total. A single slot only survived a
// repaint of the SAME week: › and ‹ threw the position away and re-opened
// the week at 9am, which reads as the page scrolling down under you. Every
// range looked at keeps its own line here, so a round trip lands where it was.
const calScrollTops = new Map();  // 'week:2026-08-30..2026-09-05' → px
let calScrollKey = '';            // the range the grid on screen belongs to
let calScrollApplying = false;    // true while WE are putting a position back
const CAL_SCROLL_KEEP = 24;       // ranges remembered, oldest dropped
const calScrollNow = () => calVisible().join('..');
const calScroller = view => view.querySelector('#cal-scroll') || view.querySelector('.cal-main');
function calScrollKeep(view) {
  const sc = calScroller(view);
  // Stored against the range the OLD grid belonged to — calState has already
  // moved on by the time a redraw starts, so calScrollNow() is the range being
  // drawn, not the one being replaced.
  if (sc && calScrollKey) calScrollTops.set(calScrollKey, sc.scrollTop);
}
function calScrollRestore(view) {
  const sc = calScroller(view);
  if (!sc) return;
  const key = calScrollNow();
  calScrollKey = key;
  // The owner's own scrolling, recorded as it happens rather than only at the next
  // redraw — nothing then depends on a repaint reaching calScrollKeep first.
  sc.addEventListener('scroll', () => {
    if (!calScrollApplying) calScrollTops.set(key, sc.scrollTop);
  }, { passive: true });
  if (calScrollTops.has(key)) { calScrollPut(sc, key, calScrollTops.get(key)); return; }
  // A range seen for the first time opens on the hour most likely wanted: the earliest thing on it, else 7am — never midnight,
  // which is eight empty hours of nothing.
  const body = view.querySelector('.cal-cols.body');
  if (!body) return;
  // Marks count as much as blocks: a past day whose whole content is the
  // record would otherwise open at 7am with the strip's marks off-screen.
  const first = [...view.querySelectorAll('.cal-ev.block, .cal-ev.mark')].map(el => parseFloat(el.style.top)).sort((a, b) => a - b)[0];
  calScrollPut(sc, key, Math.max(0, body.offsetTop + (first === undefined ? 7 * CAL_HOUR_H : first) - CAL_HOUR_H * 1.5));
}
function calScrollPut(sc, key, top) {
  calScrollApplying = true;
  sc.scrollTop = top;
  calScrollTops.set(key, top);
  // Once more after the browser has laid the new grid out. A scrollTop set
  // against a scroller that has not been measured yet is silently clamped to
  // whatever height it thinks it has, and Chrome's scroll anchoring gets a
  // vote too when a box's contents are replaced wholesale — both land a few
  // hundred pixels from where it was, which is the bug this exists to stop.
  // Skipped if the owner has scrolled in the meantime: their hand wins.
  requestAnimationFrame(() => {
    if (calScrollKey === key && sc.isConnected && sc.scrollTop !== top) sc.scrollTop = top;
    calScrollApplying = false;
  });
  for (const k of calScrollTops.keys()) {
    if (calScrollTops.size <= CAL_SCROLL_KEEP) break;
    calScrollTops.delete(k);
  }
}

// Escape closes the panel — but not while typing in its composer, where
// Escape is the browser's own dismiss, not "throw my words away". Attached on
// first draw rather than at load, so this file stays a set of pure functions
// that the node tests can run with no DOM (test/ui.test.js).
let calKeysWired = false;
function calWireKeys() {
  if (calKeysWired) return;
  calKeysWired = true;
  document.addEventListener('keydown', ev => {
    if (ev.key !== 'Escape' || !calState.open) return;
    if (document.activeElement?.closest?.('.composer')) return;
    calClose();
  });
}

// Pointer drag: pick an event up, drop it on a day column (day/time) or a
// month cell / all-day band (day only). Snap is 15 minutes. Nothing is
// written until the drop, and a failed write says so and repaints.
function calWireDrag(root) {
  // `#view` survives every redraw, so the listeners go on once. A second set
  // would move the event twice per drop.
  if (root.dataset.calDrag) return;
  root.dataset.calDrag = '1';
  let drag = null;
  const ghost = () => {
    let g = document.getElementById('cal-ghost');
    if (!g) { g = document.createElement('div'); g.id = 'cal-ghost'; document.body.appendChild(g); }
    return g;
  };
  root.addEventListener('pointerdown', ev => {
    if (ev.button !== 0) return;
    const el = ev.target.closest('.cal-ev');
    if (!el) return;
    drag = { el, id: el.dataset.id, move: el.dataset.move || '', x0: ev.clientX, y0: ev.clientY, on: false, target: null };
    el.setPointerCapture(ev.pointerId);
  });
  root.addEventListener('pointermove', ev => {
    if (!drag) return;
    const dx = ev.clientX - drag.x0, dy = ev.clientY - drag.y0;
    if (!drag.on) {
      if (Math.hypot(dx, dy) < 5) return;
      if (!drag.move) { drag = null; return; } // an immovable row is just a click
      drag.on = true; drag.el.classList.add('dragging'); document.body.classList.add('cal-dragging');
    }
    drag.el.style.transform = `translate(${dx}px, ${dy}px)`;
    drag.target = calDropTarget(ev.clientX, ev.clientY);
    const g = ghost();
    if (drag.target) {
      g.textContent = localDate(drag.target.day).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
        + (drag.target.at ? ' · ' + drag.target.at : ' · all day');
      g.style.display = 'block'; g.style.left = (ev.clientX + 14) + 'px'; g.style.top = (ev.clientY + 14) + 'px';
    } else { g.style.display = 'none'; }
  });
  const end = async ev => {
    if (!drag) return;
    const d = drag; drag = null;
    const g = document.getElementById('cal-ghost'); if (g) g.style.display = 'none';
    document.body.classList.remove('cal-dragging');
    if (!d.on) { calOpen(d.id); return; }          // a plain click opens the panel
    d.el.classList.remove('dragging'); d.el.style.transform = '';
    const t = d.target;
    if (!t || (t.day === d.el.dataset.day && t.at === d.el.dataset.at)) { pageRedraw(); return; }
    await calApplyMove(d.id, d.move, t.day, t.at);
  };
  root.addEventListener('pointerup', end);
  root.addEventListener('pointercancel', end);
}

// Where the pointer is: a time column (day + snapped minute), an all-day band
// or a month cell (day, no time).
function calDropTarget(x, y) {
  for (const el of document.elementsFromPoint(x, y)) {
    if (el.classList?.contains('cal-col')) {
      const r = el.getBoundingClientRect();
      let mins = Math.round(((y - r.top) / r.height * 1440) / CAL_SNAP) * CAL_SNAP;
      mins = Math.min(1440 - CAL_SNAP, Math.max(0, mins));
      return { day: el.dataset.day, at: calHHMM(mins) };
    }
    if (el.classList?.contains('cal-allday') || el.classList?.contains('cal-mday')) {
      return { day: el.dataset.day, at: '' };
    }
  }
  return null;
}

async function calApplyMove(id, move, day, at) {
  try {
    if (move === 'item') {
      const e = calFind(calLast || {}, id);
      const body = { day, at };
      if (e?.repeat) {
        const scope = await calScopeAsk(e, day, at);
        if (!scope) { pageRedraw(); return; }
        body.scope = scope;
      }
      const r = await patch(`/calendar/${id}`, body);
      // An agent run is never all day: the hub stamps the minute it runs
      // (08:00, then 30 min past the other agent runs that day), so a drop
      // on the band says where it landed.
      if (!at && r?.at) toast(`an agent run has a time — placed at ${day} ${r.at}`);
      else toast(at ? `moved to ${day} ${at}` : `moved to ${day}, all day`);
    } else if (move === 'run') {
      const e = calFind(calLast || {}, id);
      const cad = calNewCadence(e?.repeat || '', day, at);
      if (!cad) { toast('a check-in needs a time — drop it on the grid, not the all-day band'); pageRedraw(); return; }
      if (!confirm(`This is a standing check-in, not one event.\n\nMove EVERY check-in of "${e.title}" to ${cad.replace('@', ' at ')}?`)) { pageRedraw(); return; }
      await patch(`/threads/${e.thread_id}`, { schedule: cad });
      toast('cadence is now ' + cad);
    }
  } catch (err) { toast(err.message); }
  pageRedraw();
}

// daily@HH:MM keeps its cadence and takes the new time; weekly@Dow HH:MM
// takes the dropped day's weekday too. An all-day drop has no time to give,
// so it is refused rather than guessed.
function calNewCadence(repeat, day, at) {
  if (!at) return '';
  if (repeat.startsWith('daily@')) return `daily@${at}`;
  if (repeat.startsWith('weekly@')) return `weekly@${CAL_DOW[localDate(day).getDay()]} ${at}`;
  return '';
}
