// life hub — laptop console: SEVERAL WINDOWS AT ONCE.
//
// 2026-09-25: "it's normal for me to have multiple windows or tabs
// of the LifeHub open at any given time… we only need one of them sort of
// listening at a time… say I'm on the sessions page twice. I don't necessarily
// want like 2 different like text dumps going into section boxes… if I have 2
// different ones open, it's important to me that you still like capture my
// mouse and you capture my audio… if I'm on one and I'm speaking, then the
// other window says, you know, like waiting to speak on the sessions page".
//
// The hub's state is already one for every window: the microphone is one
// sensor (GET /mic), the board's "speaking" / "waiting to speak" pills are the
// hub's, so every window shows the same thing. What is per window, and has to
// agree, is decided here:
//
// - THE FRONT WINDOW is the console window he was in last (focused last; a
//   lone window is its own front). Only the front window's box takes his
//   dictation; the others show the mic as on — "● Listening · other window" —
//   and put nothing in any box. Clicking into a window makes it the front, and
//   the words follow the caret there from that moment (mic.js micWantKey).
// - THE MIC SWITCH is shared: switching it on in one window lights it in all
//   of them at once (they adopt the take), off ends it everywhere.
// - ONE VOICE: a card replayed in one window stops a replay playing in any
//   other (ui.js sayAgain), so two windows never talk over each other.
// - THE TRAIL is recorded in every window (a window records the pointer only
//   while the pointer is over it anyway); every row carries its window (`win`),
//   and coming into a different window is a row of its own ("switched to the
//   window on Money"), so `trail.py at` reads one story.
//
// Same-origin windows of one browser talk over a BroadcastChannel; the front
// window's id is also kept in localStorage so a window opened later knows it.
'use strict';

const WIN_KEY = 'life.front';
const win = {
  id: (() => {
    // Kept for the tab's life (a reload is the same window), fresh per tab.
    try {
      let id = sessionStorage.getItem('life.win');
      if (!id) { id = Math.random().toString(36).slice(2, 6); sessionStorage.setItem('life.win', id); }
      return id;
    } catch { return Math.random().toString(36).slice(2, 6); }
  })(),
  ch: typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel('life-console') : null,
  // Called with each message from another window: {k, from, …}.
  on: {},
};

// The front window: this one, when localStorage names it — or names nobody
// (the front one just closed) and he is in this one.
function winFront() {
  let f = null;
  try { f = localStorage.getItem(WIN_KEY); } catch { return true; }
  return f ? f === win.id : typeof document === 'undefined' || document.hasFocus();
}

// This window becomes the front one; the others hear it.
function winClaim() {
  let was = null;
  try { was = localStorage.getItem(WIN_KEY); localStorage.setItem(WIN_KEY, win.id); } catch { /* private mode */ }
  if (was === win.id) return;
  if (was && typeof trailPush === 'function') trailPush('window', { page: location.hash.replace(/^#\/?/, '') || 'sessions' });
  winSend('front', {});
  if (typeof micSync === 'function') micSync();
  if (typeof micDraw === 'function') micDraw();
}

function winSend(k, fields) {
  if (win.ch) win.ch.postMessage(Object.assign({ k, from: win.id }, fields));
}

if (win.ch) {
  win.ch.onmessage = ev => {
    const m = ev.data || {};
    if (m.from === win.id) return;
    const fn = win.on[m.k];
    if (fn) fn(m);
  };
}

// Another window took the front: the words leave this window's box.
win.on.front = () => {
  if (typeof micSync === 'function') micSync();
  if (typeof micDraw === 'function') micDraw();
};

if (typeof window !== 'undefined' && window.addEventListener && !(typeof HEADLESS !== 'undefined' && HEADLESS)) {
  window.addEventListener('focus', winClaim);
  // A window that opens in front, or opens as the only one, is the front.
  if (typeof document !== 'undefined' && (document.hasFocus() || !localStorage.getItem(WIN_KEY))) winClaim();
  // The front window closing leaves nobody named: the next one he touches (or
  // any lone survivor) is the front.
  window.addEventListener('pagehide', () => {
    try {
      if (localStorage.getItem(WIN_KEY) !== win.id) return;
      localStorage.removeItem(WIN_KEY);
      winSend('gone', {});
    } catch { /* ignore */ }
  });
}
win.on.gone = () => { if (document.hasFocus()) winClaim(); };
// A card replayed in another window: this window's replay stops (ui.js).
win.on.say = () => { if (typeof sayHush === 'function') sayHush(); };
