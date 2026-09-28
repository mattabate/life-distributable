'use strict';
// ---- one tooltip, for every plot on this console ----
//
// Tooltips should look good and fit their plot: each chart chooses what to
// show. Two separate faults this fixes. STYLE: every mark carried the browser's own `title=`, so
// the tooltip was an OS box in an OS font, half a second late, placed wherever
// the OS liked, and immune to this stylesheet — it did not go dark with the
// page and it could not hold a colour key. CONTENT: it was one flat sentence
// ("Jul 2026 · SNOW $1,283 · 65% of $1,983") that the reader had to parse to
// find a number, and it said the same kind of thing on every chart.
//
// So this file is the tooltip, and every plot fills it with what only that plot
// can say. The shape is fixed, which is what makes eight charts feel like one
// system:
//
//   title   the identity of the thing under the pointer — a day, a month, a year
//   sub     one muted line of context — the total the parts are shares of
//   rows    ONE ROW PER SERIES: a stroke of the series colour, then the VALUE
//           in bold, then the name in muted ink, then an optional trailing note
//   foot    what the number means, or where it came from
//
// Value first and bold, name after it: in a legend the reader has the number
// and wants the series; in a tooltip they have the series and want the number
// (dataviz interaction rules). The colour is a 10×3 stroke, not a filled box —
// at this density a box is data-weight ink doing a label's job.
//
// Two ways in:
//   tipShow(spec, anchorRect)   — computed hover (the line plots' crosshair)
//   data-tip='<json spec>'      — a static mark (a bar, a heat cell, a dot);
//                                 one delegated listener below handles them all
// Anchored to the MARK, never to the cursor: the card cannot jitter while the
// pointer moves inside one bar, and it never sits on top of what it describes.

const TIP_GAP = 10; // px between the mark and the card

function tipNode() {
  let el = document.getElementById('tipcard');
  if (!el) {
    el = document.createElement('div');
    el.id = 'tipcard';
    el.className = 'tip';
    el.hidden = true;
    document.body.appendChild(el);
  }
  return el;
}

/// spec: {title, sub, rows: [{c, v, k, t, on, dim}], defs: [{k, v}], lines: [string], foot}
///   c   the series colour (a CSS colour or var(--sN)); omitted = no key
///   v   the value, bold — the thing the reader came for
///   k   the series/category name, muted
///   t   a trailing note pushed to the right edge (a share, a count)
///   on  this row is the one under the pointer
///   dim this row is context, not the subject
/// `lines` is the other shape a card can hold: whole sentences that wrap, one
/// under the other — the working behind a number rather than a series key
/// (the P&L rows, 2026-09-23: "SNOW 5 sh → $1,683 (bought for $1,723)").
function tipHTML(s) {
  const rows = (s.rows || []).filter(Boolean).map(r => `<div class="tip-r${r.on ? ' on' : ''}${r.dim ? ' dim' : ''}">
    <i${r.c ? ` style="background:${esc(r.c)}"` : ' class="none"'}></i>
    <b>${esc(r.v === undefined || r.v === null ? '' : r.v)}</b>
    <span class="k">${esc(r.k || '')}</span>
    ${r.t ? `<span class="t">${esc(r.t)}</span>` : ''}
  </div>`).join('');
  const lines = (s.lines || []).filter(Boolean).map(l => `<div${/^vs\b/i.test(l) ? ' class="vs"' : ''}>${esc(l)}</div>`).join('');
  // `defs`: a term and its note, two columns — a list of caveats reads as a
  // glossary, not a wall of wrapped sentences.
  const defs = (s.defs || []).filter(Boolean).map(d => `<b>${esc(d.k)}</b><span>${esc(d.v)}</span>`).join('');
  return `${s.title ? `<div class="tip-h">${esc(s.title)}</div>` : ''}
    ${s.sub ? `<div class="tip-s">${esc(s.sub)}</div>` : ''}
    ${rows ? `<div class="tip-rows">${rows}</div>` : ''}
    ${defs ? `<div class="tip-defs">${defs}</div>` : ''}
    ${lines ? `<div class="tip-lines">${lines}</div>` : ''}
    ${s.foot ? `<div class="tip-f">${esc(s.foot)}</div>` : ''}`;
}

// A mark wider than this is a row or a band, not a point: centring the card on
// its middle would put the answer a foot away from the pointer that asked for
// it. Above that width the card centres on where the pointer entered instead
// — which does not move on a scroll, so tipFollow can still re-measure.
const TIP_WIDE = 160; // px
let tipAnchorX = null;

/// Above the mark and centred on it; below it when there is no room above;
/// always inside the window, so a bar at the right edge of a wide card still
/// gets a whole card and not a clipped one.
function tipPlace(el, a) {
  const w = el.offsetWidth, h = el.offsetHeight;
  const vw = window.innerWidth, vh = window.innerHeight;
  const wide = a.right - a.left > TIP_WIDE && tipAnchorX !== null;
  let left = (wide ? tipAnchorX : (a.left + a.right) / 2) - w / 2;
  left = Math.max(8, Math.min(left, vw - w - 8));
  let top = a.top - h - TIP_GAP;
  if (top < 8) top = Math.min(a.bottom + TIP_GAP, vh - h - 8);
  el.style.left = Math.round(left) + 'px';
  el.style.top = Math.round(Math.max(8, top)) + 'px';
}

// The mark the card is currently pointing at, when the card was raised by one
// (a bar, a cell, a dot). A crosshair hover has no element — it is a position
// on a line — and that difference decides what a scroll does, below.
let tipAnchorEl = null;

function tipShow(spec, anchor, el, x) {
  if (!spec) return tipHide();
  const card = tipNode();
  card.innerHTML = tipHTML(spec);
  card.hidden = false;
  tipAnchorEl = el || null;
  tipAnchorX = x === undefined ? null : x;
  tipPlace(card, anchor);
}

function tipHide() {
  const el = document.getElementById('tipcard');
  tipAnchorEl = null;
  tipAnchorX = null;
  if (el) { el.hidden = true; el.innerHTML = ''; }
}

/// What a mark writes into its own HTML: `<i ${tipAttr({…})}>`. The spec is
/// JSON in an attribute rather than an onmouseover call, so a chart that draws
/// 365 cells adds 365 strings and no listeners.
function tipAttr(spec) {
  return `data-tip="${esc(JSON.stringify(spec))}"`;
}

// One listener for every static mark on the console. `pointerover` (not
// mouseover) so a trackpad, a pen and a touch all raise it; the mark keeps its
// own :hover lift in CSS.
document.addEventListener('pointerover', ev => {
  const el = ev.target.closest && ev.target.closest('[data-tip]');
  if (!el) return;
  let spec = null;
  try { spec = JSON.parse(el.dataset.tip); } catch (e) { return; }
  tipShow(spec, el.getBoundingClientRect(), el, ev.clientX);
});
document.addEventListener('pointerout', ev => {
  const el = ev.target.closest && ev.target.closest('[data-tip]');
  if (el && !el.contains(ev.relatedTarget)) tipHide();
});
// A card that stayed behind after the page moved under it would be pointing at
// nothing. But a card that KNOWS its mark can be re-measured against it — the
// mark is still on screen and still under the pointer, and deleting the answer
// mid-read because a trackpad drifted is the annoying version. Anything with
// no anchor left (a redrawn page, or a hover that was only ever a position)
// goes down.
function tipFollow() {
  const el = document.getElementById('tipcard');
  if (!el || el.hidden) return;
  if (tipAnchorEl && tipAnchorEl.isConnected) tipPlace(el, tipAnchorEl.getBoundingClientRect());
  else tipHide();
}
window.addEventListener('scroll', tipFollow, true);
window.addEventListener('resize', tipFollow);
// A mark that is a link (a P&L row → its rec) navigates while its card is up;
// the new page never sends the pointerout, so the card would float over it.
window.addEventListener('hashchange', tipHide);
