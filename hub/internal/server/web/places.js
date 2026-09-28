// life hub — laptop console: WHERE the owner is, in words and in file paths.
//
// The "+ New session" button already photographs the page it was pressed on
// (views/threads.js `snapPage`). A picture is not searchable: the session that
// opens gets pixels and has to guess which file draws them. So every snap carries this
// block of text too — the route, the URL, the heading on screen, and the two
// source files that draw it, console and phone.
//
// This table is also the surface-parity table: `parity_test.go` reads it
// instead of keeping a second copy in Go, so a page can never name one file
// here and another there. Rules for editing it:
//   - one row per console page, keyed exactly as `views.<key>` in views/*.js
//   - `view`  the file that registers it, relative to web/
//   - `phone` the SwiftUI file in app/Life/Sources that shows the same thing,
//             named `<Screen>.swift` (the struct is the file name)
//   - `phone: ''` means console-only, and then `why` says why out loud
//   - `also`  a second file on EITHER side of the same page, when one page is
//             drawn by two ('views/calgrid.js', 'CalendarGrid.swift' — the
//             suffix picks the root); parity_test.go checks each one exists
//   - `deep`  sub-routes worth naming: `at` = how many segments the hash has
//             (`sessions/<id>` = 2), most specific first
'use strict';

const PLACES = {
  // Sessions is also Your turn: the page that was `asks`
  // here (`#/asks`, ThreadsView's `.yourTurn`) is deleted on both surfaces,
  // and its stack is this page's first group.
  sessions: { label: 'Sessions', view: 'views/threads.js', phone: 'ThreadsView.swift', deep: [
    { at: 2, label: 'one session', phone: 'ThreadDetail.swift' },
  ] },
  recs: { label: 'Recs', view: 'views/recs.js', phone: 'RecsView.swift' },
  // Two files a side since 08-31: `calendar.js` registers the page and draws
  // the Schedule mode, `calgrid.js` draws Day/Week/Month — and a snap of the
  // grid must name the file that actually drew it. (The phone's CalendarGrid
  // has Day only: a week of columns was unreadable at 390pt.)
  calendar: { label: 'Calendar', view: 'views/calendar.js', phone: 'CalendarView.swift',
    also: ['views/calgrid.js', 'CalendarGrid.swift'] },
  spend: { label: 'Spend', view: 'views/spend.js', phone: 'SpendView.swift' },
  goals: { label: 'Goals', view: 'views/goals.js', phone: 'GoalsView.swift' },
  sources: { label: 'Sources', view: 'views/sources.js', phone: 'SourcesView.swift' },
  runs: { label: 'Job run', view: 'views/runs.js', phone: '',
    why: 'console only: the raw transcript of one scheduled job run, opened from a hub log line; what a run finds reaches the phone as an ask or a card' },
};

const VIEW_ROOT = 'hub/internal/server/web/';
const APP_ROOT = 'app/Life/Sources/';

// Where the console is right now. `hash` may be passed in — snapPage() reads
// the route before the router navigates away, and so does this.
function placeNow(hash) {
  const h = (hash === undefined ? location.hash : hash).replace(/^#\/?/, '') || 'sessions';
  // `#/sessions` with no session open is the empty chat (2026-09-17), so a
  // second press of the button reports "Sessions", never "one session".
  const parts = h.split('/').filter(Boolean);
  const key = PLACES[parts[0]] ? parts[0] : 'sessions';
  const p = PLACES[key];
  const deep = (p.deep || []).find(d => parts.length >= d.at) || null;
  const files = [VIEW_ROOT + p.view];
  if (p.phone) files.push(APP_ROOT + p.phone);
  // `also` is a second file on either side of one page; the suffix says which
  // root it hangs off, so a row never has to repeat the path.
  for (const f of p.also || []) files.push((f.endsWith('.swift') ? APP_ROOT : VIEW_ROOT) + f);
  if (deep && deep.phone) files.push(APP_ROOT + deep.phone);
  // The words on the page beat any label we could write here. Dropped when it
  // only repeats the tab's own name ("Goals · Goals" said nothing twice).
  const head = document.querySelector('#view h1, #view h2, #view .chat-head strong');
  const heading = (head ? head.textContent : '').trim().slice(0, 80);
  return {
    key,
    hash: '#/' + h,
    url: location.origin + '/#/' + h,
    label: p.label + (deep ? ' → ' + deep.label : ''),
    heading: heading === p.label ? '' : heading,
    files,
  };
}

// The paragraph that goes into the session's first message, above whatever
// the owner typed. Kept to one short bullet list: it is read by an agent, but
// the owner sees it in the thread too. The first line must match
// consoleSnapPreamble in internal/threads/threads.go, which strips it.
function placeBlock(place) {
  const p = place || placeNow();
  const lines = [
    '[Console screenshot attached — the owner hit "+ New session" from this page of the web console.]',
    '',
    '- Page: ' + p.label + ' (`' + p.hash + '`)',
  ];
  if (p.heading) lines.push('- Heading on screen: ' + p.heading);
  lines.push('- URL: ' + p.url);
  lines.push('- Drawn by: ' + p.files.join(' · '));
  return lines.join('\n');
}
