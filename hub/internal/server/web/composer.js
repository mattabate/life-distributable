// life hub — laptop console: THE COMPOSER. One box, everywhere the owner
// writes back to an agent. Recs, calendar steps and chat answer cards the
// same way, so they share one box with the maximal set of features.
//
// Before this file there were three boxes: the chat composer (pictures,
// outcome chips, this/new session, now/later, ⌘↵), the Recs page's decide box
// (pictures, a destination select, four buttons) and the calendar's step box
// (a textarea and three buttons — no pictures at all). Same act, three
// implementations, three feature sets. Now every one of them is
// `composerHTML(key, spec)` + `wireComposer(key)`, so the MAXIMAL set is what
// each of them gets:
//
//   • pictures — Attach, drop, or ⌘V straight into the box
//   • the card it answers, with the card's own outcome chips
//   • which session hears it: this one, or a new one
//   • when: now, in an hour, tomorrow 9am, or a picked time
//   • ⌘↵ to send, a draft that survives the 5s/30s redraws, inline errors
//
// A session's own chat box has no route at all: its spec passes
// `targets: []` and `when: false`, so everything typed there goes now, to
// that session. A new session starts only from + New session and the empty
// Sessions box.
//
// A second (a Recs card was five bands tall; its send button became one
// button per answer): a spec
// with `actions` has no Send at all — each answer is its own button in the
// footer band, and pressing it IS sending. That folds the outcome chips (a
// band of their own) into the row that was already there, which is what the
// Recs page needed to lose two of its five stripes.
//
// What differs between the three is ONLY the spec: which chips, what the pick
// means in words, and the one `send` that posts it. Everything a message
// carries — words, pictures, destination, time — travels the same way.
'use strict';

// ---------- state ----------
// One state per box, keyed: 'chat' | 'cal:<id>' | 'rec:<id>' | 'new'. It
// outlives the DOM on purpose — every page here redraws under the owner on a
// timer, and a half-typed note or a half-lit chip must not blink out with it.
const composers = new Map();
function composerState(key) {
  let st = composers.get(key);
  // `arm` is null when nothing is armed, NOT '' — '' is a real action value
  // (Reply), and the two collided into every Reply button drawing itself as
  // the "are you sure" (seen in the first screenshot of this).
  // `re` is the card being answered; `also` the OTHER cards armed with it
  // (a read and a rec answered together: the agent gets both, both close).
  // One message answers them all; the first stays in
  // `re` so every single-card reader keeps working.
  if (!st) { st = { key, text: '', re: null, also: [], target: 'this', when: 'now', at: '', err: '', arm: null, spec: null, sending: false }; composers.set(key, st); }
  return st;
}
// Back to a plain message: no card, no destination, no time, nothing typed.
function composerReset(key) {
  const st = composerState(key);
  st.text = ''; st.re = null; st.also = []; st.target = 'this'; st.when = 'now'; st.at = ''; st.err = ''; st.arm = null;
  return st;
}
// Every card the box is answering, first one first.
const composerCards = key => { const st = composerState(key); return st.re ? [st.re, ...st.also] : []; };

// Arm a card WITHOUT dropping the ones already armed: a second card joins the
// strip, the same card pressed again only changes its pick. This is how a
// read and a rec end up on one Send: several cards from one session are
// answered in one message, not one prompt (and one wake) each.
function composerArm(key, re) {
  const st = composerState(key);
  const same = x => x && x.ref === re.ref && x.id === re.id;
  if (!st.re) st.re = re;
  else if (same(st.re)) st.re.outcome = re.outcome;
  else {
    const i = st.also.findIndex(same);
    if (i >= 0) st.also[i].outcome = re.outcome; else st.also.push(re);
  }
  st.err = ''; composerShowErr(key, '');
  composerDrawRe(key);
  const box = document.getElementById(cdom(key) + '-draft');
  if (box) box.focus();
}
// The DOM prefix for a box. A key carries a colon ('rec:rec-1a2b'), which is
// legal in an id but not in a bare CSS selector, so it is flattened once here
// and every element of the box hangs off it.
const cdom = key => 'c-' + String(key).replace(/[^a-z0-9]+/gi, '-');

// Attachment drafts by box — the chips under the words, uploaded as
// observations the moment they are picked (see uploadFiles).
const drafts = {};
// The session an upload is filed against, for boxes that are not a chat.
const draftThread = {};
const chipsOf = key => (drafts[key] = drafts[key] || []);

// The two rows every box offers unless its spec says otherwise.
const COMPOSER_TARGETS = [['this', 'This session'], ['new', 'New session']];
const COMPOSER_WHEN = [['now', 'Now'], ['1h', 'In an hour'], ['tomorrow', 'Tomorrow 9am'], ['at', 'At…']];

// ---------- render ----------
// spec:
//   re            the card being answered: {ref, id, title, outcomes:[{value,label}],
//                 means:{outcome: sentence}, fixed, href} — `fixed`
//                 for a box that lives ON its card (a calendar row, a rec), so
//                 the title and the × are not repeated; the chat's is armed and
//                 cleared instead. Passing `re` sets it; omit it to keep what
//                 the box already had (the chat arms its own).
//   targets       [] to drop the destination select
//   when          false to drop the time select
//   placeholder / sendLabel
//   actions       [{value,label,cls,confirmEmpty,needsWords}] — the answers AS
//                 buttons in the footer band, instead of one Send. Pressing one
//                 sends with that outcome; there is no lit chip to read, so a
//                 box with actions draws no chip strip.
//   requireWords  true = Send needs words even with an outcome lit
//   send(payload) posts it. payload = {key, text, refs, outcome, target, when, at,
//                 re, also} — `also` the other armed cards (composerArm), each
//                 with its own `outcome`, for a spec that can answer several
//   after()       run once the hub took it AND the box is empty again — the
//                 page's own redraw. It must not run inside `send`: the redraw
//                 re-renders the box from state that is still full, and the
//                 old words would come straight back.
//   inline        true for a box inside a card (no top border, tighter)
function composerHTML(key, spec) {
  const st = composerState(key);
  spec = spec || {};
  st.spec = spec;
  // A box that lives on its card re-declares the same `re` on every redraw
  // (the Calendar redraws on a timer). Keep what is lit — only a DIFFERENT
  // card replaces it.
  if (spec.re !== undefined) {
    const was = st.re;
    st.re = spec.re;
    if (was && spec.re && was.ref === spec.re.ref && was.id === spec.re.id) st.re.outcome = was.outcome;
  }
  const d = cdom(key), k = esc(key);
  const targets = spec.targets === undefined ? COMPOSER_TARGETS : spec.targets;
  const sel = (id, val, opts, on) => `<select class="sm" id="${id}" onchange="${on}">
    ${opts.map(([v, l]) => `<option value="${v}"${v === val ? ' selected' : ''}>${esc(l)}</option>`).join('')}</select>`;
  const when = spec.when === false ? '' :
    sel(`${d}-when`, st.when, COMPOSER_WHEN, `composerWhen('${k}',this.value)`)
    + (st.when === 'at' ? `<input type="datetime-local" class="sm" id="${d}-at" value="${esc(st.at)}" onchange="composerState('${k}').at=this.value">` : '');
  // The route (which session, when) — none at all in a session's chat.
  const route = !targets.length && !when ? '' : `<span id="${d}-route">
      ${targets.length ? sel(`${d}-target`, st.target, targets, `composerState('${k}').target=this.value;composerDrawRe('${k}')`) : ''}
      ${when}</span>`;
  // Three bands, in this order, flush inside one bordered box (app.css
  // "composer: ONE box, three bands"): the card being answered is the HEADING,
  // the words (with whatever is attached to them) are the middle, and the
  // Attach/where/when/Send row hangs off the bottom. Attachments moved below
  // the heading with the words they belong to — above it they pushed the card
  // being answered off the top of the box.
  // No "⌘↵ to send · paste or drop a picture" line in the bar: it was the one flexible item in a wrapping row, so in a
  // half-screen window (Magnet) it got squeezed to a word per line and stood six lines
  // tall beside Send. Paste and drop still work; nothing in the bar is
  // allowed to shrink below its own words now.
  return `<div class="composer${spec.inline ? ' inline' : ''}" id="${d}" data-c="${k}">
    <div class="re-slot" id="${d}-re">${composerReHTML(key)}</div>
    <div class="composer-body">
      <div class="chips" id="${d}-chips"></div>
      <textarea id="${d}-draft" rows="1" placeholder="${esc(composerPlaceholder(key))}"></textarea>
    </div>
    <div class="bar">
      ${spec.attach === false ? '' : `<button class="sm" onclick="document.getElementById('${d}-file').click()">Attach</button>
      <input type="file" id="${d}-file" multiple hidden>`}
      ${route}
      <span class="small err" id="${d}-err">${esc(st.err || '')}</span>
      <span class="acts" id="${d}-acts">${composerActionsHTML(key)}</span>
    </div>
  </div>`;
}

// The right end of the footer band: one Send, or — with `actions` — one button
// per answer. THE SECOND PRESS IS THE CONFIRMATION (sending with no note takes
// two presses, like an "Are you sure?"; with a note, one): an action marked `confirmEmpty` pressed
// over an empty box does not send — it renames itself and waits for a second
// press. Typing anything disarms it, because then there is a note and one
// press is the whole gesture.
const SEND_ARM = ' send';
function composerActionsHTML(key) {
  const st = composerState(key), spec = st.spec || {}, d = cdom(key), k = esc(key);
  if (!spec.actions) {
    // The same second press for an armed card that closes with or without
    // words (a read: "Are you sure?", then "Send without").
    if (st.arm === SEND_ARM) {
      return `<span class="small muted" style="align-self:center">Are you sure?</span>
        <button class="armed" id="${d}-send" onclick="composerSend('${k}')">Send without</button>`;
    }
    return `<button class="primary" id="${d}-send" onclick="composerSend('${k}')">${esc(spec.sendLabel || 'Send')}</button>`;
  }
  return spec.actions.map(a => {
    const armed = st.arm !== null && st.arm === a.value;
    const cls = armed ? 'armed' : (a.cls || '');
    return `<button class="${cls}" onclick="composerSend('${k}','${esc(a.value)}')"
      title="${esc(armed ? 'press again to send it with no note' : a.title || a.label)}">${esc(armed ? 'Send with no note' : a.label)}</button>`;
  }).join('');
}

// The hub's `outcomes` (store/close.go) as footer buttons — no box keeps a
// list of its own: the first decisive one is primary, a decisive one pressed
// over an empty box asks again, the words-only one ("Reply"/"Send") needs
// words. The hint is the tooltip.
function outcomeActions(outcomes) {
  return (outcomes || []).map((o, i) => ({
    value: o.value, label: o.label, title: o.hint || '',
    cls: i === 0 && o.value ? 'primary' : '', confirmEmpty: !!o.value, needsWords: !o.value,
  }));
}

// ---------- the reply box ----------
// replyBox: THE box that lives on a card and answers it — a rec on the Recs
// page, the owner's step and homework on the Calendar. It sits inside the card
// (`fixed`, inline), answers now to the card's own session (no destination
// or time select unless `o.targets` says otherwise), and `o.outcomes` — the
// hub's words — become its footer buttons. Anything else in `o` is the
// composer spec as is.
function replyBox(key, re, o) {
  const { outcomes, ...spec } = o;
  return composerHTML(key, {
    re: { title: '', fixed: true, outcome: '', ...re },
    actions: outcomes ? outcomeActions(outcomes) : undefined,
    inline: true, targets: [], when: false,
    ...spec,
  });
}

// postReply: the one send of a reply box. `close(p, words)` is the card's own
// close on the hub for this press — it returns the toast word when that is the
// whole answer, or undefined to let the words go on; then the words travel as
// ONE prompt (`prompt(p)` → target, in_reply_to, outcome…) and `sent(p)` is
// the toast.
async function postReply(p, r) {
  const words = !!(p.text || (p.refs || []).length);
  const closed = r.close ? await r.close(p, words) : undefined;
  if (closed !== undefined) { toast(closed); return; }
  await post('/prompts', { text: p.text, attachments: p.refs, ...r.prompt(p) });
  toast(r.sent(p));
}

function composerDrawActions(key) {
  const el = document.getElementById(cdom(key) + '-acts');
  if (el) el.innerHTML = composerActionsHTML(key);
}

// Anything typed cancels an "are you sure" — the note is the answer to it.
function composerDisarm(key) {
  const st = composerState(key);
  if (st.arm === null) return;
  st.arm = null;
  composerDrawActions(key);
}

// The strip above the words: which card this answers, its chips, and one line
// saying what the lit chip will do on Send. A `fixed` card (the
// calendar row, the rec this box belongs to) draws the chips alone — its title
// is the row six pixels above.
// With several cards armed it is one strip per card, each with its own chips
// and its own ×; Send answers them all with the one message.
function composerReHTML(key) {
  const st = composerState(key), re = st.re;
  if (!re) return '';
  // A box whose answers are buttons in the footer has nothing left to put in a
  // strip of its own: no chip to light, and the card it answers is the thing
  // it is drawn inside. Drawing it anyway is a redundant blue band.
  if ((st.spec || {}).actions && re.fixed) return '';
  return composerCards(key).map((r, i) => composerStripHTML(key, r, i)).join('');
}
function composerStripHTML(key, re, i) {
  const k = esc(key);
  const outcomes = re.outcomes && re.outcomes.length ? re.outcomes : [{ value: '', label: 'Reply' }];
  const chip = o => `<button type="button" class="sm${re.outcome === o.value ? ' primary' : ''}" onclick="composerPickOutcome('${k}','${esc(o.value)}',${i})">${esc(o.label)}</button>`;
  const means = (re.means || {})[re.outcome || ''] || '';
  const head = re.fixed ? '' : `<div class="row">
      <span class="small muted">${esc(re.label || 'Answering')}</span>
      <a class="trunc grow" href="${esc(re.href || '#')}" onclick="${re.focus ? `${re.focus}('${esc(re.id)}');return false` : ''}" title="${esc(re.title)}">${mdInline(re.title, false)}</a>
      <button type="button" class="x" onclick="composerDropRe('${k}',${i})" title="just a message, not an answer to this card">×</button>
    </div>`;
  if (re.bare) return `<div class="reply-strip">${head}</div>`;
  return `<div class="reply-strip">${head}
    <div class="row"><span class="chips-row">${outcomes.map(chip).join('')}</span>
      <span class="small muted trunc">${esc(means)}</span></div>
  </div>`;
}

function composerPlaceholder(key) {
  const st = composerState(key), spec = st.spec || {};
  if (typeof spec.placeholder === 'function') return spec.placeholder(st);
  return spec.placeholder || 'Message…';
}

function composerDrawRe(key) {
  const el = document.getElementById(cdom(key) + '-re');
  if (el) el.innerHTML = composerReHTML(key);
  const box = document.getElementById(cdom(key) + '-draft');
  if (box) box.placeholder = composerPlaceholder(key);
}

// i: which armed card (0 = `re`, the rest index `also`); absent = the first.
function composerPickOutcome(key, v, i) {
  const re = composerCards(key)[i || 0];
  if (!re) return;
  re.outcome = v;
  const st = composerState(key);
  st.err = ''; composerShowErr(key, '');
  composerDrawRe(key);
  const box = document.getElementById(cdom(key) + '-draft');
  if (box) box.focus();
}

// One card's ×: the rest stay armed. Dropping the first promotes the next.
function composerDropRe(key, i) {
  const st = composerState(key);
  if (!i) st.re = st.also.shift() || null; else st.also.splice(i - 1, 1);
  if (!st.re && st.arm === SEND_ARM) { st.arm = null; composerDrawActions(key); }
  composerDrawRe(key);
  const box = document.getElementById(cdom(key) + '-draft');
  if (box) box.focus();
}

function composerClearRe(key) {
  const st = composerState(key);
  st.re = null; st.also = [];
  if (st.arm === SEND_ARM) { st.arm = null; composerDrawActions(key); }
  composerDrawRe(key);
  const box = document.getElementById(cdom(key) + '-draft');
  if (box) box.focus();
}

// "At…" needs its picker drawn, which is a re-render of the bar: the caller's
// redraw does it, so a box that has one uses it and the rest redraw the bar.
function composerWhen(key, v) {
  const st = composerState(key);
  st.when = v;
  if (v === 'at' && !st.at) {
    const d = new Date(Date.now() + 3600000);
    d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
    st.at = d.toISOString().slice(0, 16);
  }
  const spec = st.spec || {};
  if (spec.redraw) spec.redraw();
}

function composerShowErr(key, msg) {
  const st = composerState(key);
  st.err = msg || '';
  const el = document.getElementById(cdom(key) + '-err');
  if (el) el.textContent = st.err;
  else if (msg) toast(msg);
}

// ---------- wiring ----------
// Called after the box's HTML lands. Restores the draft, grows the textarea,
// takes ⌘↵, and arms Attach / drop / the file input.
function wireComposer(key) {
  const d = cdom(key);
  const box = document.getElementById(d + '-draft');
  if (!box) return;
  const st = composerState(key);
  if (st.text && box.value !== st.text) { box.value = st.text; }
  autoGrow(box);
  box.addEventListener('input', () => { st.text = box.value; autoGrow(box); composerDisarm(key); });
  box.addEventListener('keydown', e => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); composerSend(key); }
  });
  const file = document.getElementById(d + '-file');
  if (file) file.addEventListener('change', () => { uploadFiles([...file.files], key); file.value = ''; });
  wireDrop(document.getElementById(d), key, true);
  drawChips(key);
}

function autoGrow(el) { el.style.height = 'auto'; el.style.height = Math.min(el.scrollHeight, window.innerHeight * 0.4) + 'px'; }

// ---------- sending ----------
// One road for every box: gather words + pictures + the pick, check what this
// spec insists on, hand it to the spec's `send`. Nothing here knows about
// prompts, recs or calendar items — that is the point.
async function composerSend(key, action) {
  const st = composerState(key), spec = st.spec || {};
  if (!spec.send) return;
  // One message in flight per box. The buttons are disabled below, but ⌘↵ in
  // the box is not a button: a second press while the hub still holds the
  // first (21 s during another session's hub restart once sent a message
  // twice) is dropped here.
  if (st.sending) return;
  const d = cdom(key);
  const box = document.getElementById(d + '-draft');
  const text = (box ? box.value : st.text).trim();
  const refs = draftRefs(key);
  if (refs === null) { composerShowErr(key, 'Still uploading — a moment.'); return; }
  // With `actions`, the button pressed IS the outcome — there is no lit chip.
  // ⌘↵ has no button, so it means the first action (Accept, on a rec), which
  // still has to pass the empty-box confirmation below.
  const acts = spec.actions || [];
  const act = acts.length ? (acts.find(a => a.value === action) || acts[0]) : null;
  if (act) {
    if (act.confirmEmpty && !text && !refs.length) {
      if (st.arm !== act.value) {
        st.arm = act.value;
        composerShowErr(key, '');
        composerDrawActions(key);
        return;
      }
    }
    st.arm = null;
  } else if (composerCards(key).some(r => r.confirmEmpty) && !text && !refs.length && st.arm !== SEND_ARM) {
    st.arm = SEND_ARM;
    composerShowErr(key, '');
    composerDrawActions(key);
    return;
  } else {
    st.arm = null;
  }
  const outcome = act ? act.value : (st.re ? (st.re.outcome || '') : '');
  // `also`: the other cards this one message answers, each with its own pick.
  const payload = { key, text, refs, outcome, target: st.target, when: st.when, at: st.at, re: st.re, also: st.also.slice() };
  // An outcome IS a response ("I did this" with nothing typed) — unless the
  // spec says the words are the point, which is what one of the owner's own
  // calendar steps and a rec's bare reply both say.
  const needsWords = act ? !!act.needsWords : (spec.requireWords || !(outcome || st.also.some(r => r.outcome)));
  if (!text && !refs.length && needsWords) {
    composerShowErr(key, (act && act.emptyMsg) || spec.emptyMsg || (outcome ? 'Say what happened — this one closes with words.'
      : 'Type something first — a reply with nothing in it goes nowhere.'));
    box && box.focus();
    return;
  }
  if (payload.when === 'at' && !payload.at) { composerShowErr(key, 'Pick a time.'); return; }
  const check = spec.check ? spec.check(payload) : '';
  if (check) { composerShowErr(key, check); return; }
  composerShowErr(key, '');
  // While the hub holds it every button is off and the Send button says so;
  // when it lands the box is blank and the button is back — that is the
  // whole tell (there is no other "it went").
  const busy = on => {
    document.querySelectorAll(`#${d}-acts button`).forEach(b => (b.disabled = on));
    const send = document.getElementById(d + '-send');
    if (send) {
      if (on) { send.dataset.label = send.textContent; send.textContent = 'Sending…'; }
      else if (send.dataset.label) { send.textContent = send.dataset.label; delete send.dataset.label; }
    }
  };
  st.sending = true;
  busy(true);
  try {
    await spec.send(payload);
    // The box empties only when the hub took it.
    st.text = ''; st.at = ''; st.when = 'now'; st.target = 'this'; st.also = [];
    if (st.re && !st.re.fixed) st.re = null;
    else if (st.re) st.re.outcome = spec.defaultOutcome || '';
    clearDraft(key);
    // Re-read the element: `send` may already have redrawn the page, in which
    // case the `box` above is a detached node and blanking it does nothing.
    const live = document.getElementById(d + '-draft');
    if (live) { live.value = ''; autoGrow(live); }
    composerDrawRe(key);
    composerDrawActions(key);
    if (spec.after) await spec.after();
  } catch (e) {
    composerShowErr(key, e.message);
  }
  st.sending = false;
  busy(false);
}

// The three time fields every endpoint here takes (`POST /prompts`, and a
// rec's decide/reply): in / on+at_time / at, read by the hub in Eastern.
function composerTiming(body, p) {
  if (p.when === '1h') body.in = '1h';
  else if (p.when === 'tomorrow') { body.on = easternFields(new Date(Date.now() + 86400000)).on; body.at_time = '09:00'; }
  else if (p.when === 'at' && p.at) Object.assign(body, easternFields(new Date(p.at)));
  return body;
}

// ---------- pictures ----------
// Dropping a file is the point of the laptop console: it is uploaded as an
// observation and passed to the session as a blob ref, exactly as the phone
// does with a photo — so the agent can Read a CSV, a JSON key file or a PDF
// straight from the message.
function wireDrop(el, which, highlight) {
  if (!el) return;
  ['dragenter', 'dragover'].forEach(ev => el.addEventListener(ev, e => {
    e.preventDefault(); if (highlight) el.classList.add('drop');
  }));
  ['dragleave', 'drop'].forEach(ev => el.addEventListener(ev, e => {
    e.preventDefault();
    if (ev === 'dragleave' && el.contains(e.relatedTarget)) return;
    if (highlight) el.classList.remove('drop');
  }));
  el.addEventListener('drop', e => { if (e.dataTransfer?.files?.length) uploadFiles([...e.dataTransfer.files], which); });
}

// A screenshot lives on the clipboard, not on disk, so every box takes a
// pasted image. One document-level listener, because paste only fires
// on the focused element and no box is always it — it routes to whichever box
// the caret is in, or to the only one on the page.
document.addEventListener('paste', e => {
  const files = [...(e.clipboardData?.files || [])];
  if (!files.length) return;
  const which = pasteTarget();
  if (!which) {
    // Several boxes on screen (the Recs page, a calendar day with two open
    // steps) and none chosen: guessing would put the screenshot on the wrong
    // one.
    if (document.querySelector('[data-c]')) toast('click the box you want it in first, then paste');
    return;
  }
  e.preventDefault();
  uploadFiles(files, which);
});

// Which box a paste belongs to: the one the caret is in, else the only box on
// the page (an empty new-session chat has exactly one).
function pasteTarget() {
  const ae = document.activeElement;
  const box = ae && ae.closest ? ae.closest('[data-c]') : null;
  if (box) return box.dataset.c;
  const all = document.querySelectorAll('[data-c]');
  return all.length === 1 ? all[0].dataset.c : null;
}

async function uploadFiles(files, which = 'chat') {
  for (const f of files) {
    // A pasted screenshot can arrive with no filename at all.
    const name = f.name || 'pasted.png';
    const chip = { name, ref: null, uploading: true, url: f.type.startsWith('image/') ? URL.createObjectURL(f) : null };
    chipsOf(which).push(chip); drawChips(which);
    try {
      const fd = new FormData();
      fd.append('source', 'app');
      fd.append('kind', f.type.startsWith('image/') ? 'photo' : 'upload');
      fd.append('ts', new Date().toISOString());
      fd.append('tz', Intl.DateTimeFormat().resolvedOptions().timeZone);
      fd.append('payload', JSON.stringify({ via: 'web', thread_id: which === 'chat' ? chatState.id : draftThread[which] || null, filename: name, content_type: f.type }));
      fd.append('file', f, name);
      const o = await api('/observations', { method: 'POST', body: fd });
      if (!o || !o.blob_ref) throw new Error('hub stored no blob');
      chip.ref = o.blob_ref; chip.uploading = false;
      // A picture is a note too — it cancels a pending "send with no note".
      composerDisarm(which);
    } catch (e) {
      drafts[which] = drafts[which].filter(c => c !== chip);
      toast('upload failed: ' + e.message);
    }
    drawChips(which);
  }
}

// Chips show the picture, not just its name — on the phone the owner sees
// what they attached before sending it, and the console should too.
function drawChips(which = 'chat') {
  const el = document.getElementById(cdom(which) + '-chips');
  if (!el) return;
  el.innerHTML = (drafts[which] || []).map((c, i) => `
    <span class="chip${c.url ? ' img' : ''}${c.uploading ? ' up' : ''}">
      ${c.url ? `<img src="${c.url}" alt="" title="click to see it full size" onclick="peekChip('${esc(which)}',${i})">` : '📎'}
      <span class="trunc" style="max-width:160px">${c.uploading ? '⏳ ' : ''}${esc(c.name)}</span>
      <button onclick="dropChip('${esc(which)}',${i})" title="remove">×</button>
    </span>`).join('');
}
// A chip that only says "image PNG" can't be checked. A screenshot is attached in order to be read, so every
// picture in a composer opens at full size.
function peekChip(which, i) {
  const c = (drafts[which] || [])[i];
  if (c && c.url) openImage(c.url, c.name);
}
function dropChip(which, i) {
  const c = (drafts[which] || [])[i];
  if (c && c.url) URL.revokeObjectURL(c.url);
  drafts[which].splice(i, 1); drawChips(which);
}
// The uploaded refs of a box, or null while one is still uploading.
function draftRefs(which) {
  const list = drafts[which] || [];
  if (list.some(c => c.uploading)) return null;
  return list.filter(c => c.ref).map(c => c.ref);
}
function clearDraft(which) {
  (drafts[which] || []).forEach(c => c.url && URL.revokeObjectURL(c.url));
  drafts[which] = []; drawChips(which);
}
