// life hub — laptop console: THE MICROPHONE. A speech log on every page.
//
// The mic is a SENSOR, not a feature of a message box. The switch is the
// owner's and lives on the bar of every page (the Mic button beside + New
// session, or one lone tap of the Option key while this tab has focus —
// Option pressed and released with no other key in between; ⌥← and friends
// stay what they are); the box's own Mic button is the same switch. Tap:
// `POST /mic {on: true}` and the hub runs ops/bin/mic, which turns speech
// into text ON THE MAC (Apple's SpeechAnalyzer, the phone app's engine;
// nothing leaves the tailnet). While it is on, every final phrase is a
// `console/speech` row with a time on every word, whatever page is open and
// whether or not any box is listening — that is the sensor, joinable to the
// owner's other observations by wall clock. Tap again: off, and the last
// phrase lands.
//
// The box is a second thing, layered on top: THE BOX WITH THE CARET IS WHERE
// THE WORDS GO. Click into any composer while the mic is on and the phrases
// from that moment fill it (what was there stays, the finals follow, the
// volatile tail after them); click into another box and the words follow the
// caret there. No box with the caret — reading a page, thinking aloud — and
// the words are rows only, for a session to read later (`lifectl obs speech
// --source console`). Send is the owner's: a dictation never sends itself,
// and the message goes with `via: 'console'` like any other.
//
// This file polls `GET /mic` five times a second while it is on. Nothing here
// opens a microphone: the browser never asks for one, and the hub only opens
// the input on the owner's POST (it keeps the helper WARM — model loaded,
// input closed — so the tap is instant). Off means off.
//
// The owner's words are theirs: the mic only ever owns the tail it wrote last
// (`inserted`). Each poll it replaces that tail with the new one; if the box
// no longer ends with it — it was edited — the box as it was left becomes the
// base and only phrases not yet shown are added. And nothing the hub says
// ever takes words OUT of a box: a hub that comes back from a restart with no
// take is a fresh take on top of what is shown, and the switch is turned back
// on — a restart is plumbing, not the owner's tap. A box left behind keeps
// its draft; a box come back to takes the phrases from that moment only —
// what was said on other pages is rows, never a block dumped into the box.
'use strict';

const MIC_POLL_MS = 200;
const mic = {
  // The sensor: on/off, the hub's counters, the poll.
  on: false, seq: -1, status: '', timer: null, busy: false, altAlone: false, origin: 0,
  // guest: the take was switched on where this window cannot hear of it (the
  // desktop app, another browser) and found by the watch below: this window
  // shows it and can switch it off, and takes no words until a box's Mic is
  // tapped here.
  guest: false,
  // The box the words go into — the one on the screen (micWantKey) — and
  // what has been written into it. key/thread: which box (a 'chat' box is
  // one session's), null while no box is on the page. base: the box as it
  // was when the words started landing (or as last edited); inserted: the
  // tail the mic wrote last; finalsBase: how much of the take's finals this
  // box has already shown; finalsSeen/volShown: the hub's text and tail at
  // the last poll; sentBase/skipFinal/mark: the Send bookkeeping below.
  key: null, thread: '', base: '', inserted: '', finalsBase: 0, finalsSeen: '', volShown: '',
  sentBase: 0, skipFinal: false, mark: null,
};

// Send while the mic is on: the message sends and the box keeps listening.
// The composer marks the take as the send begins — how much of it the message
// carries — and tells the mic when the hub took it. From then on the box
// holds only what was said after that mark: the take's earlier phrases never
// come back, not on the next poll, not on a hop to another session and back,
// not when a send that was still in flight when the mic was tapped on lands
// and blanks the box. A phrase still volatile at Send went with the message,
// so its final — the first to land after the mark — is skipped rather than
// shown twice.
// The words are landing in this box now: it is the target, on the page, and
// (for a session's box) the session the target was set in.
function micHere(key) {
  if (!mic.on || mic.key !== key) return false;
  if (key === 'chat' && typeof chatState !== 'undefined' && (chatState.id || '') !== mic.thread) return false;
  return true;
}
function micMark(key) {
  if (!micHere(key)) return null;
  mic.mark = { n: mic.finalsSeen.length, vol: !!mic.volShown };
  return mic.mark;
}
function micSent(key, mark) {
  // No mark = the mic was tapped on while the send was in flight, so the
  // take began over words that have now gone: all of it stays.
  if (!mark) { if (!micHere(key)) return; mark = { n: 0, vol: false }; }
  else if (mic.mark !== mark || mic.key !== key) return;
  mic.mark = null;
  mic.sentBase = mark.n; mic.finalsBase = mark.n; mic.base = ''; mic.skipFinal = mark.vol;
  mic.inserted = '';
  micApply({ seq: mic.seq, status: mic.status, on: mic.on, text: mic.finalsSeen, volatile: mic.volShown });
}

// The box the words go to RIGHT NOW: the box with the caret if the caret is
// in one (a calendar step, a rec); else the sessions page's own box — a new
// session's or the open session's — clicked into or not; else none. A box is
// a target only while it is ON THE SCREEN: leave the page and the words are
// rows only, come back and the box takes the phrases from that moment, never
// the ones said away from it (the database has those).
function micCaretKey() {
  const a = document.activeElement;
  const c = a && a.closest ? a.closest('.composer[data-c]') : null;
  return c ? c.dataset.c : null;
}
function micWantKey() {
  // Only the window in front takes words (windows.js): the same page open in
  // two windows is one box filling, never two.
  if (!micFront()) return null;
  const caret = micCaretKey();
  if (caret) return caret;
  for (const key of ['new', 'chat']) if (document.getElementById(cdom(key) + '-draft')) return key;
  return null;
}
const micFront = () => !mic.guest && (typeof winFront !== 'function' || winFront());
const micThreadOf = key => (key === 'chat' && typeof chatState !== 'undefined' && chatState.id ? chatState.id : '');

// Every poll, and on every click into a box: the target follows the page.
function micSync() {
  if (!mic.on) return;
  const key = micWantKey();
  if (key !== mic.key || (key ? micThreadOf(key) : '') !== mic.thread) micTarget(key);
}

// The words go to this box (or, with null, to no box) from now on. What the
// old box shows stays its draft — except a tail still volatile, which comes
// out of it so the phrase lands whole in the new one. The hub is told which
// session's box it is, so the rows from here on carry that thread (none
// while no box is the target).
function micTarget(key) {
  const thread = key ? micThreadOf(key) : '';
  if (mic.key === key && mic.thread === thread) return;
  if (mic.key && mic.volShown) {
    // The box may already be off the screen (the page changed mid-phrase):
    // then its draft is what gets the tail taken off.
    const old = document.getElementById(cdom(mic.key) + '-draft');
    const cur = old ? old.value : composerState(mic.key).text;
    if (cur.endsWith(mic.volShown)) {
      const kept = micUnvol(cur);
      if (old) old.value = kept;
      composerState(mic.key).text = kept;
    }
  }
  const box = key ? document.getElementById(cdom(key) + '-draft') : null;
  mic.key = key; mic.thread = thread;
  mic.base = box ? box.value : key ? composerState(key).text : '';
  mic.inserted = '';
  mic.finalsBase = mic.finalsSeen.length; mic.sentBase = mic.finalsBase;
  mic.skipFinal = false; mic.mark = null;
  micDraw();
  // Only the front window names the box to the hub: a window stepping back
  // must not tell it "no box" over the one just moved to.
  if (mic.on && micFront() && typeof post === 'function') post('/mic', micOnBody()).catch(() => {});
}

// Every switch-on names the box (its session and its key — a phrase that went
// into a box is the owner's to send, so a mic-off send leaves it out) and the
// tap on (`origin`), which a hub restart must not move.
const micOnBody = () => ({ on: true, thread_id: mic.thread, box: mic.key || '', origin_ms: mic.origin || 0 });

// A tap off on a page other than Sessions sends what was said there: the hub
// starts one session with the take's words that went into no box
// (server/mic.go micSend), to look at what the owner wanted on that page.
// Only this tap sends — never a restart, a reload, the idle switch-off or a
// page change.
function micSendFrom() {
  const name = (location.hash.replace(/^#\/?/, '') || 'sessions').split('/')[0];
  if (name === 'sessions' || (typeof views !== 'undefined' && !views[name])) return null;
  return { page: location.hash };
}

// ---------- the switch ----------
// On the bar of every page (index.html #navmic) and in every composer band
// (composerHTML → micButtonHTML): the same switch. The lit one is where the
// words are going; the bar's is lit whenever the sensor is on.
function micButtonHTML(key) {
  const on = mic.on && mic.key === key;
  return `<button class="sm mic${on ? ' on' : ''}" id="${cdom(key)}-mic" onclick="micToggle('${esc(key)}')"
    title="Option: tap to listen, tap again to stop">${esc(micLabel(key))}</button>`;
}
function micLabel(key) {
  if (!mic.on) return 'Mic';
  if (key !== undefined && mic.key !== key) return 'Mic';
  if (!micFront()) return '● Listening · other window';
  if (mic.status === 'listening' || !mic.status || mic.status === 'starting') return '● Listening';
  return '● ' + mic.status;
}
function micDraw() {
  const nav = document.getElementById('navmic');
  if (nav) { nav.classList.toggle('on', mic.on); nav.textContent = micLabel(); }
  document.querySelectorAll('.composer[data-c] button.mic').forEach(b => {
    const key = b.closest('.composer').dataset.c;
    b.classList.toggle('on', mic.on && mic.key === key);
    b.textContent = micLabel(key);
  });
}

// The tap. From the bar or the Option key (no key): on with the box on the
// page, if any (micWantKey); off. From a box's own button: on with that box;
// while on, the button of a box that is not the target makes it the target
// (and puts the caret there), the target's own button is off.
async function micToggle(key) {
  if (mic.busy) return;
  mic.busy = true;
  try {
    if (!mic.on) {
      key = key || micWantKey();
      mic.key = null; mic.thread = ''; mic.base = ''; mic.inserted = ''; mic.finalsBase = 0; mic.finalsSeen = '';
      mic.volShown = ''; mic.sentBase = 0; mic.skipFinal = false; mic.mark = null;
      mic.seq = -1; mic.status = 'starting'; mic.origin = Date.now(); mic.guest = false;
      if (key) {
        const box = document.getElementById(cdom(key) + '-draft');
        mic.key = key; mic.thread = micThreadOf(key); mic.base = box ? box.value : composerState(key).text;
      }
      const v = await post('/mic', micOnBody());
      mic.on = !!v.on;
      if (!mic.on) { toast(v.error || 'the mic did not start'); mic.key = null; micDraw(); return; }
      micApply(v);
      micStart();
      // Every other console window lights its switch now, not on its reload.
      if (typeof winSend === 'function') winSend('mic', {});
      if (key) { const box = document.getElementById(cdom(key) + '-draft'); if (box) box.focus(); }
    } else if (key && key !== mic.key) {
      mic.guest = false; // a tap on a box here: the words are this window's now
      micTarget(key);
      const box = document.getElementById(cdom(key) + '-draft');
      if (box) box.focus();
    } else {
      // A guest tap off only switches it off: the words were said to a box
      // somewhere else and are not this page's to send as a new session.
      const send = mic.guest ? null : micSendFrom();
      // The switch reads off at the tap: the hub's off waits for the helper's
      // last word. The poll stops first so it cannot take the box away before
      // the take's last phrase lands in it (micApply below).
      clearInterval(mic.timer); mic.timer = null; mic.on = false; micDraw();
      const v = await post('/mic', { on: false, origin_ms: mic.origin || 0, send });
      micApply(v);
      micStop(v.error);
      if (typeof winSend === 'function') winSend('mic', {});
      if (v.sent) toast('sent what you said to a new session');
    }
  } catch (e) {
    toast(e.message || String(e));
    micStop();
  } finally { mic.busy = false; }
}

function micStart() {
  clearInterval(mic.timer);
  mic.timer = setInterval(micPoll, MIC_POLL_MS);
  micDraw();
}

// A page that loads while the hub is already listening (a reload, a second
// tab, the console opened an hour into a take) shows it and follows it: the
// switch is the hub's state, not this tab's.
//
// And a page already open follows it too (micWatch, every MIC_WATCH_MS while
// this window's switch is off): a take switched on in another app (the
// desktop app's box) would otherwise run unseen here. Found that way the take
// is a GUEST: the switch reads "● Listening · other window" and one tap turns
// it off, but no box here takes its words and this window never names a box
// to the hub — the words are landing where the switch was turned on.
const MIC_WATCH_MS = 3000;
function micWatch() {
  if (mic.on || mic.busy || (typeof document !== 'undefined' && document.hidden)) return undefined;
  return micAdopt(true);
}
async function micAdopt(guest) {
  let v;
  try { v = await get('/mic'); } catch { return; }
  if (!v || !v.on || mic.on || mic.busy) return;
  mic.guest = guest === true;
  mic.on = true; mic.seq = -1; mic.key = null; mic.thread = ''; mic.origin = v.origin_ms || Date.now();
  mic.finalsSeen = v.text || ''; mic.volShown = v.volatile || '';
  micApply(v);
  micStart();
}

async function micPoll() {
  if (!mic.on) { micStop(); return; }
  let v;
  try { v = await get('/mic'); } catch { return; }
  // Off with no take since boot and no reason: the hub restarted underneath
  // (`ops/hub.sh restart`). The switch here is still on, so it goes on again;
  // the box keeps every word it has (micApply, `fresh`).
  if (!v.on && !v.since && !v.error && mic.on) {
    // A guest take is not this window's to turn back on.
    if (mic.guest) { micStop(); return; }
    // The front window turns it back on; the rest wait to see it on.
    if (!micFront()) return;
    try { v = await post('/mic', micOnBody()); } catch { return; }
    if (!v.on) { micApply(v); micStop(v.error || 'the mic did not come back after the hub restarted'); return; }
  }
  micSync();
  if (v.seq === mic.seq && v.on) return;
  micApply(v);
  // Off but still finalizing (a tap right after a short phrase): that
  // phrase's final lands within the second, so the box keeps following
  // until the take has ended — a stop here would leave the final no box.
  if (!v.on && !v.stopping) micStop(v.error);
}

// The box = its base + the phrases since that base + the tail still changing.
// The composer's own state gets the same words, so the chat's 5 s redraw
// (which rebuilds the box from that state) never loses a phrase.
// The text without the volatile tail this tab put at its end — and without
// the space this tab put before that tail (never one that was typed).
function micUnvol(s) {
  if (!mic.volShown || !s.endsWith(mic.volShown)) return s;
  s = s.slice(0, s.length - mic.volShown.length);
  if (mic.inserted.endsWith(' ' + mic.volShown) && s.endsWith(' ')) s = s.slice(0, -1);
  return s;
}
function micApply(v) {
  mic.seq = v.seq; mic.status = v.status || '';
  if (v.on && v.origin_ms) mic.origin = v.origin_ms;
  const finals = v.text || '';
  // The hub's take no longer continues the one this tab has seen: a new take
  // (the hub restarted, or was switched on again from elsewhere). Nothing
  // shown is taken back; the counters start over on top of it.
  const fresh = !finals.startsWith(mic.finalsSeen);
  const key = mic.key;
  if (!key) {
    // No box on the page: rows only. The counters still move so the box
    // landed on next takes the phrases from that moment.
    if (fresh) { mic.finalsBase = 0; mic.sentBase = 0; mic.skipFinal = false; mic.mark = null; }
    mic.finalsSeen = finals; mic.volShown = v.volatile || '';
    micDraw();
    return;
  }
  const st = composerState(key);
  const box = document.getElementById(cdom(key) + '-draft');
  if (fresh) {
    const cur = micUnvol(box ? box.value : mic.base + mic.inserted);
    mic.base = cur; mic.inserted = ''; mic.finalsSeen = ''; mic.finalsBase = 0; mic.sentBase = 0;
    mic.skipFinal = false; mic.mark = null; mic.volShown = '';
  } else if (box && box.value !== mic.base + mic.inserted) {
    // The box was edited — or cleared, which never touches the mic. What was
    // left is the base; phrases already shown stay, only new ones are added.
    // A stale volatile tail at the end goes, since its final is about to
    // replace it.
    mic.base = micUnvol(box.value); mic.inserted = ''; mic.finalsBase = mic.finalsSeen.length;
  }
  // The final of the phrase that was volatile at Send: it went with the
  // message, so the mark moves past it as it lands — whether the send is
  // still in flight (mic.mark) or the hub has taken it (skipFinal).
  if (mic.mark && mic.mark.vol && finals.length > mic.mark.n) { mic.mark.n = finals.length; mic.mark.vol = false; }
  if (mic.skipFinal && finals.length > mic.finalsBase) {
    mic.finalsBase = finals.length; mic.sentBase = finals.length; mic.skipFinal = false;
  }
  let take = finals.slice(mic.finalsBase).trim();
  // (while skipFinal, the volatile tail is the sent phrase still settling: not shown again)
  if (v.volatile && !mic.skipFinal) take += (take ? ' ' : '') + v.volatile;
  let inserted = '';
  if (take) inserted = (mic.base && !/\s$/.test(mic.base) ? ' ' : '') + take;
  mic.inserted = inserted; mic.finalsSeen = finals; mic.volShown = v.volatile || '';
  const text = mic.base + inserted;
  st.text = text;
  if (box && box.value !== text) {
    box.value = text;
    autoGrow(box);
    box.selectionStart = box.selectionEnd = text.length;
  }
  micDraw();
}

function micStop(err) {
  clearInterval(mic.timer);
  mic.timer = null; mic.on = false; mic.status = ''; mic.guest = false;
  const key = mic.key;
  mic.key = null; mic.thread = '';
  micDraw();
  if (err) toast(err);
  const box = key ? document.getElementById(cdom(key) + '-draft') : null;
  if (box) { box.focus(); box.selectionStart = box.selectionEnd = box.value.length; }
}

// The caret moves into a box: the words follow it at once, not at the next poll.
document.addEventListener('focusin', e => {
  const t = e.target;
  if (mic.on && t && t.tagName === 'TEXTAREA') micSync();
});

// The Option key alone. A keyup for Alt with nothing else pressed since its
// keydown is the tap; anything else in between (⌥←, ⌥⌫, a click) is not.
document.addEventListener('keydown', e => {
  if (e.key === 'Alt') { if (!e.repeat) mic.altAlone = true; return; }
  mic.altAlone = false;
});
document.addEventListener('keyup', e => {
  if (e.key !== 'Alt' || !mic.altAlone) return;
  mic.altAlone = false;
  micToggle();
});
document.addEventListener('mousedown', () => { mic.altAlone = false; });
window.addEventListener('blur', () => { mic.altAlone = false; });

// Another console window switched the mic (windows.js): this one follows now.
if (typeof win !== 'undefined' && win.on) win.on.mic = () => { mic.guest = false; if (mic.on) micPoll(); else micAdopt(); };

// The hub may already be listening when this page loads (not in the headless
// checks, and not in the Node tests, which have no hub to ask).
if (!(typeof HEADLESS !== 'undefined' && HEADLESS) && typeof get === 'function' && typeof document.readyState === 'string') {
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', micAdopt);
  else micAdopt();
  setInterval(micWatch, MIC_WATCH_MS);
}
