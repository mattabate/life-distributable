// life hub — laptop console: shared render helpers.
//
// Loaded first (index.html: ui.js, then views/*.js, then app.js — plain
// <script> tags, no modules, no build step). Everything here is a pure
// function of its arguments except toast/conn, which touch two fixed
// elements. Nothing in this file talks to the hub: that is app.js's api()
// and the views'. The Node tests in test/ load this file alone.
'use strict';

// Every page registers itself here as `views.<name> = { draw, redraw }`
// (views/<name>.js); app.js's render() dispatches `#/<name>/…` on it.
const views = {};

// Headless Chrome (make web-smoke/web-shot/web-preview) waits for the network
// to go quiet before --dump-dom, so a parked 25 s /changes long-poll kept it
// waiting forever: the live loops stand down there (2026-09-14).
const HEADLESS = typeof navigator !== 'undefined' && /HeadlessChrome/.test(navigator.userAgent);

// ---------- the current route ----------
// `gen` moves on every route change (app.js render()), and each route gets a
// fresh #view element, so a slow draw from the page just left paints into
// a detached node and can never cover the new one. `redraw` is the ONE
// in-place repaint hook: the page that is showing sets it and the router
// clears it. `live` is what the live-update loop (app.js) runs when the hub
// says something moved — the redraw unless the page names another (a refetch,
// or a narrower repaint); `every` spaces a heavy page's refetches; `typing`
// lets it run while a box on the page has the caret. A draw that lost the
// race cannot claim any of it (its view is detached).
const route = { gen: 0, redraw: null, live: null, every: 0, typing: false, wake: null };
function setRedraw(fn, view, opts) {
  if (view && view.isConnected === false) return;
  const o = opts || {};
  route.redraw = fn;
  route.live = o.live === false ? null : (o.live || fn);
  route.every = o.every || 0;
  route.typing = !!o.typing;
  route.wake = o.wake || null;
}
function pageRedraw() { return route.redraw ? route.redraw() : null; }
// What a page leaves behind that the next must not inherit — a plot's hover
// data, a list's "Show more" — each module sweeps once here, at load; the
// router runs them all on every route change (render, as it swaps #view).
const onLeave = [];
// A sort column, tab or range chip: set one key of the page's state, keep it
// in localStorage under `store` when the choice should outlive the page, and
// repaint in place. Returns the onclick handler.
const sortSetter = (state, key, store) => v => {
  state[key] = v;
  if (store) localStorage.setItem(store, v);
  pageRedraw();
};

// innerHTML, skipped when the markup is exactly what this element last got
// from here and nothing else has replaced it since — so a live refresh that
// changed nothing keeps open <details>, scroll and hover as they were.
function paint(el, html) {
  if (!el) return false;
  if (el._painted === html && el.firstChild === el._paintedFirst) return false;
  el.innerHTML = html;
  el._painted = html;
  el._paintedFirst = el.firstChild;
  return true;
}

// A fetch that failed, in the page's own words, with one way out.
// `retry` is an onclick expression; the default redraws the page.
function failHTML(what, retry) {
  return card(`Couldn't load ${esc(what)} · <button class="sm" onclick="${esc(retry || 'render()')}">Retry</button>`, 'err');
}
// Nothing to show, in the page's words (plain text; escaped here). `how`:
// none = the page's centred grey line, 'card' = the same on a white card,
// 'plot' = a small grey line where a chart would have been.
function emptyHTML(text, how) {
  if (how === 'plot') return `<div class="small muted empty-plot">${esc(text)}</div>`;
  const line = `<div class="empty">${esc(text)}</div>`;
  return how === 'card' ? card(line) : line;
}
// A page's first paint while its read is in flight.
const loadingHTML = () => `<div class="pane wide">${emptyHTML('Loading…')}</div>`;

function conn(ok) { const el = document.getElementById('conn'); if (el) el.classList.toggle('bad', !ok); }
let toastTimer;
function toast(msg) {
  const el = document.getElementById('toast');
  el.textContent = msg; el.classList.add('show');
  clearTimeout(toastTimer); toastTimer = setTimeout(() => el.classList.remove('show'), 2600);
}
// One button's write: run it, toast `done` (or the hub's error), then repaint
// either way (`after`, default the page) so a switch that failed flips back to
// what the hub holds. True on success.
async function act(fn, done, after = () => pageRedraw()) {
  let ok = true;
  try { await fn(); if (done) toast(done); } catch (e) { toast(e.message); ok = false; }
  if (after) await after();
  return ok;
}

// ---------- helpers ----------
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// ---------- refs ----------
// A ref is the typed pointer the hub sets on every row: `kind:<id>`. Where
// one opens on the console is ONE table (docs/reviews/2026-08-28-followups.md
// §2): the calendar's rows, a rec's links and anything else that follows a
// reference look here, and nothing switches on an id's own prefix. An ask or
// a proposal is a card: it opens in its session when the row knows one, else
// on the sessions board, which is where every card that needs the owner is listed
// (there is no separate #/asks page).
// A card whose row does not know its session, and a calendar item, open
// through `#/open/<id>` (app.js openRef): the hub is asked where the thing
// lives and the address is replaced with it — so an id written in a card's
// text lands ON the card or the item, not on a board to search.
const REF_PAGE = {
  rec: id => `#/recs/${id}`,
  thread: id => `#/sessions/${id}`,
  cal: id => `#/open/${id}`,
  ask: (id, tid) => tid ? `#/sessions/${tid}/${id}` : `#/open/${id}`,
  action: (id, tid) => tid ? `#/sessions/${tid}/${id}` : `#/open/${id}`,
};
const splitRef = ref => { const r = String(ref || ''), c = r.indexOf(':'); return c > 0 ? [r.slice(0, c), r.slice(c + 1)] : ['', '']; };
function refHref(ref, threadID) {
  const [kind, id] = splitRef(ref);
  const page = REF_PAGE[kind];
  return page ? page(esc(id), esc(threadID || '')) : '';
}
// refOf: the ref a BARE id would carry — mirrors store.KindOf: a known
// prefix, the stamped `YYYYMMDD-HHMMSS-hex` of a proposal, else a thread.
const REF_KINDS = { ask: 'ask', cal: 'cal', rec: 'rec', p: 'prompt' };
function refOf(id) {
  const s = String(id || ''), m = /^([a-z]+)-[0-9a-f]{4,8}$/.exec(s);
  const kind = m && REF_KINDS[m[1]] ? REF_KINDS[m[1]] : /^\d{8}-\d{6}-[0-9a-f]{4,8}$/.test(s) ? 'action' : 'thread';
  return kind + ':' + s;
}
// A hub id standing in prose: rec-/ask-/cal- plus 8 hex (4 before 2026-08-26),
// or a proposal's stamp. Never a piece of a longer slug — a session id reads
// "…-rec-<id>-accepted-<hash>" — nor of a path or a URL fragment. The
// phone's Refs.swift carries the same rule; shared/ref-cases.json pins both.
const REF_ID = '(?:rec|ask|cal)-(?:[0-9a-f]{8}|[0-9a-f]{4})|\\d{8}-\\d{6}-[0-9a-f]{4,8}';
const REF_WHOLE = new RegExp(`^(?:${REF_ID})$`);
const REF_IN_TEXT = new RegExp(`(<a [^>]*>.*?</a>)|\\u0000(\\d+)\\u0000|(?<![\\w/#-])(${REF_ID})(?![\\w-])`, 'g');
// Money and counts: one wording with the hub (internal/format) and the phone
// (Format.swift), pinned by shared/format-cases.json (2026-09-14). Minus is
// U+2212. usd: whole from $100, cents below, no ".00"; usd0: always whole;
// usdPrice: a per-share price, always cents (4 places under a cent).
const moneySign = (v, digits) => (v < 0 && /[1-9]/.test(digits) ? '−$' : '$') + digits;
const usd = n => {
  const v = Number(n) || 0, a = Math.abs(v), cents = Math.round(a * 100);
  return moneySign(v, cents >= 10000 || cents % 100 === 0 ? Math.round(a).toLocaleString('en-US') : (cents / 100).toFixed(2));
};
const usd0 = n => { const v = Number(n) || 0; return moneySign(v, Math.round(Math.abs(v)).toLocaleString('en-US')); };
const usdSigned = n => { const s = usd(n); return s[0] === '−' ? s : '+' + s; };
const usd0Signed = n => { const s = usd0(n); return s[0] === '−' ? s : '+' + s; };
const usdShort = n => {
  const v = Number(n) || 0, a = Math.abs(v);
  const tenths = x => (Math.round(x * 10) / 10).toFixed(1).replace(/\.0$/, '');
  const s = Math.round(a / 1e5) >= 10 ? tenths(a / 1e6) + 'M'
    : Math.round(a / 1e3) >= 10 ? Math.round(a / 1e3).toLocaleString('en-US') + 'k'
    : Math.round(a) >= 1000 ? tenths(a / 1e3) + 'k'
    : String(Math.round(a));
  return moneySign(v, s);
};
const usdPrice = n => {
  const v = Number(n) || 0, a = Math.abs(v);
  const [whole, frac] = a.toFixed(a > 0 && a < 0.01 ? 4 : 2).split('.');
  return moneySign(v, Number(whole).toLocaleString('en-US') + '.' + frac);
};
// A plain count with its thousands separator: 20125 messages reads as a
// serial number, 20,125 reads as a quantity.
const num = n => { const v = Math.round(Number(n) || 0); return (v < 0 ? '−' : '') + Math.abs(v).toLocaleString('en-US'); };
// What a chat has cost in tokens. Millions on any long session, so two
// significant figures and a unit beat a nine-digit number nobody reads.
const tokens = n => {
  n = Number(n) || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + 'M tok';
  if (n >= 1e3) return Math.round(n / 1e3) + 'k tok';
  return n + ' tok';
};
// Hover text for a chat's dollar figure: which model earned what. A session
// that ran all night usually changed model when a plan bucket filled, and the
// rates differ by 5x. Only GET one thread returns the split.
const costByModel = t => {
  const by = t.cost_by_model || [];
  if (!by.length) return 'total cost so far';
  return by.map(c => `${c.model || 'default model'} ${usd(c.cost_usd)}`).join(' · ');
};

// The phone's shortAgo (Chips.swift) rounds the same way and must keep doing
// so — it floored until 2026-09-01, so 45 hours read "2d ago" here and "1d"
// there. Each bucket stops 30s/30m/12h short of its ceiling so a rounded value
// is promoted rather than printed as "60m ago".
function ago(iso) {
  if (!iso) return '';
  const s = (Date.now() - new Date(iso)) / 1000;
  if (s < 45) return 'just now';
  if (s < 3600 - 30) return Math.round(s / 60) + 'm ago';
  if (s < 86400 - 1800) return Math.round(s / 3600) + 'h ago';
  if (s < 86400 * 30) return Math.round(s / 86400) + 'd ago';
  return new Date(iso).toLocaleDateString();
}
function when(iso) {
  if (!iso) return '';
  const d = new Date(iso), now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  return sameDay ? d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
                 : d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
                   d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

// The Markdown subset the agents are told to write (docs: preamble spec):
// bullets, numbered lists, # headings, **bold**, `code`, links, pipe tables,
// ``` fences — anything else renders as plain text rather than as syntax.
// Inline half of the subset, on its own: **bold**, *italic*, `code`, links.
// The one-liners on this page — card titles, list previews, calendar rows —
// printed the asterisks because md() was only ever called on message and note
// BODIES.
// They cannot call md() either: it emits <p>/<ul> blocks, which do not belong
// inside a <strong> title and break the one/two-line clamps.
// links=false renders link text and bare URLs as plain text. Mandatory for
// anything drawn INSIDE a clickable card: a card is an <a>, HTML cannot nest
// anchors, and the browser repairs it by closing the card early — which split
// every failed session in the sessions list into three loose boxes, because
// the CLI's error paragraph ends in status.claude.com.
// GFM's trailing-punctuation rule for bare URLs: ", . ; : ! ?" and quotes
// hanging off the end belong to the sentence, not the link — an ask detail
// ending "…pull/82, merge when ready" linked the comma, which 404s on GitHub.
// A trailing ")" stays only while a matching "(" sits
// inside the URL (Wikipedia's "/wiki/A_(b)"). Runs on escaped text, so a
// quote after the URL arrives as &quot;/&#39; — a trailing entity is trimmed
// as one unit, as GFM does with entity references. Returns [link, spillover].
function trimBareURL(u) {
  let rest = '';
  for (;;) {
    const ent = u.match(/&#?[a-zA-Z0-9]+;$/);
    if (ent) { u = u.slice(0, -ent[0].length); rest = ent[0] + rest; continue; }
    const c = u.slice(-1);
    if (',.;:!?\'"'.includes(c) ||
        (c === ')' && (u.match(/\(/g) || []).length < (u.match(/\)/g) || []).length)) {
      u = u.slice(0, -1); rest = c + rest; continue;
    }
    return [u, rest];
  }
}

function mdInline(src, links = true) {
  let s = esc(String(src ?? ''));
  s = links
    ? s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
       .replace(/(^|[\s(])(https?:\/\/[^\s<\]]+)/g, (_, pre, u) => {
         const [link, rest] = trimBareURL(u);
         return `${pre}<a href="${link}" target="_blank" rel="noopener">${link}</a>${rest}`;
       })
    : s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '$1');
  // An asterisk is only syntax when it opens at a word boundary (start,
  // whitespace, an opening bracket) and hugs its content on both sides.
  // Without that, "you have Bash(npm install:*) and Bash(npm run:*)" matched
  // from the first ":*" to the second and rendered as "Bash(npm install:)"
  // plus an italic run, both literal asterisks deleted — the phone had the
  // same bug via CommonMark's left-flanking rule. Also protects "ops/*.py and
  // src/*.go" and "2**32 and 2**64". Dropping characters out of a message is
  // worse than dropping the italics.
  // Code spans are lifted out first and put back last: they were substituted
  // in place, so the emphasis passes then ran over their contents and
  // "`--host *`" came out italic. Inside backticks the characters are the
  // point.
  const code = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => '\u0000' + (code.push(c) - 1) + '\u0000');
  // A bare hub id is a link to the thing: an id nobody can click is
  // meaningless. Not inside a link that is already there,
  // and not inside code to be copied — except a code span that is ONLY an id,
  // which is how a session usually writes one.
  if (links) s = s.replace(REF_IN_TEXT, (m, a, ci, id) => {
    if (a) return a;
    if (ci !== undefined) return REF_WHOLE.test(code[ci]) ? `<a href="${refHref(refOf(code[ci]))}">${m}</a>` : m;
    return `<a href="${refHref(refOf(id))}">${id}</a>`;
  });
  return s
    .replace(/(^|[\s([{])\*\*([^\s*][^*]*[^\s*]|[^\s*])\*\*/g, '$1<strong>$2</strong>')
    .replace(/(^|[\s([{])\*([^\s*][^*\n]*[^\s*]|[^\s*])\*/g, '$1<em>$2</em>')
    .replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${code[i]}</code>`);
}

// A clamped preview of a whole reply: same inline rules, and the block
// markers become "• " so two lines of a bulleted reply read as prose instead
// of "- **Did:** …".
// A preview lives inside a clickable card and is clamped to two lines, so it
// is always link-free: the whole card is the link.
function mdPreview(src) {
  return String(src ?? '').split('\n').map(l => l.trim())
    .filter(l => l && !mdTableSep(l) && !/^`{3,}[\w.+#-]*$/.test(l))
    .map(l => l.startsWith('|') && mdTableRow(l) ? mdTableRow(l).join(' · ') : l)
    .map(l => mdInline(l.replace(/^[-*•]\s+/, '• ').replace(/^(#{1,4})\s+/, ''), false))
    .join(' ');
}

// A dead turn's last "message" is the CLI's own error paragraph — three lines
// of boilerplate and a status-page URL. On a card, one line is the whole
// signal: the session failed, and why. The full text stays in the chat.
function failedPreview(text) {
  const t = String(text ?? '').trim().replace(/\s+/g, ' ');
  const m = t.match(/^(.{0,120}?[.!?])(\s|$)/);
  const first = ((m ? m[1] : t.slice(0, 120)) || '').replace(/\.$/, '');
  return '⚠ Session failed · ' + esc(first || 'no result');
}

// A pipe table row: "| a | b |" (outer pipes optional) → its cells, or null.
// Split on "|" outside code spans, so "`a | b`" stays one cell.
function mdTableRow(l) {
  const t = l.trim();
  if (!t.includes('|')) return null;
  const cells = []; let cur = '', fence = false;
  for (const c of t) {
    if (c === '`') fence = !fence;
    if (c === '|' && !fence) { cells.push(cur); cur = ''; } else cur += c;
  }
  cells.push(cur);
  if (t.startsWith('|')) cells.shift();
  if (t.endsWith('|') && cells.length) cells.pop();
  return cells.map(c => c.trim());
}
// "|---|:--:|---:|" → per-column alignment, or null when the row is not one.
function mdTableSep(l) {
  const cells = mdTableRow(l);
  if (!cells || !cells.length || !cells.every(c => /^:?-+:?$/.test(c))) return null;
  return cells.map(c => c.endsWith(':') ? (c.startsWith(':') ? 'center' : 'right') : 'left');
}

// A fenced block: the language (when given) and Copy over the text, which
// scrolls sideways rather than wrapping — a wrapped JSON line reads as two.
function mdCode(text, lang) {
  return `<div class="md-code"><div class="md-code-bar"><span>${esc(lang || '')}</span>` +
    `<button type="button" class="md-copy" onclick="mdCopy(this)">Copy</button></div>` +
    `<pre><code>${esc(text)}</code></pre></div>`;
}
// Copies exactly what the session wrote: textContent of the <code>, so the
// indentation and newlines are the file's.
function mdCopy(btn) {
  const text = btn.closest('.md-code').querySelector('code').textContent;
  const done = () => { btn.textContent = 'Copied'; setTimeout(() => { btn.textContent = 'Copy'; }, 1500); };
  // The async clipboard refuses an unfocused document; the selection copy
  // does not care.
  const old = () => {
    const ta = document.createElement('textarea');
    ta.value = text; document.body.appendChild(ta); ta.select();
    document.execCommand('copy'); ta.remove(); done();
  };
  if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(done, old);
  else old();
}

function md(src) {
  const lines = String(src ?? '').split('\n');
  let out = '', list = null;
  const inline = mdInline;
  const close = () => { if (list) { out += `</${list}>`; list = null; } };
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const l = raw.trimEnd();
    let m;
    // A fenced block (else a settings.json to paste comes out as a column of
    // paragraphs, indentation gone). ```lang … ``` is the one way a
    // session hands the owner text to copy VERBATIM — JSON, a command, a config —
    // so nothing inside is markdown, whitespace is kept, and the block has a
    // Copy button. A fence indented under a list item loses that indent; an
    // unclosed fence runs to the end (a reply still streaming in).
    if ((m = l.match(/^(\s*)(`{3,})\s*([\w.+#-]*)$/))) {
      close();
      const body = [];
      let j = i + 1;
      for (; j < lines.length; j++) {
        const t = lines[j].trim();
        if (t.startsWith(m[2]) && /^`+$/.test(t)) break;
        body.push(lines[j].startsWith(m[1]) ? lines[j].slice(m[1].length) : lines[j].trimStart());
      }
      out += mdCode(body.join('\n'), m[3]);
      i = j;
      continue;
    }
    // A pipe table (else it comes out as "| Date | Buy |" paragraphs). Header row + separator + rows; a run of pipe rows with no
    // separator is a headerless table rather than a column of pipes.
    const row0 = l.trim().startsWith('|') ? mdTableRow(l) : null;
    if (row0 && !mdTableSep(l)) {
      close();
      let j = i + 1;
      const align = j < lines.length ? mdTableSep(lines[j]) : null;
      if (align) j++;
      const rows = [];
      for (; j < lines.length && lines[j].trim().startsWith('|'); j++) {
        if (!mdTableSep(lines[j])) rows.push(mdTableRow(lines[j]));
      }
      const td = (cells, tag) => '<tr>' + cells.map((c, k) => {
        const a = align && align[k] && align[k] !== 'left' ? ` style="text-align:${align[k]}"` : '';
        return `<${tag}${a}>${inline(c)}</${tag}>`;
      }).join('') + '</tr>';
      out += '<table class="md-table">';
      if (align) out += `<thead>${td(row0, 'th')}</thead>`; else rows.unshift(row0);
      if (rows.length) out += `<tbody>${rows.map(r => td(r, 'td')).join('')}</tbody>`;
      out += '</table>';
      i = j - 1;
      continue;
    }
    if ((m = l.match(/^\s*[-*•]\s+(.*)$/))) {
      if (list !== 'ul') { close(); out += '<ul>'; list = 'ul'; }
      out += `<li>${inline(m[1])}</li>`;
    } else if ((m = l.match(/^\s*(\d+)[.)]\s+(.*)$/))) {
      // A list a block interrupted picks up at the number it was written
      // with: steps "1." ```…``` "2." "3." read 1, 1, 2 without `start`.
      if (list !== 'ol') { close(); out += m[1] === '1' ? '<ol>' : `<ol start="${+m[1]}">`; list = 'ol'; }
      out += `<li>${inline(m[2])}</li>`;
    } else if ((m = l.match(/^(#{1,4})\s+(.*)$/))) {
      close(); out += `<h3>${inline(m[2])}</h3>`;
    } else if (l.trim() === '') {
      close();
    } else {
      close(); out += `<p>${inline(l)}</p>`;
    }
  }
  close();
  return out;
}

// The fold line: past this a chat bubble clamps behind a
// "Show all · N lines" button. Counted on the text, not the rendered height,
// so the same message folds the same on both surfaces and in a test; the
// phone's isLongText (StyledText.swift) is the same rule.
const LONG_LINES = 18, LONG_CHARS = 1600;
const longLineCount = t => String(t || '').split('\n').filter(l => l.trim() !== '').length;
const isLongText = t => longLineCount(t) > LONG_LINES || String(t || '').length > LONG_CHARS;

function statusPill(s) {
  const cls = { needs_you: 'needs', running: 'running', done: 'done' }[s] || '';
  const label = s === 'needs_you' ? 'your turn' : s;
  const dot = s === 'running' ? '<span class="dot pulse"></span>' : '';
  return `<span class="pill ${cls}">${dot}${esc(label)}</span>`;
}
// The pulsing "running" pill with words of the caller's choosing — a card's
// "session running", a Respond dialog's "running now — it reads this next".
const runningPill = label => `<span class="pill running"><span class="dot pulse"></span>${esc(label)}</span>`;
// The green pulsing "speaking" pill: this session's card is being read aloud
// to the owner right now (board pills tone `speaking`).
const speakingPill = label => `<span class="pill speaking"><span class="dot pulse"></span>${esc(label)}</span>`;
// The pills that MOVE on their own: a turn in flight (blue, pulsing), a card
// being SAID into the owner's headphones right now (green, pulsing), and a
// line queued behind the microphone, a replay or a pitch round (amber, still:
// "waiting to speak"). One table for the Sessions board's rows, the open
// chat's head and a calendar row's session (`live` on GET /calendar).
const LIVE_PILL = { running: runningPill, speaking: speakingPill,
  waiting: w => `<span class="pill waiting"><span class="dot"></span>${esc(w)}</span>` };
const livePillsHTML = pills => (pills || []).map(p => LIVE_PILL[p.tone] ? LIVE_PILL[p.tone](p.word) : '').join('');

// A day + clock time is read by the hub in Eastern — the hub's clock — so the
// instant picked is formatted there rather than in the browser's zone.
function easternFields(date) {
  const p = {};
  for (const part of new Intl.DateTimeFormat('en-US', {
    timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hour12: false,
  }).formatToParts(date)) p[part.type] = part.value;
  return { on: `${p.year}-${p.month}-${p.day}`, at_time: `${p.hour === '24' ? '00' : p.hour}:${p.minute}` };
}

// A bare YYYY-MM-DD parses as UTC midnight, i.e. the evening before in the
// US; noon keeps the day. Recs dates things this way.
const day = s => new Date(String(s).length <= 10 ? s + 'T12:00:00' : s);
// Local calendar days as YYYY-MM-DD strings — never UTC: a bare day is the
// day on the owner's wall calendar, and UTC after 8 pm Eastern reads as tomorrow.
const localDate = s => { const [y, m, d] = String(s).split('-').map(Number); return new Date(y, m - 1, d); };
const ymd = d => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
const addDays = (s, n) => { const d = localDate(s); d.setDate(d.getDate() + n); return ymd(d); };
const todayYMD = () => ymd(new Date());
// Whole days from a to b (both YYYY-MM-DD); noon on each side, so a DST
// change in between cannot round a day away.
const daysBetween = (a, b) => Math.round((day(b) - day(a)) / 86400000);
const shortDay = s => day(s).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' });
// The same day without the year, for a label inside a chart or a chip.
const dayLabel = d => day(d).toLocaleDateString([], { month: 'short', day: 'numeric' });
// With the weekday, for the title line of a plot's tooltip: on a daily series
// "Sat" is half of why a day looks the way it does (nothing settles on a
// weekend), and it is the one thing the date alone will not tell you.
const fullDay = s => day(s).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' });

// The model that filed something, short: "claude-fable-5[1m]" → "fable 5",
// "claude-haiku-4-5-20251001" → "haiku 4.5". The full id stays on the page.
// Recs and Spend both print it, so it lives here.
function modelShort(id) {
  if (!id) return '';
  if (id === 'owner' || id === 'unknown') return id;
  const m = String(id).replace(/^claude-/, '').replace(/\[1m\]$/, '').replace(/-\d{8}$/, '').split('-');
  return m.length > 1 ? m[0] + ' ' + m.slice(1).join('.') : m[0];
}

// A window over a long list, with a way to show all of them:
// a page at a time, "Show all", and where you are — with hundreds of rows
// "Show more" alone says nothing. `fn` names the page's global that grows the
// window: fn() by a page, fn(true) to everything.
function pagerHTML(total, shown, page, fn) {
  if (total <= shown) return total > page ? `<div class="arow small muted">All ${total} shown.</div>` : '';
  const more = Math.min(page, total - shown);
  return `<div class="arow">
    <button class="sm" onclick="${fn}()">Show ${more} more</button>
    <button class="sm" onclick="${fn}(true)">Show all ${total}</button>
    <span class="small muted">${shown} of ${total}</span></div>`;
}

// One pager: how many rows of a list are drawn now. rows() cuts the list,
// html() is the pagerHTML line whose buttons call more() by name, paint is
// what more() repaints (settable after, for a list that draws its own table).
// reset() is a new search/tab; every pager starts over on a route change
// (pagersReset, the router's onLeave).
const pagers = {};
function makePager(name, page, paint) {
  const p = pagers[name] = {
    shown: page, paint,
    rows: rows => rows.slice(0, p.shown),
    html: (total, shown) => pagerHTML(total, shown, page, `pagers.${name}.more`),
    more: all => { p.shown = all ? Infinity : p.shown + page; if (p.paint) p.paint(); },
    reset: () => { p.shown = page; },
  };
  return p;
}
function pagersReset() { for (const p of Object.values(pagers)) p.reset(); }
onLeave.push(pagersReset);

// A labelled <select> for the forms (calendar's new item, a goal's cadence).
const select = (id, label, opts, cur) =>
  `<label class="field" style="flex:1;min-width:150px"><span>${label}</span><select id="${id}">
    ${opts.map(o => `<option value="${o}"${o === cur ? ' selected' : ''}>${o}</option>`).join('')}</select></label>`;

// ---------- the three shapes every page is built from ----------
// `card` is the white box a list is made of, `pill` the small coloured
// label. `inner` is HTML the caller already escaped; a pill's text is escaped
// here.
const card = (inner, cls = '', attrs = '') => `<div class="card${cls ? ' ' + cls : ''}"${attrs ? ' ' + attrs : ''}>${inner}</div>`;
const pill = (text, cls = '') => `<span class="pill${cls ? ' ' + cls : ''}">${esc(text)}</span>`;

// ---------- one ask ----------
// An ask is drawn INSIDE a session's chat, as the bubble under the reply that
// raised it (class `ask`, id ask-card-<id> so #/sessions/<thread>/<ask-id> can
// scroll to and flash it).
//
// `answered` counts as still needing action: the owner has replied but the
// agent has not closed it, so the card keeps its buttons (and a ⏳, not a ✓ —
// the tick reads as "this is finished"). An error card is not work the owner
// does — it is a run that wants starting again: one line, and the only button
// that means anything is Restart (retry.go).
//
// Answering is the composer's (prompts engine phase 3). Reply / Done /
// Dismiss would be three buttons for one act. A card's answer
// buttons arm the session's own composer with this card and that pick
// (views/threads.js armReply; from elsewhere it opens the session there
// first): the words and figures typed the normal way, WHICH session hears
// them and WHEN from the composer's own selects — one prompt that references
// this card.
//
// Nothing free-form goes in an onclick: a JSON-quoted title inside a
// double-quoted attribute ended the attribute at its first quote and silently
// broke the button for every ask. A button carries only the
// id and the hub's outcome value, and re-reads the card from the hub.
// The install cell's two halves, shared with the calendar's row: the
// OTA link the Install button carries — the https form, since an
// itms-services link only means something to iOS — and the detail without
// its link line, because the button IS the link.
const askInstallLink = detail =>
  ((detail || '').match(/https:\/\/[^\s)\]>"',;]+/) || (detail || '').match(/itms-services:\/\/[^\s)\]>"',;]+/) || [])[0];
const askDetailShown = detail =>
  (detail || '').split('\n').filter(l => !/https:\/\/|itms-services:\/\//.test(l)).join('\n').trim();
// Which device an install card is for (2026-09-30): the hub's `target`
// ("phone" | "mac"), or — a calendar row, an older hub — the title's word.
const askInstallTarget = a => a.target || (/^install (desktop|mac) build \d+/i.test(a.title || '') ? 'mac' : 'phone');

// askBody: a long detail folds the way a long chat message does (isLongText)
// — a screen's worth, faded, and a "Show all · N lines" button that opens it
// to full height — never a nested scroll box, and never a card long enough to
// swallow the session. `key` ("ask:<id>") keeps the fold across redraws.
// The fold's one button, under a chat bubble and a card alike: "Show all · N
// lines" shut, "Show less" open; `onclick` is the surface's own toggle.
const foldLabel = (open, n) => open ? 'Show less' : `Show all · ${n} lines`;
const foldButtonHTML = (text, open, onclick) =>
  `<button type="button" class="fold small" data-n="${longLineCount(text)}" onclick="${onclick}">${foldLabel(open, longLineCount(text))}</button>`;
const cardLongOpen = new Set();
function askBody(detail, key) {
  if (!isLongText(detail)) return md(detail);
  const open = cardLongOpen.has(key);
  return `<div class="ask-long${open ? ' open' : ''}">${md(detail)}</div>
    ${foldButtonHTML(detail, open, `toggleCardLong(this,'${esc(key)}')`)}`;
}
function toggleCardLong(btn, key) {
  const open = !cardLongOpen.delete(key);
  if (open) cardLongOpen.add(key);
  btn.previousElementSibling.classList.toggle('open', open);
  btn.textContent = foldLabel(open, btn.dataset.n);
}

// ---------- the one card ----------
// Every card in the chat — an ask, an approval, a rec — is ONE shape, with the
// same components and no excess styling code: a tinted box, a mark and the title, the body, a meta row, and ONE row
// of word buttons at the bottom, the primary in the card's tint. The words
// are the hub's — `outcomes` on an ask, an action and a rec are one shape
// (store.Outcome) — and each arms the composer with that pick; the client
// adds only the silent closes (Dismiss; a read's Read) and the two buttons
// that are not answers (Install, Restart). A closed card is grey with its
// state where the row was. A dismissed one — or a read marked Read — FOLDS
// to one grey line with Reopen inside, so it can still be reopened or reread;
// `opened` is the chat's remembered fold (data-k survives the 5 s redraw).
//
// A card is { ref, id, tint, mark, title, body, meta, acts, closed, folded, trail }:
//   ref     ask | action | rec — the id's namespace (data-k)
//   tint    '' red (a stopped session, a proposal) | read | install | rec | err-card
//   acts    [{ label, js | href, primary }] — the row of an open card
//   closed  the words where the row was (a closed card)
//   folded  the word before the title (a folded card; acts = Reopen)
const cardBtn = b => b.href
  ? `<a class="btn sm${b.primary ? ' primary' : ''}" href="${esc(b.href)}" target="_blank">${b.label}</a>`
  : `<button class="sm${b.primary ? ' primary' : ''}" onclick="${b.js}">${b.label}</button>`;
function cardHTML(c, opened) {
  const id = esc(c.id), acts = (c.acts || []).map(cardBtn).join('');
  const body = c.body ? `<div class="small">${c.body}</div>` : '';
  // THE MESSAGE IS THE CARD: audio is a main response format, and the text
  // of what was played is shown. What was spoken when this card arrived
  // (asks.said / actions.said) LEADS the card, in the card's own words, and
  // the detail — the owner's steps, when there are any — follows it. A title
  // the message already says (a read minted from a `Say:` reply is titled from
  // its first sentence) is not drawn twice: the mark moves onto the message.
  // THE HEAD LINE: one mark per row, never two emojis — the mark and the
  // card's one word (READ · DO · DECIDE ·
  // APPROVE, the Sessions cell's corner word) on a line of their own. Under
  // it the title, when the message does not already say it, then the message
  // in plain body type.
  const titled = !c.said || !saidCovers(c.said, c.title);
  const head = `<div class="card-h">${c.mark}<span class="verb">${esc(c.verb || '')}</span></div>`;
  const said = c.said ? `<div class="said">${esc(c.said)}</div>` : '';
  if (c.folded) {
    return `<details class="ask folded" id="ask-card-${id}" data-k="${c.ref}:${id}" data-def="0"${opened ? ' open' : ''}>
      <summary><span class="muted">${c.folded} ·</span> ${mdInline(c.title)}</summary>
      ${said}${body}
      <div class="acts">${acts}</div></details>`;
  }
  const cls = ['ask', c.tint, c.closed ? 'answered' : ''].filter(Boolean).join(' ');
  return `<div class="${cls}" id="ask-card-${id}">
      ${head}${titled ? `<div class="t">${mdInline(c.title)}</div>` : ''}
      ${said}${body}
      ${c.meta ? `<div class="row small muted">${c.meta}</div>` : ''}
      <div class="acts">${c.closed ? `<span class="small muted">${c.closed}</span>` : ''}${acts}</div>${c.trail || ''}</div>`;
}
// Markdown compared as words: links to their text, marks and a cut title's
// "…" dropped. Used to spot a body line that only repeats the title.
const plainText = s => (s || '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/[*_`#…]/g, '').trim();
// saidCovers: does the spoken message already say the title? Compared plain,
// case folded, the way the phone's SessionCell.body drops a title-repeating line.
function saidCovers(said, title) {
  const t = plainText(title).toLowerCase();
  return !!t && plainText(said).toLowerCase().includes(t);
}
// The hub's outcomes as the row, in the hub's order — the same order the
// composer strip draws its chips — never a different order on the bar than
// on the card. The hub lists the decisive
// ones first and "Reply" (value "") last; the first decisive one is
// primary; `arm(value)` is each button's onclick.
function outcomeBtns(outcomes, arm) {
  return (outcomes || []).map((o, i) => ({ label: esc(o.label), js: arm(o.value), primary: i === 0 && !!o.value }));
}

// Where a card stands is the hub's (store.AskStanding): `open` = the owner's move,
// `closed` = finished, neither = answered and waiting on its session;
// `folded` = a silent close drawn as one line with Reopen ("dismissed", or
// "read" — a read card closed with its own button, whose resolution is the
// hub's word for its one outcome; a read closed by a reply carries the
// reply's first line instead and stays a full card above the reply). What a
// card wants from the owner, in one word, is the ask's own `verb` (store.AskVerb).
// The paused card: a session limit is a wait with an end, so it is not the grey error: amber like "waiting", ⏸ and
// PAUSED on the head line, and the wait itself as a bar from the moment it
// stopped to the reset (hub `resumes_at`), the two clocks and what is left
// under it. The hub resumes it at the reset; Resume now is the Restart.
const PAUSE_MARK = '⏸︎';
const pauseRe = /hit your (?:[a-z0-9 ]{0,24} )?limit[^\n]*?resets\s/i;
const isPauseText = s => pauseRe.test(s || '');
const clockShort = d => d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
function pauseBarHTML(a) {
  const from = new Date(a.created_at).getTime(), to = new Date(a.resumes_at).getTime(), now = Date.now();
  const pct = to > from ? Math.max(0, Math.min(100, (now - from) / (to - from) * 100)) : 100;
  const mins = Math.ceil((to - now) / 60000);
  const left = mins <= 0 ? 'resuming…' : mins < 60 ? `${mins} min` : `${Math.floor(mins / 60)} h ${mins % 60} min`;
  return `<div class="pause-bar"><i style="width:${pct.toFixed(1)}%"></i></div>
    <div class="pause-foot"><span>${clockShort(new Date(from))}</span><span>${clockShort(new Date(to))} · ${left}</span></div>`;
}

function askHTML(a, opened) {
  const id = esc(a.id), err = a.kind === 'error', paused = err && !!a.resumes_at;
  const open = !a.closed, answered = open && !a.open;
  // A read card is blue, never red: it asks the owner to read an answer, not
  // to unblock anything — red is for a stopped session. Replying to one does
  // not turn it red either: it is still a read, now waiting on the agent to
  // close it. The install cell is teal: the
  // Install button IS the OTA link (the https form; the phone rewrites it),
  // and the link line leaves the detail.
  const read = a.kind === 'read' && open, install = a.kind === 'install' && open;
  // The desktop app's build (2026-09-30, `target` mac): no link — Install
  // runs the hub's `mac` lane, which builds if it must and restarts the app.
  const mac = install && askInstallTarget(a) === 'mac';
  const link = install && !mac && askInstallLink(a.detail);
  const dismiss = { label: 'Dismiss', js: `cardFold('ask','${id}')` };
  const respond = { label: 'Respond', js: `openRespond('${id}')`, primary: true };
  // A closed card carries Reopen when the hub says so (`reopen`: every kind
  // but an install) — the done ones too, not just a fold.
  const acts = !open ? (a.reopen ? [{ label: 'Reopen', js: `cardFold('ask','${id}',true)` }] : [])
    // An error is a run that wants starting again (2026-08-24).
    : err ? [{ label: paused ? 'Resume now' : 'Restart', js: `retryAsk('${id}')`, primary: true }, dismiss]
    : install ? [mac ? { label: 'Install', js: `macInstall('${id}')`, primary: true } : link ? { label: 'Install', href: link, primary: true } : respond, dismiss]
    // A read: Respond arms the reply (sending closes it, words or not); Read
    // is its silent close — done, "Read it", wakes nothing.
    : read ? [respond, { label: 'Read', js: `resolveAsk('${id}','done','${esc(((a.outcomes || [])[0] || {}).label || '')}')` }]
    // Every other kind: the hub's words (Decided · Not deciding · Just
    // reply), each arming the composer with that pick, then Dismiss — the
    // silent close.
    : (a.outcomes && a.outcomes.length ? outcomeBtns(a.outcomes, v => `openRespond('${id}','${v}')`) : [respond]).concat(dismiss);
  return cardHTML({
    ref: 'ask', id: a.id, title: a.title, verb: a.verb || 'for you',
    tint: paused && open ? 'paused' : err ? 'err-card' : read ? 'read' : install ? 'install' : '',
    mark: answered ? '⏳ ' : !open ? '✓ ' : paused ? PAUSE_MARK + ' ' : err ? '⚠︎ ' : read ? '🔵 ' : install ? '📲 ' : '🔴 ',
    body: paused && open ? pauseBarHTML(a) : err ? '' : askBody(install && !mac ? askDetailShown(a.detail) : (a.detail || ''), 'ask:' + a.id),
    said: a.said, thread: a.thread_id, acts, closed: open ? '' : esc(a.state),
    folded: a.folded === 'dismissed' ? '✕ dismissed' : a.folded ? '✓ read' : '',
  }, opened);
}

// forYouPill: the "N for you" on a session's row — blue "N to read" when
// every item only asks the owner to read (board `reads`), teal "N to install"
// when every item is an app build (board `installs`), red otherwise.
function forYouPill(n, reads, installs) {
  if (!n) return '';
  if (reads >= n) return `<span class="pill read">${n} to read</span>`;
  if ((installs || 0) >= n) return `<span class="pill install">${n} to install</span>`;
  return `<span class="pill needs">${n} for you</span>`;
}

const kindPill = { money: 'amber', delete: 'needs', contact: 'purple', share: 'purple', commit: 'purple' };
// The approval card, styled like every other card, buttons at the bottom
// that arm the composer: 🔴 title,
// the detail, what it will run, the kind row, and the hub's Approve · Deny ·
// Reply — each ARMS the session's composer (armActionReply,
// views/threads.js); Send posts one prompt naming the action with the pick
// and the hub decides the row from it (threads DecideAction). A scheduled
// job's proposal has no session: its card names the job and Approve/Deny
// decide it from the card (decideAction, views/asks.js). Dismiss is the
// silent close either way — nothing runs, nobody is told (for a card that went
// out of date unanswered).
// Drawn INSIDE the chat under the reply that proposed it (no message id of
// its own: after the last message that precedes it in time), with id
// `ask-card-<id>` so a deep link scrolls to it like an ask. A decided one
// (reached from a calendar row or an old link) is grey with its state where
// the row was; the audit trail below says who decided it and when.
// The job behind a thread-less action's `source` (claude:job:<name>), or ''.
const actionJob = a => (!a.thread_id && /^claude:job:/.test(a.source || '')) ? a.source.slice('claude:job:'.length) : '';
function actionHTML(a, opened) {
  const id = esc(a.id), job = actionJob(a);
  const open = a.open, dismissed = a.folded === 'dismissed';
  const payload = a.exec_payload && a.exec_type && a.exec_type !== 'none'
    ? `<details style="margin:6px 0"><summary class="muted">will run: ${esc(a.exec_type)}</summary>
       <pre class="io mono">${esc(typeof a.exec_payload === 'string' ? a.exec_payload : JSON.stringify(a.exec_payload, null, 2))}</pre></details>`
    : '';
  const arm = v => a.thread_id ? `armActionReply('${id}','${v}')` : `decideAction('${id}',${v === 'approved'})`;
  const acts = !open ? (a.reopen ? [{ label: 'Reopen', js: `cardFold('action','${id}',true)` }] : []) : outcomeBtns(a.outcomes, arm).concat({ label: 'Dismiss', js: `cardFold('action','${id}')` });
  return cardHTML({
    ref: 'action', id: a.id, title: a.title, tint: '', verb: 'approve',
    mark: open ? '🔴 ' : (a.state === 'denied' || a.state === 'failed') ? '✕ ' : '✓ ',
    body: askBody(a.detail || '', 'action:' + a.id) + payload,
    said: a.said, thread: a.thread_id,
    meta: `${pill(a.kind, kindPill[a.kind])} ${job ? pill('⚙ ' + job + ' job') : ''} ${a.project ? esc(a.project) : ''} <span>${ago(a.created_at)}</span>`,
    acts, closed: open ? '' : `${esc(a.state)}${a.decided_via ? ' via ' + esc(a.decided_via) : ''}${a.decided_at ? ' · ' + ago(a.decided_at) : ''}`, folded: dismissed ? '✕ dismissed' : '', trail: eventsHTML(a.events),
  }, opened);
}

// A recommendation's own cell in the chat, so a rec can be answered from the
// chat without going over to the Recs page. Drawn under the reply that filed it (rec.message_id, resolved by
// the hub) with the hub's Accept · Decline · Reply, each of which ARMS
// the session's own composer (armRecReply, views/threads.js), so the note is
// typed where a message is and goes where the Recs page would send it. Dismiss
// marks it expired with the note "dismissed" — silently, nothing relayed —
// and Reopen puts it back. The violet "rec" pill opens the record. Pull stays
// pull: the card is inside the session and notifies nobody. A decided rec is
// grey with the verdict where the row was.
const DOM_PILL = { money: 'amber', health: 'done', audience: 'purple', tools: 'running', home: '', other: '' };
// What saying yes costs — the hub's `cost_label` ("$11/mo", "$33,837 one-off",
// "free", "price not checked"), the phone's exact words. Unpriced is muted.
function costLabel(r) {
  const c = r.cost_label || '';
  return r.cost_cents > 0 ? `<strong>${esc(c)}</strong>` : `<span class="muted">${esc(c)}</span>`;
}
const REC_STATE_WORD = { proposed: 'open', deferred: 'later', accepted: 'accepted', declined: 'declined', done: 'done', superseded: 'superseded', expired: 'expired' };
// A rec's standing is the hub's (store.RecStanding): a parked (deferred) rec
// is not `closed`, so it keeps its buttons; `folded` marks one dismissed.
function recHTML(r, opened) {
  const id = esc(r.id), open = !r.closed;
  const dismissed = r.folded === 'dismissed';
  const note = r.decision_note ? ' — ' + mdInline(r.decision_note, false) : '';
  const acts = !open ? (r.reopen ? [{ label: 'Reopen', js: `cardFold('rec','${id}',true)` }] : []) : outcomeBtns(r.outcomes, v => `armRecReply('${id}','${v}')`).concat({ label: 'Dismiss', js: `cardFold('rec','${id}')` });
  return cardHTML({
    ref: 'rec', id: r.id, title: r.title, tint: 'rec', verb: 'rec',
    mark: open ? '💡 ' : r.status === 'declined' ? '✕ ' : '✓ ',
    body: (r.because ? `<div class="wrap2">${mdInline(r.because, false)}</div>` : '')
      + (r.status === 'deferred' ? `<div class="muted">parked${r.review_on ? ' · back ' + esc(r.review_on) : ''}${note}</div>` : ''),
    meta: `<a class="pill purple" href="#/recs/${id}">rec</a> ${pill(r.domain, DOM_PILL[r.domain])} <span>${esc(r.kind)} · ${costLabel(r)}${r.model ? ' · ' + esc(modelShort(r.model)) : ''}</span>`,
    acts, folded: dismissed ? '✕ dismissed' : '',
    closed: open ? '' : `${esc(REC_STATE_WORD[r.status] || r.status)}${r.decided_by ? ' by ' + esc(r.decided_by) : ''}${r.decided_at ? ' · ' + ago(r.decided_at) : ''}${note}`,
  }, opened);
}

// The audit trail Phase 2 gave every action (GET /actions/{id} → events[]:
// proposed, approved by app, executed…). One grey line per event, below the
// buttons; the lists never carry events[], so this is empty everywhere but a
// single-action read.
function eventsHTML(events) {
  if (!events || !events.length) return '';
  return `<div class="small muted" style="margin-top:8px">${events.map(e =>
    `<div>${esc(e.event)}${e.actor ? ' by ' + esc(e.actor) : ''} · ${when(e.ts)}${e.note ? ' · ' + esc(e.note) : ''}</div>`).join('')}</div>`;
}
